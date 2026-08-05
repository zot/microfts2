# Per-Query Chunk Cache

`ChunkCache` avoids redundant file reads and re-chunking during search result processing. Created at the start of a query, discarded when done. No LRU, no eviction — the working set of a single query is bounded.

```go
func (db *DB) NewChunkCache() *ChunkCache
```

## API

```go
// Drop-in replacement for DB.GetChunks — same signature, cached.
func (cc *ChunkCache) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)

// Fast-path single-chunk retrieval by chunkID (e.g. from SearchResult).
func (cc *ChunkCache) ChunkTextWithId(fpath string, chunkID uint64) ([]byte, bool)

// Convenience wrapper: resolves chunkID from range label, calls ChunkTextWithId.
func (cc *ChunkCache) ChunkText(fpath, rangeLabel string) ([]byte, bool)
```

Detailed behavior and cachedFile shape are specified in *ChunkCache API changes* below (alongside the `RandomAccessChunker` fast path).

## Caching strategy

- **First access to a file:** resolve path → fileid via N records (one View txn), read F record, snapshot positional `fileChunks` + `rangeIds` map, resolve Chunker. For `Chunker` backends, read file content into `data`. For `FileChunker`-only backends, `data` stays nil.
- **Retrieval path:** dispatches to `RandomAccessChunker.GetChunk` if available (fast path: C record read + pre-filled Chunk + direct extraction). Otherwise streams `Chunks`/`FileChunks` from the start, caching each encountered chunk, stopping at the target range.
- **Access-order storage:** `chunks []cachedChunk` grows in insertion order. Positional meaning is provided by the separate `fileChunks` list + `byRange` map, not by slice index.
- **Copy semantics:** the cache deep-copies Range, Content, and Attrs on store. Downstream consumers get stable references.

## Lifecycle

- Created with `db.NewChunkCache()`.
- No invalidation — the file state is assumed stable for the duration of a query.
- Goes away when the caller drops the reference (normal GC).

# ChunkCache API changes

`ChunkCache` is extended to support both range-based and chunkID-based retrieval, and its positional indexing changes shape to support random-access population.

## API

```go
// Full-window retrieval. Same contract as DB.GetChunks, cached.
func (cc *ChunkCache) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)

// Fast-path single-chunk retrieval. Callers that already have a chunkID
// (e.g. from SearchResult) skip the range→chunkID lookup.
func (cc *ChunkCache) ChunkTextWithId(fpath string, chunkID uint64) ([]byte, bool)

// Convenience wrapper for range-based callers. Resolves chunkID from
// the F record's Location list, delegates to ChunkTextWithId.
func (cc *ChunkCache) ChunkText(fpath, rangeLabel string) ([]byte, bool)
```

## cachedFile shape

The per-file cache entry gains positional access and random-access scratch:

- `fileChunks []FileChunkEntry` — positional chunk list from `frec.Chunks`. Populated at `ensureFile` time. Used by `GetChunks` for neighbor window resolution and by `ChunkTextWithId` for C record lookup.
- `rangeIds map[string]uint64` — Location → ChunkID, O(1) lookup for `ChunkText(range)`. Derived from `fileChunks` at `ensureFile` time.
- `chunks []cachedChunk` — access-order (chronological) storage. When the fast path fills chunks out of positional order, they land here in insertion order. Positional meaning is provided by `fileChunks` + `byRange`, not by slice index.
- `byRange map[string]int` — Location → index into `chunks`. Primary lookup for cached content.
- `customData any` — per-file scratch for `RandomAccessChunker`. Nil until the chunker's first GetChunk call on this file.

## GetChunks behavior

1. `ensureFile` populates `fileChunks`, `rangeIds`, allocates empty `chunks`/`byRange`/`customData`.
2. Locate `targetPos`: positional index of `targetRange` in `fileChunks`.
3. Compute window `lo, hi` around `targetPos`, clamped to bounds.
4. For each `i` in `[lo, hi]`: get `fileChunks[i].Location` and `fileChunks[i].ChunkID`. If `byRange[Location]` hit, use cached content. Miss: read C record by ChunkID, pre-fill Chunk, call `GetChunk` (fast path) or fall back to the streaming path. Store result in `chunks` + `byRange`.
5. Assemble `[]ChunkResult` in positional order.

## ChunkTextWithId / ChunkText behavior

1. `ensureFile` as above.
2. `ChunkText(range)`: resolve `chunkID := rangeIds[range]`, delegate to `ChunkTextWithId`.
3. `ChunkTextWithId(chunkID)`: find positional entry in `fileChunks` by matching ChunkID (small N, linear scan). Check `byRange[Location]` — hit: return content. Miss: read C record, pre-fill Chunk, dispatch fast path or streaming path, store, return.

## Caching strategy

- **First access to a file:** resolve path → fileid via N records, read F record for chunk list and strategy, snapshot `fileChunks` + `rangeIds`, resolve Chunker. For `Chunker` backends, read file content into `data`. For `FileChunker`-only backends, `data` stays nil.
- **Fast path (RandomAccessChunker):** C record read → pre-fill Chunk → GetChunk fills Content → deep-copy into `chunks` + `byRange`.
- **Streaming path (non-RandomAccessChunker):** `ChunkTextWithId` runs `Chunks`/`FileChunks` from the start, deep-copying each chunk into `chunks` + `byRange`, stopping when the target range is found. `GetChunks` may run the full stream.
- **Copy semantics:** deep-copy Range, Content, and Attrs on store. Downstream consumers get stable references.

## Backward compatibility

- `ChunkTexter` interface removed — no production code path dispatched to it. Tests updated.
- `ChunkText` method on `Chunker` (via `BracketChunker.ChunkText`, `IndentChunker.ChunkText`, `FuncChunker.ChunkText`) removed.
- `ChunkCache.ChunkText(fpath, rangeLabel)` signature unchanged — its implementation now delegates to `ChunkTextWithId`.
- `ChunkCache.GetChunks` signature unchanged — its implementation uses the new access-order storage.
- Callers that previously type-asserted to `ChunkTexter` must remove or migrate to `RandomAccessChunker`.
