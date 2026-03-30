package cache

import "database/sql"

// FileCacheEntry tracks a downloaded file on disk.
type FileCacheEntry struct {
	CanvasID     int64
	CourseID     int64
	Filename     string
	Size         int64
	ModifiedAt   string
	LocalPath    string
	DownloadHash string
}

// GetFileCacheEntry retrieves a file cache entry by Canvas file ID.
// Returns sql.ErrNoRows if no entry exists.
func (d *DB) GetFileCacheEntry(canvasID int64) (FileCacheEntry, error) {
	var e FileCacheEntry
	var hash sql.NullString
	err := d.db.QueryRow(
		`SELECT canvas_id, course_id, filename, size, modified_at, local_path, download_hash
		 FROM file_cache WHERE canvas_id = ?`, canvasID,
	).Scan(&e.CanvasID, &e.CourseID, &e.Filename, &e.Size, &e.ModifiedAt, &e.LocalPath, &hash)
	if err != nil {
		return FileCacheEntry{}, err
	}
	if hash.Valid {
		e.DownloadHash = hash.String
	}
	return e, nil
}

// UpsertFileCacheEntry inserts or updates a file cache entry.
func (d *DB) UpsertFileCacheEntry(e FileCacheEntry) error {
	var hash *string
	if e.DownloadHash != "" {
		hash = &e.DownloadHash
	}
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO file_cache
		 (canvas_id, course_id, filename, size, modified_at, local_path, download_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.CanvasID, e.CourseID, e.Filename, e.Size, e.ModifiedAt, e.LocalPath, hash,
	)
	return err
}

// ListFileCacheEntries returns all file cache entries for a course.
func (d *DB) ListFileCacheEntries(courseID int64) ([]FileCacheEntry, error) {
	rows, err := d.db.Query(
		`SELECT canvas_id, course_id, filename, size, modified_at, local_path, download_hash
		 FROM file_cache WHERE course_id = ? ORDER BY filename`, courseID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []FileCacheEntry
	for rows.Next() {
		var e FileCacheEntry
		var hash sql.NullString
		if err := rows.Scan(&e.CanvasID, &e.CourseID, &e.Filename, &e.Size, &e.ModifiedAt, &e.LocalPath, &hash); err != nil {
			return nil, err
		}
		if hash.Valid {
			e.DownloadHash = hash.String
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
