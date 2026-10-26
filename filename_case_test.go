package microfts2

// CRC: crc-DB.md | Test: test-FilenameCase.md

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

// caseDB creates a database with CaseInsensitiveFilenames set as given, and
// writes both spellings of one file to disk with identical content — on a
// case-sensitive filesystem that stands in for one file under two spellings.
func caseDB(t *testing.T, fold bool) (db *DB, lower, upper string) {
	t.Helper()
	dir := t.TempDir()
	db, err := Create(filepath.Join(dir, "testdb"), Options{CaseInsensitiveFilenames: fold})
	if err != nil {
		t.Fatal(err)
	}
	db.AddStrategyFunc("line", LineChunkFunc)
	t.Cleanup(func() { db.Close() })
	lower = filepath.Join(dir, "Casey.md")
	upper = filepath.Join(dir, "CASEY.md")
	for _, p := range []string{lower, upper} {
		if err := os.WriteFile(p, []byte("hello casefold\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return db, lower, upper
}

// R736, R737, R738, R743
func TestFilenameCaseOptionPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testdb")
	db, err := Create(path, Options{CaseInsensitiveFilenames: true})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := db.FilenameKey("/d/Casey.md"); got != "/D/CASEY.MD" {
		t.Errorf("reopened FilenameKey = %q, want /D/CASEY.MD", got)
	}
	if got := db.FilenameKey("tmp://Casey"); got != "tmp://Casey" {
		t.Errorf("tmp:// FilenameKey = %q, want it unfolded", got)
	}

	off, _, _ := caseDB(t, false)
	if got := off.FilenameKey("/d/Casey.md"); got != "/d/Casey.md" {
		t.Errorf("option off: FilenameKey = %q, want the path unchanged", got)
	}
}

// R739, R741
func TestFilenameCaseOtherSpellingReachesFile(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	chunks, err := db.GetChunks(upper, "1-1", 0, 0)
	if err != nil {
		t.Fatalf("GetChunks via %s: %v", filepath.Base(upper), err)
	}
	if len(chunks) != 1 {
		t.Errorf("GetChunks via other spelling: got %d chunks, want 1", len(chunks))
	}
	paths, err := db.FileIDPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("FileIDPaths: %d files, want 1", len(paths))
	}
	for _, p := range paths {
		if p != lower {
			t.Errorf("a read respelled the file: stored spelling = %q, want %q", p, lower)
		}
	}
}

// R742
func TestFilenameCaseWarmCacheFolds(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileIDPaths(); err != nil { // loads pathToID
		t.Fatal(err)
	}
	if _, err := db.GetChunks(upper, "1-1", 0, 0); err != nil {
		t.Errorf("GetChunks via other spelling with a warm cache: %v", err)
	}
}

// R739
func TestFilenameCaseRemoveByOtherSpelling(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveFile(upper); err != nil {
		t.Fatalf("RemoveFile via other spelling: %v", err)
	}
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Errorf("re-add after removal: %v", err)
	}
}

// R737: off is the old behaviour
func TestFilenameCaseOffKeepsSpellingsDistinct(t *testing.T) {
	db, lower, upper := caseDB(t, false)
	id1, err := db.AddFile(lower, "line")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := db.AddFile(upper, "line")
	if err != nil {
		t.Fatalf("option off: AddFile of other spelling: %v", err)
	}
	if id1 == id2 {
		t.Errorf("option off: both spellings got fileid %d", id1)
	}
}

// R742: a file added after the cache is warm is cached under its filename key
func TestFilenameCaseWarmCacheAdd(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.FileIDPaths(); err != nil { // warm, empty
		t.Fatal(err)
	}
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetChunks(upper, "1-1", 0, 0); err != nil {
		t.Errorf("GetChunks via other spelling after a warm-cache add: %v", err)
	}
}

// R742: removal by another spelling drops the file from a warm cache
func TestFilenameCaseWarmCacheRemove(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileIDPaths(); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveFile(upper); err != nil {
		t.Fatal(err)
	}
	paths, err := db.FileIDPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Errorf("after removal by other spelling, FileIDPaths still lists %v", paths)
	}
}

// R742: reindex by another spelling replaces the cached fileid
func TestFilenameCaseWarmCacheReindex(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileIDPaths(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Reindex(upper, "line"); err != nil {
		t.Fatal(err)
	}
	paths, err := db.FileIDPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Errorf("after reindex by other spelling, FileIDPaths lists %d files, want 1: %v", len(paths), paths)
	}
	if _, err := db.GetChunks(lower, "1-1", 0, 0); err != nil {
		t.Errorf("GetChunks after reindex by other spelling: %v", err)
	}
}

// storedFinalName reads the name in path's final N record directly — nothing
// in the package reads it back, so this is the only way to see it.
func storedFinalName(t *testing.T, db *DB, path string) string {
	t.Helper()
	var name string
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket([]byte(db.dbName)).Get(db.finalKey(path))
		if val == nil {
			return errors.New("no final N record")
		}
		name, _ = parseNFinalValue(val)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// R744, R746
func TestFilenameCaseAddFileRespells(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	id, err := db.AddFile(lower, "line")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddFile(upper, "line"); !errors.Is(err, ErrAlreadyIndexed) {
		t.Errorf("AddFile of other spelling: err = %v, want ErrAlreadyIndexed", err)
	}
	paths, err := db.FileIDPaths() // cold: read from the F records
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[id] != upper {
		t.Errorf("F record after respell: %v, want fileid %d stored as %q", paths, id, upper)
	}
	if got := storedFinalName(t, db, lower); got != upper {
		t.Errorf("final N value after respell names %q, want %q", got, upper)
	}
	if _, err := db.GetChunks(lower, "1-1", 0, 0); err != nil {
		t.Errorf("GetChunks via old spelling after respell: %v", err)
	}
}

// R744
func TestFilenameCaseRespellWarmCache(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileIDPaths(); err != nil { // warm
		t.Fatal(err)
	}
	if _, err := db.AddFile(upper, "line"); !errors.Is(err, ErrAlreadyIndexed) {
		t.Fatalf("AddFile of other spelling: err = %v, want ErrAlreadyIndexed", err)
	}
	paths, err := db.FileIDPaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if p != upper {
			t.Errorf("warm cache after respell reports %q, want %q", p, upper)
		}
	}
}

// R745
func TestFilenameCaseReindexRespells(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	if _, err := db.AddFile(lower, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Reindex(upper, "line"); err != nil {
		t.Fatal(err)
	}
	paths, err := db.FileIDPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("after reindex by other spelling: %d files, want 1", len(paths))
	}
	for _, p := range paths {
		if p != upper {
			t.Errorf("reindex stored %q, want %q", p, upper)
		}
	}
}

// R744: a respell inside an active search cache drops the cached F record
func TestFilenameCaseRespellSearchCache(t *testing.T) {
	db, lower, upper := caseDB(t, true)
	done := db.NewSearchCache()
	defer done()
	id, err := db.AddFile(lower, "line")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileInfoByID(id); err != nil { // caches the F record
		t.Fatal(err)
	}
	if _, err := db.AddFile(upper, "line"); !errors.Is(err, ErrAlreadyIndexed) {
		t.Fatalf("AddFile of other spelling: err = %v, want ErrAlreadyIndexed", err)
	}
	frec, err := db.FileInfoByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(frec.Names) == 0 || frec.Names[0] != upper {
		t.Errorf("cached F record after respell names %v, want %q", frec.Names, upper)
	}
}
