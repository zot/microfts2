package microfts2

// CRC: crc-Overlay.md | test-Overlay.md

import (
	"math"
	"path/filepath"
	"testing"
)

func TestAddTmpFileAndSearch(t *testing.T) {
	db, dir := testDB(t)

	// Add a disk file.
	p := writeTestFile(t, dir, "disk.txt", "the quick brown fox jumps")
	db.AddFile(p, "line")

	// Add a tmp:// file.
	fid, err := db.AddTmpFile("tmp://sess1/notes", "line", []byte("lazy dog jumps over\n"))
	if err != nil {
		t.Fatal(err)
	}
	if fid != math.MaxUint64 {
		t.Fatalf("expected fileid %d, got %d", uint64(math.MaxUint64), fid)
	}

	// Search for disk content.
	r, err := db.Search("quick brown")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) == 0 {
		t.Fatal("expected disk result")
	}

	// Search for tmp content.
	r, err = db.Search("lazy dog")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) == 0 {
		t.Fatal("expected tmp result")
	}
	if r.Results[0].Path != "tmp://sess1/notes" {
		t.Fatalf("expected tmp://sess1/notes, got %s", r.Results[0].Path)
	}
}

func TestAddTmpFileDuplicateGuard(t *testing.T) {
	db, _ := testDB(t)

	_, err := db.AddTmpFile("tmp://s/doc", "line", []byte("hello world\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AddTmpFile("tmp://s/doc", "line", []byte("other content\n"))
	if err != ErrAlreadyIndexed {
		t.Fatalf("expected ErrAlreadyIndexed, got %v", err)
	}
}

func TestUpdateTmpFile(t *testing.T) {
	db, _ := testDB(t)

	_, err := db.AddTmpFile("tmp://s/doc", "line", []byte("alpha bravo charlie\n"))
	if err != nil {
		t.Fatal(err)
	}

	// Old content searchable.
	r, _ := db.Search("alpha bravo")
	if len(r.Results) == 0 {
		t.Fatal("expected results for old content")
	}

	// Update content.
	err = db.UpdateTmpFile("tmp://s/doc", "line", []byte("delta echo foxtrot\n"))
	if err != nil {
		t.Fatal(err)
	}

	// Old content gone.
	r, _ = db.Search("alpha bravo")
	if len(r.Results) != 0 {
		t.Fatal("expected no results for old content after update")
	}

	// New content searchable.
	r, _ = db.Search("delta echo")
	if len(r.Results) == 0 {
		t.Fatal("expected results for new content")
	}
}

func TestUpdateTmpFileNotFound(t *testing.T) {
	db, _ := testDB(t)
	err := db.UpdateTmpFile("tmp://s/nope", "line", []byte("x\n"))
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestRemoveTmpFile(t *testing.T) {
	db, _ := testDB(t)

	_, err := db.AddTmpFile("tmp://s/doc", "line", []byte("unique xylophone content\n"))
	if err != nil {
		t.Fatal(err)
	}

	r, _ := db.Search("xylophone")
	if len(r.Results) == 0 {
		t.Fatal("expected results before removal")
	}

	err = db.RemoveTmpFile("tmp://s/doc")
	if err != nil {
		t.Fatal(err)
	}

	r, _ = db.Search("xylophone")
	if len(r.Results) != 0 {
		t.Fatal("expected no results after removal")
	}
}

func TestRemoveTmpFileNotFound(t *testing.T) {
	db, _ := testDB(t)
	err := db.RemoveTmpFile("tmp://s/nope")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestTmpFileIDCountsDown(t *testing.T) {
	db, _ := testDB(t)

	fid1, _ := db.AddTmpFile("tmp://s/a", "line", []byte("first doc\n"))
	fid2, _ := db.AddTmpFile("tmp://s/b", "line", []byte("second doc\n"))

	if fid1 != math.MaxUint64 {
		t.Fatalf("first fileid: want %d, got %d", uint64(math.MaxUint64), fid1)
	}
	if fid2 != math.MaxUint64-1 {
		t.Fatalf("second fileid: want %d, got %d", uint64(math.MaxUint64-1), fid2)
	}
}

func TestTmpFileIDs(t *testing.T) {
	db, _ := testDB(t)

	fid1, _ := db.AddTmpFile("tmp://s/a", "line", []byte("doc alpha\n"))
	fid2, _ := db.AddTmpFile("tmp://s/b", "line", []byte("doc bravo\n"))

	ids := db.TmpFileIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 fileids, got %d", len(ids))
	}
	if _, ok := ids[fid1]; !ok {
		t.Fatal("missing fid1")
	}
	if _, ok := ids[fid2]; !ok {
		t.Fatal("missing fid2")
	}
}

func TestWithExceptExcludesTmpResults(t *testing.T) {
	db, dir := testDB(t)

	p := writeTestFile(t, dir, "disk.txt", "zebra migration patterns")
	db.AddFile(p, "line")

	_, err := db.AddTmpFile("tmp://s/doc", "line", []byte("zebra migration routes\n"))
	if err != nil {
		t.Fatal(err)
	}

	// Without exclusion: both results.
	r, _ := db.Search("zebra migration")
	if len(r.Results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(r.Results))
	}

	// With exclusion: only disk.
	r, _ = db.Search("zebra migration", WithExcept(db.TmpFileIDs()))
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result after exclusion, got %d", len(r.Results))
	}
	if r.Results[0].Path != filepath.Join(dir, "disk.txt") {
		t.Fatalf("expected disk path, got %s", r.Results[0].Path)
	}
}

func TestGetChunksTmpPath(t *testing.T) {
	db, _ := testDB(t)

	content := "line one\nline two\nline three\n"
	_, err := db.AddTmpFile("tmp://s/doc", "line", []byte(content))
	if err != nil {
		t.Fatal(err)
	}

	results, err := db.GetChunks("tmp://s/doc", "1-1", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 chunk result")
	}
	if results[0].Range != "1-1" {
		t.Fatalf("expected range 1-1, got %s", results[0].Range)
	}
}

func TestOverlayDestroyedOnClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "testdb")
	db, err := Create(dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	db.AddStrategyFunc("line", LineChunkFunc)

	_, err = db.AddTmpFile("tmp://s/doc", "line", []byte("ephemeral content here\n"))
	if err != nil {
		t.Fatal(err)
	}

	r, _ := db.Search("ephemeral")
	if len(r.Results) == 0 {
		t.Fatal("expected results before close")
	}

	db.Close()

	// Reopen — overlay should be gone.
	db2, err := Open(dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	db2.AddStrategyFunc("line", LineChunkFunc)
	defer db2.Close()

	r, _ = db2.Search("ephemeral")
	if len(r.Results) != 0 {
		t.Fatal("expected no results after reopen — overlay should be gone")
	}
}

func TestWithNoTmp(t *testing.T) {
	db, dir := testDB(t)

	p := writeTestFile(t, dir, "disk.txt", "quantum entanglement patterns")
	db.AddFile(p, "line")

	_, err := db.AddTmpFile("tmp://s/doc", "line", []byte("quantum entanglement routes\n"))
	if err != nil {
		t.Fatal(err)
	}

	// Without WithNoTmp: both results.
	r, _ := db.Search("quantum entanglement")
	if len(r.Results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(r.Results))
	}

	// With WithNoTmp: only disk.
	r, _ = db.Search("quantum entanglement", WithNoTmp())
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result with WithNoTmp, got %d", len(r.Results))
	}
	if r.Results[0].Path != filepath.Join(dir, "disk.txt") {
		t.Fatalf("expected disk path, got %s", r.Results[0].Path)
	}
}

