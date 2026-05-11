package microfts2

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

// CRC: crc-Chunker.md | test-Chunker.md

type chunkResult struct {
	Range   string
	Content string
}

func collectMarkdownChunks(t *testing.T, input string) []chunkResult {
	t.Helper()
	var chunks []chunkResult
	err := MarkdownChunkFunc("", []byte(input), func(c Chunk) bool {
		chunks = append(chunks, chunkResult{
			Range:   string(c.Range),
			Content: string(c.Content),
		})
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

func TestMarkdownChunkHeadingStartsNewChunk(t *testing.T) {
	chunks := collectMarkdownChunks(t, "# Title\nsome text\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Range != "1-2" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-2")
	}
}

func TestMarkdownChunkHeadingWithParagraph(t *testing.T) {
	// Heading merges following content chunk (R567).
	chunks := collectMarkdownChunks(t, "# Title\npara line 1\npara line 2\n\nother text\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Range != "1-5" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-5")
	}
	want := "# Title\npara line 1\npara line 2\n\nother text\n"
	if chunks[0].Content != want {
		t.Errorf("content = %q, want %q", chunks[0].Content, want)
	}
}

func TestMarkdownChunkConsecutiveHeadings(t *testing.T) {
	chunks := collectMarkdownChunks(t, "# One\n## Two\n### Three\n")
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Range != "1-1" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-1")
	}
	if chunks[1].Range != "2-2" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "2-2")
	}
	if chunks[2].Range != "3-3" {
		t.Errorf("chunk 2 range = %q, want %q", chunks[2].Range, "3-3")
	}
}

func TestMarkdownChunkBlankLineCollapsing(t *testing.T) {
	chunks := collectMarkdownChunks(t, "text a\n\n\n\ntext b\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Range != "1-1" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-1")
	}
	if chunks[1].Range != "5-5" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "5-5")
	}
}

func TestMarkdownChunkNonHeadingParagraph(t *testing.T) {
	chunks := collectMarkdownChunks(t, "line one\nline two\nline three\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Range != "1-3" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-3")
	}
	if chunks[0].Content != "line one\nline two\nline three\n" {
		t.Errorf("content = %q, want %q", chunks[0].Content, "line one\nline two\nline three\n")
	}
}

func TestMarkdownChunkHeadingAfterParagraph(t *testing.T) {
	chunks := collectMarkdownChunks(t, "some text\n\n# Heading\nparagraph\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Range != "1-1" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-1")
	}
	if chunks[1].Range != "3-4" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "3-4")
	}
}

func TestMarkdownChunkEmpty(t *testing.T) {
	chunks := collectMarkdownChunks(t, "")
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestMarkdownChunkRangeFormat(t *testing.T) {
	// Heading merges following content chunk (R567).
	chunks := collectMarkdownChunks(t, "# Title\nline\n\nanother\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Range != "1-4" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-4")
	}
}

func TestMarkdownChunkCodeFenceKeepsBlankLines(t *testing.T) {
	chunks := collectMarkdownChunks(t, "text before\n```\nx = 1\n\ny = 2\n```\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-6" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-6")
	}
}

func TestMarkdownChunkCodeFenceWithInfoString(t *testing.T) {
	chunks := collectMarkdownChunks(t, "# Heading\n```go\nfunc main() {\n}\n```\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-5" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-5")
	}
}

func TestMarkdownChunkTildeFence(t *testing.T) {
	chunks := collectMarkdownChunks(t, "para\n~~~\na\n\nb\n~~~\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-6" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-6")
	}
}

func TestMarkdownChunkFenceClosingRequiresMatchingLength(t *testing.T) {
	chunks := collectMarkdownChunks(t, "text\n````\ncode\n```\nstill code\n````\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-6" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-6")
	}
}

func TestMarkdownChunkTextAfterCodeFence(t *testing.T) {
	chunks := collectMarkdownChunks(t, "before\n```\ncode\n```\n\nafter\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-4" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-4")
	}
	if chunks[1].Range != "6-6" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "6-6")
	}
}

// R563-R570: Headline merging tests

func TestMarkdownHeadlineMergeTagsAndContent(t *testing.T) {
	input := "# Bubba\n\n@subject: prootwaddles\n\nProotwaddles are funny creatures.\n"
	chunks := collectMarkdownChunks(t, input)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-5" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-5")
	}
	if chunks[0].Content != input {
		t.Errorf("content = %q, want %q", chunks[0].Content, input)
	}
}

func TestMarkdownHeadlineMergeMultipleTags(t *testing.T) {
	input := "# Title\n\n@tag1: a\n@tag2: b\n\n@tag3: c\n\nparagraph\n"
	chunks := collectMarkdownChunks(t, input)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-8" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-8")
	}
	if chunks[0].Content != input {
		t.Errorf("content = %q, want %q", chunks[0].Content, input)
	}
}

func TestMarkdownHeadlineMergeContentOnly(t *testing.T) {
	// No tags, just heading + content chunk.
	chunks := collectMarkdownChunks(t, "# Heading\n\nparagraph text\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-3" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-3")
	}
}

func TestMarkdownHeadlineMergeOnlyOnce(t *testing.T) {
	// Heading absorbs one content chunk, not two.
	chunks := collectMarkdownChunks(t, "# Heading\n\nfirst para\n\nsecond para\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-3" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-3")
	}
	if chunks[1].Range != "5-5" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "5-5")
	}
}

func TestMarkdownHeadlineMergeTagsThenHeading(t *testing.T) {
	// Tags absorbed but next chunk is a heading — emit without content.
	chunks := collectMarkdownChunks(t, "# H1\n\n@tag: val\n\n## H2\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-3" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-3")
	}
	if chunks[1].Range != "5-5" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "5-5")
	}
}

func TestMarkdownHeadlineMergeAtEOF(t *testing.T) {
	// Heading at end of file with no followers.
	chunks := collectMarkdownChunks(t, "some text\n\n# Heading\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-1" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-1")
	}
	if chunks[1].Range != "3-3" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "3-3")
	}
}

func TestMarkdownHeadlineMergeWithFence(t *testing.T) {
	// Content chunk after heading contains a fenced code block.
	input := "# Title\n\n@tag: val\n\n```go\nfunc main() {}\n```\n"
	chunks := collectMarkdownChunks(t, input)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-7" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-7")
	}
	if chunks[0].Content != input {
		t.Errorf("content = %q, want %q", chunks[0].Content, input)
	}
}

func TestMarkdownHeadlineMergeConsecutiveHeadingsUnchanged(t *testing.T) {
	// Consecutive headings with blank lines: each emitted separately.
	chunks := collectMarkdownChunks(t, "# One\n\n## Two\n\n### Three\n")
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-1" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-1")
	}
	if chunks[1].Range != "3-3" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "3-3")
	}
	if chunks[2].Range != "5-5" {
		t.Errorf("chunk 2 range = %q, want %q", chunks[2].Range, "5-5")
	}
}

func TestMarkdownHeadlineMergeTagsAtEOF(t *testing.T) {
	// Tags after heading with no content chunk following.
	chunks := collectMarkdownChunks(t, "# Title\n\n@tag: val\n")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Range != "1-3" {
		t.Errorf("range = %q, want %q", chunks[0].Range, "1-3")
	}
}

// R524-R532: RandomAccessChunker line-range fast path
func TestSliceByLineRangeSingleLine(t *testing.T) {
	data := []byte("alpha\nbeta\ngamma\ndelta\n")
	var cd any
	chunk := Chunk{Range: []byte("2-2")}
	if err := sliceByLineRange(data, &cd, &chunk); err != nil {
		t.Fatalf("sliceByLineRange: %v", err)
	}
	if string(chunk.Content) != "beta\n" {
		t.Errorf("Content = %q, want %q", chunk.Content, "beta\n")
	}
}

func TestSliceByLineRangeMultiLine(t *testing.T) {
	data := []byte("alpha\nbeta\ngamma\ndelta\n")
	var cd any
	chunk := Chunk{Range: []byte("2-3")}
	if err := sliceByLineRange(data, &cd, &chunk); err != nil {
		t.Fatalf("sliceByLineRange: %v", err)
	}
	if string(chunk.Content) != "beta\ngamma\n" {
		t.Errorf("Content = %q, want %q", chunk.Content, "beta\ngamma\n")
	}
}

func TestSliceByLineRangeLastLineNoNewline(t *testing.T) {
	data := []byte("alpha\nbeta\ngamma")
	var cd any
	chunk := Chunk{Range: []byte("3-3")}
	if err := sliceByLineRange(data, &cd, &chunk); err != nil {
		t.Fatalf("sliceByLineRange: %v", err)
	}
	if string(chunk.Content) != "gamma" {
		t.Errorf("Content = %q, want %q", chunk.Content, "gamma")
	}
}

func TestSliceByLineRangeCustomDataReuse(t *testing.T) {
	data := []byte("one\ntwo\nthree\nfour\nfive\n")
	var cd any

	chunk1 := Chunk{Range: []byte("2-2")}
	if err := sliceByLineRange(data, &cd, &chunk1); err != nil {
		t.Fatal(err)
	}
	offsetsAfter1, ok := cd.([]int)
	if !ok {
		t.Fatal("customData should be []int")
	}
	lenAfter1 := len(offsetsAfter1)

	// Second call for a later line — offsets table should extend, not rebuild.
	chunk2 := Chunk{Range: []byte("5-5")}
	if err := sliceByLineRange(data, &cd, &chunk2); err != nil {
		t.Fatal(err)
	}
	offsetsAfter2 := cd.([]int)
	if len(offsetsAfter2) <= lenAfter1 {
		t.Errorf("expected offsets to extend beyond %d, got %d", lenAfter1, len(offsetsAfter2))
	}
	// Prefix preserved
	for i := 0; i < lenAfter1; i++ {
		if offsetsAfter1[i] != offsetsAfter2[i] {
			t.Errorf("offsets[%d] changed: %d → %d", i, offsetsAfter1[i], offsetsAfter2[i])
		}
	}
	if string(chunk2.Content) != "five\n" {
		t.Errorf("Content = %q, want %q", chunk2.Content, "five\n")
	}
}

func TestSliceByLineRangeOutOfBounds(t *testing.T) {
	data := []byte("one\ntwo\n")
	var cd any
	chunk := Chunk{Range: []byte("5-5")}
	if err := sliceByLineRange(data, &cd, &chunk); err == nil {
		t.Fatal("expected error for out-of-bounds range")
	}
}

// All four built-in text chunkers implement the full quartet: Chunker,
// FileChunker, RandomAccessChunker, AppendAwareChunker. R633, R636, R637
func TestLineChunkerImplementsAllInterfaces(t *testing.T) {
	var _ Chunker = LineChunker{}
	var _ FileChunker = LineChunker{}
	var _ RandomAccessChunker = LineChunker{}
	var _ AppendAwareChunker = LineChunker{}
}

func TestMarkdownChunkerImplementsAllInterfaces(t *testing.T) {
	var _ Chunker = MarkdownChunker{}
	var _ FileChunker = MarkdownChunker{}
	var _ RandomAccessChunker = MarkdownChunker{}
	var _ AppendAwareChunker = MarkdownChunker{}
}

func TestBracketChunkerImplementsAllInterfaces(t *testing.T) {
	bc := BracketChunker(LangGo)
	if _, ok := bc.(FileChunker); !ok {
		t.Fatal("BracketChunker should implement FileChunker")
	}
	if _, ok := bc.(RandomAccessChunker); !ok {
		t.Fatal("BracketChunker should implement RandomAccessChunker")
	}
	if _, ok := bc.(AppendAwareChunker); !ok {
		t.Fatal("BracketChunker should implement AppendAwareChunker")
	}
}

func TestIndentChunkerImplementsAllInterfaces(t *testing.T) {
	ic := IndentChunker(BracketLang{}, 4)
	if _, ok := ic.(FileChunker); !ok {
		t.Fatal("IndentChunker should implement FileChunker")
	}
	if _, ok := ic.(RandomAccessChunker); !ok {
		t.Fatal("IndentChunker should implement RandomAccessChunker")
	}
	if _, ok := ic.(AppendAwareChunker); !ok {
		t.Fatal("IndentChunker should implement AppendAwareChunker")
	}
}

// collectAppendChunks runs an AppendAwareChunker against a written file and
// returns the yielded chunks (deep-copied), replacedLast, and any error.
func collectAppendChunks(t *testing.T, ac AppendAwareChunker, path string, lastLocator, newBytes []byte) ([]Chunk, bool, error) {
	t.Helper()
	var out []Chunk
	replacedLast, err := ac.AppendChunks(path, lastLocator, newBytes, func(c Chunk) bool {
		out = append(out, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Locator: append([]byte(nil), c.Locator...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	return out, replacedLast, err
}

// fileChunksByRead returns early with the supplied old hash and no yields
// when the file's hash matches — the staleness short-circuit. R632
func TestFileChunksByReadHashSkip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(fp, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("a\nb\nc\n"))

	calls := 0
	chunk := func(_ string, content []byte, yield func(Chunk) bool) error {
		calls++
		return nil
	}
	got, err := fileChunksByRead(fp, hash, chunk, func(Chunk) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got != hash {
		t.Errorf("returned hash should match file hash")
	}
	if calls != 0 {
		t.Errorf("chunker should not be invoked when old hash matches, got %d calls", calls)
	}
}

// fileChunksByRead delegates to the chunker when the supplied old hash
// differs from the file's hash. R632, R636
func TestFileChunksByReadDelegatesOnHashMiss(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(fp, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	chunk := func(_ string, content []byte, yield func(Chunk) bool) error {
		calls++
		if string(content) != "a\nb\nc\n" {
			t.Errorf("chunker received content %q, want %q", content, "a\nb\nc\n")
		}
		return nil
	}
	// old is the zero hash, so the short-circuit never fires.
	got, err := fileChunksByRead(fp, [32]byte{}, chunk, func(Chunk) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got == ([32]byte{}) {
		t.Error("returned hash should be non-zero for non-empty file")
	}
	if calls != 1 {
		t.Errorf("chunker should be invoked once on hash miss, got %d calls", calls)
	}
}

func TestFileChunksByReadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(fp, nil, 0644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	chunk := func(_ string, _ []byte, _ func(Chunk) bool) error { calls++; return nil }
	got, err := fileChunksByRead(fp, [32]byte{}, chunk, func(Chunk) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got != ([32]byte{}) {
		t.Error("empty file should return zero hash")
	}
	if calls != 0 {
		t.Error("empty file should not invoke chunker")
	}
}

// LineChunker AppendChunks — when the previous tail had no trailing newline,
// the appended content completes that line and replaces the previous chunk.
// R634, R635
func TestLineChunkerAppendChunksCompletesPartialLine(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "f.txt")
	initial := "alpha\nbeta\ngamma" // no trailing newline on last line
	appended := "_continued\n"
	if err := os.WriteFile(fp, []byte(initial+appended), 0644); err != nil {
		t.Fatal(err)
	}
	// Previous last chunk: bytes 11..16 (the partial "gamma").
	oldLocator := EncodeByteRangeLocator(11, 16)

	chunks, replacedLast, err := collectAppendChunks(t, LineChunker{}, fp, oldLocator, []byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	if !replacedLast {
		t.Fatal("partial line completion should set replacedLast=true")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 merged-line chunk, got %d", len(chunks))
	}
	if want := "gamma_continued\n"; string(chunks[0].Content) != want {
		t.Errorf("merged chunk content = %q, want %q", chunks[0].Content, want)
	}
	if want := "3-3"; string(chunks[0].Range) != want {
		t.Errorf("merged chunk range = %q, want %q", chunks[0].Range, want)
	}
}

// LineChunker AppendChunks — when the previous tail already ended in a newline,
// the appended line forms a fresh chunk and the previous tail is preserved.
// R634
func TestLineChunkerAppendChunksCleanBoundary(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "f.txt")
	initial := "alpha\nbeta\ngamma\n"
	appended := "delta\n"
	if err := os.WriteFile(fp, []byte(initial+appended), 0644); err != nil {
		t.Fatal(err)
	}
	// Previous last chunk: bytes 11..17 (the complete "gamma\n").
	oldLocator := EncodeByteRangeLocator(11, 17)

	chunks, replacedLast, err := collectAppendChunks(t, LineChunker{}, fp, oldLocator, []byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	if replacedLast {
		t.Error("clean line boundary should set replacedLast=false")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 new chunk (\"delta\\n\"), got %d", len(chunks))
	}
	if want := "delta\n"; string(chunks[0].Content) != want {
		t.Errorf("new chunk content = %q, want %q", chunks[0].Content, want)
	}
	if want := "4-4"; string(chunks[0].Range) != want {
		t.Errorf("new chunk range = %q, want %q", chunks[0].Range, want)
	}
}

func TestLineChunkerAppendChunksNilLocator(t *testing.T) {
	chunks, replacedLast, err := collectAppendChunks(t, LineChunker{}, "irrelevant.txt", nil, []byte("a\nb\n"))
	if err != nil {
		t.Fatal(err)
	}
	if replacedLast {
		t.Error("nil locator should not set replacedLast")
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks for 2 lines, got %d", len(chunks))
	}
}

// IndentChunker AppendChunks — appending a deeper-indent line extends the
// previous scope, replacing its chunk. R637, R638
func TestIndentChunkerAppendChunksExtendsScope(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "f.py")
	initial := "def hello():\n    print('hi')\n"
	appended := "    print('bye')\n"
	if err := os.WriteFile(fp, []byte(initial+appended), 0644); err != nil {
		t.Fatal(err)
	}
	// Previous last chunk: the whole def hello() group, bytes 0..len(initial).
	oldLocator := EncodeByteRangeLocator(0, len(initial))

	ic := IndentChunker(BracketLang{LineComments: []string{"#"}}, 4)
	chunks, replacedLast, err := collectAppendChunks(t, ic.(AppendAwareChunker), fp, oldLocator, []byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	if !replacedLast {
		t.Fatal("extending a scope should set replacedLast=true")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 merged scope chunk, got %d", len(chunks))
	}
	if want := initial + appended; string(chunks[0].Content) != want {
		t.Errorf("merged chunk content = %q, want %q", chunks[0].Content, want)
	}
}

// IndentChunker AppendChunks — when the appended content starts a new
// top-level definition, the previous chunk is preserved and a new chunk
// appears for the addition. R637
func TestIndentChunkerAppendChunksCleanBoundary(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "f.py")
	initial := "def hello():\n    print('hi')\n\n"
	appended := "def goodbye():\n    print('bye')\n"
	if err := os.WriteFile(fp, []byte(initial+appended), 0644); err != nil {
		t.Fatal(err)
	}
	// Previous last chunk: the def hello() group (excluding the trailing blank line).
	oldStart := 0
	oldEnd := len("def hello():\n    print('hi')\n")
	oldLocator := EncodeByteRangeLocator(oldStart, oldEnd)

	ic := IndentChunker(BracketLang{LineComments: []string{"#"}}, 4)
	chunks, replacedLast, err := collectAppendChunks(t, ic.(AppendAwareChunker), fp, oldLocator, []byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	if replacedLast {
		t.Error("clean scope boundary should set replacedLast=false")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 new chunk (def goodbye group), got %d", len(chunks))
	}
	if want := "def goodbye():\n    print('bye')\n"; string(chunks[0].Content) != want {
		t.Errorf("new chunk content = %q, want %q", chunks[0].Content, want)
	}
}
