# Test Design: IndentChunker
**Source:** crc-IndentChunker.md

## Test: Python basic
**Purpose:** imports, functions and a trailing statement split by indentation
**Input:** `import os`, two `def` blocks, `x = 1`, with `#` line comments
**Expected:** at least 3 chunks
**Refs:** crc-IndentChunker.md, R326, R327, R329, R331
**Code:** indent_chunker_test.go

## Test: nested indentation
**Purpose:** a deeper block nested inside a group belongs to that group
**Input:** a block whose body contains a further-indented block
**Expected:** one chunk
**Refs:** crc-IndentChunker.md, R326, R329
**Code:** indent_chunker_test.go

## Test: YAML
**Purpose:** indentation chunking works on a non-code format
**Input:** YAML with several top-level keys and nested maps
**Expected:** at least 3 chunks
**Refs:** crc-IndentChunker.md, R329
**Code:** indent_chunker_test.go

## Test: leading comment attaches to group
**Purpose:** comment lines directly above a group header belong to the group
**Input:** a comment line above a `def` block
**Expected:** one chunk, range `1-3`
**Refs:** crc-IndentChunker.md, R330
**Code:** indent_chunker_test.go

## Test: separate groups
**Purpose:** two top-level blocks separated by a blank line are separate chunks
**Input:** `def a():` and `def b():`, each with a `pass` body
**Expected:** at least 2 chunks
**Refs:** crc-IndentChunker.md, R327, R329
**Code:** indent_chunker_test.go

## Test: empty input
**Purpose:** empty content yields nothing
**Input:** empty byte slice
**Expected:** 0 chunks, no error
**Refs:** crc-IndentChunker.md
**Code:** indent_chunker_test.go

## Test: tab width
**Purpose:** tab width controls column counting; 0 means one column per tab
**Input:** a tab-indented block chunked with `tabWidth=0` and with `tabWidth=8`
**Expected:** one chunk in both cases
**Refs:** crc-IndentChunker.md, R328
**Code:** indent_chunker_test.go

## Test: measureIndent
**Purpose:** indentation is measured in columns, with tabs expanded per tab width
**Input:** table of lines with spaces, tabs, and mixes, at several tab widths
**Expected:** each line's measured column matches the table
**Refs:** crc-IndentChunker.md, R328
**Code:** indent_chunker_test.go
