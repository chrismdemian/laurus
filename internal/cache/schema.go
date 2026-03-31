package cache

import (
	"database/sql"
	"fmt"
)

// entityTables lists all entity table names in creation order.
var entityTables = []string{
	"courses",
	"enrollments",
	"assignments",
	"submissions",
	"assignment_groups",
	"announcements",
	"discussions",
	"modules",
	"module_items",
	"pages",
	"files",
	"folders",
	"conversations",
	"grading_standards",
	"calendar_events",
}

// migrationV1 creates the initial schema.
const migrationV1 = `
CREATE TABLE IF NOT EXISTS courses (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS enrollments (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_enrollments_course ON enrollments(course_id);

CREATE TABLE IF NOT EXISTS assignments (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_assignments_course ON assignments(course_id);

CREATE TABLE IF NOT EXISTS submissions (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_submissions_course ON submissions(course_id);

CREATE TABLE IF NOT EXISTS assignment_groups (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_assignment_groups_course ON assignment_groups(course_id);

CREATE TABLE IF NOT EXISTS announcements (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_announcements_course ON announcements(course_id);

CREATE TABLE IF NOT EXISTS discussions (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_discussions_course ON discussions(course_id);

CREATE TABLE IF NOT EXISTS modules (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_modules_course ON modules(course_id);

CREATE TABLE IF NOT EXISTS module_items (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_module_items_course ON module_items(course_id);

CREATE TABLE IF NOT EXISTS pages (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_pages_course ON pages(course_id);

CREATE TABLE IF NOT EXISTS files (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_files_course ON files(course_id);

CREATE TABLE IF NOT EXISTS folders (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_folders_course ON folders(course_id);

CREATE TABLE IF NOT EXISTS conversations (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS grading_standards (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER NOT NULL,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_grading_standards_course ON grading_standards(course_id);

CREATE TABLE IF NOT EXISTS sync_meta (
    resource_type TEXT    NOT NULL,
    course_id     INTEGER NOT NULL DEFAULT 0,
    last_sync_at  TEXT,
    item_count    INTEGER DEFAULT 0,
    status        TEXT    DEFAULT 'success',
    PRIMARY KEY (resource_type, course_id)
);

CREATE TABLE IF NOT EXISTS file_cache (
    canvas_id     INTEGER PRIMARY KEY,
    course_id     INTEGER NOT NULL,
    filename      TEXT    NOT NULL,
    size          INTEGER NOT NULL,
    modified_at   TEXT    NOT NULL,
    local_path    TEXT    NOT NULL,
    download_hash TEXT
);
CREATE INDEX IF NOT EXISTS idx_file_cache_course ON file_cache(course_id);
`

// migrations is an ordered list of SQL DDL scripts.
// Index 0 = version 1, index 1 = version 2, etc.
// migrationV2 adds calendar_events and notifications_sent tables.
const migrationV2 = `
CREATE TABLE IF NOT EXISTS calendar_events (
    id         INTEGER PRIMARY KEY,
    course_id  INTEGER,
    data       TEXT NOT NULL,
    updated_at TEXT,
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_calendar_events_course ON calendar_events(course_id);

CREATE TABLE IF NOT EXISTS notifications_sent (
    key        TEXT PRIMARY KEY,
    sent_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
`

var migrations = []string{
	migrationV1,
	migrationV2,
}

// migrate applies pending schema migrations using PRAGMA user_version.
func migrate(db *sql.DB) error {
	var currentVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&currentVersion); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	for i := currentVersion; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("setting schema version %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", i+1, err)
		}
	}
	return nil
}
