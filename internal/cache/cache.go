// Package cache provides SQLite-backed local caching for Canvas data.
package cache

import (
	"database/sql"
	"fmt"
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

// pragmas are applied on every connection open.
const pragmas = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = normal;
PRAGMA temp_store = memory;
PRAGMA mmap_size = 268435456;
PRAGMA cache_size = -32000;
PRAGMA foreign_keys = ON;
`

// Open opens or creates a SQLite cache database at the given path.
// It applies PRAGMAs and runs any pending schema migrations.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening cache database: %w", err)
	}

	// Apply performance PRAGMAs.
	if _, err := db.Exec(pragmas); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting PRAGMAs: %w", err)
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
