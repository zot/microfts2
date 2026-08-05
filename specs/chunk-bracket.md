# Bracket Chunker

A configurable chunker that groups program text into chunks based on bracket structure. Table-driven — adding a new language means adding a config entry, not code. Works for Go, Java, C, JavaScript, Lisp, nginx, Pascal, Julia, Bourne shell, and other bracket-delimited or word-delimited languages. Pascal and shell work because `begin`/`end` and `do`/`done` are brackets even though they don't look like traditional ones.

## Token types

The chunker scans content into a stream of tokens. Each token type is configurable per language:

- **comment**: `//...`, `/* ... */`, `#...`, `--...`, etc. Comment syntax varies by language. Comments inside strings are not comments. Nesting rules are per-language (most don't nest).
- **whitespace**: contiguous runs of space, newline, tab, carriage return, form feed. Always recognized, not configurable.
- **bracket**: `(`, `)`, `{`, `}`, `"..."`, `'...'`, `` `...` ``, `[[...]]`, `<!--`, `-->`, `begin`, `end`, etc. Strings, code brackets, and word brackets are all expressed as bracket groups — string-vs-bracket is a continuum, not a dichotomy (see "Bracket modes" below). Multi-character and word brackets are supported. Multi-bracket groups are allowed: `if`..`then`..`else`..`end`, `while`..`do`..`done`. Each group defines its opener(s), separator(s), and closer.
- **text**: any other contiguous non-whitespace characters.

## Chunk definition

A chunk is a **group** or a **paragraph**:

- **Group**: line-oriented. A group starts at the line containing an open bracket (not inside a comment) and continues line by line until all brackets are closed. Depth is tracked across all bracket types — `func f() {` is one group start because the parens open and close mid-line but the brace keeps depth above zero at end of line. Groups on a single line (e.g. `f()`) are not chunks. Leading comment/text lines immediately before the group (no blank line separating them) attach to the group. Groups whose opener is a string-mode bracket (e.g. `"..."`) do not by themselves start chunk groups — the chunker only counts code-mode brackets toward chunk depth. (Implementation detail: in scan-restricted brackets, depth counting is suppressed so a quoted string isn't a "group".)
- **Paragraph**: a sequence of lines not inside a group, terminated by a blank line or the start of a group. Top-level text between groups.

Range labels are `startline-endline` (1-based), consistent with other chunkers.

## Bracket modes

`BracketGroup` unifies traditional code brackets (full scanning between open and close) and string-like delimiters (suppressed scanning between open and close, with optional escape characters and optional re-entry into code via named inner brackets). The `AllowedInner` field carries the distinction:

- `AllowedInner == nil`: **code mode**. Full scanning happens inside — every comment, every other bracket group, and every text run is recognized normally. This is the default for traditional brackets like `{`, `(`, `[`.
- `AllowedInner != nil` (any non-nil slice, including empty): **scan-restricted mode**. Inside the group, the scanner recognizes *only* three things:
  1. The group's own `Close` markers (which end the group),
  2. The group's `Escape` sequence, if `Escape != ""` (which consumes the escape and the next character as literal),
  3. The openers listed in `AllowedInner` (which open a nested group, returning the scanner to whatever mode that group declares).

  Nothing else is tokenized. Comments, other strings, other brackets, whitespace runs — all become literal bytes inside the group's content. Empty list means "no escape hatches" — pure raw mode (Go backtick raw strings, traditional `"..."` strings). Non-empty list means "code mode is reachable through these openers" (JavaScript template literals: `` `text ${expr} more` ``).

`AllowedParent` is the dual: a scan-restricted bracket may declare which parent groups can contain it. The `${`...`}` interpolation bracket is only recognized inside `` ` ` `` template literals; it lists the backtick group as an allowed parent so it is not mistakenly recognized at top level (where `${` is just a `$` followed by a `{`).

Together, `AllowedInner` and `AllowedParent` define a bracket-mode graph: each restricted bracket explicitly names its escape hatches and its valid contexts.

## Language configuration

```go
// BracketLang defines the lexical rules for one language.
type BracketLang struct {
    LineComments  []string       // e.g. "//", "#", "--"
    BlockComments [][2]string    // e.g. {{"/*", "*/"}, {"<!--", "-->"}}
    Brackets      []BracketGroup // open/separator/close sets — includes strings and word brackets
}

// BracketGroup defines one set of matching brackets, code or string-like.
// Separators are mid-group markers (e.g. "else" between "if"/"end").
type BracketGroup struct {
    Open       []string // openers: e.g. ["{"], ["if","while","for"], [`"`]
    Separators []string // optional: e.g. ["else","elif","then"]
    Close      []string // closers: e.g. ["}"], ["end","done","fi"], [`"`]
    Escape     string   // escape character inside the group (empty = no escaping)

    // AllowedInner controls scanning inside the group:
    //   nil           → code mode: full scanning, all bracket groups recognized
    //   non-nil slice → scan-restricted mode: only Close, Escape, and the
    //                   listed openers are recognized; everything else is text
    //                   (use []string{} for pure raw mode with no escape hatches)
    AllowedInner []string

    // AllowedParent restricts where this bracket may be recognized:
    //   nil           → recognized in any context (default for top-level brackets)
    //   non-nil slice → only recognized when scanning is currently inside one of
    //                   the listed openers
    AllowedParent []string
}
```

Notes:

- `nil` and `[]string{}` are semantically distinct for `AllowedInner` and `AllowedParent`. `nil` means "no restriction"; an empty (but non-nil) slice means "restriction with an empty list." Users should be deliberate about which they write.
- A traditional symmetric string (e.g. C `"..."`): `Open: ["\""]`, `Close: ["\""]`, `Escape: "\\"`, `AllowedInner: []string{}`.
- A raw string (e.g. Go backticks): `Open: ["`"]`, `Close: ["`"]`, `Escape: ""`, `AllowedInner: []string{}`.
- A JavaScript template literal: `Open: ["`"]`, `Close: ["`"]`, `Escape: "\\"`, `AllowedInner: []string{"${"}`.
- A JavaScript `${` interpolation: `Open: ["${"]`, `Close: ["}"]`, `AllowedParent: []string{"`"}` (recognized only inside backquote groups; everywhere else `${` is plain text).
- A code bracket (e.g. `{`): `Open: ["{"]`, `Close: ["}"]`. `AllowedInner` and `AllowedParent` left nil.

Built-in language configs are provided as package-level variables (e.g. `LangGo`, `LangC`, `LangPython`). Users can construct custom `BracketLang` values.

## Library API

```go
// BracketChunker returns a Chunker for the given language config.
func BracketChunker(lang BracketLang) Chunker
```

Returns a chunker that implements the full text-chunker quartet: `Chunker`, `FileChunker` (the standard read-file-and-delegate wrapper — reads the file, computes SHA-256, skips chunking when the supplied old hash matches), `RandomAccessChunker` (fast-path retrieval via the byte-range locator), and `AppendAwareChunker` (boundary-aware appends — re-chunks from the previous last chunk's byte start through end-of-file; the first emitted chunk's byte range determines whether the previous last chunk is preserved as-is or replaced).

## CLI

```
microfts chunk-bracket -lang <name> <file>
```

Output: one chunk per stdout line as `range\tcontent` (tab-separated), matching the external chunker protocol. Available language names come from the built-in configs.
