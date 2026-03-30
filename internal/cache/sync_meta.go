package cache

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

// SyncMeta holds metadata about when a resource type was last synced.
type SyncMeta struct {
	ResourceType ResourceType
	CourseID     int64 // 0 for cross-course resources
	LastSyncAt   time.Time
	ItemCount    int
	Status       string // "success", "partial", "failed"
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
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO sync_meta (resource_type, course_id, last_sync_at, item_count, status)
		 VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), ?, ?)`,
		string(resource), courseID, count, status,
	)
	return err
}

// GetSyncMeta reads sync metadata for a resource type and course.
// Returns a zero-value SyncMeta if no entry exists.
func (d *DB) GetSyncMeta(resource ResourceType, courseID int64) (SyncMeta, error) {
	var lastSyncAt sql.NullString
	var itemCount int
	var status string

	err := d.db.QueryRow(
		`SELECT last_sync_at, item_count, status FROM sync_meta
		 WHERE resource_type = ? AND course_id = ?`,
		string(resource), courseID,
	).Scan(&lastSyncAt, &itemCount, &status)

	if err == sql.ErrNoRows {
		return SyncMeta{ResourceType: resource, CourseID: courseID}, nil
	}
	if err != nil {
		return SyncMeta{}, err
	}

	meta := SyncMeta{
		ResourceType: resource,
		CourseID:     courseID,
		ItemCount:    itemCount,
		Status:       status,
	}
	if lastSyncAt.Valid {
		t, err := time.Parse(time.RFC3339, lastSyncAt.String)
		if err == nil {
			meta.LastSyncAt = t
		}
	}
	return meta, nil
}

// AllSyncMeta returns all sync_meta entries.
func (d *DB) AllSyncMeta() ([]SyncMeta, error) {
	rows, err := d.db.Query(
		`SELECT resource_type, course_id, last_sync_at, item_count, status
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
		var lastSyncAt sql.NullString
		var itemCount int
		var status string

		if err := rows.Scan(&rt, &courseID, &lastSyncAt, &itemCount, &status); err != nil {
			return nil, err
		}

		meta := SyncMeta{
			ResourceType: ResourceType(rt),
			CourseID:     courseID,
			ItemCount:    itemCount,
			Status:       status,
		}
		if lastSyncAt.Valid {
			t, err := time.Parse(time.RFC3339, lastSyncAt.String)
			if err == nil {
				meta.LastSyncAt = t
			}
		}
		metas = append(metas, meta)
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
