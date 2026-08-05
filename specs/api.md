# Library API

```go
// Lifecycle
func Create(path string, opts Options) (*DB, error)
func Open(path string, opts Options) (*DB, error)
func (db *DB) Close() error
func (db *DB) Settings() Settings
func (db *DB) DB() *bbolt.DB
func (db *DB) Version() (string, error)

// Content
func (db *DB) AddFile(fpath, strategy string) (uint64, error)
func (db *DB) AddFileWithContent(fpath, strategy string) (uint64, []byte, error)
func (db *DB) RemoveFile(fpath string) error
func (db *DB) RemoveFileWithCallback(fpath string, fn RemoveCallback) error
func (db *DB) Reindex(fpath, strategy string) (uint64, error)
func (db *DB) ReindexWithCallback(fpath, strategy string, fn ReindexCallback, opts ...IndexOption) (uint64, error)
func (db *DB) ReindexWithContent(fpath, strategy string) (uint64, []byte, error)
func (db *DB) FileInfoByID(fileid uint64) (FRecord, error)
func (db *DB) AppendChunks(fileid uint64, content []byte, strategy string, opts ...AppendOption) error

// Search
func (db *DB) Search(query string, opts ...SearchOption) (*SearchResults, error)
func (db *DB) SearchRegex(pattern string, opts ...SearchOption) (*SearchResults, error)
func (db *DB) SearchMulti(query string, strategies map[string]ScoreFunc, k int, opts ...SearchOption) ([]MultiSearchResult, error)
func (db *DB) ScoreFile(query, fpath string, fn ScoreFunc, opts ...SearchOption) ([]ScoredChunk, error)
func (db *DB) BM25Func(queryTrigrams []uint32) (ScoreFunc, error)

// Chunk Retrieval
func (db *DB) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)
func (db *DB) ChunkContentLens(fileid uint64) ([]int, error)

// Strategies
func (db *DB) AddStrategy(name, cmd string) error
func (db *DB) AddChunker(name string, c any) error
func (db *DB) AddStrategyFunc(name string, fn ChunkFunc) error  // convenience: wraps fn in FuncChunker
func (db *DB) RemoveStrategy(name string) error
```

Chunker interface:
```go
type Chunker interface {
    Chunks(path string, content []byte, yield func(Chunk) bool) error
    ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool)
}
```

Chunk: `{ Range []byte, Locator []byte, Content []byte, Attrs []Pair }` — Range, Locator, and Content are reusable buffers, caller must copy before next yield. Range is the human-readable label (CLI display). Locator is opaque chunker-defined bytes used for fast random-access retrieval and append-merge resume; nil if not needed. Attrs is optional per-chunk metadata, nil by default.
Pair: `{ Key []byte, Value []byte }` — opaque key-value pair, allows duplicate keys
ChunkFunc: `func(path string, content []byte, yield func(Chunk) bool) error` — generator pattern, convenience type
FuncChunker: adapter that wraps a ChunkFunc into a Chunker (ChunkText re-runs and matches by range label)

Options:
- CaseInsensitive, Aliases — creation-time only
- DBName — bucket name, default "fts"
- Timeout — bounds how long Open/Create waits for the bbolt file lock
  before returning bbolt.ErrTimeout; zero blocks until the lock frees
  (bbolt default). The index is single-process, so a bounded timeout
  lets a second opener fail fast instead of hanging while another holds
  the DB.

(bbolt has no named-DB limit and grows the file automatically, so the former `MaxDBs` and `MapSize` options were removed.)

## TxnHolder interface

Records read from the index are tied to the transaction that read them. `TxnHolder` abstracts this — any value that carries the active bucket implements it. Internal DB methods accept `TxnHolder` instead of raw `*bbolt.Bucket`, so callers pass whatever they have (a CRecord, a bucket wrapper) without extraction or conversion. The bucket carries its transaction via `bucket.Tx()`.

```go
// TxnHolder is anything that carries the index's bbolt bucket.
type TxnHolder interface {
    Bucket() *bbolt.Bucket
}
```

CRecord implements `TxnHolder` via its `Bucket()` accessor (and offers a `Tx() *bbolt.Tx` convenience that returns `bkt.Tx()`). A simple `bucketWrap` struct wraps raw buckets from View/Update blocks. Navigation methods like `CRecord.FileRecord(fileid)` pass `self` as the TxnHolder — no extraction needed.

## Record structs

Go structs for each index record type. Encoding/decoding lives in methods on the structs. The rest of the code works with typed data, not raw bytes.

```go
// CRecord is the per-chunk record. Self-describing: everything needed
// for search, scoring, filtering, and removal.
// Carries unexported db/bkt — the chunk is tied to the bucket (and its
// transaction) that read it. Implements TxnHolder.
type CRecord struct {
    ChunkID  uint64
    Hash     [32]byte
    Trigrams []TrigramEntry          // {Trigram uint32, Count int}
    Tokens   []TokenEntry            // {Token string, Count int}
    Attrs    []Pair                  // opaque per-chunk metadata from chunker (timestamp, role, etc.)
    FileIDs  []uint64
    db       *DB                     // unexported: transaction context
    bkt      *bbolt.Bucket           // unexported: transaction context
}

// TxnHolder implementation + direct access for power-user filters.
func (c *CRecord) Bucket() *bbolt.Bucket
func (c *CRecord) Tx() *bbolt.Tx
func (c *CRecord) DB() *DB

// Convenience navigation — passes self as TxnHolder.
func (c *CRecord) FileRecord(fileid uint64) (FRecord, error)

// ReadCRecord fetches a CRecord by chunkID inside an existing tx.
// The returned CRecord has db/bkt attached so callers can use Bucket(), Tx(),
// DB(), and FileRecord(fileid) on the result. Must be called inside a View or
// Update tx — the bucket is part of the CRecord and outlives the call.
func (db *DB) ReadCRecord(tx *bbolt.Tx, chunkID uint64) (CRecord, error)

// FRecord is the per-file record. Metadata, ordered chunks, file-level token bag.
type FRecord struct {
    FileID      uint64
    ModTime     int64                // Unix nanos
    ContentHash [32]byte
    FileLength  int64
    Strategy    string
    Names       []string             // multiple names for dup/copied files
    Chunks      []FileChunkEntry     // {ChunkID uint64, Location string}
    Tokens      []TokenEntry         // aggregated bag across all chunks
}

// TRecord is the trigram inverted index entry.
type TRecord struct {
    Trigram  uint32
    ChunkIDs []uint64
}

// WRecord is the token inverted index entry.
type WRecord struct {
    TokenHash uint32
    ChunkIDs  []uint64
}

// HRecord maps content hash to chunkid.
type HRecord struct {
    Hash    [32]byte
    ChunkID uint64
}

// Supporting types
type Pair          struct { Key []byte; Value []byte }
type TrigramEntry  struct { Trigram uint32; Count int }
type TokenEntry    struct { Token string; Count int }
type FileChunkEntry struct { ChunkID uint64; Location string }
```

## Search types

SearchResult: `{ Path string, Range string, Score float64, chunkID uint64, chunk []byte }`
- `chunkID` and `chunk` are unexported. `chunkID` is set during `scoreAndResolve` (the search pipeline already has the chunkID when it builds each result). `chunk` is lazily populated by `Retrieve`.
- `Retrieve(r *SearchResult) []byte` method on `*searchConfig`: returns chunk content for the result. Checks `r.chunk` first (instant). Then checks chunkID dedup cache (`map[uint64][]byte` on searchConfig). On miss, delegates to ChunkCache — auto-creates one if no external cache was provided via `WithChunkCache`. ChunkCache handles file-level caching (read once per file), tmp:// overlay paths, and lazy chunking. Stores content on both `r.chunk` and the chunkID dedup cache.
- Post-filters (`verifyResults`, `verifyResultsRegex`, `applyRegexPostFilters`, `proximityRerank`) use `Retrieve` instead of `filterResults`+`rechunkForVerify`. The old functions (`filterResults`, `rechunkForVerify`, `rechunkForVerifyTmp`, `rechunkContent`, `fileStrategy`) are removed.
SearchResults: `{ Results []SearchResult, Status IndexStatus }`
IndexStatus: `{ Built bool }`
ScoredChunk: `{ Range string, Score float64 }`
ChunkResult: `{ Path string, Range string, Content string, Index int }` — a chunk with its content and position in the file's chunk list
MultiSearchResult: `{ Strategy string, Results []SearchResult }` — one strategy's top-k results from SearchMulti

ScoreFunc: `func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64`
SearchOption: `func(*searchConfig)` — functional option pattern
Built-in options: `WithCoverage()` (default), `WithDensity()`, `WithOverlap()`, `WithScoring(fn ScoreFunc)`, `WithVerify()` (post-filter: re-chunk file using stored strategy, tokenize query into terms — split on spaces, quoted strings as single terms — verify each term is a case-insensitive substring of the chunk content; eliminates trigram false positives), `WithTrigramFilter(fn TrigramFilter)` (caller-supplied trigram selection), `WithProximityRerank(topN int)` (post-filter: rerank top-N by query term proximity in chunk text), `WithChunkCache(cc *ChunkCache)` (optional cross-search cache — `Retrieve` checks the external ChunkCache before rechunking from disk, enabling file-read reuse across multiple searches in a session)
Built-in score functions: `ScoreOverlap` (matching trigram count), `ScoreBM25(idf, avgdl)` (returns ScoreFunc closure)

### searchConfig as search pipeline receiver

`searchConfig` embeds `*DB`. The search entry points (`Search`, `SearchRegex`, `SearchMulti`, `ScoreFile`, `SearchFuzzy`) build a `searchConfig`, then the entire search pipeline runs as methods on it — candidate collection, overlay merge, scoring, post-filtering, reranking. Functions that currently take `(*DB, *searchConfig, ...)` become methods on `*searchConfig` with shorter signatures. Pure structural — no behavior change.

## Chunk filtering

`ChunkFilter` receives the `CRecord` for a candidate chunk. Called during candidate evaluation — after T record intersection, before scoring. The C record is already loaded on the hot path (needed for per-trigram counts), so filtering adds a conditional check on data already in memory.

The CRecord carries unexported `db` and `bkt` fields — the chunk is inherently tied to the bucket (and its transaction) that read it. `Tx()` and `DB()` accessors expose the context for power-user filters. `FileRecord(fileid)` is a convenience method for the common case.

```go
type ChunkFilter func(chunk CRecord) bool

WithChunkFilter(fn ChunkFilter) SearchOption
```

Built-in chunk filters:
- `WithAfter(t time.Time)` — keep chunks with `timestamp` attr (Pair key) >= t; falls back to file mod time via `chunk.FileRecord(fileid)` if no attr
- `WithBefore(t time.Time)` — keep chunks with `timestamp` attr (Pair key) < t; same fallback

Chunk filters compose: multiple `WithChunkFilter` calls accumulate (AND semantics). `WithAfter`/`WithBefore` are sugar that append chunk filters internally.

## Trigram filtering

TrigramCount: `{ Trigram uint32, Count int }` — trigram code with its corpus document frequency
TrigramFilter: `func(trigrams []TrigramCount, totalChunks int) []TrigramCount` — selects which query trigrams to search with
Stock filters: `FilterAll` (use all), `FilterByRatio(maxRatio float64)` (skip high-frequency), `FilterBestN(n int)` (keep N lowest-frequency)

## Append options

AppendOption: `func(*appendConfig)` — functional option pattern
Built-in append options: `WithContentHash(hash string)` (full-file SHA-256 — caller pre-computed), `WithModTime(t int64)` (Unix nanos), `WithFileLength(n int64)` (full file size after append), `WithBaseLine(n int)` (1-based line number offset for line-based chunker ranges; 0 means no adjustment)

# Ark Integration

microfts2 and microvec share the same bbolt DB when used together in ark. bbolt allows one open `*bbolt.DB` per file; the first library opened shares it with the rest of the process, so the first library opened provides the handle to the second.

## Buckets

bbolt has no named-DB limit, so there is nothing to pre-size. microfts2 uses one bucket (default `fts`); other libraries (e.g. microvec) create their own buckets in the same `*bbolt.DB`. The former `MaxDBs`/`MapSize` options were removed — bbolt grows the file automatically.

## DB accessor

`DB()` returns the underlying `*bbolt.DB`. The host process opens microfts2 first, gets the handle, and passes it to microvec.

## Fileid surfacing

`AddFile` and `Reindex` return the fileid (uint64) alongside the error. The fileid is already computed internally — it just needs to be returned. microvec keys its embedding records by fileid.

## FileInfo lookup

`FileInfoByID(fileid)` resolves a fileid to its `FRecord`. microvec search returns `(fileid, chunknum, score)` — the CLI needs to resolve these to human-readable output using this method. Wraps the F record read in a read transaction.

## Scoring

`ScoreFile(query, fpath, fn ScoreFunc)` returns per-chunk scores for a single file using the given scoring function. The machinery is in the search path — this scopes it to one file's index entries.

`SearchResult` gains a `Score float64` field so the general search path also reports per-chunk scores.

## ChunkerMetadata

Optional metadata interface so callers (notably ark's curation workshop) can introspect a chunker's editability and the line-comment delimiter its underlying language uses for wrapping inline tag annotations:

```go
type ChunkerMetadata interface {
    IsWritable() bool       // true for editable text, false for binary / read-only formats
    CommentSyntax() string  // line-comment delimiter, "" when n/a
}
```

`IsWritable` reports whether the chunker handles editable text (line-based text, markdown, bracket-chunker code, indent-chunker code) versus binary or read-only formats (e.g. PDF, which lives in ark and returns false). `CommentSyntax` returns the line-comment delimiter the underlying language uses — `"//"` for Go, `"#"` for Python, `"--"` for Lua, `""` for markdown or plain text where tags are authored without a comment wrapper.

The interface is optional. Callers type-assert against it; chunkers that don't implement it have, from the caller's perspective, the defaults `IsWritable=true, CommentSyntax=""`.

Built-in implementations:
- `LineChunker`, `MarkdownChunker`: `true`, `""` — plain text and markdown carry no comment delimiter.
- `bracketChunker`, `indentChunker`: `true`, first entry of `BracketLang.LineComments` (`""` when the slice is empty) — so Go's `bracketChunker` returns `"//"`, shell's returns `"#"`, lisp's returns `";"`, etc.

`ChunkerMetadata` is kept separate from `Chunker` (rather than folded into it) so existing external `Chunker` implementations remain valid without change. This mirrors the same optional-interface pattern used for `FileChunker`, `RandomAccessChunker`, and `AppendAwareChunker`.

# Record Counts

## Introspection

```go
// RecordStats holds aggregate statistics for one record prefix.
type RecordStats struct {
    Count     int64
    KeyBytes  int64
    ValueBytes int64
}

// RecordCounts returns per-prefix-byte statistics for all records.
func (db *DB) RecordCounts() (map[byte]RecordStats, error)
```

Opens a read transaction, iterates every key in the subdatabase, and accumulates per-prefix statistics: record count, total key bytes, and total value bytes. Keyed by the first byte of each key (the prefix that distinguishes record types: 'I', 'H', 'C', 'F', 'N', 'T', 'W'). Useful for diagnostics and testing — callers can verify index health and size distribution without knowing the internal record layout.

# FileID–Path Mapping

## FileIDPaths

```go
// FileIDPaths returns a map of fileid → path for all indexed files.
// Scans N records only — much cheaper than StaleFiles which deserializes full F records.
func (db *DB) FileIDPaths() (map[uint64]string, error)
```

Lazily loaded, incrementally maintained caches. First call scans F records using `UnmarshalFHeader` to populate both `pathCache` (fileid→path) and `pathToID` (path→fileid). Subsequent calls return directly from cache. AddFile, RemoveFile, and Reindex update both caches after their index writes succeed. `lookupFileByPath` uses `pathToID` to skip the N record lookup when the cache is populated. Since microfts2 owns its bucket (the bucket name is fixed and the handle is unexported), no external writes can invalidate the caches.

## Partial F Record Unmarshal

`UnmarshalFHeader(data)` decodes only the fixed-offset header fields of an F record value: ModTime, ContentHash, FileLength, Strategy, and Names. Stops before Chunks and Tokens — those are the bulk of the value and are not needed by StaleFiles or any staleness check. `StaleFiles` uses this instead of `UnmarshalFValue` to avoid deserializing chunk and token arrays.

## Search Cache

```go
// NewSearchCache enables FRecord caching on the DB. Returns a cleanup function.
// While active, readFRecord results are cached and reused across Search,
// FileInfoByID, and any other method that reads F records.
func (db *DB) NewSearchCache() func()
```

Opt-in per-batch cache for callers that fuse multiple searches and lookups in the same goroutine. The caller activates the cache, runs a batch of operations (Search, FileInfoByID, etc.), then calls the cleanup function. `readFRecord` checks the cache before going to the index — same fileid returns the same FRecord without re-reading or re-deserializing.

# DB Copy and Cache Invalidation

Support for read/write path separation in a closure actor. The caller runs reads directly on the DB and queues writes through a copy-index-reconcile cycle: copy the DB handle, index in a goroutine, then invalidate stale caches on the original after the write commits.

## Copy

```go
// Copy returns a shallow copy of the DB suitable for indexing in a
// separate goroutine. The copy shares the bbolt handle, overlay, and
// chunker registry. Caches are nil — the copy will lazy-load from
// committed bbolt state if needed.
func (db *DB) Copy() *DB
```

- `bolt`: shared (same `*bbolt.DB`) — bbolt handles concurrent readers/single writer natively
- `dbName`, `settings`, `trigrams`: shared (read-only or safe to share)
- `overlay`: shared (has its own `sync.RWMutex`)
- `chunkers`: shared (read-only during writes — only updated by config, which runs synchronously in the main actor)
- `overlayOnce`: not copied — overlay is already initialized on the source, and the copy shares the overlay pointer directly
- `pathCache`, `pathToID`, `frecordCache`: nil — forces lazy reload from committed bbolt state, avoids stale data from the source's cache

The copy is short-lived: one write transaction, then discarded.

## InvalidateCaches

```go
// InvalidateCaches nils the path and FRecord caches, forcing lazy
// reload on next access. Called after a write transaction commits
// from another goroutine to clear stale cached state.
func (db *DB) InvalidateCaches()
```

- Nils `pathCache`, `pathToID`, and `frecordCache`
- Does NOT reset `overlayOnce` — overlay is shared and already initialized
- No state transfer from the copy — just "your cache is stale now"
- Next access triggers the existing lazy-load path
