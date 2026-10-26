# Test Design: BracketChunker
**Source:** crc-BracketChunker.md

## Test: Go basic
**Purpose:** a Go file splits into paragraph and group chunks
**Input:** `package main`, imports, and a function
**Expected:** at least 3 chunks; the first is the `package main` paragraph, range `1-1`
**Refs:** crc-BracketChunker.md, R316, R318, R319
**Code:** bracket_chunker_test.go

## Test: comment inside a string
**Purpose:** `//` inside a string literal is string text, not a comment
**Input:** `x = "// not a comment"` followed by a code line
**Expected:** the string line is chunked as code, not dropped as a comment
**Refs:** crc-BracketChunker.md, R311
**Code:** bracket_chunker_test.go

## Test: nested brackets
**Purpose:** a nested group is part of its outer group
**Input:** a function containing an `if` block
**Expected:** one chunk spanning the whole function
**Refs:** crc-BracketChunker.md, R316
**Code:** bracket_chunker_test.go

## Test: paragraph terminated by group start
**Purpose:** a paragraph ends at a blank line or the start of a group
**Input:** `package main`, two `var` lines, a function
**Expected:** paragraph and group are separate chunks
**Refs:** crc-BracketChunker.md, R318
**Code:** bracket_chunker_test.go

## Test: leading comment attaches to group
**Purpose:** comment lines immediately before an opener belong to the group
**Input:** a doc comment directly above a function
**Expected:** one chunk, range `1-4`
**Refs:** crc-BracketChunker.md, R317
**Code:** bracket_chunker_test.go

## Test: word brackets
**Purpose:** word tokens act as brackets
**Input:** Pascal `begin` … `end.`
**Expected:** the `begin`/`end` block forms a group chunk
**Refs:** crc-BracketChunker.md, R313
**Code:** bracket_chunker_test.go

## Test: shell if/else/fi is one group
**Purpose:** one group with several word tokens — `if` opens, `else` separates, `fi` closes
**Input:** shebang, blank, `if … then … else … fi` on lines 3–7, blank, `echo "done"`
**Expected:** exactly 3 chunks: `1-1`, `3-7`, `9-9`
**Refs:** crc-BracketChunker.md, R313, R314
**Code:** bracket_chunker_test.go
**Fire alarm:** make `else` close the `if` group (add it to `Close`); red is 4 chunks with the group torn at `3-5` / `6-7`. The earlier `>= 2` assertion passed under this defect
**Inject:** bracket_chunker.go:LangShell
**Pulled:** 2026-10-26 — rang: `expected 3 chunks, got 4`

## Test: multi-line parameters
**Purpose:** multi-line parens followed by a brace form one group, not two
**Input:** `func fred(` with parameters on separate lines, then `) {` … `}`
**Expected:** one chunk, range `1-6`
**Refs:** crc-BracketChunker.md, R316
**Code:** bracket_chunker_test.go

## Test: empty input
**Purpose:** empty content yields nothing
**Input:** empty byte slice
**Expected:** 0 chunks, no error
**Refs:** crc-BracketChunker.md
**Code:** bracket_chunker_test.go

## Test: language by name
**Purpose:** every built-in language config is reachable by name
**Input:** `go`, `c`, `cpp`, `java`, `js`, `lisp`, `nginx`, `pascal`, `shell`, `bash`, and an unknown name
**Expected:** all known names resolve; the unknown name returns false
**Refs:** crc-BracketChunker.md, R321
**Code:** bracket_chunker_test.go

## Test: Go raw string with braces
**Purpose:** braces inside a raw backtick string open no group
**Input:** ``x := `hello { world }` `` then `y := 1`
**Expected:** one paragraph chunk
**Refs:** crc-BracketChunker.md, R618, R621
**Code:** bracket_chunker_test.go

## Test: Go string with brace
**Purpose:** a brace inside `"..."` opens or closes no group
**Input:** `x := "}"` then `y := 1`
**Expected:** one chunk
**Refs:** crc-BracketChunker.md, R621
**Code:** bracket_chunker_test.go

## Test: escaped quote keeps the string open
**Purpose:** the group's `Escape` consumes the escape and the next byte, so `\"` does not close the string
**Input:** `s := "\"{"`, blank, `u := 2`
**Expected:** exactly 2 chunks, `1-1` and `3-3`
**Refs:** crc-BracketChunker.md, R617
**Code:** bracket_chunker_test.go
**Fire alarm:** skip the Escape check in the scan-restricted scanner; the string closes at `\"`, `{` opens a group that never closes, and red is chunk 0 at `1-2`
**Inject:** bracket_chunker.go:scanner.stepRestricted
**Pulled:** 2026-10-26 — rang: `chunk 0 range = 1-2, want 1-1`

## Test: JS template, single-line interpolation
**Purpose:** `${...}` opening and closing on one line forms no group
**Input:** a template literal with a one-line interpolation
**Expected:** one paragraph chunk
**Refs:** crc-BracketChunker.md, R618, R619
**Code:** bracket_chunker_test.go

## Test: JS template, multi-line interpolation
**Purpose:** `${` is a code-mode bracket whose depth crosses lines; the backticks contribute no depth
**Input:** a template literal whose interpolation spans lines 1–5
**Expected:** a group chunk spanning `1-5`
**Refs:** crc-BracketChunker.md, R618, R619
**Code:** bracket_chunker_test.go

## Test: JS `${` at top level
**Purpose:** `${` is recognized only inside a backquote
**Input:** `${` at top level and inside a `"..."` string
**Expected:** one paragraph chunk
**Refs:** crc-BracketChunker.md, R619
**Code:** bracket_chunker_test.go

## Test: string does not recognize brackets
**Purpose:** inside a scan-restricted group the language's other openers are literal text
**Input:** a function containing `s := "{ ( ["`
**Expected:** the function is one group chunk
**Refs:** crc-BracketChunker.md, R620
**Code:** bracket_chunker_test.go

## Test: append with nil locator
**Purpose:** with no previous chunks, AppendChunks chunks `newBytes` alone
**Input:** AppendChunks with a nil locator
**Expected:** no file read, `replacedLast=false`
**Refs:** crc-BracketChunker.md, R629
**Code:** bracket_chunker_test.go

## Test: append completes a group
**Purpose:** appended bytes that complete a partial bracket block replace the last chunk
**Input:** a file ending mid-group, then the closing lines appended
**Expected:** a single replacement chunk, `replacedLast=true`
**Refs:** crc-BracketChunker.md, R628
**Code:** bracket_chunker_test.go

## Test: append at a clean boundary
**Purpose:** a re-chunked first chunk identical to the previous last chunk is dropped
**Input:** a file ending at a clean boundary, then new chunks appended
**Expected:** only the appended chunks are emitted, `replacedLast=false`
**Refs:** crc-BracketChunker.md, R628
**Code:** bracket_chunker_test.go

## Test: append extends a paragraph
**Purpose:** re-chunking from the prior chunk's start detects an extended paragraph
**Input:** a paragraph, then comment lines appended to it
**Expected:** the merged paragraph replaces the previous chunk
**Refs:** crc-BracketChunker.md, R627
**Code:** bracket_chunker_test.go
