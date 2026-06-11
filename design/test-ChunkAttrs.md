# Test Design: Chunk Attrs and full-text indexing
**Source:** crc-DB.md, crc-ChunkCache.md, crc-Overlay.md

The ContentTransform hook was rolled back (retire-content-transform). These tests
cover the steady state: the trigram index is full-text (content indexed verbatim),
dedup is by content hash, and native chunker `Attrs` are stored and surfaced on
every retrieval path. A small test chunker yields one chunk carrying a native Attr.

## Test: the index keeps tags (full-text)
**Purpose:** R656 — content is indexed verbatim, including `@tag:` spans; microfts2 strips nothing.
**Input:** register a plain chunker; index a chunk whose content is `the quick @note: bubba maluba`.
**Expected:** a search for `bubba` (inside the tag) finds the chunk; a search for `maluba` finds it.
**Refs:** crc-DB.md

## Test: native Attrs survive streaming retrieval
**Purpose:** R655 — a native chunker Attr stored at index time is surfaced on retrieval, including the streaming (non-RandomAccessChunker) path that re-yielded the chunker's Attrs before the all-paths fix.
**Input:** register a Chunker (not RandomAccessChunker) that yields a chunk with `Attrs=[kind=doc]`; index a file; `GetChunks` the chunk.
**Expected:** returned Content is the original file bytes; `Attrs` contains `kind=doc`.
**Refs:** crc-ChunkCache.md, crc-DB.md

## Test: dedup by content hash
**Purpose:** R657 — dedup identity is the content hash.
**Input:** index two files with identical content and a third with different content.
**Expected:** the two identical-content files share one chunkid; the third gets a distinct chunkid.
**Refs:** crc-DB.md
