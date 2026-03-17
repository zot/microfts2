package microfts2

// CRC: crc-ChunkCache.md

import (
	"testing"
)

func TestChunkCacheChunkText(t *testing.T) {
	db, dir := testDB(t)
	defer db.Close()

	f := writeTestFile(t, dir, "test.txt", "hello\nworld\nfoo\nbar\nbaz\n")
	db.AddFile(f, "line")
	cache := db.NewChunkCache()

	// First access — lazy chunk.
	content, ok := cache.ChunkText(f, "1-1")
	if !ok {
		t.Fatal("ChunkText: not found")
	}
	if string(content) != "hello\n" {
		t.Errorf("ChunkText: got %q, want %q", content, "hello\n")
	}

	// Second access — cached.
	content2, ok := cache.ChunkText(f, "1-1")
	if !ok {
		t.Fatal("ChunkText cached: not found")
	}
	if string(content2) != "hello\n" {
		t.Errorf("ChunkText cached: got %q", content2)
	}

	// Different range — triggers lazy chunking past already-seen chunks.
	content3, ok := cache.ChunkText(f, "3-3")
	if !ok {
		t.Fatal("ChunkText range 3-3: not found")
	}
	if string(content3) != "foo\n" {
		t.Errorf("ChunkText range 3-3: got %q", content3)
	}
}

func TestChunkCacheGetChunks(t *testing.T) {
	db, dir := testDB(t)
	defer db.Close()

	f := writeTestFile(t, dir, "test.txt", "aaa\nbbb\nccc\nddd\neee\n")
	db.AddFile(f, "line")
	cache := db.NewChunkCache()

	results, err := cache.GetChunks(f, "3-3", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("GetChunks: got %d results, want 3", len(results))
	}
	if results[0].Range != "2-2" || results[1].Range != "3-3" || results[2].Range != "4-4" {
		t.Errorf("GetChunks ranges: got %s, %s, %s", results[0].Range, results[1].Range, results[2].Range)
	}

	// Subsequent ChunkText should be cached (full scan already done).
	content, ok := cache.ChunkText(f, "5-5")
	if !ok {
		t.Fatal("ChunkText after GetChunks: not found")
	}
	if string(content) != "eee\n" {
		t.Errorf("ChunkText after GetChunks: got %q", content)
	}
}

func TestChunkCacheNonexistentRange(t *testing.T) {
	db, dir := testDB(t)
	defer db.Close()

	f := writeTestFile(t, dir, "test.txt", "aaa\nbbb\n")
	db.AddFile(f, "line")
	cache := db.NewChunkCache()

	content, ok := cache.ChunkText(f, "999-999")
	if ok {
		t.Errorf("ChunkText nonexistent: should return false, got %q", content)
	}

	_, err := cache.GetChunks(f, "999-999", 0, 0)
	if err == nil {
		t.Error("GetChunks nonexistent: should return error")
	}
}

func TestChunkCacheMultipleFiles(t *testing.T) {
	db, dir := testDB(t)
	defer db.Close()

	f1 := writeTestFile(t, dir, "file1.txt", "alpha\n")
	db.AddFile(f1, "line")
	f2 := writeTestFile(t, dir, "file2.txt", "beta\n")
	db.AddFile(f2, "line")
	cache := db.NewChunkCache()

	c1, ok := cache.ChunkText(f1, "1-1")
	if !ok || string(c1) != "alpha\n" {
		t.Errorf("file1: got %q, ok=%v", c1, ok)
	}

	c2, ok := cache.ChunkText(f2, "1-1")
	if !ok || string(c2) != "beta\n" {
		t.Errorf("file2: got %q, ok=%v", c2, ok)
	}
}

func TestChunkCacheLazyThenFull(t *testing.T) {
	db, dir := testDB(t)
	defer db.Close()

	f := writeTestFile(t, dir, "test.txt", "aaa\nbbb\nccc\n")
	db.AddFile(f, "line")
	cache := db.NewChunkCache()

	// Lazy access — stops at chunk 1.
	_, ok := cache.ChunkText(f, "1-1")
	if !ok {
		t.Fatal("lazy: not found")
	}

	// Full access — completes all chunks.
	results, err := cache.GetChunks(f, "2-2", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("GetChunks: got %d, want 2", len(results))
	}
	if results[0].Range != "2-2" || results[1].Range != "3-3" {
		t.Errorf("GetChunks ranges: got %s, %s", results[0].Range, results[1].Range)
	}
}
