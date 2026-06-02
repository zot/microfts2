# Test Design: ContentTransform
**Source:** crc-Chunker.md, crc-DB.md, crc-ChunkCache.md, crc-Overlay.md

A registered transform strips `@tag:` lines from a markdown-ish paragraph and
appends the extracted tag values to `Attrs`. Tests use a small deterministic
transform so they assert exact Content and Attrs.

## Test: transform strips content and derives attrs at index time
**Purpose:** R646, R647 — the transform runs before hashing/trigram extraction, so the index reflects stripped Content.
**Input:** AddChunker("md-strip", MarkdownChunker{}, stripTags); index a file whose paragraph is `@a: 1` / `maluba`.
**Expected:** the chunk's C record stores Content `maluba` and Attrs containing `a=1`; a search for the tag literal `@a` finds nothing (tags not trigram-indexed); a search for `maluba` finds the chunk.
**Refs:** crc-DB.md, seq-chunker-dispatch.md

## Test: retrieved content equals indexed content — streaming fallback
**Purpose:** R648 — non-RandomAccessChunker retrieval re-derives identical Content.
**Input:** register a transform-carrying Chunker that is NOT a RandomAccessChunker; index, then GetChunks the chunk.
**Expected:** returned Content is `maluba` (stripped), Attrs contains `a=1` — identical to indexed.
**Refs:** crc-ChunkCache.md, crc-DB.md

## Test: retrieved content equals indexed content — RandomAccessChunker fast path
**Purpose:** R648, R651 — fast-path retrieval starts with empty Attrs and the transform repopulates; no double-append.
**Input:** transform-carrying RandomAccessChunker; index, then GetChunks / ChunkCache.ChunkText the chunk.
**Expected:** Content `maluba`; Attrs contains exactly one `a=1` (not duplicated); equals the streaming-path result.
**Refs:** crc-ChunkCache.md

## Test: append adding a tag yields a new chunkid and fires the indexed callback
**Purpose:** R652, R654 — Attrs change with unchanged Content produces a fresh chunkid; WithIndexedChunkCallback fires on append.
**Input:** index `@a: 1` / `maluba` as the file's last paragraph; AppendChunks bytes that add `@b: 2` to that paragraph, with WithIndexedChunkCallback recording fired chunkids.
**Expected:** the recomputed tail has Content `maluba` but a different chunkid than before; the indexed callback fires once carrying Attrs `a=1, b=2`; a later retrieval returns both attributes.
**Refs:** crc-DB.md

## Test: identical content, differing attrs are not deduplicated
**Purpose:** R652, R653 — chunk identity includes Attrs across files.
**Input:** index file X (`@a: 1` / `maluba`) and file Y (`@b: 2` / `maluba`) with the transform.
**Expected:** the two `maluba` chunks have distinct chunkids; each C record carries its own Attrs.
**Refs:** crc-DB.md, crc-Overlay.md

## Test: empty-attrs hash is unchanged — no rebuild, dedup still works
**Purpose:** R652 — a chunk with no Attrs hashes as SHA-256 over Content alone.
**Input:** a chunker with no transform (Attrs empty) indexing identical content in two files.
**Expected:** the two chunks dedup to one chunkid (hash equals the historical content-only hash); behavior identical to before the change.
**Refs:** crc-DB.md

## Test: nil transform leaves behavior unchanged
**Purpose:** R643, R644, R645 — AddChunker with nil transform, and AddStrategyFunc, behave exactly as before.
**Input:** AddChunker("plain", MarkdownChunker{}, nil) and AddStrategyFunc("lines", LineChunkFunc); index and retrieve.
**Expected:** Content and Attrs pass through untouched; stored Attrs pre-filled on the fast path as before.
**Refs:** crc-DB.md
