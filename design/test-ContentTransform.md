# Test Design: ContentTransform
**Source:** crc-Chunker.md, crc-DB.md, crc-ChunkCache.md, crc-Overlay.md

A registered transform strips `@tag:` lines from a markdown-ish paragraph and
appends the extracted tag values to `Attrs`. The transform shapes only the
index and the derived `Attrs`; retrieval returns the original chunker content.
Tests use a small deterministic transform so they assert exact Content and Attrs.

## Test: transform strips the index but stores original content
**Purpose:** R646, R647, R656 — the transform's stripped Content feeds the trigram index while the C record stores the original Content (hashed over the original).
**Input:** AddChunker("md-strip", MarkdownChunker{}, stripTags); index a file whose paragraph is `@author: zorblax` / `the quick maluba`.
**Expected:** a search for `maluba` finds the chunk (body indexed); a search for `zorblax` finds nothing (tag value not trigram-indexed); the chunk's stored Content is the ORIGINAL `@author: zorblax` / `the quick maluba`, and its Attrs contain `author=zorblax`.
**Refs:** crc-DB.md, seq-chunker-dispatch.md

## Test: retrieval returns original content — streaming fallback
**Purpose:** R655 — non-RandomAccessChunker retrieval returns the original chunker Content (transform not applied).
**Input:** register a transform-carrying Chunker that is NOT a RandomAccessChunker; index, then GetChunks the chunk.
**Expected:** returned Content is the ORIGINAL `@author: zorblax` / `the quick maluba` (tags intact); Attrs (from the stored C record) contains `author=zorblax`.
**Refs:** crc-ChunkCache.md, crc-DB.md

## Test: retrieval returns original content — RandomAccessChunker fast path
**Purpose:** R655 — fast-path retrieval pre-fills Attrs from the stored C record and returns the original Content; no transform, no double-append.
**Input:** transform-carrying RandomAccessChunker; index, then GetChunks / ChunkCache.ChunkText the chunk.
**Expected:** Content contains `the quick maluba` AND the original tag text `zorblax` (original, not stripped); Attrs contains exactly one `author=zorblax` (from the C record, not duplicated).
**Refs:** crc-ChunkCache.md

## Test: all-tag chunk retrieves its original text, not empty (regression guard)
**Purpose:** R655, R656 — a chunk that is entirely `@tag:` lines strips to empty for the index but must retrieve as its original non-empty text.
**Input:** transform-carrying chunker; index a file whose only chunk is `@from: ark` / `@to: microfts2` (all tag lines).
**Expected:** the chunk indexes with empty body (no body trigrams; searching a tag value finds nothing), yet GetChunks returns the ORIGINAL `@from: ark` / `@to: microfts2` text (non-empty); Attrs contains `from=ark` and `to=microfts2`.
**Refs:** crc-DB.md, crc-ChunkCache.md

## Test: append adding a tag yields a new chunkid and fires the indexed callback
**Purpose:** R658 — a tag added to the trailing chunk changes its original Content, producing a fresh chunkid; WithIndexedChunkCallback fires on append.
**Input:** index `@author: zorblax` / `maluba body` as the file's last paragraph; AppendChunks bytes that add `@extra: nine` to that paragraph, with WithIndexedChunkCallback recording fired chunks.
**Expected:** the recomputed tail has a different chunkid than before (its original Content gained the tag line); the indexed callback fires carrying Attrs `author=zorblax, extra=nine`; a later retrieval returns the original text with both tag lines.
**Refs:** crc-DB.md

## Test: dedup identity is the original content
**Purpose:** R656, R657 — identical original Content dedups to one chunkid; differing tags give different original Content and distinct chunkids.
**Input:** with the transform, index file X (`@a: 1` / `shared body`) and file Y (`@b: 2` / `shared body`); separately index file P and file Q both holding identical `@a: 1` / `shared body`.
**Expected:** X and Y receive distinct chunkids (their original Content differs by the tag line); P and Q dedup to one chunkid (identical original Content).
**Refs:** crc-DB.md, crc-Overlay.md

## Test: no-transform dedup is unchanged
**Purpose:** R656 — a chunk produced without a transform hashes as SHA-256 over Content, exactly as before.
**Input:** a chunker with no transform indexing identical content in two files.
**Expected:** the two chunks dedup to one chunkid (hash equals the historical content-only hash); behavior identical to before the change.
**Refs:** crc-DB.md

## Test: nil transform leaves behavior unchanged
**Purpose:** R643, R644, R645 — AddChunker with nil transform, and AddStrategyFunc, behave exactly as before.
**Input:** AddChunker("plain", MarkdownChunker{}, nil) and AddStrategyFunc("lines", LineChunkFunc); index and retrieve.
**Expected:** Content and Attrs pass through untouched; stored Attrs pre-filled on the fast path as before.
**Refs:** crc-DB.md
