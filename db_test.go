package microfts2

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if db2.settings.NextFileID != 1 {
		t.Errorf("NextFileID = %d, want 1", db2.settings.NextFileID)
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

	// Build index so active set is available
	if err := db.BuildIndex(100); err != nil {
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

func TestDBRebuildWithDifferentCutoff(t *testing.T) {
	db, dir := testDB(t)
	// Add files with diverse content to create trigram distribution
	for i := 0; i < 5; i++ {
		content := strings.Repeat("unique"+string(rune('a'+i))+" ", 20) + "\n"
		fp := writeTestFile(t, dir, "test"+string(rune('0'+i))+".txt", content)
		if _, err := db.AddFile(fp, "line"); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.BuildIndex(50); err != nil {
		t.Fatal(err)
	}
	active50 := len(db.activeTrigrams)

	if err := db.BuildIndex(30); err != nil {
		t.Fatal(err)
	}
	active30 := len(db.activeTrigrams)

	if active30 >= active50 {
		t.Errorf("30%% cutoff (%d active) should have fewer trigrams than 50%% (%d)", active30, active50)
	}
}

func TestDBReindex(t *testing.T) {
	db, dir := testDB(t)
	// Add a second strategy that chunks every 10 bytes
	db.AddStrategy("fixed10", "awk 'BEGIN{for(i=10;i<=600;i+=10)print i}'")

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

	// Use 100% cutoff so all trigrams are active (small corpus)
	if err := db.BuildIndex(100); err != nil {
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
	if db2.settings.SearchCutoff != 50 {
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

	// Build index with 100% so all trigrams are active
	if err := db.BuildIndex(100); err != nil {
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

func TestDBBuildIndexCutoff(t *testing.T) {
	db, dir := testDB(t)
	// Add diverse files to create trigram distribution
	for i := 0; i < 5; i++ {
		content := strings.Repeat("word"+string(rune('a'+i))+" ", 20) + "\n"
		fp := writeTestFile(t, dir, "f"+string(rune('0'+i))+".txt", content)
		if _, err := db.AddFile(fp, "line"); err != nil {
			t.Fatal(err)
		}
	}

	// Build with higher cutoff — active set should be larger
	if err := db.BuildIndex(60); err != nil {
		t.Fatal(err)
	}
	active60 := len(db.activeTrigrams)

	if err := db.BuildIndex(30); err != nil {
		t.Fatal(err)
	}
	active30 := len(db.activeTrigrams)

	if active30 >= active60 {
		t.Errorf("cutoff 30%% (%d active) should be less than cutoff 60%% (%d)", active30, active60)
	}

	// Settings should reflect the configured values
	if db.settings.SearchCutoff != 30 {
		t.Errorf("SearchCutoff = %d, want 30", db.settings.SearchCutoff)
	}
}

func TestDBEnv(t *testing.T) {
	db, _ := testDB(t)
	env := db.Env()
	if env == nil {
		t.Fatal("Env() returned nil")
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
	db.AddStrategy("fixed10", "awk 'BEGIN{for(i=10;i<=600;i+=10)print i}'")
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

	info, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatal(err)
	}
	if info.Filename != fp {
		t.Errorf("Filename = %q, want %q", info.Filename, fp)
	}
	if info.ChunkingStrategy != "line" {
		t.Errorf("ChunkingStrategy = %q, want line", info.ChunkingStrategy)
	}
	if len(info.ChunkStartLines) == 0 {
		t.Error("ChunkStartLines should not be empty")
	}
}

func TestDBScoreFile(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar baz\nthe quick brown fox\n")

	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}

	// Build index so active set is established
	if err := db.BuildIndex(100); err != nil {
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
	if err := db.BuildIndex(50); err != nil {
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

	// Register a func strategy that puts each line in its own chunk
	lineFunc := func(path string, content []byte) ([]int64, error) {
		var offsets []int64
		for i, b := range content {
			if b == '\n' {
				offsets = append(offsets, int64(i+1))
			}
		}
		return offsets, nil
	}
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

	// Build a corpus with enough variety so trigrams survive the active set cutoff.
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
	db.BuildIndex(100)

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
	db.BuildIndex(50)

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
	db.BuildIndex(50)

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
