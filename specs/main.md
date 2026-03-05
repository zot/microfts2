# microfts
A dynamic LMDB trigram index, written in Go. CLI command, structured so it can also be used as a library.

# features

## configurable chunking strategies

- add/remove chunking strategies dynamically
- files track their indexed chunking strategy
  - can reindex with a different strategy -- allows migration to better strategies
- configurable character set -- supports up to 255 characters plus space, unindexed character == space (runs are collapsed)
- character aliases -- map input characters to charset characters before encoding (e.g. newline → `^` for line-start matching)
  - 8 bits / character, 24 bits per trigram
  - 16M possible trigrams (2^24 = 16,777,216)
  - active trigrams (A record): sparse packed sorted trigram list (3 bytes each, only active trigrams stored)
  - trigram counts (C records): sparse individual LMDB records, one per non-zero trigram

# Representation

## data-in-key pattern using lexical sort

Certain data is stored in keys, taking advantages of lexical sorting:
- [key]: position before first item
- [key] [info1]: first item
- [key] [infoN]: last item
- [key+1]: position after last item
- [key] ... [key+1]: information range for key

Sets: this pattern can represent a set for each key
- [key] [info] -> [empty]

## Key Chains

LMDB only supports 511 bytes per key. Long filenames (F records below) use multiple keys.

## Two Trees: content and index

LMDB supports multiple named subdatabases.
The content DB stores file metadata, trigram frequency counts, and the active set. The index DB stores the trigram-to-chunk mappings with occurrence counts, maintained incrementally.

Subdatabase names are parameters: defaults 'ftscontent' and 'ftsindex', settable via CLI and library API.
These are not stored in the I record -- they're needed to open the databases in the first place.

- content db:
  - C [trigram: 3] -> count: 8 — per-trigram occurrence count, sparse (only non-zero trigrams stored)
  - 'I' -> JSON -- database settings
    - chunkingStrategies: object
      - {name: cmd}: [cmd] [filename] -> list of file offsets
    - characterAliases: object -- maps input characters to charset characters (e.g. {"\n": "^"})
    - caseInsensitive: boolean
    - characterSet: string -- this is set during initialization and cannot be changed
      - a string of up to 256 characters
      - No spaces allowed
      - For punctuation, case-insensitive is recommended
    - searchCutoff: number -- frequency percentile for literal search (default 50, bottom N% used in literal queries)
  - 'N' [fileid: 8] -> JSON -- information about file
    - filename: string -- full file path
    - chunkOffsets: array[number] -- start offsets of each chunk
    - chunkStartLines: array[number] -- start line number per chunk
    - chunkEndLines: array[number] -- end line number per chunk
    - chunkTokenCounts: array[number] -- token count per chunk (for density scoring normalization)
    - chunkingStrategy: string -- which chunking strategy was used
    - modTime: number -- file modification time (Unix nanoseconds) at index time
    - contentHash: string -- SHA-256 hex of file contents at index time
  - 'F' [name part: 1] filename-prefix -> filename prefix, name part 255 indicates end of prefix (see previous)
  - 'F' 255 filename-suffix -> [fileid: 8]: file info for reindexing changed files / deleting
  - 'A' -> active trigrams packed sorted list (3 bytes per trigram) -- bottom searchCutoff% by frequency, used to filter literal queries

- index db:
  - forward entries: [trigram: 3] [count: 2 desc] [fileid: 8] [chunknum: 8] -- trigram occurrences per chunk, high counts sort first
    - count is 2-byte big-endian stored as 0xFFFF - count for descending lexical sort
    - capped at 65535
  - reverse entries: R [fileid: 8] -> chunk-grouped packed format
    - repeated: [chunknum: 8] [numTrigrams: 2] [[trigram: 3] [count: 2]]...
    - one record per file, used only for removal
    - contains all info needed to reconstruct forward keys for deletion

# Full Trigram Index

The index DB contains entries for ALL trigrams present in the content — not just a frequency-selected subset. This makes the index complete and usable for both literal and regex search.

The A record stores the "active set": trigrams in the bottom `searchCutoff`% by frequency. This set controls literal search selectivity — literal queries filter their trigrams to the active set, which are the most discriminating. Regex search bypasses the active set and queries the full index.

Because the index is always complete, there is no drift, no margin erosion, and no rebuild watermarks. The only reason to rebuild is to recompute the active set after significant corpus changes (a performance tuning decision, not a correctness concern).

The index is the authoritative record of which trigrams appear in which chunks and how many times. There are no per-chunk bitsets (T records) — the index is maintained incrementally on add/remove. If the index DB is lost, files must be re-added from disk.

# The process

We add a file to the database with a chosen chunking strategy:
- create the F record for the file to get its fileid
- chunk the file, compute trigrams per chunk with counts
- create the N record (including chunkTokenCounts)
- update C records (increment count for each trigram; insert if first occurrence)
- write forward index entries: [trigram][desc-count][fileid][chunknum]
- write reverse index entry: R[fileid] -> packed chunk/trigram/count data

When removing a file:
- look up R[fileid] to get all (chunknum, trigram, count) triples
- construct and delete each forward index entry
- delete the R record
- remove F/N records, update C records (decrement counts; delete record if count reaches zero)

When searching for a literal string
- if the index does not exist, compute the index
- compute the trigrams for the string
- filter to active trigrams (bottom `searchCutoff`% by frequency)
- intersect file+chunk sets from the index for each active query trigram
- output the file names and chunks, sorted by filename and chunknum
- CLI output format: one result per line, `filepath:startline-endline`
- library returns struct slices with the same information, plus IndexStatus

When searching for a regex pattern
- if the index does not exist, compute the index
- extract a trigram query (boolean AND/OR expression) from the regex AST, using rsc's approach (github.com/google/codesearch/regexp)
- evaluate the trigram query against the full index (not filtered to active set)
- AND nodes intersect candidate sets, OR nodes union them
- output format same as literal search
- library returns struct slices with the same information, plus IndexStatus

To compute the active set (build-index):
- cursor scan all C records → collect (trigram, count) pairs
- sort by count, take bottom `searchCutoff`% → write A record as packed sorted trigram list
- index entries are always present (maintained incrementally); build-index only recomputes the A record

# Built-in Chunking Strategies

The binary includes built-in chunkers invoked as `microfts chunk-* <file>`, outputting byte offsets to stdout — same interface as external chunkers. This lets `microfts` be its own strategy command.

## chunk-lines

Break file at line boundaries.

`microfts chunk-lines <file>`

Every line is a chunk. Equivalent to outputting the byte offset after each newline.

## chunk-lines-overlap

Fixed-size line windows with overlap.

`microfts chunk-lines-overlap [-lines N] [-overlap M] <file>`

- `-lines`: lines per chunk (default 50)
- `-overlap`: lines of overlap between consecutive chunks (default 10)

Chunk boundaries are at byte offsets corresponding to line starts. Each chunk starts `lines - overlap` lines after the previous chunk's start.

## chunk-words-overlap

Fixed-size word windows with overlap. Good for vector databases and hybrid search.

`microfts chunk-words-overlap [-words N] [-overlap M] [-pattern P] <file>`

- `-words`: words per chunk (default 200)
- `-overlap`: words of overlap between consecutive chunks (default 50)
- `-pattern`: regexp defining a "word" (default `\S+`)

Chunk boundaries are at byte offsets of the first word in each window. Each chunk starts `words - overlap` words after the previous chunk's start.

# CLI

All commands require `-db <path>`. Optional shared flags: `-content-db`, `-index-db`.

- `microfts init -db <path> [-charset <chars>] [-case-insensitive] [-aliases <from=to,...>]`
  Create a new database. Character set is immutable after creation.
- `microfts add -db <path> -strategy <name> <file>...`
  Add files using the named chunking strategy.
- `microfts search -db <path> [-regex] [-score coverage|density] <query>...`
  Search for text. Builds index on demand if needed. Output: `filepath:startline-endline`
  With `-regex`, query is a Go regexp pattern; trigram query extracted from the regex AST.
  With `-score`, select scoring strategy (default: coverage).
- `microfts delete -db <path> <file>...`
  Remove files from the database.
- `microfts reindex -db <path> -strategy <name> <file>...`
  Re-chunk and reindex files with a different strategy.
- `microfts build-index -db <path> [-cutoff <percentile>]`
  Explicitly build/rebuild the index. Default cutoff: 50.
- `microfts strategy add -db <path> -name <name> -cmd <command>`
  Register a chunking strategy.
- `microfts strategy remove -db <path> -name <name>`
  Remove a chunking strategy.
- `microfts strategy list -db <path>`
  List registered strategies.
- `microfts chunk-lines <file>`
  Output byte offsets for line-based chunking.
- `microfts chunk-lines-overlap [-lines N] [-overlap M] <file>`
  Output byte offsets for overlapping line windows.
- `microfts chunk-words-overlap [-words N] [-overlap M] [-pattern P] <file>`
  Output byte offsets for overlapping word windows.
- `microfts stale -db <path>`
  List all stale and missing files. Output: one line per file, `status\tpath` (tab-separated).
- `microfts score -db <path> <query> <file>...`
  Score named files against a query. Output: one line per chunk, `filepath:startline-endline\tscore`.

- `-r` flag (global, before subcommand):
  Refresh all stale files before executing the subcommand. Uses each file's existing chunking strategy.
  - `microfts -r -db <path>` — refresh only, no subcommand
  - `microfts search -r -db <path> <query>` — refresh then search
  - When used without a subcommand, just refreshes and exits (printing refreshed files)
  - Missing files are reported but not deleted

# Library API

```go
// Lifecycle
func Create(path string, opts Options) (*DB, error)
func Open(path string, opts Options) (*DB, error)
func (db *DB) Close() error
func (db *DB) Settings() Settings
func (db *DB) Env() *lmdb.Env

// Content
func (db *DB) AddFile(fpath, strategy string) (uint64, error)
func (db *DB) RemoveFile(fpath string) error
func (db *DB) Reindex(fpath, strategy string) (uint64, error)
func (db *DB) FileInfoByID(fileid uint64) (FileInfo, error)

// Search
func (db *DB) Search(query string, opts ...SearchOption) (*SearchResults, error)
func (db *DB) SearchRegex(pattern string, opts ...SearchOption) (*SearchResults, error)
func (db *DB) ScoreFile(query, fpath string, fn ScoreFunc) ([]ScoredChunk, error)

// Index
func (db *DB) BuildIndex(cutoff int) error

// Strategies
func (db *DB) AddStrategy(name, cmd string) error
func (db *DB) RemoveStrategy(name string) error
```

Options:
- CharSet, CaseInsensitive, Aliases — creation-time only
- ContentDBName, IndexDBName — defaults "ftscontent"/"ftsindex"
- MaxDBs — LMDB max named databases, default 2

SearchResult: `{ Path string, StartLine int, EndLine int, Score float64 }`
SearchResults: `{ Results []SearchResult, Status IndexStatus }`
IndexStatus: `{ Built bool }`
FileInfo: `{ Filename string, ChunkOffsets []int64, ChunkStartLines []int, ChunkEndLines []int, ChunkTokenCounts []int, ChunkingStrategy string, ModTime int64, ContentHash string }`
ScoredChunk: `{ StartLine int, EndLine int, Score float64 }`

ScoreFunc: `func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64`
SearchOption: `func(*searchConfig)` — functional option pattern
Built-in options: `WithCoverage()` (default), `WithDensity()`, `WithScoring(fn ScoreFunc)`

# Scoring Strategies

The search function accepts a scoring strategy that determines how candidate chunks are ranked. microfts2 provides built-in strategies and allows custom ones via `ScoreFunc`.

## Coverage (default)

"Does this chunk contain what I searched for?"

For intentional, short queries. User typed specific terms and wants chunks that match them.

Score = matching active trigrams / total active query trigrams

Binary match — counts are available but not consulted. A trigram either matches or it doesn't.

## Density

"Is this chunk about any of my terms?"

For long queries (conversation turns, full documents) where most query tokens won't match any given chunk. Separates "chunk is about this topic" from "chunk shares a few common trigrams."

1. Tokenize query on spaces
2. For each token, extract trigrams, filter to active set. Tokens with no surviving trigrams are discarded.
3. For each candidate chunk, for each surviving query token:
   - Look up that token's trigram counts in the chunk
   - Token match strength = min count across the token's trigrams. This approximates word frequency — "turnip" produces trigrams `tur`, `urn`, `rni`, `nip`; if counts are [3, 3, 1, 3] then the word appears ~1 time (bottleneck trigram governs).
   - If any trigram has count 0, the token doesn't match.
4. Score = sum of token match strengths / chunk token count

Normalizing by chunk token count prevents long chunks from winning on surface area alone. A 50-word chunk with 10 matching words scores higher than a 500-word chunk with the same 10 words.

# Staleness Detection

Each file's N record JSON stores the file's modification time (Unix nanoseconds) and a content hash (SHA-256) at the time it was indexed.

A file is **stale** when it exists on disk and either:
- its modification time differs from the stored value, AND
- its content hash differs from the stored value

A file is **missing** when it no longer exists on disk.

Checking modification time first avoids hashing when the file hasn't changed. When mod time matches, the file is considered fresh without hashing.

When mod time differs but the content hash matches (file was touched but not changed), update the stored mod time in the N record so subsequent checks short-circuit at the mod time comparison instead of re-hashing.

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

# Ark Integration

microfts2 and microvec share the same LMDB environment when used together in ark. LMDB does not allow two env handles on the same database path within one process, so the first library opened provides the env to the second.

## MaxDBs

LMDB requires `SetMaxDBs` before opening the environment. microfts2 uses 2 named subdatabases (content and index). When sharing the environment with other libraries (e.g. microvec), the host process needs a higher limit. `Options.MaxDBs` sets this, defaulting to 2.

## Env accessor

`Env()` returns the underlying `*lmdb.Env`. The host process opens microfts2 first, gets the env, and passes it to microvec.

## Fileid surfacing

`AddFile` and `Reindex` return the fileid (uint64) alongside the error. The fileid is already computed internally — it just needs to be returned. microvec keys its embedding records by fileid.

## FileInfo lookup

`FileInfoByID(fileid)` resolves a fileid to its FileInfo (filename, chunk offsets, line numbers, strategy, modTime, contentHash). microvec search returns `(fileid, chunknum, score)` — the CLI needs to resolve these to human-readable output using this method. Wraps the existing `readFileInfo` in a read transaction.

## Scoring

`ScoreFile(query, fpath, fn ScoreFunc)` returns per-chunk scores for a single file using the given scoring function. The machinery is in the search path — this scopes it to one file's index entries.

`SearchResult` gains a `Score float64` field so the general search path also reports per-chunk scores.
