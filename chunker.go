package microfts2

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Pair is an opaque key-value pair for per-chunk metadata.
// Allows duplicate keys. Mirrors the DB wire format.
type Pair struct {
	Key   []byte
	Value []byte
}

// Chunk is a single chunk yielded by a Chunker.
// Range is an opaque label (e.g. "1-10" for lines); Content is the chunk text.
// Range and Content may be reused between yields — caller must copy if retaining.
// Attrs is optional per-chunk metadata (nil means no attrs).
type Chunk struct {
	Range   []byte
	Content []byte
	Attrs   []Pair
}

// PairGet returns the value for the first Pair matching key, or nil if not found.
func PairGet(pairs []Pair, key string) ([]byte, bool) {
	kb := []byte(key)
	for _, p := range pairs {
		if bytes.Equal(p.Key, kb) {
			return p.Value, true
		}
	}
	return nil, false
}

// copyPairs deep-copies a slice of Pair.
func copyPairs(src []Pair) []Pair {
	if len(src) == 0 {
		return nil
	}
	dst := make([]Pair, len(src))
	for i, p := range src {
		dst[i] = Pair{
			Key:   append([]byte(nil), p.Key...),
			Value: append([]byte(nil), p.Value...),
		}
	}
	return dst
}

// chunkTextByRange scans a Chunker's output for a matching range label and returns its content.
// Shared by BracketChunker and IndentChunker.
func chunkTextByRange(c Chunker, path string, content []byte, rangeLabel string) ([]byte, bool) {
	var result []byte
	var found bool
	c.Chunks(path, content, func(ch Chunk) bool {
		if string(ch.Range) == rangeLabel {
			result = make([]byte, len(ch.Content))
			copy(result, ch.Content)
			found = true
			return false
		}
		return true
	})
	return result, found
}

// Chunker is the interface for chunking strategies.
// Chunks produces chunks for indexing; ChunkText retrieves a single chunk's content.
type Chunker interface {
	Chunks(path string, content []byte, yield func(Chunk) bool) error
	ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool)
}

// ChunkFunc is a generator that yields chunks for a file.
// Convenience type — wrap with FuncChunker to get a full Chunker.
type ChunkFunc func(path string, content []byte, yield func(Chunk) bool) error

// FuncChunker wraps a bare ChunkFunc into a Chunker.
// ChunkText re-runs the function and returns the first chunk matching the range label.
type FuncChunker struct {
	Fn ChunkFunc
}

func (fc FuncChunker) Chunks(path string, content []byte, yield func(Chunk) bool) error {
	return fc.Fn(path, content, yield)
}

func (fc FuncChunker) ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool) {
	var result []byte
	var found bool
	fc.Fn(path, content, func(c Chunk) bool {
		if string(c.Range) == rangeLabel {
			result = make([]byte, len(c.Content))
			copy(result, c.Content)
			found = true
			return false
		}
		return true
	})
	return result, found
}

// CRC: crc-Chunker.md | R116, R128, R130, R131, R169, R170, R171, R172, R173, R174, R177, R291, R292, R295, R296

// MarkdownChunkFunc splits markdown content into paragraph-based chunks.
// Heading lines start new chunks; a heading and its following paragraph
// (up to the next blank line or heading) form one chunk. Blank lines are
// boundaries only and are not included in any chunk's content.
func MarkdownChunkFunc(_ string, content []byte, yield func(Chunk) bool) error {
	if len(content) == 0 {
		return nil
	}

	lineNum := 0       // 1-indexed current line (after increment)
	startLine := -1    // 1-indexed start of current chunk
	startByte := 0     // byte offset of current chunk start
	endLine := 0       // 1-indexed end of most recent content line
	endByte := 0       // byte offset past end of most recent content line
	pos := 0           // current byte position

	flush := func() bool {
		if startLine < 0 {
			return true
		}
		r := fmt.Sprintf("%d-%d", startLine, endLine)
		if !yield(Chunk{Range: []byte(r), Content: content[startByte:endByte]}) {
			return false
		}
		startLine = -1
		return true
	}

	for pos < len(content) {
		lineStart := pos
		nl := bytes.IndexByte(content[pos:], '\n')
		var lineEnd int
		if nl < 0 {
			lineEnd = len(content)
		} else {
			lineEnd = pos + nl + 1
		}
		lineNum++
		line := content[lineStart:lineEnd]

		blank := isBlankLine(line)
		heading := !blank && line[0] == '#'

		if blank {
			if !flush() {
				return nil
			}
		} else if heading {
			if startLine >= 0 {
				if !flush() {
					return nil
				}
			}
			startLine = lineNum
			startByte = lineStart
			endLine = lineNum
			endByte = lineEnd
		} else {
			if startLine < 0 {
				startLine = lineNum
				startByte = lineStart
			}
			endLine = lineNum
			endByte = lineEnd
		}

		pos = lineEnd
	}

	flush()
	return nil
}

// isBlankLine reports whether a line (possibly including trailing \n) is blank.
func isBlankLine(line []byte) bool {
	for _, b := range line {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
	}
	return true
}

// RunChunkerFunc returns a ChunkFunc that executes an external command.
// The command receives the filepath as an argument and outputs
// one chunk per line on stdout as "range\tcontent".
func RunChunkerFunc(cmd string) ChunkFunc {
	return func(path string, content []byte, yield func(Chunk) bool) error {
		c := exec.Command("sh", "-c", cmd+` "$1"`, "--", path)
		out, err := c.Output()
		if err != nil {
			return fmt.Errorf("chunker %q: %w", cmd, err)
		}
		scanner := bufio.NewScanner(bytes.NewReader(out))
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // 16MB max line
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			tab := strings.IndexByte(line, '\t')
			if tab < 0 {
				return fmt.Errorf("chunker output: missing tab in line %q", line)
			}
			chunk := Chunk{
				Range:   []byte(line[:tab]),
				Content: []byte(line[tab+1:]),
			}
			if !yield(chunk) {
				break
			}
		}
		return scanner.Err()
	}
}
