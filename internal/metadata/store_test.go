package metadata

import (
	"os"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := t.TempDir() + "/test.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	return s
}

func TestOpenAndClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestUpsertFile(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	f := FileRecord{
		ItemID:   100,
		FileID:   1,
		Source:   SourceTorrent,
		Name:     "test.mkv",
		Path:     "/Movies/test.mkv",
		Size:     1024,
		MimeType: "video/x-matroska",
	}
	if err := s.UpsertFile(f); err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}
}

func TestGetFileByFileID(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	f := FileRecord{
		ItemID:   42,
		FileID:   1,
		Source:   SourceTorrent,
		Name:     "movie.mkv",
		Path:     "/Movies/movie.mkv",
		Size:     4096,
		MimeType: "video/x-matroska",
	}
	s.UpsertFile(f)

	got, err := s.GetFileByFileID(SourceTorrent, 1)
	if err != nil {
		t.Fatalf("GetFileByFileID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected file, got nil")
	}
	if got.Name != "movie.mkv" {
		t.Errorf("name = %q, want %q", got.Name, "movie.mkv")
	}
	if got.Size != 4096 {
		t.Errorf("size = %d, want %d", got.Size, 4096)
	}
	if got.ItemID != 42 {
		t.Errorf("item_id = %d, want 42", got.ItemID)
	}
}

func TestGetFileByFileIDNotFound(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	got, err := s.GetFileByFileID(SourceTorrent, 999)
	if err != nil {
		t.Fatalf("GetFileByFileID failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing file_id, got %+v", got)
	}
}

func TestGetFileByPath(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	s.UpsertFile(FileRecord{
		ItemID:   1,
		FileID:   10,
		Source:   SourceTorrent,
		Name:     "file.txt",
		Path:     "/docs/file.txt",
		Size:     100,
	})

	got, err := s.GetFileByPath("/docs/file.txt")
	if err != nil {
		t.Fatalf("GetFileByPath failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected file, got nil")
	}
	if got.FileID != 10 {
		t.Errorf("file_id = %d, want 10", got.FileID)
	}
	if got.ItemID != 1 {
		t.Errorf("item_id = %d, want 1", got.ItemID)
	}
}

func TestListDir(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	files := []FileRecord{
		{ItemID: 1, FileID: 1, Source: SourceTorrent, Name: "a.mkv", Path: "/Movies/a.mkv", Size: 100},
		{ItemID: 1, FileID: 2, Source: SourceTorrent, Name: "b.mkv", Path: "/Movies/b.mkv", Size: 200},
		{ItemID: 2, FileID: 1, Source: SourceUsenet, Name: "c.mp3", Path: "/Music/c.mp3", Size: 300},
	}
	for _, f := range files {
		s.UpsertFile(f)
	}

	// List /Movies prefix.
	got, err := s.ListDir("/Movies")
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 files in /Movies, got %d", len(got))
	}

	// List / (all files).
	got, err = s.ListDir("")
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 files in /, got %d", len(got))
	}
}

func TestSetGetCDNURL(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	s.UpsertFile(FileRecord{ItemID: 1, FileID: 1, Source: SourceTorrent, Name: "f.mkv", Path: "/f.mkv", Size: 100})

	// Fetch the internal ID that was assigned.
	file, _ := s.GetFileByFileID(SourceTorrent, 1)
	internalID := file.ID

	// Set CDN URL with 1 hour expiry.
	expiry := time.Now().Add(1 * time.Hour)
	if err := s.SetCDNURL(internalID, "https://cdn.example.com/file", expiry); err != nil {
		t.Fatalf("SetCDNURL failed: %v", err)
	}

	// Get it back (should be fresh).
	url, err := s.GetCDNURL(internalID)
	if err != nil {
		t.Fatalf("GetCDNURL failed: %v", err)
	}
	if url != "https://cdn.example.com/file" {
		t.Errorf("got %q, want %q", url, "https://cdn.example.com/file")
	}
}

func TestGetExpiredCDNURL(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	s.UpsertFile(FileRecord{ItemID: 2, FileID: 1, Source: SourceTorrent, Name: "g.mkv", Path: "/g.mkv", Size: 100})
	file, _ := s.GetFileByFileID(SourceTorrent, 1)
	internalID := file.ID

	// Set CDN URL that already expired.
	expiry := time.Now().Add(-1 * time.Hour)
	if err := s.SetCDNURL(internalID, "https://cdn.example.com/old", expiry); err != nil {
		t.Fatalf("SetCDNURL failed: %v", err)
	}

	url, err := s.GetCDNURL(internalID)
	if err != nil {
		t.Fatalf("GetCDNURL failed: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty for expired URL, got %q", url)
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	s.UpsertFile(FileRecord{ItemID: 1, FileID: 1, Source: SourceTorrent, Name: "old.mkv", Path: "/same/path.mkv", Size: 100})
	s.UpsertFile(FileRecord{ItemID: 1, FileID: 1, Source: SourceTorrent, Name: "new.mkv", Path: "/same/path.mkv", Size: 200})

	got, _ := s.GetFileByFileID(SourceTorrent, 1)
	if got.Name != "new.mkv" {
		t.Errorf("name = %q, want %q", got.Name, "new.mkv")
	}
	if got.Size != 200 {
		t.Errorf("size = %d, want %d", got.Size, 200)
	}
}

func TestGetItemIDByFileID(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	s.UpsertFile(FileRecord{ItemID: 55, FileID: 7, Source: SourceTorrent, Name: "f.mkv", Path: "/f.mkv", Size: 100})

	tid, err := s.GetItemIDByFileID(SourceTorrent, 7)
	if err != nil {
		t.Fatalf("GetItemIDByFileID failed: %v", err)
	}
	if tid != 55 {
		t.Errorf("item_id = %d, want 55", tid)
	}
}

func TestDatabaseFileCreated(t *testing.T) {
	path := t.TempDir() + "/persist.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	s.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("database file was not created on disk")
	}
}

func TestUpsertDuplicatePathDifferentTorrent(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	// Different torrents, same path — upsert should work (path is unique).
	s.UpsertFile(FileRecord{ItemID: 1, FileID: 1, Source: SourceTorrent, Name: "f.mkv", Path: "/f.mkv", Size: 100})
	s.UpsertFile(FileRecord{ItemID: 2, FileID: 1, Source: SourceTorrent, Name: "f.mkv", Path: "/f.mkv", Size: 200})

	got, _ := s.GetFileByFileID(SourceTorrent, 1)
	if got.ItemID != 2 {
		t.Errorf("item_id = %d, want 2 (last upsert wins)", got.ItemID)
	}
	if got.Size != 200 {
		t.Errorf("size = %d, want 200", got.Size)
	}
}

func TestGetFileByFileIDSourceDisambiguation(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	// Two files with the same file_id but different source: should coexist.
	s.UpsertFile(FileRecord{ItemID: 100, FileID: 1, Source: SourceTorrent, Name: "torrent.mkv", Path: "/torrent/file.mkv", Size: 500})
	s.UpsertFile(FileRecord{ItemID: 200, FileID: 1, Source: SourceUsenet, Name: "usenet.mkv", Path: "/usenet/file.mkv", Size: 300})

	// Look up by source=torrent should return the torrent file.
	torFile, err := s.GetFileByFileID(SourceTorrent, 1)
	if err != nil {
		t.Fatalf("GetFileByFileID(SourceTorrent, 1) failed: %v", err)
	}
	if torFile == nil {
		t.Fatal("expected torrent file, got nil")
	}
	if torFile.ItemID != 100 {
		t.Errorf("torrent item_id = %d, want 100", torFile.ItemID)
	}

	// Look up by source=usenet should return the usenet file.
	usenetFile, err := s.GetFileByFileID(SourceUsenet, 1)
	if err != nil {
		t.Fatalf("GetFileByFileID(SourceUsenet, 1) failed: %v", err)
	}
	if usenetFile == nil {
		t.Fatal("expected usenet file, got nil")
	}
	if usenetFile.ItemID != 200 {
		t.Errorf("usenet item_id = %d, want 200", usenetFile.ItemID)
	}

	// Verify they are different records.
	if torFile.ID == usenetFile.ID {
		t.Error("torrent and usenet files should have different internal IDs")
	}
}

func TestGetItemIDByFileIDSourceDisambiguation(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	s.UpsertFile(FileRecord{ItemID: 100, FileID: 5, Source: SourceTorrent, Name: "t.mkv", Path: "/t.mkv", Size: 100})
	s.UpsertFile(FileRecord{ItemID: 200, FileID: 5, Source: SourceUsenet, Name: "u.mkv", Path: "/u.mkv", Size: 200})

	// Should return the correct item_id for each source.
	torID, err := s.GetItemIDByFileID(SourceTorrent, 5)
	if err != nil {
		t.Fatalf("GetItemIDByFileID(SourceTorrent, 5) failed: %v", err)
	}
	if torID != 100 {
		t.Errorf("torrent item_id = %d, want 100", torID)
	}

	usenetID, err := s.GetItemIDByFileID(SourceUsenet, 5)
	if err != nil {
		t.Fatalf("GetItemIDByFileID(SourceUsenet, 5) failed: %v", err)
	}
	if usenetID != 200 {
		t.Errorf("usenet item_id = %d, want 200", usenetID)
	}
}
