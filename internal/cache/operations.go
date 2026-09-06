package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CacheItem represents a single entity to be cached.
type CacheItem struct {
	ID        int64
	CourseID  int64
	Data      any
	UpdatedAt *time.Time
}

// errInvalidTable is returned when a ResourceType doesn't match a known entity table.
var errInvalidTable = fmt.Errorf("invalid resource type")

// Get fetches a single entity by ID from the given table and unmarshals it into dest.
// Returns sql.ErrNoRows if not found.
func (d *DB) Get(table ResourceType, id int64, dest any) error {
	if !validTable(table) {
		return fmt.Errorf("%w: %s", errInvalidTable, table)
	}
	var data string
	err := d.db.QueryRow(
		fmt.Sprintf("SELECT data FROM %s WHERE id = ?", table), id,
	).Scan(&data)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), dest)
}

// List fetches all entities for a course (or all if courseID == 0) and unmarshals into dest.
// dest must be a pointer to a slice (e.g., *[]canvas.Course).
func (d *DB) List(table ResourceType, courseID int64, dest any) error {
	if !validTable(table) {
		return fmt.Errorf("%w: %s", errInvalidTable, table)
	}
	var query string
	var args []any
	if courseID == 0 {
		query = fmt.Sprintf("SELECT data FROM %s ORDER BY id", table)
	} else {
		query = fmt.Sprintf("SELECT data FROM %s WHERE course_id = ? ORDER BY id", table)
		args = append(args, courseID)
	}

	return d.listQuery(table, query, args, dest)
}

func (d *DB) listQuery(table ResourceType, query string, args []any, dest any) error {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("querying %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	// Collect all JSON blobs, then unmarshal as a JSON array into the dest slice.
	var blobs []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return fmt.Errorf("scanning %s row: %w", table, err)
		}
		blobs = append(blobs, json.RawMessage(data))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating %s rows: %w", table, err)
	}

	// Marshal the collected blobs as a JSON array and unmarshal into the dest slice.
	arrayJSON, err := json.Marshal(blobs)
	if err != nil {
		return fmt.Errorf("marshaling %s array: %w", table, err)
	}
	return json.Unmarshal(arrayJSON, dest)
}

// Upsert inserts or replaces a single entity in the given table.
func (d *DB) Upsert(table ResourceType, id, courseID int64, data any, updatedAt *time.Time) error {
	if !validTable(table) {
		return fmt.Errorf("%w: %s", errInvalidTable, table)
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling data: %w", err)
	}

	var updatedAtStr *string
	if updatedAt != nil {
		s := updatedAt.UTC().Format(time.RFC3339)
		updatedAtStr = &s
	}

	_, err = d.db.Exec(
		fmt.Sprintf(`INSERT OR REPLACE INTO %s (id, course_id, data, updated_at, fetched_at)
			VALUES (?, ?, ?, ?, strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ', 'now'))`, table),
		id, courseID, string(jsonData), updatedAtStr,
	)
	return err
}

// UpsertMany inserts or replaces multiple entities in a single transaction.
func (d *DB) UpsertMany(table ResourceType, items []CacheItem) error {
	if len(items) == 0 {
		return nil
	}
	if !validTable(table) {
		return fmt.Errorf("%w: %s", errInvalidTable, table)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	stmt, err := tx.Prepare(
		fmt.Sprintf(`INSERT OR REPLACE INTO %s (id, course_id, data, updated_at, fetched_at)
			VALUES (?, ?, ?, ?, strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ', 'now'))`, table),
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("preparing upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, item := range items {
		jsonData, err := json.Marshal(item.Data)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("marshaling item %d: %w", item.ID, err)
		}

		var updatedAtStr *string
		if item.UpdatedAt != nil {
			s := item.UpdatedAt.UTC().Format(time.RFC3339)
			updatedAtStr = &s
		}

		if _, err := stmt.Exec(item.ID, item.CourseID, string(jsonData), updatedAtStr); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upserting item %d: %w", item.ID, err)
		}
	}

	return tx.Commit()
}

// ReplaceOptions tunes ReplaceAll.
type ReplaceOptions struct {
	// Truncated says the fetch is known to be incomplete (a pagination
	// truncation error was seen). The rows are stored but nothing is pruned
	// and the freshness stamp is not advanced.
	Truncated bool
}

// Status values written to sync_meta by ReplaceAll and the sync layer.
const (
	StatusSuccess = "success"
	StatusSuspect = "suspect" // fetch looked incomplete; old rows kept
	StatusSkipped = "skipped" // endpoint disabled for this course (403/404)
	StatusFailed  = "failed"  // fetch errored; see sync_meta.error
)

// ReplaceAll makes the cached set for (table, courseID) equal to items, in
// ONE transaction: upsert every item with a single fetched_at, delete every
// row of that course whose id is not in items, and stamp sync_meta. Callers
// must not swallow the error: a failed ReplaceAll leaves the previous state.
//
// Truncation guard: when opts.Truncated is set the fetched rows are stored
// but nothing is deleted and last_sync_at is NOT advanced, so ListFresh
// keeps serving the previous complete set and sync_meta.status reads
// "suspect". A truncated page can therefore never wipe a course.
//
// An empty fetch WITHOUT a truncation error is taken at its word: every row
// of the course is pruned, because upstream deleting everything (all
// announcements removed, a course's pages cleared) is a real state that a
// reader must see. The residual is a Canvas 200 with an empty body and no
// pagination fault; that wipes the course's rows until the next refresh.
func (d *DB) ReplaceAll(table ResourceType, courseID int64, items []CacheItem, opts ReplaceOptions) (string, error) {
	if !validTable(table) {
		return "", fmt.Errorf("%w: %s", errInvalidTable, table)
	}
	now := timestamp(time.Now())

	tx, err := d.db.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	suspect := opts.Truncated

	if len(items) > 0 {
		stmt, err := tx.Prepare(
			fmt.Sprintf(`INSERT OR REPLACE INTO %s (id, course_id, data, updated_at, fetched_at)
				VALUES (?, ?, ?, ?, ?)`, table),
		)
		if err != nil {
			return "", fmt.Errorf("preparing upsert: %w", err)
		}
		for _, item := range items {
			jsonData, err := json.Marshal(item.Data)
			if err != nil {
				_ = stmt.Close()
				return "", fmt.Errorf("marshaling item %d: %w", item.ID, err)
			}
			var updatedAtStr *string
			if item.UpdatedAt != nil {
				s := item.UpdatedAt.UTC().Format(time.RFC3339)
				updatedAtStr = &s
			}
			if _, err := stmt.Exec(item.ID, courseID, string(jsonData), updatedAtStr, now); err != nil {
				_ = stmt.Close()
				return "", fmt.Errorf("upserting item %d: %w", item.ID, err)
			}
		}
		_ = stmt.Close()
	}

	status := StatusSuccess
	if suspect {
		status = StatusSuspect
	} else {
		// Everything of this course not in the fetched set is gone upstream.
		// The keep-set goes through a temp table (per connection, and this
		// handle has one) so there is no bound-parameter cap and no reliance
		// on second-granular timestamps.
		if _, err := tx.Exec("CREATE TEMP TABLE IF NOT EXISTS keep_ids (id INTEGER PRIMARY KEY)"); err != nil {
			return "", fmt.Errorf("preparing prune: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM keep_ids"); err != nil {
			return "", fmt.Errorf("preparing prune: %w", err)
		}
		for _, item := range items {
			if _, err := tx.Exec("INSERT OR IGNORE INTO keep_ids (id) VALUES (?)", item.ID); err != nil {
				return "", fmt.Errorf("preparing prune: %w", err)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE course_id = ? AND id NOT IN (SELECT id FROM keep_ids)", table), courseID); err != nil {
			return "", fmt.Errorf("pruning %s: %w", table, err)
		}
		if _, err := tx.Exec("DELETE FROM keep_ids"); err != nil {
			return "", fmt.Errorf("finishing prune: %w", err)
		}
	}
	if err := setSyncMeta(tx, table, courseID, now, len(items), status, !suspect); err != nil {
		return "", fmt.Errorf("recording sync: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}
	return status, nil
}

// ListFresh is List restricted to rows fetched at or after the last complete
// sync of (table, courseID), so rows an interrupted or suspect sync did not
// touch are still served (they were part of the last complete set) while
// nothing older than that set leaks through. With no sync_meta row it
// behaves like List.
func (d *DB) ListFresh(table ResourceType, courseID int64, dest any) (SyncMeta, error) {
	meta, err := d.GetSyncMeta(table, courseID)
	if err != nil {
		return meta, err
	}
	if !validTable(table) {
		return meta, fmt.Errorf("%w: %s", errInvalidTable, table)
	}
	if meta.Status == StatusSkipped && meta.ItemCount == 0 {
		// Refused by Canvas with no complete set kept: the fresh set is
		// empty by definition, whatever opportunistic rows the table holds
		// (they may share the stamp's second and would otherwise leak in).
		return meta, nil
	}
	query := fmt.Sprintf("SELECT data FROM %s WHERE fetched_at >= ?", table)
	args := []any{timestamp(meta.LastSyncAt)}
	if meta.LastSyncAt.IsZero() {
		query = fmt.Sprintf("SELECT data FROM %s WHERE 1 = ?", table)
		args = []any{1}
	}
	if courseID != 0 {
		query += " AND course_id = ?"
		args = append(args, courseID)
	}
	query += " ORDER BY id"
	return meta, d.listQuery(table, query, args, dest)
}

// Prune deletes rows from a table where course_id matches and id is NOT in validIDs.
// This handles server-side deletions (e.g., instructor removes an assignment).
func (d *DB) Prune(table ResourceType, courseID int64, validIDs []int64) error {
	if !validTable(table) {
		return fmt.Errorf("%w: %s", errInvalidTable, table)
	}
	if len(validIDs) == 0 {
		// No valid IDs means delete everything for this course.
		_, err := d.db.Exec(
			fmt.Sprintf("DELETE FROM %s WHERE course_id = ?", table), courseID,
		)
		return err
	}

	// Build placeholders for the IN clause.
	placeholders := make([]string, len(validIDs))
	args := make([]any, 0, len(validIDs)+1)
	args = append(args, courseID)
	for i, id := range validIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	_, err := d.db.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE course_id = ? AND id NOT IN (%s)",
			table, strings.Join(placeholders, ",")),
		args...,
	)
	return err
}

// Count returns the number of rows in a table, optionally filtered by course.
func (d *DB) Count(table ResourceType, courseID int64) (int, error) {
	if !validTable(table) {
		return 0, fmt.Errorf("%w: %s", errInvalidTable, table)
	}
	var query string
	var args []any
	if courseID == 0 {
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	} else {
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE course_id = ?", table)
		args = append(args, courseID)
	}

	var count int
	err := d.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// FetchedAt returns when the given entity was last fetched, or zero time if not found.
func (d *DB) FetchedAt(table ResourceType, id int64) (time.Time, error) {
	var fetchedAt string
	err := d.db.QueryRow(
		fmt.Sprintf("SELECT fetched_at FROM %s WHERE id = ?", table), id,
	).Scan(&fetchedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, fetchedAt)
}
