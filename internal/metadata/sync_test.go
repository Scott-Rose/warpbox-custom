package metadata

import (
	"testing"

	"github.com/ben/warpbox/internal/torbox"
)

func TestBuildFileRecordTorrent(t *testing.T) {
	f := torbox.TorrentFile{
		ID:        10,
		Name:      "movie.mkv",
		Size:      500,
		MimeType:  "video/x-matroska",
		S3Path:    "abc123/dir/movie.mkv",
		ShortName: "movie.mkv",
	}
	rec := buildFileRecord(42, f, 7, SourceTorrent, "2025-01-01T00:00:00Z")

	if rec.ItemID != 42 {
		t.Errorf("ItemID = %d, want 42", rec.ItemID)
	}
	if rec.FileID != 10 {
		t.Errorf("FileID = %d, want 10", rec.FileID)
	}
	if rec.Source != SourceTorrent {
		t.Errorf("Source = %d, want %d (SourceTorrent)", rec.Source, SourceTorrent)
	}
	if rec.SyncTag != 7 {
		t.Errorf("SyncTag = %d, want 7", rec.SyncTag)
	}
	if rec.CreatedAt != "2025-01-01T00:00:00Z" {
		t.Errorf("CreatedAt = %q, want %q", rec.CreatedAt, "2025-01-01T00:00:00Z")
	}
	if rec.Path != "dir/movie.mkv" {
		t.Errorf("Path = %q, want %q", rec.Path, "dir/movie.mkv")
	}
}

func TestBuildFileRecordUsenet(t *testing.T) {
	f := torbox.TorrentFile{
		ID:        20,
		Name:      "usenet_file.mkv",
		Size:      1000,
		MimeType:  "video/x-matroska",
		S3Path:    "def456/usenet_file.mkv",
		ShortName: "usenet_file.mkv",
	}
	rec := buildFileRecord(1644029, f, 3, SourceUsenet, "2025-06-01T12:00:00Z")

	if rec.ItemID != 1644029 {
		t.Errorf("ItemID = %d, want 1644029", rec.ItemID)
	}
	if rec.Source != SourceUsenet {
		t.Errorf("Source = %d, want %d (SourceUsenet)", rec.Source, SourceUsenet)
	}
	if rec.SyncTag != 3 {
		t.Errorf("SyncTag = %d, want 3", rec.SyncTag)
	}
	if rec.Path != "usenet_file.mkv" {
		t.Errorf("Path = %q, want %q", rec.Path, "usenet_file.mkv")
	}
}

func TestBuildFileRecordSingleFileAtRoot(t *testing.T) {
	// Single-file items have s3_path like "hash/filename.ext" with no directory.
	f := torbox.TorrentFile{
		ID:        1,
		S3Path:    "abc123/movie.mkv",
		ShortName: "movie.mkv",
	}
	rec := buildFileRecord(1, f, 1, SourceTorrent, "")

	if rec.Path != "movie.mkv" {
		t.Errorf("single-file Path = %q, want %q", rec.Path, "movie.mkv")
	}
}

func TestBuildFileRecordMultiFileWithDir(t *testing.T) {
	// Multi-file items have s3_path like "hash/dir/file.ext".
	f := torbox.TorrentFile{
		ID:        2,
		S3Path:    "abc123/Season 1/episode.mkv",
		ShortName: "episode.mkv",
	}
	rec := buildFileRecord(1, f, 1, SourceTorrent, "")

	if rec.Path != "Season 1/episode.mkv" {
		t.Errorf("multi-file Path = %q, want %q", rec.Path, "Season 1/episode.mkv")
	}
}

func TestBuildFileRecordSanitizesPath(t *testing.T) {
	// Characters like & should be replaced.
	f := torbox.TorrentFile{
		ID:        3,
		S3Path:    "abc123/A & B/show.mkv",
		ShortName: "show.mkv",
	}
	rec := buildFileRecord(1, f, 1, SourceTorrent, "")

	if rec.Path != "A _ B/show.mkv" {
		t.Errorf("sanitized Path = %q, want %q", rec.Path, "A _ B/show.mkv")
	}
	if rec.Name != "show.mkv" {
		t.Errorf("sanitized Name = %q, want %q", rec.Name, "show.mkv")
	}
}