package microfts2

import (
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

func TestLineChunkerImplementsRandomAccess(t *testing.T) {
	var _ Chunker = LineChunker{}
	var _ RandomAccessChunker = LineChunker{}
}

func TestMarkdownChunkerImplementsRandomAccess(t *testing.T) {
	var _ Chunker = MarkdownChunker{}
	var _ RandomAccessChunker = MarkdownChunker{}
}

func TestBracketChunkerImplementsRandomAccess(t *testing.T) {
	bc := BracketChunker(LangGo)
	if _, ok := bc.(RandomAccessChunker); !ok {
		t.Fatal("BracketChunker should implement RandomAccessChunker")
	}
}

func TestIndentChunkerImplementsRandomAccess(t *testing.T) {
	ic := IndentChunker(BracketLang{}, 4)
	if _, ok := ic.(RandomAccessChunker); !ok {
		t.Fatal("IndentChunker should implement RandomAccessChunker")
	}
}
