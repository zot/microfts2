package microfts2

// CRC: crc-BracketChunker.md | Seq: seq-bracket-chunk.md
// R307, R308, R309, R310, R311, R312, R313, R314, R315, R316, R317, R318, R319, R320, R321, R322, R323, R324

import (
	"bytes"
	"fmt"
	"strings"
)

// BracketLang defines the lexical rules for one language. R307
type BracketLang struct {
	LineComments  []string       // e.g. "//", "#", "--"
	BlockComments [][2]string    // e.g. {{"/*", "*/"}, {"<!--", "-->"}}
	StringDelims  []StringDelim  // e.g. {`"`, `"`, `\`}
	Brackets      []BracketGroup // open/separator/close sets
}

// StringDelim defines a string delimiter and its escape character. R308
type StringDelim struct {
	Open   string // opening delimiter
	Close  string // closing delimiter (same as Open for symmetric quotes)
	Escape string // escape character (empty = no escaping)
}

// BracketGroup defines one set of matching brackets. R309
// Separators are mid-group markers (e.g. "else" between "if"/"end").
type BracketGroup struct {
	Open       []string // openers: e.g. ["{"], ["if","while","for"]
	Separators []string // optional: e.g. ["else","elif","then"]
	Close      []string // closers: e.g. ["}"], ["end","done","fi"]
}

// Token types for the scanner. R310
const (
	tokComment    = iota // R311: comments inside strings are not comments
	tokString            // R311: strings inside comments are not strings
	tokWhitespace        // R312: contiguous whitespace runs
	tokBracketOpen
	tokBracketClose
	tokBracketSep
	tokText // R315: any other contiguous non-whitespace
)

type token struct {
	kind      int
	start     int // byte offset in content
	end       int // byte offset past end
	startLine int // 1-based
	endLine   int // 1-based
}

// bracketChunker implements Chunker for bracket-delimited languages. R320
type bracketChunker struct {
	lang BracketLang
}

// BracketChunker returns a Chunker for the given language config. R320
func BracketChunker(lang BracketLang) Chunker {
	return &bracketChunker{lang: lang}
}

func (bc *bracketChunker) Chunks(path string, content []byte, yield func(Chunk) bool) error {
	if len(content) == 0 {
		return nil
	}
	tokens := tokenize(content, bc.lang)
	groups := findGroups(tokens)
	groups = attachLeading(groups, tokens, content)
	return emitChunks(content, groups, yield)
}

func (bc *bracketChunker) ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool) {
	return chunkTextByRange(bc, path, content, rangeLabel)
}

// lineIndex builds a byte-offset-to-line-number lookup.
// Returns lineStarts where lineStarts[i] is the byte offset of line i+1.
func lineIndex(content []byte) []int {
	starts := []int{0}
	for i, b := range content {
		if b == '\n' && i+1 < len(content) {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineAt returns the 1-based line number for byte offset pos.
func lineAt(lineStarts []int, pos int) int {
	lo, hi := 0, len(lineStarts)
	for lo < hi {
		mid := (lo + hi) / 2
		if lineStarts[mid] <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo // 1-based: lineStarts[lo-1] <= pos < lineStarts[lo]
}

// tokenize scans content into a token stream. R310, R311
func tokenize(content []byte, lang BracketLang) []token {
	ls := lineIndex(content)
	var tokens []token
	pos := 0

	for pos < len(content) {
		// Whitespace run. R312
		if isWS(content[pos]) {
			start := pos
			for pos < len(content) && isWS(content[pos]) {
				pos++
			}
			tokens = append(tokens, token{tokWhitespace, start, pos, lineAt(ls, start), lineAt(ls, pos - 1)})
			continue
		}

		// Line comments. R311
		if tok, end := tryLineComment(content, pos, lang.LineComments); end > pos {
			tokens = append(tokens, token{tok, pos, end, lineAt(ls, pos), lineAt(ls, end - 1)})
			pos = end
			continue
		}

		// Block comments. R311
		if tok, end := tryBlockComment(content, pos, lang.BlockComments); end > pos {
			tokens = append(tokens, token{tok, pos, end, lineAt(ls, pos), lineAt(ls, end - 1)})
			pos = end
			continue
		}

		// Strings. R311
		if tok, end := tryString(content, pos, lang.StringDelims); end > pos {
			tokens = append(tokens, token{tok, pos, end, lineAt(ls, pos), lineAt(ls, end - 1)})
			pos = end
			continue
		}

		// Brackets (open, separator, close). R313, R314
		if tok, end := tryBracket(content, pos, lang.Brackets); end > pos {
			tokens = append(tokens, token{tok, pos, end, lineAt(ls, pos), lineAt(ls, end - 1)})
			pos = end
			continue
		}

		// Text: contiguous non-whitespace. R315
		start := pos
		for pos < len(content) && !isWS(content[pos]) {
			// Stop if the next position would match a comment, string, or bracket
			if _, end := tryLineComment(content, pos, lang.LineComments); end > pos {
				break
			}
			if _, end := tryBlockComment(content, pos, lang.BlockComments); end > pos {
				break
			}
			if _, end := tryString(content, pos, lang.StringDelims); end > pos {
				break
			}
			if _, end := tryBracket(content, pos, lang.Brackets); end > pos {
				break
			}
			pos++
		}
		if pos > start {
			tokens = append(tokens, token{tokText, start, pos, lineAt(ls, start), lineAt(ls, pos - 1)})
		}
	}

	return tokens
}

func isWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func tryLineComment(content []byte, pos int, markers []string) (int, int) {
	for _, m := range markers {
		if pos+len(m) <= len(content) && string(content[pos:pos+len(m)]) == m {
			end := bytes.IndexByte(content[pos:], '\n')
			if end < 0 {
				return tokComment, len(content)
			}
			return tokComment, pos + end + 1
		}
	}
	return 0, pos
}

func tryBlockComment(content []byte, pos int, markers [][2]string) (int, int) {
	for _, m := range markers {
		open, close := m[0], m[1]
		if pos+len(open) <= len(content) && string(content[pos:pos+len(open)]) == open {
			idx := bytes.Index(content[pos+len(open):], []byte(close))
			if idx < 0 {
				return tokComment, len(content) // unclosed — consume rest
			}
			return tokComment, pos + len(open) + idx + len(close)
		}
	}
	return 0, pos
}

func tryString(content []byte, pos int, delims []StringDelim) (int, int) {
	for _, d := range delims {
		if pos+len(d.Open) <= len(content) && string(content[pos:pos+len(d.Open)]) == d.Open {
			closer := d.Close
			if closer == "" {
				closer = d.Open
			}
			i := pos + len(d.Open)
			for i < len(content) {
				if d.Escape != "" && i+len(d.Escape) <= len(content) && string(content[i:i+len(d.Escape)]) == d.Escape {
					i += len(d.Escape) + 1 // skip escaped char
					continue
				}
				if i+len(closer) <= len(content) && string(content[i:i+len(closer)]) == closer {
					return tokString, i + len(closer)
				}
				i++
			}
			return tokString, len(content) // unclosed
		}
	}
	return 0, pos
}

// tryBracket checks for word or symbol brackets at pos. R313
// Word brackets only match at word boundaries.
func tryBracket(content []byte, pos int, groups []BracketGroup) (int, int) {
	for _, g := range groups {
		for _, op := range g.Open {
			if matchBracketAt(content, pos, op) {
				return tokBracketOpen, pos + len(op)
			}
		}
		for _, sep := range g.Separators {
			if matchBracketAt(content, pos, sep) {
				return tokBracketSep, pos + len(sep)
			}
		}
		for _, cl := range g.Close {
			if matchBracketAt(content, pos, cl) {
				return tokBracketClose, pos + len(cl)
			}
		}
	}
	return 0, pos
}

// matchBracketAt checks if bracket b occurs at pos in content.
// For word brackets (alphanumeric), requires word boundaries.
func matchBracketAt(content []byte, pos int, b string) bool {
	if pos+len(b) > len(content) {
		return false
	}
	if string(content[pos:pos+len(b)]) != b {
		return false
	}
	// Word brackets need word boundary check
	if isWordChar(b[0]) {
		if pos > 0 && isWordChar(content[pos-1]) {
			return false
		}
		end := pos + len(b)
		if end < len(content) && isWordChar(content[end]) {
			return false
		}
	}
	return true
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// groupSpan tracks a bracket group's line range.
type groupSpan struct {
	startLine int
	endLine   int
}

// findGroups walks the token stream and identifies bracket groups. R316
// Line-oriented: a group starts at the line containing an open bracket and
// continues line by line until all brackets are closed. Depth is only checked
// at line boundaries, so "func f() {" is one group start — the parens open
// and close mid-line, but the brace keeps depth > 0 at end of line.
func findGroups(tokens []token) []groupSpan {
	// Build per-line depth deltas from the token stream.
	// For each line, track whether it contains any bracket open (while not in group)
	// and what the depth is at end of line.
	type lineState struct {
		hasOpen bool // line contains an open bracket while depth was 0
		endDepth int // depth at end of this line
	}

	maxLine := 0
	for _, t := range tokens {
		if t.endLine > maxLine {
			maxLine = t.endLine
		}
	}
	if maxLine == 0 {
		return nil
	}

	// Process tokens, tracking depth and which lines have bracket opens
	lines := make([]lineState, maxLine+1) // 1-indexed
	depth := 0
	for _, t := range tokens {
		switch t.kind {
		case tokBracketOpen:
			if depth == 0 {
				lines[t.startLine].hasOpen = true
			}
			depth++
		case tokBracketClose:
			if depth > 0 {
				depth--
			}
		}
		lines[t.endLine].endDepth = depth
	}

	// Walk lines to build groups
	var groups []groupSpan
	inGroup := false
	groupStart := 0

	for lineNum := 1; lineNum <= maxLine; lineNum++ {
		ls := lines[lineNum]
		if !inGroup && ls.hasOpen {
			groupStart = lineNum
			inGroup = true
		}
		if inGroup && ls.endDepth == 0 {
			groups = append(groups, groupSpan{groupStart, lineNum})
			inGroup = false
		}
	}

	// Unclosed bracket — group extends to EOF
	if inGroup {
		groups = append(groups, groupSpan{groupStart, maxLine})
	}

	// Filter single-line groups — e.g. "x = f()" on one line
	var filtered []groupSpan
	for _, g := range groups {
		if g.endLine > g.startLine {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

// attachLeading extends group starts to include preceding comment/text lines. R317
func attachLeading(groups []groupSpan, tokens []token, content []byte) []groupSpan {
	if len(groups) == 0 {
		return groups
	}

	// Build a line→content-type map: for each line, what kind of tokens are on it
	maxLine := tokens[len(tokens)-1].endLine
	lineHasBlank := make([]bool, maxLine+1)

	// Mark blank lines
	lineNum := 0
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
		lineNum++
		if isBlankLine(content[lineStart:lineEnd]) {
			if lineNum <= maxLine {
				lineHasBlank[lineNum] = true
			}
		}
		pos = lineEnd
	}

	// For each group, scan backward to attach leading lines
	for i := range groups {
		minLine := 1
		if i > 0 {
			minLine = groups[i-1].endLine + 1
		}
		for line := groups[i].startLine - 1; line >= minLine; line-- {
			if lineHasBlank[line] {
				break
			}
			groups[i].startLine = line
		}
	}
	return groups
}

// emitChunks walks content line by line, emitting group and paragraph chunks. R316, R318, R319
func emitChunks(content []byte, groups []groupSpan, yield func(Chunk) bool) error {
	// Build line boundaries
	type lineBounds struct {
		start, end int // byte offsets
	}
	var lines []lineBounds
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
		lines = append(lines, lineBounds{lineStart, lineEnd})
		pos = lineEnd
	}

	gi := 0 // current group index
	paraStart := -1
	paraStartLine := 0

	flush := func(startLine, endLine, startByte, endByte int) bool {
		if startLine < 0 {
			return true
		}
		r := fmt.Sprintf("%d-%d", startLine, endLine)
		return yield(Chunk{Range: []byte(r), Content: content[startByte:endByte]})
	}

	for lineIdx, lb := range lines {
		lineNum := lineIdx + 1

		// Check if this line starts a group
		if gi < len(groups) && lineNum == groups[gi].startLine {
			// Flush any pending paragraph
			if paraStart >= 0 {
				if !flush(paraStartLine, lineNum-1, paraStart, lines[lineIdx-1].end) {
					return nil
				}
				paraStart = -1
			}
			// Emit the group chunk
			gEnd := min(groups[gi].endLine, len(lines))
			if !flush(groups[gi].startLine, gEnd, lb.start, lines[gEnd-1].end) {
				return nil
			}
			gi++
			continue
		}

		// Skip lines inside current group
		if gi > 0 && lineNum <= groups[gi-1].endLine {
			continue
		}
		if gi < len(groups) && lineNum >= groups[gi].startLine && lineNum <= groups[gi].endLine {
			continue
		}

		blank := isBlankLine(content[lb.start:lb.end])
		if blank {
			// Flush paragraph
			if paraStart >= 0 {
				if !flush(paraStartLine, lineNum-1, paraStart, lines[lineIdx-1].end) {
					return nil
				}
				paraStart = -1
			}
		} else {
			if paraStart < 0 {
				paraStart = lb.start
				paraStartLine = lineNum
			}
		}
	}

	// Flush trailing paragraph
	if paraStart >= 0 {
		lastLine := len(lines)
		flush(paraStartLine, lastLine, paraStart, lines[lastLine-1].end)
	}

	return nil
}

// --- Built-in language configs --- R321, R322

// LangGo is the bracket language config for Go.
var LangGo = BracketLang{
	LineComments:  []string{"//"},
	BlockComments: [][2]string{{"/*", "*/"}},
	StringDelims: []StringDelim{
		{Open: `"`, Close: `"`, Escape: `\`},
		{Open: "`", Close: "`"},
		{Open: "'", Close: "'", Escape: `\`},
	},
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
	},
}

// LangC is the bracket language config for C/C++.
var LangC = BracketLang{
	LineComments:  []string{"//"},
	BlockComments: [][2]string{{"/*", "*/"}},
	StringDelims: []StringDelim{
		{Open: `"`, Close: `"`, Escape: `\`},
		{Open: "'", Close: "'", Escape: `\`},
	},
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
	},
}

// LangJava is the bracket language config for Java.
var LangJava = LangC

// LangJS is the bracket language config for JavaScript.
var LangJS = BracketLang{
	LineComments:  []string{"//"},
	BlockComments: [][2]string{{"/*", "*/"}},
	StringDelims: []StringDelim{
		{Open: `"`, Close: `"`, Escape: `\`},
		{Open: "'", Close: "'", Escape: `\`},
		{Open: "`", Close: "`", Escape: `\`},
	},
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
	},
}

// LangLisp is the bracket language config for Lisp/Scheme/Clojure.
var LangLisp = BracketLang{
	LineComments: []string{";"},
	StringDelims: []StringDelim{
		{Open: `"`, Close: `"`, Escape: `\`},
	},
	Brackets: []BracketGroup{
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
	},
}

// LangNginx is the bracket language config for nginx.
var LangNginx = BracketLang{
	LineComments: []string{"#"},
	StringDelims: []StringDelim{
		{Open: `"`, Close: `"`, Escape: `\`},
		{Open: "'", Close: "'"},
	},
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
	},
}

// LangPascal is the bracket language config for Pascal.
var LangPascal = BracketLang{
	BlockComments: [][2]string{{"{", "}"}, {"(*", "*)"}},
	StringDelims: []StringDelim{
		{Open: "'", Close: "'"},
	},
	Brackets: []BracketGroup{
		{
			Open:       []string{"begin", "record", "class"},
			Separators: []string{},
			Close:      []string{"end"},
		},
		{
			Open:       []string{"if"},
			Separators: []string{"then", "else"},
			Close:      []string{"end"},
		},
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
	},
}

// LangShell is the bracket language config for Bourne shell / bash.
var LangShell = BracketLang{
	LineComments: []string{"#"},
	StringDelims: []StringDelim{
		{Open: `"`, Close: `"`, Escape: `\`},
		{Open: "'", Close: "'"},
	},
	Brackets: []BracketGroup{
		{
			Open:       []string{"if"},
			Separators: []string{"then", "elif", "else"},
			Close:      []string{"fi"},
		},
		{
			Open:       []string{"while", "for"},
			Separators: []string{"do"},
			Close:      []string{"done"},
		},
		{
			Open:  []string{"case"},
			Close: []string{"esac"},
		},
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{"("}, Close: []string{")"}},
	},
}

// langRegistry maps CLI language names to configs. R321
var langRegistry = map[string]BracketLang{
	"go":      LangGo,
	"c":       LangC,
	"cpp":     LangC,
	"java":    LangJava,
	"js":      LangJS,
	"lisp":    LangLisp,
	"nginx":   LangNginx,
	"pascal":  LangPascal,
	"shell":   LangShell,
	"bash":    LangShell,
}

// LangByName returns a BracketLang config by name, or false if not found.
func LangByName(name string) (BracketLang, bool) {
	lang, ok := langRegistry[strings.ToLower(name)]
	return lang, ok
}
