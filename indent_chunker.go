package microfts2

// CRC: crc-IndentChunker.md | Seq: seq-indent-chunk.md
// R325, R326, R327, R328, R329, R330, R331, R332, R333, R334, R335

import (
	"bytes"
	"fmt"
)

// indentLine holds per-line info for indent chunking.
type indentLine struct {
	start, end int // byte offsets
	indent     int // column count, -1 for blank
	blank      bool
}

// indentChunker implements Chunker for indentation-scoped languages. R333
type indentChunker struct {
	lang     BracketLang
	tabWidth int
}

// IndentChunker returns a Chunker for indentation-scoped languages. R333
// tabWidth controls how tabs count for column calculation (0 = one column per tab).
func IndentChunker(lang BracketLang, tabWidth int) Chunker {
	return &indentChunker{lang: lang, tabWidth: tabWidth}
}

func (ic *indentChunker) Chunks(path string, content []byte, yield func(Chunk) bool) error {
	if len(content) == 0 {
		return nil
	}

	var lines []indentLine
	pos := 0
	for pos < len(content) {
		lineStart := pos
		nl := bytes.IndexByte(content[pos:], '\n')
		var lineEnd int
		if nl < 0 {
			lineEnd = len(content)
		} else {
			lineEnd = pos + nl + 1
		}
		line := content[lineStart:lineEnd]
		blank := isBlankLine(line)
		indent := -1
		if !blank {
			indent = measureIndent(line, ic.tabWidth)
		}
		lines = append(lines, indentLine{lineStart, lineEnd, indent, blank})
		pos = lineEnd
	}

	// R335: mark lines inside block comments as blank for indent purposes
	markLiteralLines(content, lines, ic.lang.BlockComments)

	// Build scope groups from indentation. R326, R327
	groups := buildIndentGroups(lines)

	// R330: attach leading comment lines
	groups = attachLeadingIndent(groups, lines, content, ic.lang.LineComments)

	// Emit chunks
	return emitIndentChunks(content, lines, groups, yield)
}

func (ic *indentChunker) ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool) {
	return chunkTextByRange(ic, path, content, rangeLabel)
}

// measureIndent counts the leading whitespace columns. R328
func measureIndent(line []byte, tabWidth int) int {
	col := 0
	for _, b := range line {
		switch b {
		case ' ':
			col++
		case '\t':
			if tabWidth <= 0 {
				col++
			} else {
				col += tabWidth - (col % tabWidth)
			}
		default:
			return col
		}
	}
	return col
}

// markLiteralLines marks lines inside block comments as blank. R335
func markLiteralLines(content []byte, lines []indentLine, blockComments [][2]string) {
	for _, bc := range blockComments {
		open, close := []byte(bc[0]), []byte(bc[1])
		pos := 0
		for pos < len(content) {
			idx := bytes.Index(content[pos:], open)
			if idx < 0 {
				break
			}
			startByte := pos + idx
			endIdx := bytes.Index(content[startByte+len(open):], close)
			var endByte int
			if endIdx < 0 {
				endByte = len(content)
			} else {
				endByte = startByte + len(open) + endIdx + len(close)
			}
			for i := range lines {
				if lines[i].start > startByte && lines[i].end <= endByte {
					lines[i].indent = -1
					lines[i].blank = true
				}
			}
			pos = endByte
		}
	}
}

// buildIndentGroups finds groups based on indentation increases. R326, R327, R329
func buildIndentGroups(lines []indentLine) []groupSpan {
	var groups []groupSpan

	type stackEntry struct {
		indent    int
		startLine int // 1-based, the header line
	}

	var stack []stackEntry
	prevIndent := 0

	for i, li := range lines {
		lineNum := i + 1
		if li.blank {
			continue
		}

		indent := li.indent

		// Dedent: close scopes. R327
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			endLine := lineNum - 1
			for endLine > top.startLine && lines[endLine-1].blank {
				endLine--
			}
			groups = append(groups, groupSpan{top.startLine, endLine})
		}

		// Indent increase: new scope. R326
		if indent > prevIndent {
			headerLine := 0
			for j := i - 1; j >= 0; j-- {
				if !lines[j].blank {
					headerLine = j + 1
					break
				}
			}
			if headerLine > 0 {
				stack = append(stack, stackEntry{indent: prevIndent, startLine: headerLine})
			}
		}

		prevIndent = indent
	}

	// Flush remaining stack at EOF
	lastLine := len(lines)
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		endLine := lastLine
		for endLine > top.startLine && lines[endLine-1].blank {
			endLine--
		}
		groups = append(groups, groupSpan{top.startLine, endLine})
	}

	sortGroups(groups)
	groups = removeNested(groups)
	return groups
}

func sortGroups(groups []groupSpan) {
	for i := 1; i < len(groups); i++ {
		g := groups[i]
		j := i
		for j > 0 && groups[j-1].startLine > g.startLine {
			groups[j] = groups[j-1]
			j--
		}
		groups[j] = g
	}
}

func removeNested(groups []groupSpan) []groupSpan {
	if len(groups) <= 1 {
		return groups
	}
	var result []groupSpan
	result = append(result, groups[0])
	for i := 1; i < len(groups); i++ {
		top := result[len(result)-1]
		if groups[i].startLine >= top.startLine && groups[i].endLine <= top.endLine {
			continue
		}
		result = append(result, groups[i])
	}
	return result
}

// attachLeadingIndent extends group starts for preceding comment lines. R330
func attachLeadingIndent(groups []groupSpan, lines []indentLine, content []byte, lineComments []string) []groupSpan {
	for i := range groups {
		minLine := 1
		if i > 0 {
			minLine = groups[i-1].endLine + 1
		}
		for line := groups[i].startLine - 1; line >= minLine; line-- {
			if lines[line-1].blank {
				break
			}
			lineContent := content[lines[line-1].start:lines[line-1].end]
			trimmed := bytes.TrimLeft(lineContent, " \t")
			isComment := false
			for _, m := range lineComments {
				if bytes.HasPrefix(trimmed, []byte(m)) {
					isComment = true
					break
				}
			}
			if !isComment {
				break
			}
			groups[i].startLine = line
		}
	}
	return groups
}

// emitIndentChunks emits group and paragraph chunks. R329, R331, R332
func emitIndentChunks(content []byte, lines []indentLine, groups []groupSpan, yield func(Chunk) bool) error {
	gi := 0
	paraStart := -1
	paraStartLine := 0

	flush := func(startLine, endLine, startByte, endByte int) bool {
		if startLine < 0 {
			return true
		}
		r := fmt.Sprintf("%d-%d", startLine, endLine)
		return yield(Chunk{Range: []byte(r), Content: content[startByte:endByte]})
	}

	for i, li := range lines {
		lineNum := i + 1

		if gi < len(groups) && lineNum == groups[gi].startLine {
			if paraStart >= 0 {
				if !flush(paraStartLine, lineNum-1, paraStart, lines[i-1].end) {
					return nil
				}
				paraStart = -1
			}
			gEnd := min(groups[gi].endLine, len(lines))
			if !flush(groups[gi].startLine, gEnd, li.start, lines[gEnd-1].end) {
				return nil
			}
			gi++
			continue
		}

		if gi > 0 && lineNum <= groups[gi-1].endLine {
			continue
		}
		if gi < len(groups) && lineNum >= groups[gi].startLine && lineNum <= groups[gi].endLine {
			continue
		}

		if li.blank {
			if paraStart >= 0 {
				if !flush(paraStartLine, lineNum-1, paraStart, lines[i-1].end) {
					return nil
				}
				paraStart = -1
			}
		} else {
			if paraStart < 0 {
				paraStart = li.start
				paraStartLine = lineNum
			}
		}
	}

	if paraStart >= 0 {
		lastLine := len(lines)
		flush(paraStartLine, lastLine, paraStart, lines[lastLine-1].end)
	}

	return nil
}
