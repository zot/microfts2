package microfts2

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bmatsuo/lmdb-go/lmdb"
)

// CRC: crc-DB.md

func testDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "testdb")
	db, err := Create(dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Add a line-based chunking strategy using built-in func
	db.AddStrategyFunc("line", LineChunkFunc)
	t.Cleanup(func() { db.Close() })
	return db, dir
}

// fixedChunkFunc returns a ChunkFunc that splits content into fixed-size byte chunks.
func fixedChunkFunc(size int) ChunkFunc {
	return func(_ string, content []byte, yield func(Chunk) bool) error {
		chunkNum := 1
		for start := 0; start < len(content); start += size {
			end := start + size
			if end > len(content) {
				end = len(content)
			}
			r := fmt.Sprintf("b%d", chunkNum)
			if !yield(Chunk{Range: []byte(r), Content: content[start:end]}) {
				return nil
			}
			chunkNum++
		}
		return nil
	}
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

	db, err := Create(dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := Open(dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if !db2.settings.CaseInsensitive {
		// Settings loaded successfully — basic lifecycle check
	}
}

func TestDBAddAndSearch(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\nthe quick brown fox\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	sr, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search returned no results")
	}
	if sr.Results[0].Path != fp {
		t.Errorf("Path = %q, want %q", sr.Results[0].Path, fp)
	}
}

func TestDBSearchFindsContent(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "searchable content here\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	sr, err := db.Search("searchable")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search returned no results")
	}
}

func TestDBRemoveFile(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "removable content\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveFile(fp); err != nil {
		t.Fatal(err)
	}

	sr, err := db.Search("removable")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) != 0 {
		t.Errorf("expected 0 results after remove, got %d", len(sr.Results))
	}
}



func TestDBReindex(t *testing.T) {
	db, dir := testDB(t)
	// Add a second strategy that chunks every 10 bytes
	db.AddStrategyFunc("fixed10", fixedChunkFunc(10))

	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\nthe quick brown fox\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// Search before reindex
	sr, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search returned no results before reindex")
	}

	// Reindex with different strategy
	if _, err := db.Reindex(fp, "fixed10"); err != nil {
		t.Fatal(err)
	}

	// Search after reindex should still find content
	sr, err = db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
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

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	sr, err := db.Search("nested")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search returned no results for long filename")
	}
	if sr.Results[0].Path != fp {
		t.Errorf("Path = %q, want %q", sr.Results[0].Path, fp)
	}
}

func TestDBCheckFileFresh(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	status, err := db.CheckFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "fresh" {
		t.Errorf("Status = %q, want fresh", status.Status)
	}
}

func TestDBCheckFileStale(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// Modify the file content and ensure mod time changes
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(fp, []byte("changed content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	status, err := db.CheckFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "stale" {
		t.Errorf("Status = %q, want stale", status.Status)
	}
}

func TestDBCheckFileMissing(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	os.Remove(fp)

	status, err := db.CheckFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "missing" {
		t.Errorf("Status = %q, want missing", status.Status)
	}
}

func TestDBStaleFiles(t *testing.T) {
	db, dir := testDB(t)
	fresh := writeTestFile(t, dir, "fresh.txt", "stays the same\n")
	stale := writeTestFile(t, dir, "stale.txt", "will change\n")
	missing := writeTestFile(t, dir, "missing.txt", "will vanish\n")

	for _, fp := range []string{fresh, stale, missing} {
		if _, err := db.AddFile(fp, "line"); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(10 * time.Millisecond)
	os.WriteFile(stale, []byte("different content\n"), 0644)
	os.Remove(missing)

	statuses, err := db.StaleFiles()
	if err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{}
	for _, s := range statuses {
		counts[s.Status]++
	}
	if counts["fresh"] != 1 {
		t.Errorf("fresh count = %d, want 1", counts["fresh"])
	}
	if counts["stale"] != 1 {
		t.Errorf("stale count = %d, want 1", counts["stale"])
	}
	if counts["missing"] != 1 {
		t.Errorf("missing count = %d, want 1", counts["missing"])
	}
}

func TestDBRefreshStale(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "original content hello\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// Verify searchable
	sr, err := db.Search("original")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search for 'original' returned no results")
	}

	// Modify file — keep "content" so shared trigrams remain searchable
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(fp, []byte("changed content hello\n"), 0644)

	// Refresh
	refreshed, err := db.RefreshStale("")
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed) == 0 {
		t.Fatal("RefreshStale returned empty list")
	}
	if refreshed[0].Status != "refreshed" {
		t.Errorf("Status = %q, want refreshed", refreshed[0].Status)
	}

	// Old-unique content gone from index
	sr, err = db.Search("original")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) != 0 {
		t.Errorf("search for 'original' after refresh returned %d results, want 0", len(sr.Results))
	}

	// Shared content still searchable via incremental index
	sr, err = db.Search("content hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search for 'content hello' after refresh returned no results")
	}
}

func TestDBCustomDBNames(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "testdb")

	db, err := Create(dbPath, Options{
		CaseInsensitive: true,
		DBName:          "mydb",
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := Open(dbPath, Options{
		DBName: "mydb",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if !db2.settings.CaseInsensitive {
		t.Error("settings not preserved with custom DB names")
	}
}

func TestDBIndexStatus(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world testing\n")
	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// No index yet — search should auto-build
	sr, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if !sr.Status.Built {
		t.Error("IndexStatus.Built should be true after search")
	}
}

func TestDBIncrementalIndex(t *testing.T) {
	db, dir := testDB(t)
	fp1 := writeTestFile(t, dir, "a.txt", "alpha bravo charlie\n")
	if _, err := db.AddFile(fp1, "line"); err != nil {
		t.Fatal(err)
	}

	// Add another file — should get incremental index entries
	fp2 := writeTestFile(t, dir, "b.txt", "alpha delta echo\n")
	if _, err := db.AddFile(fp2, "line"); err != nil {
		t.Fatal(err)
	}

	// Search for shared content should find both files
	sr, err := db.Search("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) != 2 {
		t.Errorf("search 'alpha' returned %d results, want 2", len(sr.Results))
	}

	// Remove first file — index entries should be cleaned up
	if err := db.RemoveFile(fp1); err != nil {
		t.Fatal(err)
	}
	sr, err = db.Search("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) != 1 {
		t.Errorf("after remove, search 'alpha' returned %d results, want 1", len(sr.Results))
	}
}

func TestDBSearchReturnsIndexStatus(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "searchable content here\n")
	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	sr, err := db.Search("searchable")
	if err != nil {
		t.Fatal(err)
	}
	if !sr.Status.Built {
		t.Error("SearchResults.Status.Built should be true")
	}
}


func TestDBEnv(t *testing.T) {
	db, _ := testDB(t)
	env := db.Env()
	if env == nil {
		t.Fatal("Env() returned nil")
	}
}

func TestDBVersion(t *testing.T) {
	db, _ := testDB(t)
	v, err := db.Version()
	if err != nil {
		t.Fatal(err)
	}
	if v != "2" {
		t.Fatalf("Version() = %q, want %q", v, "2")
	}
}

func TestDBAddFileReturnsFileid(t *testing.T) {
	db, dir := testDB(t)
	fp1 := writeTestFile(t, dir, "a.txt", "hello world\n")
	fp2 := writeTestFile(t, dir, "b.txt", "foo bar\n")

	id1, err := db.AddFile(fp1, "line")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := db.AddFile(fp2, "line")
	if err != nil {
		t.Fatal(err)
	}
	if id1 == 0 {
		t.Error("first fileid should be non-zero")
	}
	if id2 <= id1 {
		t.Errorf("second fileid (%d) should be greater than first (%d)", id2, id1)
	}
}

func TestDBReindexReturnsFileid(t *testing.T) {
	db, dir := testDB(t)
	db.AddStrategyFunc("fixed10", fixedChunkFunc(10))
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\n")

	origID, err := db.AddFile(fp, "line")
	if err != nil {
		t.Fatal(err)
	}

	newID, err := db.Reindex(fp, "fixed10")
	if err != nil {
		t.Fatal(err)
	}
	if newID == 0 {
		t.Error("reindex fileid should be non-zero")
	}
	if newID == origID {
		t.Logf("reindex allocated new fileid %d (orig was %d)", newID, origID)
	}
}

func TestDBFileInfoByID(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar\n")

	fileid, err := db.AddFile(fp, "line")
	if err != nil {
		t.Fatal(err)
	}

	frec, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatal(err)
	}
	if len(frec.Names) == 0 || frec.Names[0] != fp {
		t.Errorf("Names = %v, want [%q]", frec.Names, fp)
	}
	if frec.Strategy != "line" {
		t.Errorf("Strategy = %q, want line", frec.Strategy)
	}
	if len(frec.Chunks) == 0 {
		t.Error("Chunks should not be empty")
	}
}

func TestDBScoreFile(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\nthe quick brown fox\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	chunks, err := db.ScoreFile("hello", fp, ScoreCoverage)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("ScoreFile returned no chunks")
	}

	// The chunk containing "hello" should have a positive score
	foundPositive := false
	for _, c := range chunks {
		if c.Score > 0 {
			foundPositive = true
		}
		if c.Score < 0 || c.Score > 1 {
			t.Errorf("Score %f out of range [0,1]", c.Score)
		}
	}
	if !foundPositive {
		t.Error("expected at least one chunk with positive score")
	}
}

func TestDBSearchRegex(t *testing.T) {
	db, dir := testDB(t)
	fp1 := writeTestFile(t, dir, "a.txt", "hello world\nfoo bar baz\n")
	fp2 := writeTestFile(t, dir, "b.txt", "goodbye moon\nalpha beta\n")

	if _, err := db.AddFile(fp1, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddFile(fp2, "line"); err != nil {
		t.Fatal(err)
	}

	// Regex matching "hel" trigram (present in "hello")
	sr, err := db.SearchRegex("hel+o")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("SearchRegex returned no results")
	}
	// Should find the file containing "hello"
	found := false
	for _, r := range sr.Results {
		if r.Path == fp1 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q in results", fp1)
	}
}

func TestDBSearchRegexMatchAll(t *testing.T) {
	db, dir := testDB(t)
	fp1 := writeTestFile(t, dir, "a.txt", "hello world\n")
	fp2 := writeTestFile(t, dir, "b.txt", "foo bar baz\n")

	if _, err := db.AddFile(fp1, "line"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddFile(fp2, "line"); err != nil {
		t.Fatal(err)
	}

	// Match-everything pattern should return all chunks
	sr, err := db.SearchRegex(".*")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("SearchRegex(\".*\") returned no results, expected all chunks")
	}
}

func TestDBSearchReturnsScore(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	sr, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search returned no results")
	}
	// Matching chunk should have a positive score
	found := false
	for _, r := range sr.Results {
		if r.Score > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one result with positive Score")
	}
}

func TestDBAddFileRejectsInvalidUTF8(t *testing.T) {
	db, dir := testDB(t)
	// Write a file with invalid UTF-8 bytes
	fp := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(fp, []byte{0xFF, 0xFE, 0x80, 'h', 'e', 'l', 'l', 'o', '\n'}, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := db.AddFile(fp, "line")
	if err == nil {
		t.Fatal("AddFile should reject non-UTF-8 content")
	}
	if !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Errorf("error should mention invalid UTF-8, got: %v", err)
	}
}

func TestDBAddFileCJKContent(t *testing.T) {
	db, dir := testDB(t)
	// Valid UTF-8 CJK content should be accepted and searchable
	fp := writeTestFile(t, dir, "cjk.txt", "你好世界\n")
	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}
	// Search for cross-boundary bytes of 你好
	sr, err := db.Search("你好")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("CJK search returned no results")
	}
}

func TestDBAddStrategyFunc(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "testdb")
	db, err := Create(dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Register the built-in line chunker under a different name
	lineFunc := LineChunkFunc
	if err := db.AddStrategyFunc("linefunc", lineFunc); err != nil {
		t.Fatal(err)
	}

	fp := writeTestFile(t, dir, "hello.txt", "hello world\ngoodbye world\n")
	fileid, err := db.AddFile(fp, "linefunc")
	if err != nil {
		t.Fatal(err)
	}
	if fileid == 0 {
		t.Error("expected non-zero fileid")
	}

	sr, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("search with func strategy returned no results")
	}
}

func TestDBAddFileWithContent(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "content.txt", "the quick brown fox\n")

	fileid, content, err := db.AddFileWithContent(fp, "line")
	if err != nil {
		t.Fatal(err)
	}
	if fileid == 0 {
		t.Error("expected non-zero fileid")
	}
	if string(content) != "the quick brown fox\n" {
		t.Errorf("content mismatch: got %q", content)
	}
}

func TestDBReindexWithContent(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "reindex.txt", "original content\n")
	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// Update file and reindex
	os.WriteFile(fp, []byte("updated content\n"), 0644)

	fileid, content, err := db.ReindexWithContent(fp, "line")
	if err != nil {
		t.Fatal(err)
	}
	if fileid == 0 {
		t.Error("expected non-zero fileid")
	}
	if string(content) != "updated content\n" {
		t.Errorf("content mismatch: got %q", content)
	}
}

func TestParseQueryTerms(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello world", []string{"hello", "world"}},
		{`"hello world" foo`, []string{"hello world", "foo"}},
		{`foo "bar baz" qux`, []string{"foo", "bar baz", "qux"}},
		{`"quoted only"`, []string{"quoted only"}},
		{"  spaced  ", []string{"spaced"}},
		{"", nil},
		{`unclosed "quote`, []string{"unclosed", `"quote`}},
	}
	for _, tt := range tests {
		got := parseQueryTerms(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseQueryTerms(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseQueryTerms(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestSearchWithVerify(t *testing.T) {
	db, dir := testDB(t)

	// Build a corpus with enough variety so trigrams are distinctive.
	// "daneel" has trigrams dan, ane, nee, eel.
	// A chunk with "danger" + "caneen" + "heels" shares those trigrams
	// but doesn't contain the word "daneel" — a false positive without verify.
	fp1 := writeTestFile(t, dir, "real.txt",
		"daneel olivaw is a robot\nhe serves humanity\npatient and tireless\n")
	fp2 := writeTestFile(t, dir, "false.txt",
		"The danger of caneen and heels\nUnrelated content here\nMore filler text\n")
	// Add more files for corpus diversity
	fp3 := writeTestFile(t, dir, "filler1.txt",
		"Alpha beta gamma delta\nEpsilon zeta eta theta\nIota kappa lambda mu\n")
	fp4 := writeTestFile(t, dir, "filler2.txt",
		"Quick brown fox jumps\nLazy dog sleeps all day\nThe sun shines bright\n")

	db.AddFile(fp1, "line")
	db.AddFile(fp2, "line")
	db.AddFile(fp3, "line")
	db.AddFile(fp4, "line")


	// Without verify: should find candidates
	sr, err := db.Search("daneel")
	if err != nil {
		t.Fatal(err)
	}
	noVerifyCount := len(sr.Results)

	// With verify: only the real match survives
	sr, err = db.Search("daneel", WithVerify())
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, r := range sr.Results {
		if r.Path == fp2 {
			t.Error("WithVerify should have filtered out false positive")
		}
		if r.Path == fp1 {
			found = true
		}
	}
	if !found {
		t.Error("WithVerify should have kept the real match")
	}
	if noVerifyCount < len(sr.Results) {
		t.Error("verify should not add results")
	}
}

func TestSearchWithVerifyQuotedTerms(t *testing.T) {
	db, dir := testDB(t)

	fp1 := writeTestFile(t, dir, "match.txt", "the quick brown fox\n")
	fp2 := writeTestFile(t, dir, "partial.txt", "brown dogs are quick\n")

	db.AddFile(fp1, "line")
	db.AddFile(fp2, "line")


	// "quick brown" as a quoted phrase — must appear as substring
	sr, err := db.Search(`"quick brown"`, WithVerify())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range sr.Results {
		if r.Path == fp2 {
			t.Error("quoted phrase should not match when words are separated")
		}
	}
}

func TestSearchRegexVerifies(t *testing.T) {
	db, dir := testDB(t)

	fp1 := writeTestFile(t, dir, "match.txt", "the cat sat on the mat\n")
	fp2 := writeTestFile(t, dir, "nomatch.txt", "category matters catalog\n")

	db.AddFile(fp1, "line")
	db.AddFile(fp2, "line")


	// regex `\bcat\b` should only match whole word "cat"
	sr, err := db.SearchRegex(`\bcat\b`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range sr.Results {
		if r.Path == fp2 {
			t.Error("regex verify should have filtered out 'category/catalog'")
		}
	}
	found := false
	for _, r := range sr.Results {
		if r.Path == fp1 {
			found = true
		}
	}
	if !found {
		t.Error("regex verify should have kept the real match")
	}
}

func TestDBSearchWithTrigramFilter(t *testing.T) {
	db, dir := testDB(t)
	fp1 := writeTestFile(t, dir, "a.txt", "hello world\n")
	fp2 := writeTestFile(t, dir, "b.txt", "hello there\n")
	fp3 := writeTestFile(t, dir, "c.txt", "goodbye moon\n")

	for _, fp := range []string{fp1, fp2, fp3} {
		if _, err := db.AddFile(fp, "line"); err != nil {
			t.Fatal(err)
		}
	}

	// FilterAll should return all matches for "hello"
	sr, err := db.Search("hello", WithTrigramFilter(FilterAll))
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) < 2 {
		t.Errorf("FilterAll: got %d results, want >= 2", len(sr.Results))
	}

	// FilterBestN(1) should still find results (using only the rarest trigram)
	sr, err = db.Search("hello", WithTrigramFilter(FilterBestN(1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Error("FilterBestN(1): expected results")
	}

	// FilterByRatio(0.0) should filter everything (no trigram appears in 0% of chunks)
	sr, err = db.Search("hello", WithTrigramFilter(FilterByRatio(0.0)))
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) != 0 {
		t.Errorf("FilterByRatio(0.0): got %d results, want 0", len(sr.Results))
	}
}

func TestDBScoreFileWithTrigramFilter(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// ScoreFile with FilterAll
	chunks, err := db.ScoreFile("hello", fp, ScoreCoverage, WithTrigramFilter(FilterAll))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("ScoreFile with FilterAll returned no chunks")
	}
	foundPositive := false
	for _, c := range chunks {
		if c.Score > 0 {
			foundPositive = true
		}
	}
	if !foundPositive {
		t.Error("expected at least one chunk with positive score")
	}
}

// --- Append Detection Tests ---

func TestFileLengthStoredOnAdd(t *testing.T) {
	db, dir := testDB(t)
	content := "hello world\nfoo bar\nbaz qux\n"
	fpath := writeTestFile(t, dir, "test.txt", content)

	fileid, err := db.AddFile(fpath, "line")
	if err != nil {
		t.Fatal(err)
	}

	frec, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatal(err)
	}

	if frec.FileLength != int64(len(content)) {
		t.Errorf("FileLength = %d, want %d", frec.FileLength, len(content))
	}
}

func TestAppendChunks(t *testing.T) {
	db, dir := testDB(t)
	fpath := writeTestFile(t, dir, "test.txt", "alpha\nbeta\ngamma\n")

	fileid, err := db.AddFile(fpath, "line")
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial state: 3 chunks
	frec, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatal(err)
	}
	if len(frec.Chunks) != 3 {
		t.Fatalf("initial chunks = %d, want 3", len(frec.Chunks))
	}

	// Append 2 more lines
	appendContent := []byte("delta\nepsilon\n")
	err = db.AppendChunks(fileid, appendContent, "line",
		WithBaseLine(3),
		WithContentHash("fakehash"),
		WithModTime(12345),
		WithFileLength(99),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Verify: 5 chunks total
	frec, err = db.FileInfoByID(fileid)
	if err != nil {
		t.Fatal(err)
	}
	if len(frec.Chunks) != 5 {
		t.Errorf("total chunks = %d, want 5", len(frec.Chunks))
	}

	// Old chunks intact
	if frec.Chunks[0].Location != "1-1" || frec.Chunks[1].Location != "2-2" || frec.Chunks[2].Location != "3-3" {
		t.Errorf("old ranges changed: %v %v %v", frec.Chunks[0].Location, frec.Chunks[1].Location, frec.Chunks[2].Location)
	}

	// New chunks searchable (no WithVerify — disk file doesn't have appended content)
	sr, err := db.Search("delta")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Error("search for 'delta' returned no results after append")
	}

	sr, err = db.Search("epsilon")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Error("search for 'epsilon' returned no results after append")
	}

	// Old content still searchable
	sr, err = db.Search("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Error("search for 'alpha' returned no results after append")
	}
}

func TestAppendChunksWithBaseLine(t *testing.T) {
	db, dir := testDB(t)
	fpath := writeTestFile(t, dir, "test.txt", "line1\nline2\nline3\n")

	fileid, err := db.AddFile(fpath, "line")
	if err != nil {
		t.Fatal(err)
	}

	// Append with base line offset of 3
	err = db.AppendChunks(fileid, []byte("line4\nline5\n"), "line", WithBaseLine(3))
	if err != nil {
		t.Fatal(err)
	}

	frec, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatal(err)
	}

	// New ranges should be "4-4" and "5-5", not "1-1" and "2-2"
	if len(frec.Chunks) != 5 {
		t.Fatalf("total chunks = %d, want 5", len(frec.Chunks))
	}
	if frec.Chunks[3].Location != "4-4" {
		t.Errorf("chunk 3 range = %q, want %q", frec.Chunks[3].Location, "4-4")
	}
	if frec.Chunks[4].Location != "5-5" {
		t.Errorf("chunk 4 range = %q, want %q", frec.Chunks[4].Location, "5-5")
	}
}

func TestAppendChunksUpdatesMetadata(t *testing.T) {
	db, dir := testDB(t)
	fpath := writeTestFile(t, dir, "test.txt", "hello\n")

	fileid, err := db.AddFile(fpath, "line")
	if err != nil {
		t.Fatal(err)
	}

	err = db.AppendChunks(fileid, []byte("world\n"), "line",
		WithContentHash("abc123"),
		WithModTime(999999),
		WithFileLength(12),
		WithBaseLine(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	frec, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatal(err)
	}

	wantHash := "abc123"
	gotHash := hex.EncodeToString(frec.ContentHash[:])
	if !strings.HasPrefix(gotHash, wantHash) {
		t.Errorf("ContentHash = %q, want prefix %q", gotHash, wantHash)
	}
	if frec.ModTime != 999999 {
		t.Errorf("ModTime = %d, want %d", frec.ModTime, 999999)
	}
	if frec.FileLength != 12 {
		t.Errorf("FileLength = %d, want %d", frec.FileLength, 12)
	}
	if len(frec.Chunks) != 2 {
		t.Errorf("Chunks len = %d, want 2", len(frec.Chunks))
	}
	if len(frec.Tokens) == 0 {
		t.Error("Tokens should not be empty after append")
	}
}

func TestAppendChunksInvalidFileid(t *testing.T) {
	db, _ := testDB(t)

	err := db.AppendChunks(99999, []byte("data\n"), "line")
	if err == nil {
		t.Error("expected error for nonexistent fileid")
	}
}

// test-DB.md: per-token trigram search order independence | R180, R181, R182
func TestSearchOrderIndependence(t *testing.T) {
	db, dir := testDB(t)

	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("daneel olivaw is here\n"), 0644)
	db.AddFile(f, "line")

	r1, err := db.Search("daneel olivaw")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := db.Search("olivaw daneel")
	if err != nil {
		t.Fatal(err)
	}

	if len(r1.Results) != len(r2.Results) {
		t.Fatalf("order-dependent results: %q returned %d, %q returned %d",
			"daneel olivaw", len(r1.Results), "olivaw daneel", len(r2.Results))
	}
	if len(r1.Results) == 0 {
		t.Fatal("expected at least one result")
	}
}

// test-DB.md: quoted phrase trigrams preserve adjacency | R179, R180
func TestSearchQuotedPhrase(t *testing.T) {
	db, dir := testDB(t)

	f1 := filepath.Join(dir, "adjacent.txt")
	os.WriteFile(f1, []byte("hello world greeting\n"), 0644)
	db.AddFile(f1, "line")

	f2 := filepath.Join(dir, "separated.txt")
	os.WriteFile(f2, []byte("hello other world greeting\n"), 0644)
	db.AddFile(f2, "line")

	// Quoted phrase should produce cross-boundary trigrams for adjacency
	r, err := db.Search(`"hello world"`)
	if err != nil {
		t.Fatal(err)
	}

	// Should match the adjacent file
	found := false
	for _, res := range r.Results {
		if res.Path == f1 {
			found = true
		}
	}
	if !found {
		t.Error("expected adjacent file in results")
	}
}

// test-DB.md: trailing whitespace trimmed | R178
func TestSearchTrailingWhitespace(t *testing.T) {
	db, dir := testDB(t)

	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("daneel is a robot\n"), 0644)
	db.AddFile(f, "line")

	r1, err := db.Search("daneel")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := db.Search("daneel ")
	if err != nil {
		t.Fatal(err)
	}

	if len(r1.Results) != len(r2.Results) {
		t.Fatalf("trailing space changed results: %q returned %d, %q returned %d",
			"daneel", len(r1.Results), "daneel ", len(r2.Results))
	}
}

// test-DB.md: regex filter AND | R183, R185, R188, R189
func TestRegexFilterAND(t *testing.T) {
	db, dir := testDB(t)

	f1 := writeTestFile(t, dir, "ab.txt", "alpha beta\n")
	f2 := writeTestFile(t, dir, "ag.txt", "alpha gamma\n")
	f3 := writeTestFile(t, dir, "abg.txt", "alpha beta gamma\n")
	db.AddFile(f1, "line")
	db.AddFile(f2, "line")
	db.AddFile(f3, "line")

	r, err := db.Search("alpha", WithRegexFilter("beta", "gamma"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(r.Results))
	}
	if r.Results[0].Path != f3 {
		t.Errorf("expected %s, got %s", f3, r.Results[0].Path)
	}
}

// test-DB.md: except-regex subtract | R184, R188, R189
func TestExceptRegexSubtract(t *testing.T) {
	db, dir := testDB(t)

	f1 := writeTestFile(t, dir, "open.txt", "@status: open task\n")
	f2 := writeTestFile(t, dir, "done.txt", "@status: done task\n")
	db.AddFile(f1, "line")
	db.AddFile(f2, "line")

	r, err := db.Search("task", WithExceptRegex(`@status:.*done`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(r.Results))
	}
	if r.Results[0].Path != f1 {
		t.Errorf("expected %s, got %s", f1, r.Results[0].Path)
	}
}

// test-DB.md: regex filter with SearchRegex | R189, R190
func TestRegexFilterWithSearchRegex(t *testing.T) {
	db, dir := testDB(t)

	f1 := writeTestFile(t, dir, "ab.txt", "alpha beta\n")
	f2 := writeTestFile(t, dir, "ag.txt", "alpha gamma\n")
	db.AddFile(f1, "line")
	db.AddFile(f2, "line")

	r, err := db.SearchRegex("alpha", WithExceptRegex("gamma"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(r.Results))
	}
	if r.Results[0].Path != f1 {
		t.Errorf("expected %s, got %s", f1, r.Results[0].Path)
	}
}

// test-DB.md: regex filter bad pattern returns error | R186
func TestRegexFilterBadPattern(t *testing.T) {
	db, dir := testDB(t)

	f := writeTestFile(t, dir, "test.txt", "hello world\n")
	db.AddFile(f, "line")

	_, err := db.Search("hello", WithRegexFilter("[invalid"))
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

// test-DB.md: regex filter combined with verify | R188
func TestRegexFilterCombinedWithVerify(t *testing.T) {
	db, dir := testDB(t)

	f := writeTestFile(t, dir, "test.txt", "alpha beta gamma\n")
	db.AddFile(f, "line")

	r, err := db.Search("alpha", WithVerify(), WithExceptRegex("gamma"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 0 {
		t.Fatalf("expected 0 results (except-regex should reject), got %d", len(r.Results))
	}
}

// --- GetChunks tests ---

func TestGetChunksTargetOnly(t *testing.T) {
	db, dir := testDB(t)
	f := writeTestFile(t, dir, "test.txt", "line one\nline two\nline three\nline four\nline five\n")
	db.AddFile(f, "line")

	chunks, err := db.GetChunks(f, "3-3", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Range != "3-3" {
		t.Errorf("expected range 3-3, got %s", chunks[0].Range)
	}
	if chunks[0].Index != 2 {
		t.Errorf("expected index 2, got %d", chunks[0].Index)
	}
	if chunks[0].Content != "line three\n" {
		t.Errorf("expected 'line three\\n', got %q", chunks[0].Content)
	}
}

func TestGetChunksWithNeighbors(t *testing.T) {
	db, dir := testDB(t)
	f := writeTestFile(t, dir, "test.txt", "one\ntwo\nthree\nfour\nfive\n")
	db.AddFile(f, "line")

	chunks, err := db.GetChunks(f, "3-3", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	// Verify positional order and indices.
	for i, want := range []struct {
		rng string
		idx int
	}{{"2-2", 1}, {"3-3", 2}, {"4-4", 3}} {
		if chunks[i].Range != want.rng {
			t.Errorf("chunk %d: expected range %s, got %s", i, want.rng, chunks[i].Range)
		}
		if chunks[i].Index != want.idx {
			t.Errorf("chunk %d: expected index %d, got %d", i, want.idx, chunks[i].Index)
		}
	}
}

func TestGetChunksClampedAtBoundaries(t *testing.T) {
	db, dir := testDB(t)
	f := writeTestFile(t, dir, "test.txt", "one\ntwo\nthree\nfour\nfive\n")
	db.AddFile(f, "line")

	// Request 3 before first chunk — should clamp to just chunk 0.
	chunks, err := db.GetChunks(f, "1-1", 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (clamped), got %d", len(chunks))
	}
	if chunks[0].Index != 0 {
		t.Errorf("expected index 0, got %d", chunks[0].Index)
	}

	// Request 3 after last chunk — should clamp to just chunk 4.
	chunks, err = db.GetChunks(f, "5-5", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (clamped), got %d", len(chunks))
	}
	if chunks[0].Index != 4 {
		t.Errorf("expected index 4, got %d", chunks[0].Index)
	}
}

func TestGetChunksRangeNotFound(t *testing.T) {
	db, dir := testDB(t)
	f := writeTestFile(t, dir, "test.txt", "one\ntwo\nthree\n")
	db.AddFile(f, "line")

	_, err := db.GetChunks(f, "999-999", 0, 0)
	if err == nil {
		t.Fatal("expected error for missing range")
	}
}

func TestGetChunksFileNotInDB(t *testing.T) {
	db, _ := testDB(t)

	_, err := db.GetChunks("/nonexistent/file.txt", "1-1", 0, 0)
	if err == nil {
		t.Fatal("expected error for file not in database")
	}
}

// test-DB.md: add file already indexed returns ErrAlreadyIndexed | R213, R214, R215, R216
func TestAddFileAlreadyIndexed(t *testing.T) {
	db, dir := testDB(t)

	fp := filepath.Join(dir, "dup.txt")
	os.WriteFile(fp, []byte("hello world\n"), 0644)

	_, err := db.AddFile(fp, "line")
	if err != nil {
		t.Fatal(err)
	}

	// Second add should return ErrAlreadyIndexed
	_, err = db.AddFile(fp, "line")
	if !errors.Is(err, ErrAlreadyIndexed) {
		t.Fatalf("expected ErrAlreadyIndexed, got %v", err)
	}

	// Original file still searchable, no duplication
	sr, err := db.Search("hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sr.Results))
	}
}

func TestScoreOverlap(t *testing.T) {
	db, dir := testDB(t)
	writeTestFile(t, dir, "a.txt", "alpha beta gamma delta\n")
	writeTestFile(t, dir, "b.txt", "alpha zeta\n")
	db.AddFile(dir+"/a.txt", "line")
	db.AddFile(dir+"/b.txt", "line")

	// Search for a single term present in both files
	sr, err := db.Search("alpha", WithOverlap())
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) < 2 {
		t.Fatalf("expected >=2 results, got %d", len(sr.Results))
	}
	// Both should have positive overlap scores
	for _, r := range sr.Results {
		if r.Score <= 0 {
			t.Errorf("expected positive score, got %.2f for %s", r.Score, r.Path)
		}
	}
}

func TestSearchMulti(t *testing.T) {
	db, dir := testDB(t)
	writeTestFile(t, dir, "a.txt", "alpha beta gamma delta\n")
	writeTestFile(t, dir, "b.txt", "alpha beta\n")
	db.AddFile(dir+"/a.txt", "line")
	db.AddFile(dir+"/b.txt", "line")

	strategies := map[string]ScoreFunc{
		"coverage": scoreCoverage,
		"overlap":  ScoreOverlap,
	}
	results, err := db.SearchMulti("alpha beta", strategies, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 strategy results, got %d", len(results))
	}
	for _, mr := range results {
		if mr.Strategy == "" {
			t.Error("strategy name is empty")
		}
		if len(mr.Results) == 0 {
			t.Errorf("strategy %s has no results", mr.Strategy)
		}
	}
}

func TestSearchMultiSharedFilters(t *testing.T) {
	db, dir := testDB(t)
	writeTestFile(t, dir, "a.txt", "alpha beta\n")
	writeTestFile(t, dir, "b.txt", "alpha gamma\n")
	db.AddFile(dir+"/a.txt", "line")
	db.AddFile(dir+"/b.txt", "line")

	// Filter that rejects any chunk whose file contains "gamma"
	filter := func(c CRecord) bool {
		for _, fid := range c.FileIDs {
			frec, err := c.FileRecord(fid)
			if err == nil && len(frec.Names) > 0 {
				if frec.Names[0] == dir+"/b.txt" {
					return false
				}
			}
		}
		return true
	}

	strategies := map[string]ScoreFunc{
		"coverage": scoreCoverage,
		"overlap":  ScoreOverlap,
	}
	results, err := db.SearchMulti("alpha", strategies, 10, WithChunkFilter(filter))
	if err != nil {
		t.Fatal(err)
	}
	for _, mr := range results {
		for _, r := range mr.Results {
			if r.Path == dir+"/b.txt" {
				t.Errorf("strategy %s should not contain filtered file b.txt", mr.Strategy)
			}
		}
	}
}

func TestBM25Func(t *testing.T) {
	db, dir := testDB(t)
	writeTestFile(t, dir, "a.txt", "hello world foo bar\n")
	writeTestFile(t, dir, "b.txt", "hello world\n")
	db.AddFile(dir+"/a.txt", "line")
	db.AddFile(dir+"/b.txt", "line")

	tris := db.trigrams.ExtractTrigrams([]byte("hello world"))
	scoreFn, err := db.BM25Func(tris)
	if err != nil {
		t.Fatal(err)
	}
	if scoreFn == nil {
		t.Fatal("BM25Func returned nil ScoreFunc")
	}

	sr, err := db.Search("hello world", WithScoring(scoreFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) == 0 {
		t.Fatal("expected results from BM25 search")
	}
}

func TestIRecordCounters(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "a.txt", "hello world\nfoo bar\n")
	db.AddFile(fp, "line")

	var totalChunks, totalTokens uint64
	db.env.View(func(txn *lmdb.Txn) error {
		th := txnWrap{txn}
		totalChunks, _ = iCounter(th, db.dbi, "totalChunks")
		totalTokens, _ = iCounter(th, db.dbi, "totalTokens")
		return nil
	})

	if totalChunks == 0 {
		t.Error("totalChunks should be > 0 after add")
	}
	if totalTokens == 0 {
		t.Error("totalTokens should be > 0 after add")
	}

	db.RemoveFile(fp)

	db.env.View(func(txn *lmdb.Txn) error {
		th := txnWrap{txn}
		totalChunks, _ = iCounter(th, db.dbi, "totalChunks")
		totalTokens, _ = iCounter(th, db.dbi, "totalTokens")
		return nil
	})

	if totalChunks != 0 {
		t.Errorf("totalChunks should be 0 after remove, got %d", totalChunks)
	}
	if totalTokens != 0 {
		t.Errorf("totalTokens should be 0 after remove, got %d", totalTokens)
	}
}

func TestProximityRerank(t *testing.T) {
	db, dir := testDB(t)
	// Chunk with terms adjacent
	writeTestFile(t, dir, "close.txt", "alpha beta nearby words\n")
	// Chunk with terms far apart
	writeTestFile(t, dir, "far.txt", "alpha one two three four five six seven eight nine ten beta\n")
	db.AddFile(dir+"/close.txt", "line")
	db.AddFile(dir+"/far.txt", "line")

	sr, err := db.Search("alpha beta", WithProximityRerank(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) < 2 {
		t.Fatalf("expected >=2 results, got %d", len(sr.Results))
	}
	// close.txt should rank first after proximity rerank
	if sr.Results[0].Path != dir+"/close.txt" {
		t.Errorf("expected close.txt first, got %s", sr.Results[0].Path)
	}
}
