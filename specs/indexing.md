# The process

We add a file to the database with a chosen chunking strategy:
- read file content, check utf8.Valid
- check for existing F records via FinalKey — return ErrAlreadyIndexed if present
- allocate fileid, create N records (filename key chain) and F record
- chunk: call Chunker.Chunks, which yields {Range, Locator, Content, Attrs} per chunk
  - caller copies Range, Locator, Content, and Attrs before next yield
  - for external command strategies, RunChunkerFunc wraps the command as a Chunker
- for each chunk: compute the dedup hash (SHA-256 over Content, then the marshaled Attrs when present — see "Chunk identity includes Attrs"), extract trigrams on Content, tokenize Content, copy Attrs
  - look up H record by hash — if chunkid exists, increment this fileid's count in the existing C record's fileids list (insert with count=1 if absent)
  - if new chunk: allocate chunkid, create H record, create C record (hash + trigrams + tokens + attrs + [fileid, count=1]), append chunkid to T records for each trigram, append chunkid to W records for each token
- update F record: append (chunkid, location, locator) entry, merge tokens into file-level token bag
- batch T/W record updates: accumulate all chunkids per trigram/token across the file, then one read-modify-write per affected T/W record

When removing a file:
- read F record to get the file's chunk list
- for each (chunkid, location, locator) entry: read C record, decrement this fileid's count in the fileids list (or remove the fileid entry entirely, since removing a whole file drops all its occurrences at once)
  - if C record has no remaining fileids: delete C record, remove chunkid from each T record (by trigram), remove chunkid from each W record (by token hash), delete H record
- delete F record, delete N records (key chain)

The same orphan-cascade logic is used by append-merge's drop-and-replace path (see `AppendAwareChunker` below): a chunk dropped from a single file decrements that fileid's count, with the same C/T/W/H cleanup if it was the last reference.

When reindexing a file (re-chunking it after an edit), preserve the chunkids of unchanged content. Re-chunk the new content, then delete only the old file's F and N records — the path metadata, not its chunks. Add the new chunks next: the old chunks' H records still exist, so a chunk whose content (hash) is unchanged is a dedup hit that keeps its chunkid, while genuinely new content allocates a fresh chunkid. Finally drop the old fileid's occurrences from the old chunk list. Chunks whose content survived were just re-added under the new fileid, so they live on; chunks whose content is gone lose their last reference and orphan-cascade as when removing a file. The net effect is a content diff, so chunkid-keyed external state (an embedding keyed by chunkid, say) survives an edit that leaves a chunk untouched.

When searching for a literal string:
- trim leading and trailing whitespace from the query before trigram extraction
- parse the query into terms using `parseQueryTerms`: unquoted words split on spaces, double-quoted phrases treated as single terms with quotes stripped
- extract trigrams per term (not from the whole query as one byte sequence) — this avoids cross-boundary trigrams between unrelated words (e.g. "daneel olivaw" must not produce trigrams "l o", " ol")
- the candidate set is the intersection of all terms' trigram candidate sets — a chunk must match all terms
- select query trigrams via TrigramFilter (default: FilterAll — use all trigrams); filter is applied to the combined trigram set
  - look up each query trigram's document frequency from T record value length
  - call filter function to select subset
- for each selected query trigram, read T record to get candidate chunkid lists
- intersect candidate chunkid sets across all selected query trigrams
- for each surviving chunkid, read C record to get per-trigram counts and fileids
- score each candidate using the selected scoring function (coverage or density)
- resolve chunkid → (filepath, range) via C record fileids → F record chunk list
- sort by score descending, return top-k
- CLI output format: one result per line, `filepath:range` (range is the chunk's opaque label)
- library returns struct slices with the same information, plus IndexStatus

When searching for a regex pattern:
- extract a trigram query (boolean AND/OR expression) from the regex AST, using rsc's approach (github.com/google/codesearch/regexp)
- evaluate the trigram query against T records (no trigram filtering)
- AND nodes intersect candidate chunkid sets, OR nodes union them
- verify: re-chunk each candidate file using the stored chunking strategy, run the compiled regex against the chunk content, discard non-matches (always, not opt-in — trigram query is a superset filter)
- output format same as literal search
- library returns struct slices with the same information, plus IndexStatus

# AddFile Duplicate Guard

`AddFile` and `AddFileWithContent` must not create duplicate entries for an already-indexed path. Before allocating a new fileid, `addFileInTxn` checks whether F records already exist for the path (via `FinalKey` lookup). If the file is already indexed, return `ErrAlreadyIndexed` — a sentinel error the caller can check with `errors.Is`. The caller should use `Reindex` or `AppendChunks` instead.

```go
var ErrAlreadyIndexed = errors.New("file already indexed")
```

This is a guard, not a policy decision — the caller decides what to do when they get this error.

# Staleness Detection

Each file's F record stores the file's modification time (Unix nanoseconds) and a content hash (SHA-256) at the time it was indexed.

A file is **stale** when it exists on disk and either:
- its modification time differs from the stored value, AND
- its content hash differs from the stored value

A file is **missing** when it no longer exists on disk.

Checking modification time first avoids hashing when the file hasn't changed. When mod time matches, the file is considered fresh without hashing.

When mod time differs but the content hash matches (file was touched but not changed), update the stored mod time in the F record so subsequent checks short-circuit at the mod time comparison instead of re-hashing.

## Library API

```go
type FileStatus struct {
    Path     string
    Status   string // "fresh", "stale", "missing"
    FileID   uint64
    Strategy string
}

func (db *DB) CheckFile(fpath string) (FileStatus, error)
func (db *DB) StaleFiles() ([]FileStatus, error)
func (db *DB) RefreshStale(strategy string) ([]FileStatus, error)
```

- `CheckFile`: check a single file's staleness
- `StaleFiles`: return status of all indexed files (fresh, stale, or missing)
- `RefreshStale`: reindex all stale files using the given strategy (empty string = use each file's existing strategy). Returns the list of files that were refreshed and an error. Missing files are skipped (not deleted).

# Chunk Processor Callback

A callback that fires for each chunk during indexing, giving the caller access to clean chunk text without re-reading the file. Motivated by ark's need to extract tags from chunk content during indexing — the chunker (e.g. chat-jsonl) has already stripped metadata noise, so the callback sees clean text.

## Problem

Without this callback, callers that need per-chunk text during indexing must either:
1. Re-read the file and run the chunker themselves (double work)
2. Extract data from the raw file, which includes noise the chunker would have stripped

## Design

A functional option on all indexing methods. The callback receives each chunk's text as a string after the chunker produces it but before microfts2 processes it (hashing, trigram extraction, etc.).

```go
// ChunkCallback receives clean chunk text during indexing.
// Called once per chunk, in chunk order. Must not retain the string
// beyond the call (copy if needed for accumulation).
type ChunkCallback func(chunkText string)

// WithChunkCallback supplies a callback for indexing methods.
func WithChunkCallback(fn ChunkCallback) IndexOption
```

## IndexOption type

A new functional option type for indexing methods, parallel to `SearchOption` for search and `AppendOption` for append:

```go
type IndexOption func(*indexConfig)
```

## Option constructors

Two constructors produce options for their respective method families, sharing the same `ChunkCallback` type:

```go
// WithChunkCallback supplies a chunk callback for indexing methods (AddFile, etc.).
func WithChunkCallback(fn ChunkCallback) IndexOption

// WithAppendChunkCallback supplies a chunk callback for append methods (AppendChunks, AppendTmpFile).
func WithAppendChunkCallback(fn ChunkCallback) AppendOption
```

No existing signatures change. Append methods keep `...AppendOption`. Indexing methods gain `...IndexOption`.

## Affected methods

Indexing methods gain `...IndexOption`:

```go
func (db *DB) AddFile(fpath, strategy string, opts ...IndexOption) (uint64, error)
func (db *DB) AddFileWithContent(fpath, strategy string, opts ...IndexOption) (uint64, []byte, error)
func (db *DB) RefreshStale(strategy string, opts ...IndexOption) ([]FileStatus, error)
func (db *DB) AddTmpFile(path, strategy string, content []byte, opts ...IndexOption) (uint64, error)
func (db *DB) UpdateTmpFile(path, strategy string, content []byte, opts ...IndexOption) error
```

Append methods use existing `...AppendOption` (no change):

```go
func (db *DB) AppendChunks(fileid uint64, content []byte, strategy string, opts ...AppendOption) error
func (db *DB) AppendTmpFile(path, strategy string, content []byte, opts ...AppendOption) (uint64, error)
```

## Callback behavior

- Called once per chunk, in the order the chunker produces them
- Receives `string(chunk.Content)` — a copy, safe to retain
- A nil callback (no `WithChunkCallback` option) means no-op — zero overhead on the existing path
- The callback fires inside `collectChunks` and equivalent overlay paths, after UTF-8 validation, before hashing
- Errors from the callback are not propagated — the callback is for observation, not control flow. If the caller needs to signal failure, it sets a flag in its closure and checks after indexing returns.

## Backward compatibility

Indexing methods gain a variadic `...IndexOption` — existing callers pass zero args, no breakage. Append methods are unchanged — `WithAppendChunkCallback` is just a new `AppendOption` constructor.

# Remove File with Callback

When microfts2 removes a file, some chunks become orphaned (no remaining file references). Callers that maintain their own index records keyed by chunkid — in the same `*bbolt.DB`, different bucket — need a hook to clean up those records transactionally. Without this, callers must track chunk→file mappings externally or accept stale records.

## Design

`RemoveFileWithCallback` wraps `RemoveFile` with a caller-supplied callback that fires inside the same write transaction, after orphaned chunks are identified and cleaned up from microfts2's records but before the transaction commits.

```go
// RemoveCallback receives the bbolt write transaction and the list of
// chunk IDs that were orphaned (deleted from the index) during removal.
// Returning a non-nil error aborts the entire transaction.
type RemoveCallback func(tx *bbolt.Tx, orphanedChunkIDs []uint64) error

func (db *DB) RemoveFileWithCallback(fpath string, fn RemoveCallback) error
```

- `fn` receives the raw `*bbolt.Tx` and a slice of orphaned chunkids — chunks that were fully removed from the index because the removed file was their last reference
- Chunks still referenced by other files (dedup survivors) are not included — their C records were updated but not deleted
- The callback runs inside the write transaction — `fn` can read/write any bucket in the same `*bbolt.DB`
- If `fn` returns a non-nil error, the entire transaction aborts — both microfts2's removals and the caller's changes roll back atomically
- If `fn` is nil, behavior is identical to `RemoveFile`
- No orphaned chunks (all chunks shared with other files) → `fn` is called with an empty slice
- Cache invalidation (pathCache, pathToID) happens after the transaction commits, same as `RemoveFile`

# Reindex File with Callback

Same pattern as `RemoveFileWithCallback`, but for reindex. Reindexing is a content diff in one transaction (see "When reindexing a file" above): unchanged content keeps its chunkid, so `orphanedChunkIDs` holds only the chunks whose content is gone from the file (their last reference dropped) and `newChunkIDs` holds the re-indexed file's chunk list. The caller cleans up stale records and creates new ones atomically, and chunkid-keyed external state for unchanged content is left untouched.

## Design

`ReindexWithCallback` wraps `Reindex` with a caller-supplied callback that fires inside the write transaction, after the new chunks are added and the old fileid's occurrences dropped, but before the transaction commits.

```go
// ReindexCallback receives the bbolt write transaction, the chunk IDs
// orphaned because their content is gone from the file, and the chunk IDs
// present in the newly re-indexed file.
// Returning a non-nil error aborts the entire transaction.
type ReindexCallback func(tx *bbolt.Tx, orphanedChunkIDs, newChunkIDs []uint64) error

func (db *DB) ReindexWithCallback(fpath, strategy string, fn ReindexCallback, opts ...IndexOption) (uint64, error)
```

- `orphanedChunkIDs`: chunks fully removed from the index because the old file was their last reference (same semantics as `RemoveCallback`)
- `newChunkIDs`: all chunk IDs in the re-indexed file, in chunk-list order — includes dedup hits (chunks shared with other files), not just genuinely new allocations. The caller needs every chunk ID to create its own per-chunk records
- The callback runs inside the write transaction — `fn` can read/write any bucket in the same `*bbolt.DB`
- If `fn` returns a non-nil error, the entire transaction aborts — remove, add, and caller's changes all roll back
- If `fn` is nil, behavior is identical to `Reindex`
- Cache invalidation (pathCache, pathToID) happens after the transaction commits, same as `Reindex`
