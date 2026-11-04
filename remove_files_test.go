package microfts2

// CRC: crc-DB.md | Test: test-DB.md | R754, R755, R756, R757, R758

import (
	"errors"
	"testing"

	"go.etcd.io/bbolt"
)

// TestRemoveFilesRewritesEachPostingOnce: one set of posting edits for the
// whole batch, so a record several files touch is rewritten once; a chunk
// shared only by files in the batch is orphaned once; a file outside the batch
// keeps its chunks. R754, R756
func TestRemoveFilesRewritesEachPostingOnce(t *testing.T) {
	db, dir := testDB(t)
	a := writeTestFile(t, dir, "a.txt", commonLines("a", 6)+"shared batch line\n")
	b := writeTestFile(t, dir, "b.txt", commonLines("b", 6)+"shared batch line\n")
	c := writeTestFile(t, dir, "c.txt", commonLines("c", 6))
	keeper := writeTestFile(t, dir, "keeper.txt", "common keeper\n")
	for _, p := range []string{a, b, c, keeper} {
		if _, err := db.AddFile(p, "line"); err != nil {
			t.Fatal(err)
		}
	}
	writes := countPostingWrites(db)
	var gotIDs, gotOrphans []uint64
	err := db.RemoveFiles([]string{a, b, c}, func(tx *bbolt.Tx, fileIDs, orphans []uint64) error {
		gotIDs = append(gotIDs, fileIDs...)
		gotOrphans = append(gotOrphans, orphans...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEachPostingWrittenOnce(t, writes)
	if len(gotIDs) != 3 {
		t.Errorf("fileIDs: got %v, want three", gotIDs)
	}
	// 6 + 6 + 6 unique lines, plus the line a and b share: 19 chunks, each once.
	seen := map[uint64]bool{}
	for _, id := range gotOrphans {
		if seen[id] {
			t.Errorf("chunk %d orphaned twice", id)
		}
		seen[id] = true
	}
	if len(seen) != 19 {
		t.Errorf("orphans: got %d distinct, want 19 (%v)", len(seen), gotOrphans)
	}
	sr, _ := db.Search("common")
	if len(sr.Results) != 1 || sr.Results[0].Path != keeper {
		t.Errorf("common: got %v, want only %s", sr.Results, keeper)
	}
}

// TestRemoveFilesSkipsUnindexed: a path the index does not hold is skipped,
// and the rest of the batch is removed. R755
func TestRemoveFilesSkipsUnindexed(t *testing.T) {
	db, dir := testDB(t)
	a := writeTestFile(t, dir, "a.txt", "alpha only\n")
	b := writeTestFile(t, dir, "b.txt", "beta only\n")
	for _, p := range []string{a, b} {
		if _, err := db.AddFile(p, "line"); err != nil {
			t.Fatal(err)
		}
	}
	never := dir + "/never-indexed.txt"
	var gotIDs []uint64
	err := db.RemoveFiles([]string{a, never, b}, func(tx *bbolt.Tx, fileIDs, _ []uint64) error {
		gotIDs = fileIDs
		return nil
	})
	if err != nil {
		t.Fatalf("RemoveFiles: %v", err)
	}
	if len(gotIDs) != 2 {
		t.Errorf("fileIDs: got %v, want the two indexed files", gotIDs)
	}
	for _, q := range []string{"alpha", "beta"} {
		if sr, _ := db.Search(q); len(sr.Results) != 0 {
			t.Errorf("%s still found after the batch", q)
		}
	}
}

// TestRemoveFilesCallbackRollsBack: the callback's error aborts the whole
// batch; a nil callback is allowed; the path caches forget every removed path,
// so it can be added again. R757, R758
func TestRemoveFilesCallbackRollsBack(t *testing.T) {
	db, dir := testDB(t)
	a := writeTestFile(t, dir, "a.txt", "alpha kept\n")
	b := writeTestFile(t, dir, "b.txt", "beta kept\n")
	for _, p := range []string{a, b} {
		if _, err := db.AddFile(p, "line"); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := errors.New("abort")
	err := db.RemoveFiles([]string{a, b}, func(*bbolt.Tx, []uint64, []uint64) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the sentinel", err)
	}
	for _, q := range []string{"alpha", "beta"} {
		if sr, _ := db.Search(q); len(sr.Results) != 1 {
			t.Errorf("%s: got %d results after an aborted batch, want 1", q, len(sr.Results))
		}
	}

	// Load the path caches first, as ark always has them loaded; they are
	// filled lazily, and a test that never loads them cannot see them go stale.
	if _, err := db.FileIDPaths(); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveFiles([]string{a, b}, nil); err != nil {
		t.Fatalf("nil callback: %v", err)
	}
	if sr, _ := db.Search("alpha"); len(sr.Results) != 0 {
		t.Error("alpha still found after the batch")
	}
	paths, err := db.FileIDPaths()
	if err != nil {
		t.Fatal(err)
	}
	for id, p := range paths {
		if p == a || p == b {
			t.Errorf("path cache still lists %s (fileid %d) after the batch", p, id)
		}
	}
	if _, err := db.CheckFile(a); !errors.Is(err, ErrNotIndexed) {
		t.Errorf("CheckFile on a removed path: got %v, want ErrNotIndexed", err)
	}
	if _, err := db.AddFile(a, "line"); err != nil {
		t.Errorf("re-adding a removed path: %v", err)
	}
}
