package microfts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CRC: crc-DB.md

func testDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "testdb")
	db, err := Create(dbPath, Options{
		CharSet: "abcdefghijklmnopqrstuvwxyz0123456789",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Add a line-based chunking strategy (each line is a chunk)
	db.AddStrategy("line", "awk 'BEGIN{pos=0} {pos+=length($0)+1; print pos}' ")
	t.Cleanup(func() { db.Close() })
	return db, dir
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDBCreateAndOpen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "testdb")

	db, err := Create(dbPath, Options{
		CharSet: "abcdefghijklmnopqrstuvwxyz0123456789",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.settings.CharacterSet != "abcdefghijklmnopqrstuvwxyz0123456789" {
		t.Error("charset not preserved")
	}
	db.Close()

	db2, err := Open(dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if db2.settings.CharacterSet != "abcdefghijklmnopqrstuvwxyz0123456789" {
		t.Error("charset not preserved after reopen")
	}
	if db2.settings.NextFileID != 1 {
		t.Errorf("NextFileID = %d, want 1", db2.settings.NextFileID)
	}
}

func TestDBAddAndSearch(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\nthe quick brown fox\n")

	if err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	results, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("search returned no results")
	}
	if results[0].Path != fp {
		t.Errorf("Path = %q, want %q", results[0].Path, fp)
	}
}

func TestDBSearchBuildsIndex(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "searchable content here\n")

	if err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}
	if db.indexExists {
		t.Error("index should not exist before search")
	}

	results, err := db.Search("searchable")
	if err != nil {
		t.Fatal(err)
	}
	if !db.indexExists {
		t.Error("index should exist after search")
	}
	if len(results) == 0 {
		t.Fatal("search returned no results")
	}
}

func TestDBRemoveFile(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "removable content\n")

	if err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveFile(fp); err != nil {
		t.Fatal(err)
	}

	results, err := db.Search("removable")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after remove, got %d", len(results))
	}
}

func TestDBRebuildWithDifferentCutoff(t *testing.T) {
	db, dir := testDB(t)
	// Add files with diverse content to create trigram distribution
	for i := 0; i < 5; i++ {
		content := strings.Repeat("unique"+string(rune('a'+i))+" ", 20) + "\n"
		fp := writeTestFile(t, dir, "test"+string(rune('0'+i))+".txt", content)
		if err := db.AddFile(fp, "line"); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.BuildIndex(50); err != nil {
		t.Fatal(err)
	}
	active50 := len(db.settings.ActiveTrigrams)

	if err := db.BuildIndex(30); err != nil {
		t.Fatal(err)
	}
	active30 := len(db.settings.ActiveTrigrams)

	if active30 >= active50 {
		t.Errorf("30%% cutoff (%d active) should have fewer trigrams than 50%% (%d)", active30, active50)
	}
}

func TestDBReindex(t *testing.T) {
	db, dir := testDB(t)
	// Add a second strategy that chunks every 10 bytes
	db.AddStrategy("fixed10", "awk 'BEGIN{for(i=10;i<=600;i+=10)print i}'")

	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\nthe quick brown fox\n")

	if err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// Search before reindex
	results, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("search returned no results before reindex")
	}

	// Reindex with different strategy
	if err := db.Reindex(fp, "fixed10"); err != nil {
		t.Fatal(err)
	}

	// Search after reindex should still find content
	results, err = db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("search returned no results after reindex")
	}
}

func TestDBLongFilename(t *testing.T) {
	db, dir := testDB(t)
	// Create a deeply nested directory to get a path > 511 bytes
	longDir := dir
	for i := 0; i < 10; i++ {
		longDir = filepath.Join(longDir, strings.Repeat("d", 55))
	}
	if err := os.MkdirAll(longDir, 0755); err != nil {
		t.Fatal(err)
	}
	fp := writeTestFile(t, longDir, "test.txt", "deep nested content here\n")
	if len(fp) <= 511 {
		t.Fatalf("test path only %d bytes, need > 511", len(fp))
	}

	if err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// Use 100% cutoff so all trigrams are active (small corpus)
	if err := db.BuildIndex(100); err != nil {
		t.Fatal(err)
	}

	results, err := db.Search("nested")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("search returned no results for long filename")
	}
	if results[0].Path != fp {
		t.Errorf("Path = %q, want %q", results[0].Path, fp)
	}
}

func TestDBCustomDBNames(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "testdb")

	db, err := Create(dbPath, Options{
		CharSet:       "abcdefghijklmnopqrstuvwxyz",
		ContentDBName: "myc",
		IndexDBName:   "myi",
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := Open(dbPath, Options{
		ContentDBName: "myc",
		IndexDBName:   "myi",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if db2.settings.CharacterSet != "abcdefghijklmnopqrstuvwxyz" {
		t.Error("settings not preserved with custom DB names")
	}
}
