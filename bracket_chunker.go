package microfts2

// CRC: crc-BracketChunker.md | Seq: seq-bracket-chunk.md | R307, R309, R310, R311, R312, R313, R314, R315, R316, R317, R318, R319, R320, R321, R322, R323, R324, R617, R618, R619, R620, R621, R622

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// BracketLang defines the lexical rules for one language. R307
// Strings are expressed as scan-restricted bracket groups in Brackets;
// no separate StringDelims field exists.
type BracketLang struct {
	LineComments  []string       // e.g. "//", "#", "--"
	BlockComments [][2]string    // e.g. {{"/*", "*/"}, {"<!--", "-->"}}
	Brackets      []BracketGroup // open/separator/close sets — includes strings and word brackets
}

// BracketGroup defines one set of matching brackets, code or string-like. R309
// Separators are mid-group markers (e.g. "else" between "if"/"end").
//
// AllowedInner controls scanning inside the group (R618):
//
//	nil           → code mode: full scanning, all bracket groups recognized
//	non-nil slice → scan-restricted mode: only Close, Escape, and listed openers
//	                are recognized; everything else is literal text
//	                (use []string{} for pure raw mode with no escape hatches)
//
// AllowedParent restricts where this bracket may be recognized (R619):
//
//	nil           → recognized in any context (default for top-level brackets)
//	non-nil slice → only recognized when scanning is currently inside one of
//	                the listed openers (e.g. "${" inside "`")
//
// Escape (R617) is consumed inside a scan-restricted group along with the byte
// that follows it; empty means no escaping (raw strings).
//
// nil and []string{} are semantically distinct for AllowedInner and AllowedParent (R622).
type BracketGroup struct {
	Open          []string
	Separators    []string
	Close         []string
	Escape        string
	AllowedInner  []string
	AllowedParent []string
}

// isRestricted reports whether this group is in scan-restricted mode.
// R618, R622: nil AllowedInner means code mode; non-nil (even empty) means restricted.
func (g *BracketGroup) isRestricted() bool {
	return g.AllowedInner != nil
}

// Token types for the scanner. R310
const (
	tokComment    = iota // R311
	tokWhitespace        // R312
	tokBracketOpen
	tokBracketClose
	tokBracketSep
	tokText // R315
)

type token struct {
	kind      int
	start     int           // byte offset in content
	end       int           // byte offset past end
	startLine int           // 1-based
	endLine   int           // 1-based
	group     *BracketGroup // for bracket open/close/sep tokens; otherwise nil
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
	tokens := tokenize(content, &bc.lang)
	groups := findGroups(tokens)
	groups = attachLeading(groups, tokens, content)
	return emitChunks(content, groups, yield)
}

// FileChunks reads the file and delegates to Chunks via the shared
// fileChunksByRead helper — supports stat-skip via the old-hash short-circuit.
// R633, R636
func (bc *bracketChunker) FileChunks(path string, old [32]byte, yield func(Chunk) bool) ([32]byte, error) {
	return fileChunksByRead(path, old, bc.Chunks, yield)
}

// GetChunk is the RandomAccessChunker fast path — slices data by line range. R531
func (bc *bracketChunker) GetChunk(path string, data []byte, customData *any, chunk *Chunk) error {
	return sliceByLineRange(data, customData, chunk)
}

// CRC: crc-BracketChunker.md | R641
func (bc *bracketChunker) IsWritable() bool { return true }

// CommentSyntax returns the language's first line-comment delimiter
// (or "" when the language has none) for callers wrapping inline tag
// annotations.
// CRC: crc-BracketChunker.md | R641
func (bc *bracketChunker) CommentSyntax() string {
	if len(bc.lang.LineComments) == 0 {
		return ""
	}
	return bc.lang.LineComments[0]
}

// AppendChunks delegates to appendByRechunkResume so bracket-block boundaries
// (including paragraph extension and leading-comment attachment) are
// recognised across the append boundary via re-chunking from the previous
// last chunk's start through EOF.
// R626, R627, R628, R629, R630, R633
func (bc *bracketChunker) AppendChunks(path string, lastLocator []byte, newBytes []byte, yield func(Chunk) bool) (bool, error) {
	return appendByRechunkResume(path, lastLocator, newBytes, bc.Chunks, yield)
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
	return lo
}

// stackEntry tracks an active bracket group on the mode stack.
type stackEntry struct {
	group  *BracketGroup
	opener string // the actual opener string used (for AllowedParent matching)
}

// scanner is the mode-aware bracket-chunker tokenizer. R620
type scanner struct {
	content []byte
	lang    *BracketLang
	ls      []int
	pos     int
	tokens  []token
	stack   []stackEntry
}

func tokenize(content []byte, lang *BracketLang) []token {
	s := &scanner{content: content, lang: lang, ls: lineIndex(content)}
	for s.pos < len(s.content) {
		s.step()
	}
	return s.tokens
}

func (s *scanner) top() *stackEntry {
	if len(s.stack) == 0 {
		return nil
	}
	return &s.stack[len(s.stack)-1]
}

func (s *scanner) emit(kind, start, end int, g *BracketGroup) {
	if end <= start {
		return
	}
	s.tokens = append(s.tokens, token{
		kind:      kind,
		start:     start,
		end:       end,
		startLine: lineAt(s.ls, start),
		endLine:   lineAt(s.ls, end-1),
		group:     g,
	})
}

func (s *scanner) step() {
	if t := s.top(); t != nil && t.group.isRestricted() {
		s.stepRestricted(t.group)
		return
	}
	s.stepCode()
}

// stepRestricted scans inside a scan-restricted bracket group.
// R617, R618, R620: only the group's own Close, its Escape sequence, and
// openers in AllowedInner are recognized; all other bytes accumulate as
// literal text.
func (s *scanner) stepRestricted(g *BracketGroup) {
	textStart := s.pos
	for s.pos < len(s.content) {
		// Try the group's own Close markers (innermost match wins).
		if _, n := matchAnyAt(s.content, s.pos, g.Close); n > 0 {
			s.emit(tokText, textStart, s.pos, nil)
			s.emit(tokBracketClose, s.pos, s.pos+n, g)
			s.pos += n
			s.stack = s.stack[:len(s.stack)-1]
			return
		}
		// Try the group's Escape sequence: consume escape + next byte as literal text.
		if g.Escape != "" {
			n := len(g.Escape)
			if s.pos+n <= len(s.content) && string(s.content[s.pos:s.pos+n]) == g.Escape {
				s.pos += n
				if s.pos < len(s.content) {
					s.pos++
				}
				continue
			}
		}
		// Try AllowedInner openers.
		if op, inner, n := s.matchAllowedInner(g); n > 0 {
			s.emit(tokText, textStart, s.pos, nil)
			s.emit(tokBracketOpen, s.pos, s.pos+n, inner)
			s.pos += n
			s.stack = append(s.stack, stackEntry{group: inner, opener: op})
			return
		}
		s.pos++
	}
	// Reached EOF inside restricted group — flush trailing text.
	s.emit(tokText, textStart, s.pos, nil)
}

// matchAllowedInner finds the first opener listed in g.AllowedInner that
// matches at the current position, returning the opener string, the bracket
// group that owns it, and the matched length.
func (s *scanner) matchAllowedInner(g *BracketGroup) (string, *BracketGroup, int) {
	for _, op := range g.AllowedInner {
		owner := s.findGroupByOpen(op)
		if owner == nil {
			continue
		}
		if matchBracketAt(s.content, s.pos, op) {
			return op, owner, len(op)
		}
	}
	return "", nil, 0
}

func (s *scanner) findGroupByOpen(op string) *BracketGroup {
	for i := range s.lang.Brackets {
		g := &s.lang.Brackets[i]
		if slices.Contains(g.Open, op) {
			return g
		}
	}
	return nil
}

// stepCode scans in code mode (full lexical scanning). R310, R311, R312, R313, R314, R315
func (s *scanner) stepCode() {
	// Whitespace run. R312
	if isWS(s.content[s.pos]) {
		start := s.pos
		for s.pos < len(s.content) && isWS(s.content[s.pos]) {
			s.pos++
		}
		s.emit(tokWhitespace, start, s.pos, nil)
		return
	}
	// Line comments. R311
	if end := tryLineComment(s.content, s.pos, s.lang.LineComments); end > s.pos {
		s.emit(tokComment, s.pos, end, nil)
		s.pos = end
		return
	}
	// Block comments. R311
	if end := tryBlockComment(s.content, s.pos, s.lang.BlockComments); end > s.pos {
		s.emit(tokComment, s.pos, end, nil)
		s.pos = end
		return
	}
	// Brackets — opens (subject to AllowedParent), then top's close, then top's separators.
	if s.tryCodeBracket() {
		return
	}
	// Text: contiguous non-whitespace, stopping when a recognized token would start.
	start := s.pos
	for s.pos < len(s.content) && !isWS(s.content[s.pos]) {
		if end := tryLineComment(s.content, s.pos, s.lang.LineComments); end > s.pos {
			break
		}
		if end := tryBlockComment(s.content, s.pos, s.lang.BlockComments); end > s.pos {
			break
		}
		if s.peekCodeBracket() {
			break
		}
		s.pos++
	}
	s.emit(tokText, start, s.pos, nil)
}

// tryCodeBracket attempts to recognize a bracket open/close/sep at s.pos in
// code mode. Returns true if it consumed a bracket.
// codeMatch describes a recognized code-mode bracket marker at s.pos.
type codeMatch struct {
	kind   int           // tokBracketOpen, tokBracketClose, or tokBracketSep
	group  *BracketGroup // the group the marker belongs to
	marker string        // the matched string
	push   bool          // open → push group onto stack
	pop    bool          // top-of-stack close → pop after emit
}

// findCodeBracket scans for the first bracket marker recognized at s.pos in
// code mode. The lookup order encodes operator precedence:
//  1. Opens of any group whose AllowedParent permits the current stack top
//  2. Top-of-stack's own close markers (innermost match), then separators
//  3. Fallback: any code-mode group's close, so depth tracking stays
//     consistent even when the stack is unbalanced (stray "}" at top level)
func (s *scanner) findCodeBracket() (codeMatch, bool) {
	for i := range s.lang.Brackets {
		g := &s.lang.Brackets[i]
		if !s.parentAllowed(g) {
			continue
		}
		for _, op := range g.Open {
			if matchBracketAt(s.content, s.pos, op) {
				return codeMatch{kind: tokBracketOpen, group: g, marker: op, push: true}, true
			}
		}
	}
	if t := s.top(); t != nil {
		for _, cl := range t.group.Close {
			if matchBracketAt(s.content, s.pos, cl) {
				return codeMatch{kind: tokBracketClose, group: t.group, marker: cl, pop: true}, true
			}
		}
		for _, sep := range t.group.Separators {
			if matchBracketAt(s.content, s.pos, sep) {
				return codeMatch{kind: tokBracketSep, group: t.group, marker: sep}, true
			}
		}
	}
	for i := range s.lang.Brackets {
		g := &s.lang.Brackets[i]
		if g.isRestricted() {
			continue
		}
		for _, cl := range g.Close {
			if matchBracketAt(s.content, s.pos, cl) {
				return codeMatch{kind: tokBracketClose, group: g, marker: cl}, true
			}
		}
	}
	return codeMatch{}, false
}

func (s *scanner) tryCodeBracket() bool {
	m, ok := s.findCodeBracket()
	if !ok {
		return false
	}
	end := s.pos + len(m.marker)
	s.emit(m.kind, s.pos, end, m.group)
	s.pos = end
	switch {
	case m.push:
		s.stack = append(s.stack, stackEntry{group: m.group, opener: m.marker})
	case m.pop:
		s.stack = s.stack[:len(s.stack)-1]
	}
	return true
}

// peekCodeBracket reports whether tryCodeBracket would match at s.pos without
// consuming. Used to terminate text runs.
func (s *scanner) peekCodeBracket() bool {
	_, ok := s.findCodeBracket()
	return ok
}

// parentAllowed checks AllowedParent against the current stack top.
// R619: nil AllowedParent means recognized anywhere; non-nil restricts to
// scans currently inside one of the listed openers.
func (s *scanner) parentAllowed(g *BracketGroup) bool {
	if g.AllowedParent == nil {
		return true
	}
	t := s.top()
	if t == nil {
		return false
	}
	return slices.Contains(g.AllowedParent, t.opener)
}

// matchAnyAt tries each marker at pos; returns the first match string and length.
func matchAnyAt(content []byte, pos int, markers []string) (string, int) {
	for _, m := range markers {
		if matchBracketAt(content, pos, m) {
			return m, len(m)
		}
	}
	return "", 0
}

func isWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func tryLineComment(content []byte, pos int, markers []string) int {
	for _, m := range markers {
		if pos+len(m) <= len(content) && string(content[pos:pos+len(m)]) == m {
			end := bytes.IndexByte(content[pos:], '\n')
			if end < 0 {
				return len(content)
			}
			return pos + end + 1
		}
	}
	return pos
}

func tryBlockComment(content []byte, pos int, markers [][2]string) int {
	for _, m := range markers {
		open, close := m[0], m[1]
		if pos+len(open) <= len(content) && string(content[pos:pos+len(open)]) == open {
			idx := bytes.Index(content[pos+len(open):], []byte(close))
			if idx < 0 {
				return len(content)
			}
			return pos + len(open) + idx + len(close)
		}
	}
	return pos
}

// matchBracketAt checks if bracket b occurs at pos in content.
// For word brackets (alphanumeric), requires word boundaries. R313
func matchBracketAt(content []byte, pos int, b string) bool {
	if pos+len(b) > len(content) {
		return false
	}
	if string(content[pos:pos+len(b)]) != b {
		return false
	}
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

// findGroups walks the token stream and identifies bracket groups.
// R316, R621: only code-mode brackets (group.isRestricted() == false)
// contribute to depth — strings and other scan-restricted spans never
// open a chunk group.
func findGroups(tokens []token) []groupSpan {
	type lineState struct {
		hasOpen  bool
		endDepth int
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
	lines := make([]lineState, maxLine+1)
	depth := 0
	for _, t := range tokens {
		switch t.kind {
		case tokBracketOpen:
			if t.group != nil && t.group.isRestricted() {
				continue // string-like brackets don't count toward chunk depth (R621)
			}
			if depth == 0 {
				lines[t.startLine].hasOpen = true
			}
			depth++
		case tokBracketClose:
			if t.group != nil && t.group.isRestricted() {
				continue
			}
			if depth > 0 {
				depth--
			}
		}
		lines[t.endLine].endDepth = depth
	}

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
	if inGroup {
		groups = append(groups, groupSpan{groupStart, maxLine})
	}
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
	maxLine := tokens[len(tokens)-1].endLine
	lineHasBlank := make([]bool, maxLine+1)

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
	type lineBounds struct {
		start, end int
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

	gi := 0
	paraStart := -1
	paraStartLine := 0

	flush := func(startLine, endLine, startByte, endByte int) bool {
		if startLine < 0 {
			return true
		}
		r := fmt.Sprintf("%d-%d", startLine, endLine)
		loc := EncodeByteRangeLocator(startByte, endByte)
		return yield(Chunk{Range: []byte(r), Locator: loc, Content: content[startByte:endByte]})
	}

	for lineIdx, lb := range lines {
		lineNum := lineIdx + 1

		if gi < len(groups) && lineNum == groups[gi].startLine {
			if paraStart >= 0 {
				if !flush(paraStartLine, lineNum-1, paraStart, lines[lineIdx-1].end) {
					return nil
				}
				paraStart = -1
			}
			gEnd := min(groups[gi].endLine, len(lines))
			if !flush(groups[gi].startLine, gEnd, lb.start, lines[gEnd-1].end) {
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

		blank := isBlankLine(content[lb.start:lb.end])
		if blank {
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
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
		{Open: []string{`"`}, Close: []string{`"`}, Escape: `\`, AllowedInner: []string{}},
		{Open: []string{"`"}, Close: []string{"`"}, AllowedInner: []string{}},
		{Open: []string{"'"}, Close: []string{"'"}, Escape: `\`, AllowedInner: []string{}},
	},
}

// LangC is the bracket language config for C/C++.
var LangC = BracketLang{
	LineComments:  []string{"//"},
	BlockComments: [][2]string{{"/*", "*/"}},
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
		{Open: []string{`"`}, Close: []string{`"`}, Escape: `\`, AllowedInner: []string{}},
		{Open: []string{"'"}, Close: []string{"'"}, Escape: `\`, AllowedInner: []string{}},
	},
}

// LangJava is the bracket language config for Java.
var LangJava = LangC

// LangJS is the bracket language config for JavaScript.
// Backquote template literals permit ${...} interpolation back to code mode.
var LangJS = BracketLang{
	LineComments:  []string{"//"},
	BlockComments: [][2]string{{"/*", "*/"}},
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
		{Open: []string{`"`}, Close: []string{`"`}, Escape: `\`, AllowedInner: []string{}},
		{Open: []string{"'"}, Close: []string{"'"}, Escape: `\`, AllowedInner: []string{}},
		{Open: []string{"`"}, Close: []string{"`"}, Escape: `\`, AllowedInner: []string{"${"}},
		{Open: []string{"${"}, Close: []string{"}"}, AllowedParent: []string{"`"}},
	},
}

// LangLisp is the bracket language config for Lisp/Scheme/Clojure.
var LangLisp = BracketLang{
	LineComments: []string{";"},
	Brackets: []BracketGroup{
		{Open: []string{"("}, Close: []string{")"}},
		{Open: []string{"["}, Close: []string{"]"}},
		{Open: []string{`"`}, Close: []string{`"`}, Escape: `\`, AllowedInner: []string{}},
	},
}

// LangNginx is the bracket language config for nginx.
var LangNginx = BracketLang{
	LineComments: []string{"#"},
	Brackets: []BracketGroup{
		{Open: []string{"{"}, Close: []string{"}"}},
		{Open: []string{`"`}, Close: []string{`"`}, Escape: `\`, AllowedInner: []string{}},
		{Open: []string{"'"}, Close: []string{"'"}, AllowedInner: []string{}},
	},
}

// LangPascal is the bracket language config for Pascal.
var LangPascal = BracketLang{
	BlockComments: [][2]string{{"{", "}"}, {"(*", "*)"}},
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
		{Open: []string{"'"}, Close: []string{"'"}, AllowedInner: []string{}},
	},
}

// LangShell is the bracket language config for Bourne shell / bash.
var LangShell = BracketLang{
	LineComments: []string{"#"},
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
		{Open: []string{`"`}, Close: []string{`"`}, Escape: `\`, AllowedInner: []string{}},
		{Open: []string{"'"}, Close: []string{"'"}, AllowedInner: []string{}},
	},
}

// langRegistry maps CLI language names to configs. R321
var langRegistry = map[string]BracketLang{
	"go":     LangGo,
	"c":      LangC,
	"cpp":    LangC,
	"java":   LangJava,
	"js":     LangJS,
	"lisp":   LangLisp,
	"nginx":  LangNginx,
	"pascal": LangPascal,
	"shell":  LangShell,
	"bash":   LangShell,
}

// LangByName returns a BracketLang config by name, or false if not found.
func LangByName(name string) (BracketLang, bool) {
	lang, ok := langRegistry[strings.ToLower(name)]
	return lang, ok
}
