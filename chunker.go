package microfts2

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
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

// CopyPairs deep-copies a slice of Pair.
func CopyPairs(src []Pair) []Pair {
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

// Chunker is the content-based chunking interface for text formats. // CRC: crc-Chunker.md | R502
type Chunker interface {
	Chunks(path string, content []byte, yield func(Chunk) bool) error
}

// FileChunker is the file-based chunking interface for binary formats. // CRC: crc-Chunker.md | R505, R506, R507, R508, R509, R510, R511
// FileChunks reads from path, computes the SHA-256 hash, and yields chunks.
// If old is non-zero and matches the file hash, chunking may be skipped (yield never called).
// Returns the content hash. Zero hash signals no content.
// The method is named FileChunks (not Chunks) so a single type can also implement Chunker.
type FileChunker interface {
	FileChunks(path string, old [32]byte, yield func(Chunk) bool) ([32]byte, error)
}

// RandomAccessChunker retrieves a single chunk by pre-filled range. // CRC: crc-Chunker.md | R524, R525, R526, R527, R528
// Caller pre-fills chunk.Range (from F record Location) and chunk.Attrs (from C record).
// Chunker fills chunk.Content and may replace or augment chunk.Attrs.
// customData is per-file scratch — nil on first call, chunker populates lazily and reuses.
type RandomAccessChunker interface {
	GetChunk(path string, data []byte, customData *any, chunk *Chunk) error
}

// sliceByLineRange is the shared fast-path helper for chunkers whose range
// label is "startLine-endLine". Uses a lazy line-offset table in customData.
// CRC: crc-Chunker.md | R531, R532
func sliceByLineRange(data []byte, customData *any, chunk *Chunk) error {
	startLine, endLine, err := parseLineRange(string(chunk.Range))
	if err != nil {
		return err
	}
	offsets := ensureLineOffsets(customData, data, endLine)
	if startLine < 1 || startLine >= len(offsets) {
		return fmt.Errorf("startLine %d out of range (have %d lines)", startLine, len(offsets)-1)
	}
	if endLine < startLine || endLine >= len(offsets) {
		return fmt.Errorf("endLine %d out of range (have %d lines)", endLine, len(offsets)-1)
	}
	chunk.Content = data[offsets[startLine-1]:offsets[endLine]]
	return nil
}

// ensureLineOffsets lazily populates a []int of line-start byte offsets in customData,
// extending as needed to cover at least `needLines` lines. Final entry may be len(data)
// for the last line without trailing newline, acting as an end-of-content sentinel.
// CRC: crc-Chunker.md | R532
func ensureLineOffsets(customData *any, data []byte, needLines int) []int {
	var offsets []int
	if *customData != nil {
		if o, ok := (*customData).([]int); ok {
			offsets = o
		}
	}
	if offsets == nil {
		offsets = []int{0}
	}
	for len(offsets) <= needLines {
		last := offsets[len(offsets)-1]
		if last >= len(data) {
			break
		}
		nl := bytes.IndexByte(data[last:], '\n')
		if nl < 0 {
			offsets = append(offsets, len(data))
			break
		}
		offsets = append(offsets, last+nl+1)
	}
	*customData = offsets
	return offsets
}

// parseLineRange parses "startLine-endLine" into (start, end, err).
func parseLineRange(s string) (int, int, error) {
	dash := strings.IndexByte(s, '-')
	if dash < 0 {
		return 0, 0, fmt.Errorf("invalid line range %q", s)
	}
	start, err := strconv.Atoi(s[:dash])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start in %q: %w", s, err)
	}
	end, err := strconv.Atoi(s[dash+1:])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid end in %q: %w", s, err)
	}
	return start, end, nil
}

// LineChunker exposes LineChunkFunc as a Chunker + RandomAccessChunker. // CRC: crc-Chunker.md | R531
type LineChunker struct{}

func (LineChunker) Chunks(path string, content []byte, yield func(Chunk) bool) error {
	return LineChunkFunc(path, content, yield)
}

func (LineChunker) GetChunk(path string, data []byte, customData *any, chunk *Chunk) error {
	return sliceByLineRange(data, customData, chunk)
}

// MarkdownChunker exposes MarkdownChunkFunc as a Chunker + RandomAccessChunker. // CRC: crc-Chunker.md | R531
type MarkdownChunker struct{}

func (MarkdownChunker) Chunks(path string, content []byte, yield func(Chunk) bool) error {
	return MarkdownChunkFunc(path, content, yield)
}

func (MarkdownChunker) GetChunk(path string, data []byte, customData *any, chunk *Chunk) error {
	return sliceByLineRange(data, customData, chunk)
}

// ChunkFunc is a generator that yields chunks for a file.
// Convenience type — wrap with FuncChunker to get a full Chunker.
type ChunkFunc func(path string, content []byte, yield func(Chunk) bool) error

// FuncChunker wraps a bare ChunkFunc into a Chunker. // CRC: crc-Chunker.md
type FuncChunker struct {
	Fn ChunkFunc
}

func (fc FuncChunker) Chunks(path string, content []byte, yield func(Chunk) bool) error {
	return fc.Fn(path, content, yield)
}

// CRC: crc-Chunker.md | R116, R128, R130, R131, R169, R170, R171, R172, R173, R174, R177, R291, R292, R295, R296

// MarkdownChunkFunc splits markdown content into paragraph-based chunks.
// Heading lines start new chunks; a heading and its following paragraph
// (up to the next blank line or heading) form one chunk. Blank lines are
// boundaries only and are not included in any chunk's content.
// Fenced code blocks (``` or ~~~) suppress blank-line splitting — all lines
// from opening fence through matching close belong to the current chunk. // R465, R466, R467, R468
// Headline merging: after a heading chunk, consecutive tag-only chunks
// (lines starting with @) and one following content chunk are absorbed
// into a single merged chunk with internal blank lines included. // R563, R564, R565, R566, R567, R568, R569, R570
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
	fenceChar := byte(0) // backtick or tilde when inside a fence
	fenceLen := 0        // number of fence characters in the opening fence

	chunkIsHeading := false // current chunk was started by a heading line
	chunkAllTags := true    // all lines in current chunk start with '@'

	merging := false     // buffering a heading chunk for merge
	mStartLine := 0
	mStartByte := 0
	mEndLine := 0
	mEndByte := 0

	emitMerge := func() bool {
		r := fmt.Sprintf("%d-%d", mStartLine, mEndLine)
		if !yield(Chunk{Range: []byte(r), Content: content[mStartByte:mEndByte]}) {
			return false
		}
		merging = false
		return true
	}

	flush := func() bool {
		if startLine < 0 {
			return true
		}
		sl, sb, el, eb := startLine, startByte, endLine, endByte
		isHead, allTags := chunkIsHeading, chunkAllTags
		startLine = -1
		chunkIsHeading = false
		chunkAllTags = true

		if merging {
			if allTags {
				mEndLine = el
				mEndByte = eb
				return true
			}
			if isHead {
				if !emitMerge() {
					return false
				}
				merging = true
				mStartLine, mStartByte = sl, sb
				mEndLine, mEndByte = el, eb
				return true
			}
			mEndLine = el
			mEndByte = eb
			return emitMerge()
		}

		if isHead {
			merging = true
			mStartLine, mStartByte = sl, sb
			mEndLine, mEndByte = el, eb
			return true
		}

		r := fmt.Sprintf("%d-%d", sl, el)
		return yield(Chunk{Range: []byte(r), Content: content[sb:eb]})
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

		// Inside a fenced code block: only check for closing fence.
		if fenceChar != 0 {
			if isClosingFence(line, fenceChar, fenceLen) {
				fenceChar = 0
				fenceLen = 0
			}
			if startLine < 0 {
				startLine = lineNum
				startByte = lineStart
			}
			chunkAllTags = false
			endLine = lineNum
			endByte = lineEnd
			pos = lineEnd
			continue
		}

		// Check for opening fence.
		if fc, fl := parseFenceOpen(line); fc != 0 {
			fenceChar = fc
			fenceLen = fl
			// Fence continues the current chunk (R466).
			if startLine < 0 {
				startLine = lineNum
				startByte = lineStart
			}
			chunkAllTags = false
			endLine = lineNum
			endByte = lineEnd
			pos = lineEnd
			continue
		}

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
			if merging {
				if !emitMerge() {
					return nil
				}
			}
			startLine = lineNum
			startByte = lineStart
			endLine = lineNum
			endByte = lineEnd
			chunkIsHeading = true
			chunkAllTags = false
		} else {
			if startLine < 0 {
				startLine = lineNum
				startByte = lineStart
				chunkAllTags = true
			}
			if len(line) > 0 && line[0] != '@' {
				chunkAllTags = false
			}
			endLine = lineNum
			endByte = lineEnd
		}

		pos = lineEnd
	}

	flush()
	if merging {
		emitMerge()
	}
	return nil
}

// parseFenceOpen checks if line is a code fence opening (``` or ~~~).
// Returns the fence character and count, or (0, 0) if not a fence.
func parseFenceOpen(line []byte) (byte, int) {
	trimmed := bytes.TrimRight(line, "\n\r")
	if len(trimmed) < 3 {
		return 0, 0
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	n := 1
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	// Info string allowed after backticks, but no backticks in info string.
	if ch == '`' {
		for _, b := range trimmed[n:] {
			if b == '`' {
				return 0, 0
			}
		}
	}
	return ch, n
}

// isClosingFence checks if line closes a fence opened with fenceChar repeated fenceLen times.
func isClosingFence(line []byte, fenceChar byte, fenceLen int) bool {
	trimmed := bytes.TrimRight(line, "\n\r")
	if len(trimmed) < fenceLen {
		return false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == fenceChar {
		n++
	}
	if n < fenceLen {
		return false
	}
	// Rest must be only whitespace.
	for _, b := range trimmed[n:] {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	return true
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
