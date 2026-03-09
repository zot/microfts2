# Test Design: MarkdownChunkFunc
**Source:** crc-Chunker.md

## Test: heading starts new chunk
**Purpose:** A heading line always begins a new chunk
**Input:** "# Title\nsome text\n"
**Expected:** One chunk "1-2" containing both lines
**Refs:** crc-Chunker.md, R170, R171

## Test: heading with following paragraph
**Purpose:** Heading + paragraph up to blank line form one chunk
**Input:** "# Title\npara line 1\npara line 2\n\nother text\n"
**Expected:** Chunk "1-3" (heading + para), chunk "5-5" (other text)
**Refs:** crc-Chunker.md, R171

## Test: consecutive headings
**Purpose:** Each heading starts its own chunk
**Input:** "# One\n## Two\n### Three\n"
**Expected:** Three chunks, each a single line
**Refs:** crc-Chunker.md, R170

## Test: blank line collapsing
**Purpose:** Multiple consecutive blank lines are a single boundary, not included in chunks
**Input:** "text a\n\n\n\ntext b\n"
**Expected:** Two chunks: "1-1" and "5-5"; blank lines 2-4 not in any chunk
**Refs:** crc-Chunker.md, R172, R177

## Test: non-heading paragraph
**Purpose:** Text between boundaries forms one chunk
**Input:** "line one\nline two\nline three\n"
**Expected:** One chunk "1-3" with all lines
**Refs:** crc-Chunker.md, R173

## Test: heading after paragraph with blank line
**Purpose:** Blank line + heading = two boundaries, heading starts new chunk
**Input:** "some text\n\n# Heading\nparagraph\n"
**Expected:** Chunk "1-1", chunk "3-4"
**Refs:** crc-Chunker.md, R170, R171

## Test: empty input
**Purpose:** No content produces no chunks
**Input:** ""
**Expected:** Zero chunks yielded
**Refs:** crc-Chunker.md

## Test: range format
**Purpose:** Range is 1-based startline-endline
**Input:** "# Title\nline\n\nanother\n"
**Expected:** Chunk ranges are "1-2" and "4-4"
**Refs:** crc-Chunker.md, R174
