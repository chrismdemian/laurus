package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// testDB creates a temporary cache database for testing.
func testDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-cache.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s) error: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testCourse is a minimal struct that mirrors canvas.Course for testing serialization.
type testCourse struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CourseCode string `json:"course_code"`
}

type testAssignment struct {
	ID             int64   `json:"id"`
	CourseID       int64   `json:"course_id"`
	Name           string  `json:"name"`
	PointsPossible float64 `json:"points_possible"`
}

func TestOpen_CreatesDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "new.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer func() { _ = db.Close() }()

	if db.Path() != path {
		t.Errorf("Path() = %q, want %q", db.Path(), path)
	}
}

func TestOpen_PRAGMAs(t *testing.T) {
	db := testDB(t)

	tests := []struct {
		pragma string
		want   string
	}{
		{"PRAGMA journal_mode", "wal"},
		{"PRAGMA synchronous", "1"}, // normal = 1
		{"PRAGMA foreign_keys", "1"},
	}

	for _, tt := range tests {
		var val string
		if err := db.db.QueryRow(tt.pragma).Scan(&val); err != nil {
			t.Fatalf("%s error: %v", tt.pragma, err)
		}
		if val != tt.want {
			t.Errorf("%s = %q, want %q", tt.pragma, val, tt.want)
		}
	}
}

func TestOpen_SchemaVersion(t *testing.T) {
	db := testDB(t)

	var version int
	if err := db.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("PRAGMA user_version error: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("user_version = %d, want %d", version, len(migrations))
	}
}

func TestMigration_Idempotent(t *testing.T) {
	db := testDB(t)
	// Running migrate again should be a no-op.
	if err := migrate(db.db); err != nil {
		t.Fatalf("second migrate error: %v", err)
	}
}

func TestUpsert_And_Get(t *testing.T) {
	db := testDB(t)

	course := testCourse{ID: 42, Name: "Intro to CS", CourseCode: "CSC108"}
	if err := db.Upsert(ResourceCourses, course.ID, 0, course, nil); err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	var got testCourse
	if err := db.Get(ResourceCourses, 42, &got); err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got.Name != "Intro to CS" || got.CourseCode != "CSC108" {
		t.Errorf("Get = %+v, want name=Intro to CS, code=CSC108", got)
	}
}

func TestUpsert_Replace(t *testing.T) {
	db := testDB(t)

	course := testCourse{ID: 1, Name: "Old Name", CourseCode: "OLD"}
	if err := db.Upsert(ResourceCourses, 1, 0, course, nil); err != nil {
		t.Fatalf("first Upsert error: %v", err)
	}

	course.Name = "New Name"
	course.CourseCode = "NEW"
	if err := db.Upsert(ResourceCourses, 1, 0, course, nil); err != nil {
		t.Fatalf("second Upsert error: %v", err)
	}

	var got testCourse
	if err := db.Get(ResourceCourses, 1, &got); err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got.Name != "New Name" {
		t.Errorf("Get after replace = %q, want %q", got.Name, "New Name")
	}
}

func TestGet_NotFound(t *testing.T) {
	db := testDB(t)

	var got testCourse
	err := db.Get(ResourceCourses, 999, &got)
	if err != sql.ErrNoRows {
		t.Errorf("Get for nonexistent ID: err = %v, want sql.ErrNoRows", err)
	}
}

func TestUpsertMany_And_List(t *testing.T) {
	db := testDB(t)

	items := []CacheItem{
		{ID: 1, CourseID: 100, Data: testAssignment{ID: 1, CourseID: 100, Name: "HW1", PointsPossible: 10}},
		{ID: 2, CourseID: 100, Data: testAssignment{ID: 2, CourseID: 100, Name: "HW2", PointsPossible: 20}},
		{ID: 3, CourseID: 200, Data: testAssignment{ID: 3, CourseID: 200, Name: "Lab1", PointsPossible: 5}},
	}
	if err := db.UpsertMany(ResourceAssignments, items); err != nil {
		t.Fatalf("UpsertMany error: %v", err)
	}

	// List for course 100 should return 2 items.
	var course100 []testAssignment
	if err := db.List(ResourceAssignments, 100, &course100); err != nil {
		t.Fatalf("List(100) error: %v", err)
	}
	if len(course100) != 2 {
		t.Errorf("List(100) returned %d items, want 2", len(course100))
	}

	// List for course 200 should return 1 item.
	var course200 []testAssignment
	if err := db.List(ResourceAssignments, 200, &course200); err != nil {
		t.Fatalf("List(200) error: %v", err)
	}
	if len(course200) != 1 {
		t.Errorf("List(200) returned %d items, want 1", len(course200))
	}

	// List all (courseID=0) should return 3 items.
	var all []testAssignment
	if err := db.List(ResourceAssignments, 0, &all); err != nil {
		t.Fatalf("List(0) error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List(0) returned %d items, want 3", len(all))
	}
}

func TestUpsertMany_Empty(t *testing.T) {
	db := testDB(t)
	if err := db.UpsertMany(ResourceCourses, nil); err != nil {
		t.Fatalf("UpsertMany(nil) error: %v", err)
	}
}

func TestPrune(t *testing.T) {
	db := testDB(t)

	items := []CacheItem{
		{ID: 1, CourseID: 100, Data: testAssignment{ID: 1, CourseID: 100, Name: "Keep1"}},
		{ID: 2, CourseID: 100, Data: testAssignment{ID: 2, CourseID: 100, Name: "Keep2"}},
		{ID: 3, CourseID: 100, Data: testAssignment{ID: 3, CourseID: 100, Name: "Remove"}},
		{ID: 4, CourseID: 100, Data: testAssignment{ID: 4, CourseID: 100, Name: "Remove2"}},
		{ID: 5, CourseID: 200, Data: testAssignment{ID: 5, CourseID: 200, Name: "OtherCourse"}},
	}
	if err := db.UpsertMany(ResourceAssignments, items); err != nil {
		t.Fatalf("UpsertMany error: %v", err)
	}

	// Prune course 100, keeping only IDs 1 and 2.
	if err := db.Prune(ResourceAssignments, 100, []int64{1, 2}); err != nil {
		t.Fatalf("Prune error: %v", err)
	}

	var course100 []testAssignment
	if err := db.List(ResourceAssignments, 100, &course100); err != nil {
		t.Fatalf("List(100) after prune error: %v", err)
	}
	if len(course100) != 2 {
		t.Errorf("after prune: List(100) = %d items, want 2", len(course100))
	}

	// Course 200 should be unaffected.
	var course200 []testAssignment
	if err := db.List(ResourceAssignments, 200, &course200); err != nil {
		t.Fatalf("List(200) error: %v", err)
	}
	if len(course200) != 1 {
		t.Errorf("after prune: List(200) = %d items, want 1", len(course200))
	}
}

func TestPrune_EmptyValidIDs(t *testing.T) {
	db := testDB(t)

	items := []CacheItem{
		{ID: 1, CourseID: 100, Data: testAssignment{ID: 1, CourseID: 100, Name: "A"}},
		{ID: 2, CourseID: 100, Data: testAssignment{ID: 2, CourseID: 100, Name: "B"}},
	}
	if err := db.UpsertMany(ResourceAssignments, items); err != nil {
		t.Fatalf("UpsertMany error: %v", err)
	}

	// Empty validIDs should delete all for that course.
	if err := db.Prune(ResourceAssignments, 100, nil); err != nil {
		t.Fatalf("Prune(nil) error: %v", err)
	}

	count, err := db.Count(ResourceAssignments, 100)
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 0 {
		t.Errorf("after Prune(nil): count = %d, want 0", count)
	}
}

func TestIsStale_NeverSynced(t *testing.T) {
	db := testDB(t)

	if !db.IsStale(ResourceCourses, 0) {
		t.Error("IsStale for never-synced resource should return true")
	}
}

func TestIsStale_Fresh(t *testing.T) {
	db := testDB(t)

	if err := db.SetSyncMeta(ResourceCourses, 0, 5, "success"); err != nil {
		t.Fatalf("SetSyncMeta error: %v", err)
	}

	if db.IsStale(ResourceCourses, 0) {
		t.Error("IsStale immediately after SetSyncMeta should return false")
	}
}

func TestGetSyncMeta_NotFound(t *testing.T) {
	db := testDB(t)

	meta, err := db.GetSyncMeta(ResourceCourses, 0)
	if err != nil {
		t.Fatalf("GetSyncMeta error: %v", err)
	}
	if !meta.LastSyncAt.IsZero() {
		t.Errorf("LastSyncAt for missing entry = %v, want zero", meta.LastSyncAt)
	}
}

func TestSetSyncMeta_And_GetSyncMeta(t *testing.T) {
	db := testDB(t)

	if err := db.SetSyncMeta(ResourceAssignments, 100, 42, "success"); err != nil {
		t.Fatalf("SetSyncMeta error: %v", err)
	}

	meta, err := db.GetSyncMeta(ResourceAssignments, 100)
	if err != nil {
		t.Fatalf("GetSyncMeta error: %v", err)
	}
	if meta.ItemCount != 42 {
		t.Errorf("ItemCount = %d, want 42", meta.ItemCount)
	}
	if meta.Status != "success" {
		t.Errorf("Status = %q, want %q", meta.Status, "success")
	}
	if meta.LastSyncAt.IsZero() {
		t.Error("LastSyncAt should not be zero after SetSyncMeta")
	}
	if time.Since(meta.LastSyncAt) > 10*time.Second {
		t.Errorf("LastSyncAt = %v, should be recent", meta.LastSyncAt)
	}
}

func TestAllSyncMeta(t *testing.T) {
	db := testDB(t)

	_ = db.SetSyncMeta(ResourceCourses, 0, 5, "success")
	_ = db.SetSyncMeta(ResourceAssignments, 100, 10, "success")
	_ = db.SetSyncMeta(ResourceAssignments, 200, 8, "partial")

	metas, err := db.AllSyncMeta()
	if err != nil {
		t.Fatalf("AllSyncMeta error: %v", err)
	}
	if len(metas) != 3 {
		t.Errorf("AllSyncMeta returned %d entries, want 3", len(metas))
	}
}

func TestStats(t *testing.T) {
	db := testDB(t)

	items := []CacheItem{
		{ID: 1, CourseID: 0, Data: testCourse{ID: 1, Name: "C1"}},
		{ID: 2, CourseID: 0, Data: testCourse{ID: 2, Name: "C2"}},
	}
	_ = db.UpsertMany(ResourceCourses, items)

	counts, fileSize, err := db.Stats()
	if err != nil {
		t.Fatalf("Stats error: %v", err)
	}
	if counts[ResourceCourses] != 2 {
		t.Errorf("Stats courses = %d, want 2", counts[ResourceCourses])
	}
	if fileSize == 0 {
		t.Error("Stats fileSize should be > 0")
	}
}

func TestReset(t *testing.T) {
	db := testDB(t)

	_ = db.Upsert(ResourceCourses, 1, 0, testCourse{ID: 1, Name: "C1"}, nil)
	_ = db.SetSyncMeta(ResourceCourses, 0, 1, "success")

	if err := db.Reset(); err != nil {
		t.Fatalf("Reset error: %v", err)
	}

	count, _ := db.Count(ResourceCourses, 0)
	if count != 0 {
		t.Errorf("after Reset: courses count = %d, want 0", count)
	}

	meta, _ := db.GetSyncMeta(ResourceCourses, 0)
	if !meta.LastSyncAt.IsZero() {
		t.Error("after Reset: sync_meta should be empty")
	}
}

func TestFileCacheEntry(t *testing.T) {
	db := testDB(t)

	entry := FileCacheEntry{
		CanvasID:     42,
		CourseID:     100,
		Filename:     "slides.pdf",
		Size:         1024000,
		ModifiedAt:   "2026-01-15T10:30:00Z",
		LocalPath:    "/home/user/School/CSC108/Lectures/slides.pdf",
		DownloadHash: "abc123",
	}
	if err := db.UpsertFileCacheEntry(entry); err != nil {
		t.Fatalf("UpsertFileCacheEntry error: %v", err)
	}

	got, err := db.GetFileCacheEntry(42)
	if err != nil {
		t.Fatalf("GetFileCacheEntry error: %v", err)
	}
	if got.Filename != "slides.pdf" {
		t.Errorf("Filename = %q, want %q", got.Filename, "slides.pdf")
	}
	if got.Size != 1024000 {
		t.Errorf("Size = %d, want 1024000", got.Size)
	}
	if got.DownloadHash != "abc123" {
		t.Errorf("DownloadHash = %q, want %q", got.DownloadHash, "abc123")
	}

	// List by course.
	entries, err := db.ListFileCacheEntries(100)
	if err != nil {
		t.Fatalf("ListFileCacheEntries error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("ListFileCacheEntries returned %d, want 1", len(entries))
	}
}

func TestFileCacheEntry_NotFound(t *testing.T) {
	db := testDB(t)

	_, err := db.GetFileCacheEntry(999)
	if err != sql.ErrNoRows {
		t.Errorf("GetFileCacheEntry(999) err = %v, want sql.ErrNoRows", err)
	}
}

func TestCount(t *testing.T) {
	db := testDB(t)

	items := []CacheItem{
		{ID: 1, CourseID: 100, Data: testAssignment{ID: 1, CourseID: 100, Name: "A"}},
		{ID: 2, CourseID: 100, Data: testAssignment{ID: 2, CourseID: 100, Name: "B"}},
		{ID: 3, CourseID: 200, Data: testAssignment{ID: 3, CourseID: 200, Name: "C"}},
	}
	_ = db.UpsertMany(ResourceAssignments, items)

	total, err := db.Count(ResourceAssignments, 0)
	if err != nil {
		t.Fatalf("Count(0) error: %v", err)
	}
	if total != 3 {
		t.Errorf("Count(0) = %d, want 3", total)
	}

	c100, err := db.Count(ResourceAssignments, 100)
	if err != nil {
		t.Fatalf("Count(100) error: %v", err)
	}
	if c100 != 2 {
		t.Errorf("Count(100) = %d, want 2", c100)
	}
}

func TestUpsert_WithUpdatedAt(t *testing.T) {
	db := testDB(t)

	ts := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	course := testCourse{ID: 1, Name: "Test", CourseCode: "T101"}
	if err := db.Upsert(ResourceCourses, 1, 0, course, &ts); err != nil {
		t.Fatalf("Upsert with updatedAt error: %v", err)
	}

	var updatedAt string
	err := db.db.QueryRow("SELECT updated_at FROM courses WHERE id = 1").Scan(&updatedAt)
	if err != nil {
		t.Fatalf("query updated_at error: %v", err)
	}
	if updatedAt != "2026-03-15T12:00:00Z" {
		t.Errorf("updated_at = %q, want %q", updatedAt, "2026-03-15T12:00:00Z")
	}
}

func TestTTL(t *testing.T) {
	tests := []struct {
		rt   ResourceType
		want time.Duration
	}{
		{ResourceCourses, 24 * time.Hour},
		{ResourceSubmissions, 15 * time.Minute},
		{ResourceConversations, 5 * time.Minute},
		{ResourceAssignments, 2 * time.Hour},
		{ResourceGradingStandards, 6 * time.Hour},
	}

	for _, tt := range tests {
		got := TTL(tt.rt)
		if got != tt.want {
			t.Errorf("TTL(%s) = %v, want %v", tt.rt, got, tt.want)
		}
	}
}
