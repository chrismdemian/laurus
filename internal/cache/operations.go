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
