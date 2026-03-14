# microfts
A dynamic LMDB trigram index, written in Go. CLI command, structured so it can also be used as a library.

# features

## configurable chunking strategies

- add/remove chunking strategies dynamically (external commands or Go functions)
  - external: `AddStrategy(name, cmd)` — command is persisted in I record
  - function: `AddStrategyFunc(name, fn)` — in-memory only, must re-register on Open
  - `ChunkFunc` type: generator using Go iterator pattern
    - `func(path string, content []byte, yield func(Chunk) bool) error`
    - yields `Chunk{Range []byte, Content []byte}` structs
    - both Range and Content are reusable buffers — the caller must copy before the next yield
    - Range has string semantics: opaque to microfts2, meaningful to the chunker and the user
    - Content is the text to be trigram-indexed for this chunk
  - when AddFile/Reindex uses a func strategy, calls the function directly (no exec)
  - I record stores name with empty cmd for func strategies (marks as registered)
  - built-in chunkers (chunk-lines, chunk-lines-overlap, chunk-words-overlap) register as func strategies
- chunkers serve two purposes:
  - indexing: produce chunks with content to trigram-index and a range label
  - extraction: given the same file, re-chunk to retrieve a specific chunk's content by its range
  - the range is an opaque string label: for text chunkers it's "startline-endline", for other formats it's whatever the chunker needs (e.g. "sheet1:A1-B20", "slides/3:para/7")
  - chunkers must be deterministic: same file produces same chunks with same ranges
- files track their indexed chunking strategy
  - can reindex with a different strategy -- allows migration to better strategies
- raw byte trigrams -- every byte is its own value, no character set mapping
  - whitespace bytes (space, tab, newline, carriage return) are word boundaries; runs collapse
  - all non-whitespace bytes are indexed
  - case insensitivity: bytes.ToLower() on input before trigram extraction
  - byte aliases: map byte→byte before extraction (e.g. newline → `^` for line-start matching). Both source and target bytes must be ASCII (< 0x80) — aliasing UTF-8 continuation or leading bytes would corrupt multibyte characters and break character-internal trigram skipping.
  - UTF-8 required — AddFile checks each chunk's Content for valid UTF-8 (utf8.Valid). The raw file itself may be binary (e.g. ODF zip); the chunker is responsible for producing UTF-8 text content.
  - character-internal byte trigrams are skipped during extraction
    - a 3-byte window that falls entirely within a single multibyte character is not emitted
    - 3-byte characters (CJK): 1 internal trigram skipped per character
    - 4-byte characters (emoji): 2 internal trigrams skipped per character
    - 2-byte characters: no internal trigrams possible, no skipping needed
    - ASCII: no multibyte characters, identical behavior
    - cross-boundary trigrams preserved — effectively encode character bigrams for CJK search
  - 8 bits / byte, 24 bits per trigram
  - 16M possible trigrams (2^24 = 16,777,216)
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
The content DB stores file metadata and trigram frequency counts. The index DB stores the trigram-to-chunk mappings with occurrence counts, maintained incrementally.

Subdatabase names are parameters: defaults 'ftscontent' and 'ftsindex', settable via CLI and library API.
These are not stored in the I record -- they're needed to open the databases in the first place.

- content db:
  - C [trigram: 3] -> count: 8 (big-endian) — per-trigram occurrence count, sparse (only non-zero trigrams stored)
  - 'I' -> JSON -- database settings
    - chunkingStrategies: object
      - {name: cmd}: [cmd] [filename] -> stream of `range\tcontent` lines; empty cmd means func strategy (in-memory only)
    - aliases: object -- maps input bytes to replacement bytes before trigram extraction (e.g. {"\n": "^"})
    - caseInsensitive: boolean
  - 'N' [fileid: 8] -> JSON -- information about file
    - filename: string -- full file path
    - chunkRanges: array[string] -- opaque range label per chunk (from chunker); array index = chunk number (0-based), preserving chunker emission order
    - chunkTokenCounts: array[number] -- token count per chunk (for density scoring normalization); parallel to chunkRanges
    - chunkingStrategy: string -- which chunking strategy was used
    - modTime: number -- file modification time (Unix nanoseconds) at index time
    - contentHash: string -- SHA-256 hex of file contents at index time
    - fileLength: number -- file size in bytes at index time
  - 'F' [name part: 1] filename-prefix -> filename prefix, name part 255 indicates end of prefix (see previous)
  - 'F' 255 filename-suffix -> [fileid: 8]: file info for reindexing changed files / deleting

- index db:
  - forward entries: [trigram: 3] [count: 2 desc] [fileid: 8] [chunknum: 8] -- trigram occurrences per chunk, high counts sort first
    - count is 2-byte big-endian stored as 0xFFFF - count for descending lexical sort
    - capped at 65535
  - reverse entries: R [fileid: 8] -> chunk-grouped packed format
    - repeated: [chunknum: 8] [numTrigrams: 2] [[trigram: 3] [count: 2]]...
    - one record per file, used only for removal
    - contains all info needed to reconstruct forward keys for deletion

# Full Trigram Index

The index DB contains entries for ALL trigrams present in the content. This makes the index complete and usable for both literal and regex search.

Trigram selection for queries is handled dynamically via `TrigramFilter` functions supplied at search time. This allows callers to adapt filtering strategy per query rather than relying on a static global cutoff.

The index is the authoritative record of which trigrams appear in which chunks and how many times. There are no per-chunk bitsets (T records) — the index is maintained incrementally on add/remove. If the index DB is lost, files must be re-added from disk.

# The process

We add a file to the database with a chosen chunking strategy:
- read file content, check utf8.Valid
- create the F record for the file to get its fileid
- chunk: call ChunkFunc generator, which yields {Range, Content} per chunk
  - caller copies Range (as string) and Content before next yield
  - for external command strategies, a wrapper ChunkFunc parses the command output
- for each chunk: compute trigrams on Content (not raw file bytes), count tokens on Content
- create the N record (chunkRanges, chunkTokenCounts)
- update C records (increment count for each trigram; insert if first occurrence)
- write forward index entries: [trigram][desc-count][fileid][chunknum]
- write reverse index entry: R[fileid] -> packed chunk/trigram/count data

When removing a file:
- look up R[fileid] to get all (chunknum, trigram, count) triples
- construct and delete each forward index entry
- delete the R record
- remove F/N records, update C records (decrement counts; delete record if count reaches zero)

When searching for a literal string:
- trim leading and trailing whitespace from the query before trigram extraction
- parse the query into terms using `parseQueryTerms`: unquoted words split on spaces, double-quoted phrases treated as single terms with quotes stripped
- extract trigrams per term (not from the whole query as one byte sequence) — this avoids cross-boundary trigrams between unrelated words (e.g. "daneel olivaw" must not produce trigrams "l o", " ol")
- the candidate set is the intersection of all terms' trigram candidate sets — a chunk must match all terms
- select query trigrams via TrigramFilter (default: FilterAll — use all trigrams); filter is applied to the combined trigram set
  - look up each query trigram's C record count
  - get total chunk count from N records
  - call filter function to select subset
- for each selected query trigram, scan the index by trigram prefix
  (ignoring count field) to collect candidate (fileid, chunknum) pairs
- intersect candidate sets across all selected query trigrams
- for each surviving candidate, collect per-trigram counts from the
  index keys
- score each candidate using the selected scoring function
  (coverage or density)
- sort by score descending, return top-k
- CLI output format: one result per line, `filepath:range` (range is the chunk's opaque label)
- library returns struct slices with the same information, plus IndexStatus

When searching for a regex pattern
- extract a trigram query (boolean AND/OR expression) from the regex AST, using rsc's approach (github.com/google/codesearch/regexp)
- evaluate the trigram query against the full index (no trigram filtering)
- AND nodes intersect candidate sets, OR nodes union them
- verify: re-chunk each candidate file using the stored chunking strategy, run the compiled regex against the chunk content, discard non-matches (always, not opt-in — trigram query is a superset filter)
- output format same as literal search
- library returns struct slices with the same information, plus IndexStatus

# Built-in Chunking Strategies

The binary includes built-in chunkers registered as func strategies. They can also be invoked as CLI subcommands (`microfts chunk-* <file>`) outputting `range\tcontent` lines to stdout.

For all built-in text chunkers, the range is `startline-endline` (1-based, inclusive) and the content is the raw text of those lines. This means CLI search output like `filepath:3-17` is the same format as before.

## chunk-lines

Break file at line boundaries.

`microfts chunk-lines <file>`

Every line is a chunk. Range: `N-N` (single line number). Content: the line text.

## chunk-lines-overlap

Fixed-size line windows with overlap.

`microfts chunk-lines-overlap [-lines N] [-overlap M] <file>`

- `-lines`: lines per chunk (default 50)
- `-overlap`: lines of overlap between consecutive chunks (default 10)

Each chunk starts `lines - overlap` lines after the previous chunk's start. Range: `startline-endline`. Content: the text of those lines.

## chunk-words-overlap

Fixed-size word windows with overlap. Good for vector databases and hybrid search.

`microfts chunk-words-overlap [-words N] [-overlap M] [-pattern P] <file>`

- `-words`: words per chunk (default 200)
- `-overlap`: words of overlap between consecutive chunks (default 50)
- `-pattern`: regexp defining a "word" (default `\S+`)

Each chunk starts `words - overlap` words after the previous chunk's start. Range: `startline-endline` (lines spanning the word window). Content: the text of those lines.

## chunk-markdown

Paragraph-based splitting for markdown files.

`microfts chunk-markdown <file>`

Splits on blank lines and heading transitions:
- A heading line (`#`...) always starts a new chunk
- A heading and its following paragraph (up to the next blank line or heading) form one chunk
- Consecutive blank lines collapse to a single boundary
- Non-heading text between boundaries is one chunk
- Blank lines are boundaries only — they are not included in any chunk's content
- Gaps between chunks are expected; each chunk's range notes its precise position in the file

Range: `startline-endline` (1-based, inclusive). Content: the raw text of those lines (excluding boundary blank lines).

Exported as `microfts2.MarkdownChunkFunc` for direct use as a `ChunkFunc`.

# CLI

All commands require `-db <path>`. Optional shared flags: `-content-db`, `-index-db`.

- `microfts init -db <path> [-case-insensitive] [-aliases <from=to,...>]`
  Create a new database.
- `microfts add -db <path> -strategy <name> <file>...`
  Add files using the named chunking strategy.
- `microfts search -db <path> [-regex] [-score coverage|density] [-verify] <query>...`
  Search for text. Builds index on demand if needed. Output: `filepath:range`
  With `-regex`, query is a Go regexp pattern; trigram query extracted from the regex AST.
  With `-score`, select scoring strategy (default: coverage).
  With `-verify`, post-filter results: for each candidate chunk, re-chunk the file using the stored chunking strategy to recover the chunk content (same text the trigrams were built from), tokenize the query into terms, and verify that every term appears as a case-insensitive substring in the chunk content. Chunks that fail are discarded. This eliminates false positives where trigrams match independently but the actual words are absent.
  Query tokenization for verify: split on spaces, but quoted strings (double quotes) are treated as a single term with quotes stripped. E.g. `"hello world" foo` produces terms `hello world` and `foo`.
- `microfts delete -db <path> <file>...`
  Remove files from the database.
- `microfts reindex -db <path> -strategy <name> <file>...`
  Re-chunk and reindex files with a different strategy.
- `microfts strategy add -db <path> -name <name> -cmd <command>`
  Register a chunking strategy.
- `microfts strategy remove -db <path> -name <name>`
  Remove a chunking strategy.
- `microfts strategy list -db <path>`
  List registered strategies.
- `microfts chunk-lines <file>`
  Output chunks for line-based chunking (`range\tcontent` per line).
- `microfts chunk-lines-overlap [-lines N] [-overlap M] <file>`
  Output chunks for overlapping line windows (`range\tcontent` per chunk).
- `microfts chunk-words-overlap [-words N] [-overlap M] [-pattern P] <file>`
  Output chunks for overlapping word windows (`range\tcontent` per chunk).
- `microfts chunk-markdown <file>`
  Output chunks for markdown paragraph-based chunking (`range\tcontent` per chunk).
- `microfts stale -db <path>`
  List all stale and missing files. Output: one line per file, `status\tpath` (tab-separated).
- `microfts score -db <path> [-score coverage|density] <query> <file>...`
  Score named files against a query. Output: one line per chunk, `filepath:range\tscore`.
- `microfts chunks -db <path> [-before N] [-after N] <file> <range>`
  Retrieve a target chunk and its neighbors. Looks up the file's chunk list from the N record, finds the target by range label match, returns the target plus up to N chunks before and after. Output: JSONL, one object per chunk with `path`, `range`, `content` fields. The target chunk is always included; neighbors are positional (chunk index ± N). Requires re-chunking the file to recover content. `-before` and `-after` default to 0.

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
func (db *DB) AddFileWithContent(fpath, strategy string) (uint64, []byte, error)
func (db *DB) RemoveFile(fpath string) error
func (db *DB) Reindex(fpath, strategy string) (uint64, error)
func (db *DB) ReindexWithContent(fpath, strategy string) (uint64, []byte, error)
func (db *DB) FileInfoByID(fileid uint64) (FileInfo, error)
func (db *DB) AppendChunks(fileid uint64, content []byte, strategy string, opts ...AppendOption) error

// Search
func (db *DB) Search(query string, opts ...SearchOption) (*SearchResults, error)
func (db *DB) SearchRegex(pattern string, opts ...SearchOption) (*SearchResults, error)
func (db *DB) ScoreFile(query, fpath string, fn ScoreFunc, opts ...SearchOption) ([]ScoredChunk, error)

// Chunk Retrieval
func (db *DB) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)

// Strategies
func (db *DB) AddStrategy(name, cmd string) error
func (db *DB) AddStrategyFunc(name string, fn ChunkFunc) error
func (db *DB) RemoveStrategy(name string) error
```

Chunk: `{ Range []byte, Content []byte }` — both reusable buffers, caller must copy before next yield
ChunkFunc: `func(path string, content []byte, yield func(Chunk) bool) error` — generator pattern

Options:
- CaseInsensitive, Aliases — creation-time only
- ContentDBName, IndexDBName — defaults "ftscontent"/"ftsindex"
- MaxDBs — LMDB max named databases, default 2

SearchResult: `{ Path string, Range string, Score float64 }`
SearchResults: `{ Results []SearchResult, Status IndexStatus }`
IndexStatus: `{ Built bool }`
FileInfo: `{ Filename string, ChunkRanges []string, ChunkTokenCounts []int, ChunkingStrategy string, ModTime int64, ContentHash string, FileLength int64 }`
ScoredChunk: `{ Range string, Score float64 }`
ChunkResult: `{ Path string, Range string, Content string, Index int }` — a chunk with its content and position in the file's chunk list

ScoreFunc: `func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64`
SearchOption: `func(*searchConfig)` — functional option pattern
Built-in options: `WithCoverage()` (default), `WithDensity()`, `WithScoring(fn ScoreFunc)`, `WithVerify()` (post-filter: re-chunk file using stored strategy, tokenize query into terms — split on spaces, quoted strings as single terms — verify each term is a case-insensitive substring of the chunk content; eliminates trigram false positives), `WithTrigramFilter(fn TrigramFilter)` (caller-supplied trigram selection)

TrigramCount: `{ Trigram uint32, Count int }` — trigram code with its corpus document frequency
TrigramFilter: `func(trigrams []TrigramCount, totalChunks int) []TrigramCount` — selects which query trigrams to search with
Stock filters: `FilterAll` (use all), `FilterByRatio(maxRatio float64)` (skip high-frequency), `FilterBestN(n int)` (keep N lowest-frequency)

AppendOption: `func(*appendConfig)` — functional option pattern
Built-in append options: `WithContentHash(hash string)` (full-file SHA-256 — caller pre-computed), `WithModTime(t int64)` (Unix nanos), `WithFileLength(n int64)` (full file size after append), `WithBaseLine(n int)` (1-based line number offset for line-based chunker ranges; 0 means no adjustment)

# AddFile Duplicate Guard

`AddFile` and `AddFileWithContent` must not create duplicate entries for an already-indexed path. Before allocating a new fileid, `addFileInTxn` checks whether F records already exist for the path (via `FinalKey` lookup). If the file is already indexed, return `ErrAlreadyIndexed` — a sentinel error the caller can check with `errors.Is`. The caller should use `Reindex` or `AppendChunks` instead.

```go
var ErrAlreadyIndexed = errors.New("file already indexed")
```

This is a guard, not a policy decision — the caller decides what to do when they get this error.

# Scoring Strategies

The search function accepts a scoring strategy that determines how candidate chunks are ranked. microfts2 provides built-in strategies and allows custom ones via `ScoreFunc`.

## Coverage (default)

"Does this chunk contain what I searched for?"

For intentional, short queries. User typed specific terms and wants chunks that match them.

Score = matching selected trigrams / total selected query trigrams

Binary match — counts are available but not consulted. A trigram either matches or it doesn't.

## Density

"Is this chunk about any of my terms?"

For long queries (conversation turns, full documents) where most query tokens won't match any given chunk. Separates "chunk is about this topic" from "chunk shares a few common trigrams."

1. Tokenize query on spaces
2. For each token, extract trigrams, apply trigram filter. Tokens with no surviving trigrams are discarded.
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

# Append Detection Support

For append-only files (e.g. JSONL conversation logs), ark wants to detect that a file change was an append and skip full reindex. microfts2 provides the primitives; ark implements the detection logic.

## FileLength in N record

The N record stores `fileLength` (int64): the file size in bytes at index time. `AddFile` and `Reindex` set this from the file content they already read. Ark reads this to hash only the prefix up to the stored length, detecting whether a change was purely an append.

## AppendChunks API

Add chunks to an existing file without full reindex.

```go
func (db *DB) AppendChunks(fileid uint64, content []byte, strategy string) error
```

Chunks `content` using the named strategy, adds the resulting chunks and trigrams to the existing file's records. The `content` parameter is only the new bytes (the appended portion), not the full file.

Updates the N record: new ContentHash (of the full file — caller provides or computed externally), ModTime, FileLength, appended ChunkRanges, appended ChunkTokenCounts. Does NOT touch existing chunks or trigrams — they remain valid.

Chunk numbering continues from the file's existing chunk count. Forward and reverse index entries are added for the new chunks only. C records are incremented for the new chunks' trigrams.

The reverse index entry (R record) is replaced with a new packed record containing both old and new chunk data.

## Chunker offset support

When `AppendChunks` passes content to a `ChunkFunc`, the content starts at an arbitrary byte offset in the original file, not byte 0. For line-based chunkers, this means line numbering must account for lines already processed.

`AppendChunks` passes a base line number to line-based chunkers so that Range labels (e.g. "51-60") are absolute, not relative to the appended slice. The mechanism: `ChunkFunc` signature is unchanged; `AppendChunks` counts newlines in a prefix window or accepts a base line count from the caller, then adjusts the Range values after chunking.

Suggestion: `AppendChunks` accepts an optional base line number. When zero, ranges are used as-is (for non-line-based chunkers). When non-zero, line-based ranges are offset by that amount.

# Ark Integration

microfts2 and microvec share the same LMDB environment when used together in ark. LMDB does not allow two env handles on the same database path within one process, so the first library opened provides the env to the second.

## MaxDBs

LMDB requires `SetMaxDBs` before opening the environment. microfts2 uses 2 named subdatabases (content and index). When sharing the environment with other libraries (e.g. microvec), the host process needs a higher limit. `Options.MaxDBs` sets this, defaulting to 2.

## Env accessor

`Env()` returns the underlying `*lmdb.Env`. The host process opens microfts2 first, gets the env, and passes it to microvec.

## Fileid surfacing

`AddFile` and `Reindex` return the fileid (uint64) alongside the error. The fileid is already computed internally — it just needs to be returned. microvec keys its embedding records by fileid.

## FileInfo lookup

`FileInfoByID(fileid)` resolves a fileid to its FileInfo (filename, chunk ranges, strategy, modTime, contentHash). microvec search returns `(fileid, chunknum, score)` — the CLI needs to resolve these to human-readable output using this method. Wraps the existing `readFileInfo` in a read transaction.

## Scoring

`ScoreFile(query, fpath, fn ScoreFunc)` returns per-chunk scores for a single file using the given scoring function. The machinery is in the search path — this scopes it to one file's index entries.

`SearchResult` gains a `Score float64` field so the general search path also reports per-chunk scores.

# Dynamic Trigram Filtering

## Problem

A static global cutoff can't adapt to what you're searching for. Different content types have different frequency distributions — trigrams that are noise in one corpus are signal in another.

## Design: Caller-Supplied Filter Function

Move the trigram selection policy out of microfts2 and into the caller. microfts2 provides the mechanism (trigram counts, search pipeline); the caller provides the strategy.

### TrigramFilter type

```go
// TrigramCount pairs a trigram code with its document frequency.
type TrigramCount struct {
    Trigram uint32
    Count   int
}

// TrigramFilter decides which trigrams to use for a given query.
// Receives the query's trigrams with their corpus-wide document
// frequencies, and the total number of indexed chunks.
// Returns the subset to search with.
type TrigramFilter func(trigrams []TrigramCount, totalChunks int) []TrigramCount
```

### Search integration

`WithTrigramFilter(fn TrigramFilter)` search option supplies a filter function.

- The search path looks up each query trigram's C record count, calls the filter, and uses the returned subset.
- When no filter is supplied, `FilterAll` is used (all query trigrams searched).
- `WithTrigramFilter` applies to both `Search` and `ScoreFile`.

### Stock filters

microfts2 ships stock filter functions:

- `FilterAll`: uses every query trigram, no filtering.
- `FilterByRatio(maxRatio float64)`: skips trigrams appearing in more than `maxRatio` of total chunks. E.g., `FilterByRatio(0.50)` skips trigrams in >50% of chunks.
- `FilterBestN(n int)`: keeps the N trigrams with the lowest document frequency. Good for long queries where only the most discriminating trigrams matter.

### Trigram count lookup

Per-query C record point reads: look up each query trigram's sparse C record at search time. Typically 3-10 LMDB reads per query.

The total chunk count is derived from the database (sum of file chunk counts from N records, or maintained as a counter).

# Multi-Regex Post-Filtering

Search results can be post-filtered at the chunk level using multiple regex patterns. Two kinds of filter:

- **Regex filters (AND):** every pattern must match the chunk content. A chunk is kept only if all regex filters match.
- **Except-regex filters (subtract):** any pattern matching rejects the chunk. A chunk is discarded if any except-regex matches.

Both filter types operate on chunk content recovered by re-chunking the file (same mechanism as `WithVerify`). They apply after trigram candidate selection and scoring, before final sort — to both `Search` and `SearchRegex`.

When combined with `SearchRegex`, the primary regex still drives trigram extraction and candidate selection. Regex filters and except-regex filters are independent post-filters applied to those candidates.

## Library API

```go
// WithRegexFilter adds AND post-filters: every pattern must match chunk content.
// Multiple calls accumulate patterns.
func WithRegexFilter(patterns ...string) SearchOption

// WithExceptRegex adds subtract post-filters: any match rejects the chunk.
// Multiple calls accumulate patterns.
func WithExceptRegex(patterns ...string) SearchOption
```

Patterns are stored as strings in the search config. They are compiled to `*regexp.Regexp` at the start of `Search`/`SearchRegex`, which already return errors — compilation failure is a normal error return. Filtering uses the existing `filterResults` helper with a combined match function that checks all compiled regexes.

## CLI

```
microfts search -db <path> [-regex] [-contains <text>] [-filter-regex <pattern>]... [-except-regex <pattern>]... <query>
```

- `-filter-regex` is repeatable: each invocation adds an AND regex filter
- `-except-regex` is repeatable: each invocation adds a subtract regex filter
- Both work with literal and regex search modes
- Implemented via a custom `flag.Value` type for string slice accumulation

### Composing `--contains` with `--regex`

`--contains` provides an explicit FTS text query. When combined with `--regex`, the two compose naturally:

- `--regex` alone with positional args: positional args are the regex pattern → `SearchRegex` (unchanged)
- `--contains` alone (no positional args needed): FTS text query → `Search`
- `--contains` with `--regex` and positional args: FTS on the `--contains` text via `Search`, with the positional regex pattern added as a `WithRegexFilter` post-filter
- Neither flag, positional args: FTS text query → `Search` (unchanged)

This removes the mutual exclusion between FTS and regex — `--contains` narrows candidates via trigram index, `--regex` verifies via post-filter.

## Use cases

```
microfts search -db idx --contains "chunk" --regex '@to-project:.*\bmicrofts2\b'
ark search --regex '@to-project:.*\bark\b' --except-regex '@status:.*\bdone\b'
```

Ark translates its own `--regex`/`--except-regex` flags to `WithRegexFilter`/`WithExceptRegex` options on the microfts2 library call. Finds open requests filed against ark — positive match on the project tag, subtract anything marked done.

# Chunk Context Retrieval

Retrieve a target chunk and its positional neighbors from an indexed file. This enables "flip pages" research loops: search → hit → expand context → decide.

## How it works

1. Look up the file's N record to get its ordered `chunkRanges` array
2. Find the target chunk by range label match (exact string comparison)
3. Compute the window: `max(0, targetIndex - before)` to `min(len-1, targetIndex + after)`
4. Re-chunk the file using its stored chunking strategy to recover chunk content
5. Return the chunks in the window with their range labels, content, and chunk indices

The expansion unit is chunks, not lines or bytes. Range labels are opaque and strategy-dependent — the only universal coordinate is the chunk index within the file's ordered chunk list.

## Error cases

- File not in database: error
- Target range not found in chunk list: error
- File missing from disk (can't re-chunk): error
- Chunking strategy not registered: error

## Library API

```go
// ChunkResult holds a single chunk with its content and position.
type ChunkResult struct {
    Path    string
    Range   string
    Content string
    Index   int  // 0-based position in the file's chunk list
}

// GetChunks retrieves the target chunk (identified by range label) and
// up to before/after positional neighbors. Returns chunks in order.
func (db *DB) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)
```

## CLI

```
microfts chunks -db <path> [-before N] [-after N] <file> <range>
```

Output: JSONL, one JSON object per line with `path`, `range`, `content`, `index` fields. Chunks are in positional order. `-before` and `-after` default to 0 (target only).

