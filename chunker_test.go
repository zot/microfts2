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
	chunks := collectMarkdownChunks(t, "# Title\npara line 1\npara line 2\n\nother text\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Range != "1-3" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-3")
	}
	if chunks[1].Range != "5-5" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "5-5")
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
	chunks := collectMarkdownChunks(t, "# Title\nline\n\nanother\n")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Range != "1-2" {
		t.Errorf("chunk 0 range = %q, want %q", chunks[0].Range, "1-2")
	}
	if chunks[1].Range != "4-4" {
		t.Errorf("chunk 1 range = %q, want %q", chunks[1].Range, "4-4")
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
