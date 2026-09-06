// Package cache provides SQLite-backed local caching for Canvas data.
package cache

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/chrismdemian/laurus/internal/config"

	_ "modernc.org/sqlite" // SQLite driver (pure Go, no CGO)
)

// DB wraps a *sql.DB with SQLite-specific lifecycle management.
type DB struct {
	db   *sql.DB
	path string
}

// ResourceType identifies a cacheable entity kind for TTL and sync_meta.
type ResourceType string

const (
	ResourceCourses          ResourceType = "courses"
	ResourceEnrollments      ResourceType = "enrollments"
	ResourceAssignments      ResourceType = "assignments"
	ResourceSubmissions      ResourceType = "submissions"
	ResourceAssignmentGroups ResourceType = "assignment_groups"
	ResourceAnnouncements    ResourceType = "announcements"
	ResourceDiscussions      ResourceType = "discussions"
	ResourceModules          ResourceType = "modules"
	ResourceModuleItems      ResourceType = "module_items"
	ResourcePages            ResourceType = "pages"
	ResourceFiles            ResourceType = "files"
	ResourceFolders          ResourceType = "folders"
	ResourceConversations    ResourceType = "conversations"
	ResourceGradingStandards ResourceType = "grading_standards"
	ResourceCalendarEvents   ResourceType = "calendar_events"
)

// TTL returns the freshness duration for a resource type.
func TTL(rt ResourceType) time.Duration {
	switch rt {
	case ResourceCourses, ResourceEnrollments:
		return 24 * time.Hour
	case ResourceAssignmentGroups, ResourceGradingStandards:
		return 6 * time.Hour
	case ResourceModules, ResourcePages:
		return 4 * time.Hour
	case ResourceAssignments:
		return 2 * time.Hour
	case ResourceFiles, ResourceFolders:
		return 1 * time.Hour
	case ResourceAnnouncements, ResourceDiscussions, ResourceCalendarEvents:
		return 30 * time.Minute
	case ResourceSubmissions:
		return 15 * time.Minute
	case ResourceConversations:
		return 5 * time.Minute
	default:
		return 1 * time.Hour
	}
}

// validTable returns true if the ResourceType corresponds to a known entity table.
func validTable(rt ResourceType) bool {
	for _, t := range entityTables {
		if string(rt) == t {
			return true
		}
	}
	return false
}

// connPragmas are applied by the driver on EVERY connection it opens. They
// live in the DSN on purpose: a one-shot "PRAGMA ..." Exec only configures
// whichever pooled connection happened to run it, and database/sql can drop
// and reopen connections at any time. That was the SQLITE_BUSY bug: a second
// connection had busy_timeout=0 and most concurrent writes failed.
var connPragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"temp_store(MEMORY)",
	"foreign_keys(ON)",
	"cache_size(-32000)",
}

// dsn builds the modernc.org/sqlite connection string for path.
func dsn(path string) string {
	q := url.Values{}
	for _, p := range connPragmas {
		q.Add("_pragma", p)
	}
	// Every transaction this package opens is a writer, so BEGIN IMMEDIATE
	// takes the write lock up front and waits busy_timeout for it. A
	// DEFERRED tx that reads first and then writes cannot be upgraded once
	// another process has committed in between: SQLite returns SQLITE_BUSY
	// immediately (busy_timeout is not consulted for a snapshot upgrade),
	// which is what made half of all cross-process ReplaceAll calls fail.
	q.Set("_txlock", "immediate")
	// The driver strips the query itself, so the path is passed verbatim
	// (spaces included, e.g. ~/Library/Application Support/laurus).
	return "file:" + path + "?" + q.Encode()
}

// Open opens or creates a SQLite cache database at the given path.
// It applies PRAGMAs and runs any pending schema migrations.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("opening cache database: %w", err)
	}
	// One connection per handle: writes from this process serialise in Go
	// rather than contending in SQLite, and every statement sees the same
	// connection-level pragmas. Cross-process contention is handled by
	// busy_timeout in the DSN. Any code holding a *sql.Tx must therefore
	// never call a d.db.* method until the tx is finished.
	db.SetMaxOpenConns(1)

	// Fail early if the file is unusable rather than on the first query.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("opening cache database: %w", err)
	}

	// Run schema migrations.
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating cache schema: %w", err)
	}

	return &DB{db: db, path: path}, nil
}

// OpenDefault opens the cache database at the platform-appropriate default path
// (alongside config.toml, e.g., ~/.config/laurus/cache.db).
func OpenDefault() (*DB, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return Open(filepath.Join(dir, "cache.db"))
}

// Close runs PRAGMA optimize and closes the underlying database.
func (d *DB) Close() error {
	_, _ = d.db.Exec("PRAGMA optimize")
	return d.db.Close()
}

// Path returns the filesystem path to the cache database file.
func (d *DB) Path() string {
	return d.path
}

// Reset drops all tables and recreates the schema from scratch.
func (d *DB) Reset() error {
	// Drop all entity tables, sync_meta, and file_cache.
	for _, table := range entityTables {
		if _, err := d.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
			return fmt.Errorf("dropping table %s: %w", table, err)
		}
	}
	for _, table := range []string{"sync_meta", "file_cache", "notifications_sent"} {
		if _, err := d.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
			return fmt.Errorf("dropping table %s: %w", table, err)
		}
	}

	// Reset version counter so migrations re-run.
	if _, err := d.db.Exec("PRAGMA user_version = 0"); err != nil {
		return fmt.Errorf("resetting schema version: %w", err)
	}

	return migrate(d.db)
}

// HasNotified returns true if a notification with the given key has been sent.
func (d *DB) HasNotified(key string) bool {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM notifications_sent WHERE key = ?", key).Scan(&count)
	return err == nil && count > 0
}

// MarkNotified records that a notification with the given key has been sent.
func (d *DB) MarkNotified(key string) error {
	_, err := d.db.Exec(
		"INSERT OR IGNORE INTO notifications_sent (key) VALUES (?)", key)
	return err
}

// CleanNotifications removes notification records older than the given duration.
func (d *DB) CleanNotifications(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge).UTC().Format(time.RFC3339)
	_, err := d.db.Exec("DELETE FROM notifications_sent WHERE sent_at < ?", cutoff)
	return err
}
