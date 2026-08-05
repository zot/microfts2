# Temporary Documents (tmp:// Overlay)

An in-memory overlay that lets callers index content without writing files to disk. Temporary documents are searchable alongside disk-backed documents through the same query interface. The overlay never touches the index — all data lives in RAM and disappears when the DB handle is closed or the process exits.

## URI Scheme

Temporary documents use `tmp://` paths: `tmp://SESSIONID/human-readable-name` (e.g. `tmp://abc123/scoring-notes`). The path is opaque to microfts2 — it's stored and returned in search results like any file path. The caller (ark) assigns session IDs and human-readable names.

## FileID Allocation

Temporary document fileids count down from `math.MaxUint64`. The overlay maintains its own counter starting at `math.MaxUint64` and decrementing for each new document. Index fileids count up from 1. The two ranges can never collide — structural guarantee, no coordination needed.

## In-Memory Data

The overlay holds the same data that the index holds for disk-backed files, but in Go maps:

- Per-chunk data: hash, trigram counts, token counts, attrs, fileids — equivalent to C records
- Per-file data: chunk list with locations, token bag — equivalent to F records
- Trigram inverted index: trigram → chunkid set — equivalent to T records
- Token inverted index: token hash → chunkid set — equivalent to W records
- Content hash → chunkid lookup — equivalent to H records
- Chunk deduplication within the overlay using the same SHA-256 mechanism

Chunk IDs within the overlay also count down from a separate counter starting at `math.MaxUint64`, same structural separation as fileids.

## Lifecycle

- Created implicitly on first use, or explicitly via a constructor
- Tied to the `*DB` handle — lives as long as the DB is open
- No persistence. Process dies → gone. If content is worth keeping, the caller writes a file and indexes it normally.
- Individual tmp:// documents can be removed explicitly

## Adding Documents

```go
func (db *DB) AddTmpFile(path, strategy string, content []byte) (uint64, error)
```

- `path`: the `tmp://` URI — treated as the document's file path
- `strategy`: chunking strategy name (must be registered)
- `content`: the document content (must be valid UTF-8)
- Returns the allocated fileid (counting down from MaxUint64)
- Chunks the content using the named strategy, extracts trigrams and tokens, stores everything in the overlay
- Chunk deduplication within the overlay — same content hash = same chunkid
- No cross-dedup between overlay and the index (they have separate chunkid spaces)
- `ErrAlreadyIndexed` if the path is already in the overlay

## Updating Documents

```go
func (db *DB) UpdateTmpFile(path, strategy string, content []byte) error
```

- Replaces the content of an existing tmp:// document
- Removes old chunks (and their trigram/token entries) from the overlay, adds new ones
- Atomic from the caller's perspective — no moment where the document is absent from search
- Error if path not found in overlay

This is the mutable document operation. Sessions can edit tmp:// documents: update content, re-chunk, re-tag (via attrs), all in memory.

## Appending to Documents

```go
func (db *DB) AppendTmpFile(path, strategy string, content []byte, opts ...AppendOption) (uint64, error)
```

Shell `>>` semantics: append new content to an existing tmp:// document, or create it if not found.

- `content` is only the appended bytes, not the full document
- Content must be valid UTF-8
- If path not found in overlay: auto-create via `AddTmpFile` (create-if-absent)
- If path found: strategy must match stored strategy (error on mismatch)
- Chunks the appended content using the named strategy, adds resulting chunks to the existing file's chunk list
- Existing chunks are untouched — append only extends
- Chunk deduplication within the overlay applies to new chunks
- F record equivalent updated: chunk entries appended, token bag merged, content extended
- Returns the file's fileid (same fileid whether created or appended)
- `WithBaseLine(n int)` option: 1-based line offset applied to chunk ranges so line numbers are absolute, not relative to the appended slice. Uses `adjustRange` to shift range strings.
- Chunking happens outside the write lock (RLock to read file state, then chunk, then Lock to mutate)
- Double-check after acquiring write lock — if the file was removed between RLock and Lock, return error rather than silently re-creating
- Empty chunk result from chunking is a no-op (returns fileid, nil)

This is the incremental-growth operation. For append-only content like conversation logs, it avoids the cost of re-chunking the entire document on each addition.

## Removing Documents

```go
func (db *DB) RemoveTmpFile(path string) error
```

- Removes the document and all its chunks from the overlay
- Cleans up trigram/token index entries for orphaned chunks
- Error if path not found

## Search Integration

Searches always include the overlay. The search path queries both the index and the in-memory overlay, merging results into a single result set sorted by score.

For each search:
1. Collect candidates from the index (existing path — T record reads, C record reads, scoring)
2. Collect candidates from overlay (same trigram intersection logic, against in-memory maps)
3. Merge and sort by score descending
4. Apply post-filters (verify, regex, proximity) to merged results

The overlay participates in all search modes: `Search`, `SearchRegex`, `SearchMulti`, `ScoreFile`. All `SearchOption`s apply uniformly — `WithChunkFilter`, `WithVerify`, `WithTrigramFilter`, etc.

## Excluding Temporary Documents

`WithNoTmp()` is the simple opt-out — it skips the overlay entirely during search. No allocation, no lock acquisition on the overlay. Suitable when a caller knows it only wants disk-backed results.

```go
func WithNoTmp() SearchOption
```

`TmpContent` retrieves the raw stored content of a tmp:// document — the original bytes passed to `AddTmpFile`. Distinct from `GetChunks` which returns chunked content. Returns a `*bytes.Reader` over the stored bytes — no copy, caller streams directly.

```go
func (db *DB) TmpContent(path string) (*bytes.Reader, error)
```

`HasTmp()` reports whether any tmp:// documents exist in the overlay. Useful for callers that want to conditionally adjust behavior (e.g. display a marker, change search strategy) without enumerating fileids.

```go
func (db *DB) HasTmp() bool
```

For finer control, `TmpFileIDs` + `WithExcept` allows excluding specific tmp:// fileids while keeping others:

```go
func (db *DB) TmpFileIDs() map[uint64]struct{}
```

Returns the set of all current tmp:// fileids. Passed to `WithExcept` to exclude tmp:// results:

```go
results, _ := db.Search(query, microfts2.WithExcept(db.TmpFileIDs()))
```

## Existing Fileid Filtering Options

`WithOnly` and `WithExcept` are fileid-set filters on the search path:

```go
func WithOnly(ids map[uint64]struct{}) SearchOption   // keep only these fileids
func WithExcept(ids map[uint64]struct{}) SearchOption  // exclude these fileids
```

- `WithOnly`: candidate chunks are kept only if at least one of their fileids is in the set
- `WithExcept`: candidate chunks are discarded if any of their fileids is in the set
- Both apply during candidate evaluation (same phase as ChunkFilter)
- These exist today in the codebase; this spec anchors them

## Chunk Retrieval

`GetChunks` and `ChunkCache` work with tmp:// documents. Since content lives in memory (not on disk), retrieval reads from the overlay's stored content rather than re-reading a file from disk. The overlay stores the original content bytes for each document.

## Thread Safety

The overlay must be safe for concurrent reads. Writes (add/update/remove) are serialized. This matches bbolt's model: concurrent readers, one writer at a time. A `sync.RWMutex` on the overlay is sufficient.

## Corpus Counters

The overlay maintains its own `totalChunks` and `totalTokens` counters. BM25 and other corpus-level computations sum index counters and overlay counters to get the true corpus size.

## CLI

No CLI changes for tmp:// — these are library-only operations. The CLI works with files on disk. Ark's CLI may expose tmp:// through its own commands.

## Indexed-chunk callback on tmp:// paths

`WithIndexedChunkCallback` fires for `tmp://` paths too — via `AddTmpFile`,
`UpdateTmpFile`, and `AppendTmpFile` — mirroring the contract on persistent
paths. It fires once per genuinely new chunk, in chunk order, after the
overlay chunk exists; a chunk that dedup-hits an existing hash fires nothing.
`AppendTmpFile`'s auto-create path (the file did not previously exist) routes
through `AddTmpFile` and propagates the callback rather than dropping it.

The `IndexedChunk.CRecord` handed to an overlay-fired callback carries
ChunkID, Hash, ContentLen, Attrs, FileIDs, and Trigrams, but **no index
transaction context**: `CRecord.Tx()` and `CRecord.DB()` return nil, because
the overlay never touches the index. Overlay chunkids count down from
MaxUint64, so a caller that needs to tell overlay-fired callbacks from
persistent ones can do so by chunkid range.
