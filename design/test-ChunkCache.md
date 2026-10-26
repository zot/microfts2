# Test Design: ChunkCache
**Source:** crc-ChunkCache.md

## Test: ChunkText, lazy and cached
**Purpose:** a range is fetched by label, and a second fetch is served from the cache
**Input:** index a file; `ChunkText` for range `1-1` twice, then for `3-3`
**Expected:** `hello\n` both times; range `3-3` resolves too
**Refs:** crc-ChunkCache.md, R300, R302
**Code:** cache_test.go

## Test: GetChunks fills the cache
**Purpose:** full chunking fills every slot; later ChunkText calls are lookups
**Input:** index a three-chunk file; `GetChunks`, then `ChunkText`
**Expected:** 3 results in file order; ChunkText afterwards returns the right text
**Refs:** crc-ChunkCache.md, R299, R303
**Code:** cache_test.go

## Test: nonexistent range
**Purpose:** a range the file does not have is reported, not invented
**Input:** `ChunkText` and `GetChunks` for a range not in the file
**Expected:** ChunkText returns false; GetChunks returns an error
**Refs:** crc-ChunkCache.md, R299, R300
**Code:** cache_test.go

## Test: multiple files
**Purpose:** one cache serves several files without mixing them up
**Input:** index two files; ChunkText from each
**Expected:** each returns its own file's text
**Refs:** crc-ChunkCache.md, R297, R301
**Code:** cache_test.go

## Test: lazy, then full
**Purpose:** a lazy partial chunking is completed by a later GetChunks
**Input:** ChunkText for the first range, then GetChunks for the file
**Expected:** GetChunks returns both chunks with the right ranges
**Refs:** crc-ChunkCache.md, R302, R303
**Code:** cache_test.go
