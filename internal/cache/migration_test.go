package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestMigrateV2ToV3 builds a V2 database the way a pre-upgrade laurus left
// it (rows included), opens it with the current code, and checks the schema
// advanced and the rows survived. Reset() reruns from zero, so a fresh
// database is covered by every other test.
func TestMigrateV2ToV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := raw.Exec(migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	if _, err := raw.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO sync_meta (resource_type, course_id, last_sync_at, item_count, status) VALUES ('assignments', 7, '2026-09-01T00:00:00Z', 12, 'success')`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO assignments (id, course_id, data) VALUES (1, 7, '{"id":1}')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open on V2 file: %v", err)
	}
	defer func() { _ = db.Close() }()

	var version int
	if err := db.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != len(migrations) {
		t.Errorf("user_version = %d, want %d", version, len(migrations))
	}
	meta, err := db.GetSyncMeta(ResourceAssignments, 7)
	if err != nil {
		t.Fatal(err)
	}
	oldStamp := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if meta.ItemCount != 12 || meta.Status != StatusSuccess || !meta.LastSyncAt.Equal(oldStamp) || meta.Error != "" || !meta.LastAttemptAt.IsZero() {
		t.Errorf("row after migration = %+v", meta)
	}

	// New columns are usable and the old ones untouched by a failure.
	if err := db.RecordSyncFailure(ResourceAssignments, 7, errBoom); err != nil {
		t.Fatal(err)
	}
	meta, _ = db.GetSyncMeta(ResourceAssignments, 7)
	if meta.Status != StatusFailed || meta.Error != "boom" || time.Since(meta.LastAttemptAt) > time.Minute || !meta.LastSyncAt.Equal(oldStamp) || meta.ItemCount != 12 {
		t.Errorf("after failure = %+v (last_sync_at/item_count must be untouched)", meta)
	}
	// A later success clears the error and stamps both columns.
	if _, err := db.ReplaceAll(ResourceAssignments, 7, []CacheItem{{ID: 1, Data: map[string]any{"id": 1}}}, ReplaceOptions{}); err != nil {
		t.Fatal(err)
	}
	meta, _ = db.GetSyncMeta(ResourceAssignments, 7)
	if meta.Status != StatusSuccess || meta.Error != "" || meta.LastAttemptAt.IsZero() || !meta.LastSyncAt.After(oldStamp) {
		t.Errorf("after recovery = %+v", meta)
	}
}

type constErr string

func (e constErr) Error() string { return string(e) }

const errBoom = constErr("boom")
