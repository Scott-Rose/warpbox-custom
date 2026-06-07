// Package metadata provides a SQLite-backed store for the TorBox directory tree.
//
// Runs in WAL mode for high concurrent read performance. Browsing the
// virtual filesystem from Plex costs zero TorBox API calls.
package metadata

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Store is a SQLite-backed metadata cache.
type Store struct {
	db *sql.DB
}

// FileRecord represents a cached file entry from the TorBox directory.
type FileRecord struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	MimeType     string `json:"mime_type"`
	CDNURL       string `json:"cdn_url,omitempty"`
	CDNURLExpiry string `json:"cdn_url_expires,omitempty"`
}

// Open opens (or creates) the SQLite database at the given path.
// WAL mode is enabled for high concurrency.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates tables if they do not exist and runs schema migrations.
func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id              INTEGER PRIMARY KEY,
		name            TEXT    NOT NULL,
		path            TEXT    NOT NULL UNIQUE,
		size            INTEGER NOT NULL DEFAULT 0,
		mime_type       TEXT    NOT NULL DEFAULT '',
		cdn_url         TEXT    NOT NULL DEFAULT '',
		cdn_url_expires TEXT    NOT NULL DEFAULT '',
		updated         TEXT    NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Migrate v1 → v2: add cdn_url and cdn_url_expires columns if missing.
	// This is a no-op if the columns already exist.
	_, _ = s.db.Exec(`ALTER TABLE files ADD COLUMN cdn_url TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE files ADD COLUMN cdn_url_expires TEXT NOT NULL DEFAULT ''`)

	return nil
}

// UpsertFile inserts or replaces a file record.
func (s *Store) UpsertFile(f FileRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO files (id, name, path, size, mime_type, cdn_url, cdn_url_expires, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(path) DO UPDATE SET
			id             = excluded.id,
			name           = excluded.name,
			size           = excluded.size,
			mime_type      = excluded.mime_type,
			cdn_url        = excluded.cdn_url,
			cdn_url_expires = excluded.cdn_url_expires,
			updated        = excluded.updated
	`, f.ID, f.Name, f.Path, f.Size, f.MimeType, f.CDNURL, f.CDNURLExpiry)
	return err
}

// ListDir returns all files under the given virtual directory path.
func (s *Store) ListDir(prefix string) ([]FileRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, name, path, size, mime_type FROM files
		WHERE path LIKE ? ORDER BY name
	`, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("querying files: %w", err)
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.Name, &f.Path, &f.Size, &f.MimeType); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetFileByPath returns a single file record by its virtual path.
// Returns nil if the path is not found.
func (s *Store) GetFileByPath(path string) (*FileRecord, error) {
	row := s.db.QueryRow(`
		SELECT id, name, path, size, mime_type, cdn_url, cdn_url_expires
		FROM files WHERE path = ?
	`, path)

	var f FileRecord
	err := row.Scan(&f.ID, &f.Name, &f.Path, &f.Size, &f.MimeType, &f.CDNURL, &f.CDNURLExpiry)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying file by path: %w", err)
	}
	return &f, nil
}

// GetFileByID returns a single file record by its TorBox file ID.
// Returns nil if the ID is not found.
func (s *Store) GetFileByID(id int64) (*FileRecord, error) {
	row := s.db.QueryRow(`
		SELECT id, name, path, size, mime_type, cdn_url, cdn_url_expires
		FROM files WHERE id = ?
	`, id)

	var f FileRecord
	err := row.Scan(&f.ID, &f.Name, &f.Path, &f.Size, &f.MimeType, &f.CDNURL, &f.CDNURLExpiry)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying file by id: %w", err)
	}
	return &f, nil
}

// SetCDNURL stores a CDN download URL for a file with an expiry timestamp.
func (s *Store) SetCDNURL(fileID int64, cdnURL string, expiresAt time.Time) error {
	_, err := s.db.Exec(`
		UPDATE files SET cdn_url = ?, cdn_url_expires = ?, updated = datetime('now')
		WHERE id = ?
	`, cdnURL, expiresAt.UTC().Format(time.RFC3339), fileID)
	return err
}

// GetCDNURL returns a cached CDN URL for a file, or empty string if not cached or expired.
func (s *Store) GetCDNURL(fileID int64) (string, error) {
	row := s.db.QueryRow(`
		SELECT cdn_url, cdn_url_expires FROM files WHERE id = ?
	`, fileID)

	var url, expires string
	if err := row.Scan(&url, &expires); err == sql.ErrNoRows {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("querying CDN URL: %w", err)
	}

	if url == "" {
		return "", nil
	}
	if expires == "" {
		return url, nil // no expiry set, use cached value
	}

	expiryTime, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return "", nil // bad expiry format, treat as uncached
	}

	if time.Now().UTC().After(expiryTime) {
		return "", nil // expired
	}

	return url, nil
}