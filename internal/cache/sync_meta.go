package cache

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

// SyncMeta holds metadata about when a resource type was last synced.
type SyncMeta struct {
	ResourceType  ResourceType
	CourseID      int64     // 0 for cross-course resources
	LastSyncAt    time.Time // last COMPLETE sync; the as_of stamp readers show
	LastAttemptAt time.Time // last attempt, successful or not
	ItemCount     int
	Status        string // StatusSuccess, StatusSuspect, StatusSkipped, StatusFailed
	Error         string // why the last attempt failed, cleared on success
}

// IsStale returns true if the given resource needs to be re-fetched.
// A resource is stale if it has never been synced or its TTL has expired.
func (d *DB) IsStale(resource ResourceType, courseID int64) bool {
	meta, err := d.GetSyncMeta(resource, courseID)
	if err != nil || meta.LastSyncAt.IsZero() {
		return true
	}
	return time.Since(meta.LastSyncAt) > TTL(resource)
}

// SetSyncMeta records that a resource type was synced now.
func (d *DB) SetSyncMeta(resource ResourceType, courseID int64, count int, status string) error {
	return setSyncMeta(d.db, resource, courseID, timestamp(time.Now()), count, status, true)
}

// SetSyncMetaAt is SetSyncMeta with an explicit timestamp (tests and
// maintenance tooling; the sync layer always stamps "now").
func (d *DB) SetSyncMetaAt(resource ResourceType, courseID int64, at time.Time, count int, status string) error {
	return setSyncMeta(d.db, resource, courseID, timestamp(at), count, status, true)
}

// RecordSkipped marks (resource, courseID) as refused by Canvas (403/404).
// When the course already has cached rows the freshness stamp is NOT
// advanced, mirroring the truncation guard: a throttle or a temporarily
// disabled tab must not hide a whole tier's worth of real data behind an
// empty set. With no rows the empty set is the truth and the stamp advances.
// It returns the number of rows kept.
func (d *DB) RecordSkipped(resource ResourceType, courseID int64) (int, error) {
	existing := 0
	if validTable(resource) {
		n, err := d.Count(resource, courseID)
		if err != nil {
			return 0, err
		}
		existing = n
	}
	return existing, setSyncMeta(d.db, resource, courseID, timestamp(time.Now()), existing, StatusSkipped, existing == 0)
}

// execer is satisfied by *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// setSyncMeta upserts one sync_meta row by named columns (never INSERT OR
// REPLACE, which would null any column not listed). When advance is false
// the existing last_sync_at is kept: used for a suspect sync, so ListFresh
// keeps serving the last complete set.
func setSyncMeta(x execer, resource ResourceType, courseID int64, at string, count int, status string, advance bool) error {
	_, err := x.Exec(
		`INSERT INTO sync_meta (resource_type, course_id, last_sync_at, last_attempt_at, item_count, status, error)
		 VALUES (?, ?, ?, ?, ?, ?, NULL)
		 ON CONFLICT(resource_type, course_id) DO UPDATE SET
		   last_sync_at    = CASE WHEN ? THEN excluded.last_sync_at ELSE sync_meta.last_sync_at END,
		   last_attempt_at = excluded.last_attempt_at,
		   item_count      = excluded.item_count,
		   status          = excluded.status,
		   error           = NULL`,
		string(resource), courseID, nullIfNotAdvancing(at, advance), at, count, status, advance,
	)
	return err
}

// RecordSyncFailure marks the last attempt for (resource, courseID) as
// failed with the reason. last_sync_at and item_count are untouched: the
// previous complete set is still what readers serve, and its stamp must
// keep telling the truth.
func (d *DB) RecordSyncFailure(resource ResourceType, courseID int64, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	_, err := d.db.Exec(
		`INSERT INTO sync_meta (resource_type, course_id, last_sync_at, last_attempt_at, item_count, status, error)
		 VALUES (?, ?, NULL, ?, 0, ?, ?)
		 ON CONFLICT(resource_type, course_id) DO UPDATE SET
		   last_attempt_at = excluded.last_attempt_at,
		   status          = excluded.status,
		   error           = excluded.error`,
		string(resource), courseID, timestamp(time.Now()), StatusFailed, msg,
	)
	return err
}

func nullIfNotAdvancing(at string, advance bool) any {
	if advance {
		return at
	}
	return nil
}

// timestamp formats t the way every fetched_at/last_sync_at column stores
// it, so the strings compare correctly in SQL.
func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// GetSyncMeta reads sync metadata for a resource type and course.
// Returns a zero-value SyncMeta if no entry exists.
func (d *DB) GetSyncMeta(resource ResourceType, courseID int64) (SyncMeta, error) {
	var lastSyncAt, lastAttemptAt, errText sql.NullString
	var itemCount int
	var status string

	err := d.db.QueryRow(
		`SELECT last_sync_at, last_attempt_at, item_count, status, error FROM sync_meta
		 WHERE resource_type = ? AND course_id = ?`,
		string(resource), courseID,
	).Scan(&lastSyncAt, &lastAttemptAt, &itemCount, &status, &errText)

	if err == sql.ErrNoRows {
		return SyncMeta{ResourceType: resource, CourseID: courseID}, nil
	}
	if err != nil {
		return SyncMeta{}, err
	}

	meta := SyncMeta{
		ResourceType:  resource,
		CourseID:      courseID,
		ItemCount:     itemCount,
		Status:        status,
		LastSyncAt:    parseStamp(lastSyncAt),
		LastAttemptAt: parseStamp(lastAttemptAt),
		Error:         errText.String,
	}
	return meta, nil
}

func parseStamp(v sql.NullString) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

// AllSyncMeta returns all sync_meta entries.
func (d *DB) AllSyncMeta() ([]SyncMeta, error) {
	rows, err := d.db.Query(
		`SELECT resource_type, course_id, last_sync_at, last_attempt_at, item_count, status, error
		 FROM sync_meta ORDER BY resource_type, course_id`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var metas []SyncMeta
	for rows.Next() {
		var rt string
		var courseID int64
		var lastSyncAt, lastAttemptAt, errText sql.NullString
		var itemCount int
		var status string

		if err := rows.Scan(&rt, &courseID, &lastSyncAt, &lastAttemptAt, &itemCount, &status, &errText); err != nil {
			return nil, err
		}

		metas = append(metas, SyncMeta{
			ResourceType:  ResourceType(rt),
			CourseID:      courseID,
			ItemCount:     itemCount,
			Status:        status,
			LastSyncAt:    parseStamp(lastSyncAt),
			LastAttemptAt: parseStamp(lastAttemptAt),
			Error:         errText.String,
		})
	}
	return metas, rows.Err()
}

// Stats returns row counts per entity table and the total database file size.
func (d *DB) Stats() (map[ResourceType]int, int64, error) {
	counts := make(map[ResourceType]int)
	for _, table := range entityTables {
		var count int
		if err := d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err != nil {
			return nil, 0, fmt.Errorf("counting %s: %w", table, err)
		}
		if count > 0 {
			counts[ResourceType(table)] = count
		}
	}

	var fileSize int64
	if info, err := os.Stat(d.path); err == nil {
		fileSize = info.Size()
	}

	return counts, fileSize, nil
}
