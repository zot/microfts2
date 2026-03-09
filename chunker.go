package microfts2

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// CRC: crc-Chunker.md | R169, R170, R171, R172, R173, R174, R177

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
