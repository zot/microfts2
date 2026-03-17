# microfts
A dynamic LMDB trigram index, written in Go. CLI command, structured so it can also be used as a library.

# features

## configurable chunking strategies

- add/remove chunking strategies dynamically (external commands or Go functions)
  - external: `AddStrategy(name, cmd)` — command is persisted in I record
  - function: `AddChunker(name, c)` — in-memory only, must re-register on Open
  - `Chunker` interface with two methods:
    - `Chunks(path string, content []byte, yield func(Chunk) bool) error` — producer: yields chunks for indexing
    - `ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool)` — retriever: extracts a single chunk's content by its range label
  - `Chunk` struct: `{ Range []byte, Content []byte, Attrs []Pair }`
    - Range and Content are reusable buffers — the caller must copy before the next yield
    - Range has string semantics: opaque to microfts2, meaningful to the chunker and the user
    - Content is the text to be trigram-indexed for this chunk
    - Attrs is optional per-chunk metadata (e.g. timestamp, role). Opaque to microfts2 — stored in C records and exposed to ChunkFilters. nil means no attrs.
  - `Pair` type: `{ Key []byte, Value []byte }` — opaque key-value pair. Allows duplicate keys. Mirrors the DB wire format.
  - `ChunkFunc` type preserved for convenience: `func(path string, content []byte, yield func(Chunk) bool) error`
  - `FuncChunker` adapter wraps a bare `ChunkFunc` into a `Chunker`:
    - `Chunks` delegates to the wrapped function
    - `ChunkText` re-runs the wrapped function and returns the first chunk whose Range matches the label
  - `AddStrategyFunc(name, fn)` convenience: wraps fn in FuncChunker, calls AddChunker
  - when AddFile/Reindex uses a func strategy, calls the Chunker directly (no exec)
  - I record stores name with empty cmd for func strategies (marks as registered)
  - built-in chunkers (chunk-lines, chunk-lines-overlap, chunk-words-overlap) register as func strategies
- chunkers serve two purposes via the Chunker interface:
  - indexing (Chunks method): produce chunks with content to trigram-index, a range label, and optional attrs
  - extraction (ChunkText method): given the same file, retrieve a specific chunk's content by its range label — may be optimized (e.g. a markdown chunker can jump to the right heading without full scan)
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

## Single Subdatabase with Chunk Deduplication

All records live in one LMDB subdatabase, distinguished by prefix byte. Chunks are deduplicated by content hash — the same chunk content appearing in multiple files is stored once.

Subdatabase name is a parameter: default 'fts', settable via CLI and library API.
Not stored in the I record — needed to open the database in the first place.

### Why one tree

One B-tree instead of two halves the LMDB page overhead and simplifies transactions (no cross-database coordination).

### Why chunk deduplication

Overlapping chunking strategies produce shared content across adjacent windows. Files with common boilerplate share chunks. Deduplication means shared content is indexed once — fewer C records, fewer T record entries, smaller mmap.

### Encoding conventions

- Integer fields use varint encoding (Go binary.PutUvarint / binary.ReadUvarint)
- Trigram fields are fixed 3 bytes (24-bit)
- Hash fields are fixed 32 bytes (SHA-256)
- Strings are length-prefixed (varint length + bytes), except the final field in a key can use remaining bytes (computed from record length)

### Record types

| Prefix | Key                    | Value                                 | Purpose                                                          |
|--------|------------------------|---------------------------------------|------------------------------------------------------------------|
| I      | name: str              | empty (value encoded in key)          | Config settings, data-in-key pattern                             |
| H      | hash: 32               | chunkid: varint                       | Content hash → chunkid lookup                                    |
| C      | chunkid: varint        | hash + trigrams + tokens + fileids    | Per-chunk: all analysis data                                     |
| F      | fileid: varint         | metadata + names + chunks + token bag | Per-file: staleness info, ordered chunk list, file-level scoring |
| N      | chain-byte + name: str | (varies by chain-byte)                | Filename → fileid mapping via key chains                         |
| T      | trigram: 3             | chunkid: varint...                    | Trigram inverted index                                           |
| W      | token-hash: 4          | chunkid: varint...                    | Token inverted index for IDF                                     |

### Record details

- `I` [name: str] = [value: str] -> empty
  Config record using data-in-key pattern. Each setting is independently readable and writable — no JSON parse/serialize cycle.

- `H` [hash: 32] -> [chunkid: varint]
  Content hash to chunkid lookup. Used during AddFile to detect duplicate chunks.

- `C` [chunkid: varint] -> [hash: 32] [n-trigrams: varint] [[trigram: 3] [count: varint]]... [n-tokens: varint] [[count: varint] [token: str]]... [n-attrs: varint] [[key: bytes] [value: bytes]]... [n-fileids: varint] [fileid: varint]...
  Per-chunk record. Contains everything known about the chunk: content hash, packed trigram+count pairs, packed token+count pairs, optional attributes (opaque key-value pairs from chunker Attrs), and the list of files containing this chunk. Self-describing — all data needed for search, scoring, filtering, and removal. Date filtering reads the `timestamp` attr directly from C during candidate evaluation — zero extra reads.

- `F` [fileid: varint] -> [modTime: 8] [contentHash: 32] [fileLength: varint] [strategy: str] [filecount: varint] [name: str]... [chunkcount: varint] [[chunkid: varint] [location: str]]... [tokencount: varint] [[token: str] [count: varint]]
  Per-file record. Stores file metadata (mod time as Unix nanos, SHA-256 content hash, file length, chunking strategy name). Multiple names handle duplicate/copied files mapping to the same fileid. Ordered chunk list with opaque location labels (range strings from chunker). Aggregated token bag (union of all chunk tokens with summed counts) for file-level scoring without reading every chunk's C record.

- `N` [0-254] [name: str] -> empty — filename prefix chain key
- `N` [255] [name: str] -> [[full-name: str] [fileid: varint]]... — final chain key; value has full filename + fileid

- `T` [trigram: 3] -> [chunkid: varint]...
  Trigram inverted index. Value is a packed list of chunkids. Document frequency is free: value length / chunkid size. One entry per distinct trigram rather than one per trigram-chunk pair — the primary source of mmap reduction.

- `W` [token-hash: 4] -> [chunkid: varint]...
  Token inverted index. Same structure as T records but keyed by token hash. Provides exact token-level IDF for BM25 scoring. Document frequency from value length, same as T records.

### Data at three levels

| Level  | Source                                         | Use                                   |
|--------|------------------------------------------------|---------------------------------------|
| Chunk  | C record: per-trigram counts, per-token counts | Per-chunk TF, density scoring, verify |
| Chunk  | C record: attrs (e.g. timestamp, role)         | Date filtering, metadata-aware search |
| File   | F record: aggregated token bag                 | File-level ranking, pre-filtering     |
| Corpus | T record: chunkid list length = trigram DF     | Trigram IDF                           |
| Corpus | W record: chunkid list length = token DF       | Token IDF for BM25                    |

### Estimated entry counts (ark scale: 74K chunks, 2K files, 500K distinct trigrams)

| Record type | Estimated entries |
|-------------|-------------------|
| T (trigram → chunkids) | ~500K |
| C (per-chunk data) | ≤74K (fewer with dedup) |
| H (hash → chunkid) | ≤74K |
| W (token → chunkids) | ~50K (est.) |
| F (per-file data) | ~2K |
| N (name lookup) | ~2K |
| I (config) | ~10 |
| **Total** | **~630K** |

LMDB mmap pressure scales with B-tree entry count, not data volume. Packing per-trigram data into T record values (one entry per distinct trigram) and per-chunk data into C record values (one entry per unique chunk) keeps the entry count low while the data volume stays comparable.

# Full Trigram Index

The T records contain entries for ALL trigrams present in the content. This makes the index complete and usable for both literal and regex search.

Trigram selection for queries is handled dynamically via `TrigramFilter` functions supplied at search time. This allows callers to adapt filtering strategy per query rather than relying on a static global cutoff.

The index is maintained incrementally on add/remove. If the database is lost, files must be re-added from disk.

# The process

We add a file to the database with a chosen chunking strategy:
- read file content, check utf8.Valid
- check for existing F records via FinalKey — return ErrAlreadyIndexed if present
- allocate fileid, create N records (filename key chain) and F record
- chunk: call Chunker.Chunks, which yields {Range, Content, Attrs} per chunk
  - caller copies Range (as string), Content, and Attrs before next yield
  - for external command strategies, RunChunkerFunc wraps the command as a Chunker
- for each chunk: compute SHA-256 hash, extract trigrams on Content, tokenize Content, copy Attrs
  - look up H record by hash — if chunkid exists, add fileid to existing C record (dedup)
  - if new chunk: allocate chunkid, create H record, create C record (hash + trigrams + tokens + attrs + fileid), append chunkid to T records for each trigram, append chunkid to W records for each token
- update F record: append (chunkid, location) pair, merge tokens into file-level token bag
- batch T/W record updates: accumulate all chunkids per trigram/token across the file, then one read-modify-write per affected T/W record

When removing a file:
- read F record to get the file's chunk list
- for each chunkid: read C record, remove this fileid from the fileid list
  - if C record has no remaining fileids: delete C record, remove chunkid from each T record (by trigram), remove chunkid from each W record (by token hash), delete H record
- delete F record, delete N records (key chain)

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

Exported as `microfts2.MarkdownChunkFunc` for direct use as a `ChunkFunc` (wraps into a Chunker via FuncChunker when registered).

# CLI

All commands require `-db <path>`. Optional shared flag: `-db-name` (subdatabase name, default "fts").

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
  Retrieve a target chunk and its neighbors. Looks up the file's chunk list from the F record, finds the target by range label match, returns the target plus up to N chunks before and after. Output: JSONL, one object per chunk with `path`, `range`, `content` fields. The target chunk is always included; neighbors are positional (chunk index ± N). Requires re-chunking the file to recover content. `-before` and `-after` default to 0.

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
func (db *DB) Version() (string, error)

// Content
func (db *DB) AddFile(fpath, strategy string) (uint64, error)
func (db *DB) AddFileWithContent(fpath, strategy string) (uint64, []byte, error)
func (db *DB) RemoveFile(fpath string) error
func (db *DB) Reindex(fpath, strategy string) (uint64, error)
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

// Strategies
func (db *DB) AddStrategy(name, cmd string) error
func (db *DB) AddChunker(name string, c Chunker) error
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

Chunk: `{ Range []byte, Content []byte, Attrs []Pair }` — Range and Content are reusable buffers, caller must copy before next yield. Attrs is optional per-chunk metadata, nil by default.
Pair: `{ Key []byte, Value []byte }` — opaque key-value pair, allows duplicate keys
ChunkFunc: `func(path string, content []byte, yield func(Chunk) bool) error` — generator pattern, convenience type
FuncChunker: adapter that wraps a ChunkFunc into a Chunker (ChunkText re-runs and matches by range label)

Options:
- CaseInsensitive, Aliases — creation-time only
- DBName — subdatabase name, default "fts"
- MaxDBs — LMDB max named databases, default 2

## TxnHolder interface

Records read from LMDB are tied to the transaction that read them. `TxnHolder` abstracts this — any value that carries a transaction implements it. Internal DB methods accept `TxnHolder` instead of raw `*lmdb.Txn`, so callers pass whatever they have (a CRecord, a transaction wrapper) without extraction or conversion.

```go
// TxnHolder is anything that carries an LMDB transaction.
type TxnHolder interface {
    Txn() *lmdb.Txn
}
```

CRecord implements `TxnHolder` via its `Txn()` accessor. A simple `txnWrap` struct wraps raw transactions from View/Update blocks. Navigation methods like `CRecord.FileRecord(fileid)` pass `self` as the TxnHolder — no extraction needed.

## Record structs

Go structs for each LMDB record type. Encoding/decoding lives in methods on the structs. The rest of the code works with typed data, not raw bytes.

```go
// CRecord is the per-chunk record. Self-describing: everything needed
// for search, scoring, filtering, and removal.
// Carries unexported db/txn — the chunk is tied to the transaction that read it.
// Implements TxnHolder.
type CRecord struct {
    ChunkID  uint64
    Hash     [32]byte
    Trigrams []TrigramEntry          // {Trigram uint32, Count int}
    Tokens   []TokenEntry            // {Token string, Count int}
    Attrs    []Pair                  // opaque per-chunk metadata from chunker (timestamp, role, etc.)
    FileIDs  []uint64
    db       *DB                     // unexported: transaction context
    txn      *lmdb.Txn              // unexported: transaction context
}

// TxnHolder implementation + direct access for power-user filters.
func (c *CRecord) Txn() *lmdb.Txn
func (c *CRecord) DB() *DB

// Convenience navigation — passes self as TxnHolder.
func (c *CRecord) FileRecord(fileid uint64) (FRecord, error)

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

SearchResult: `{ Path string, Range string, Score float64 }`
SearchResults: `{ Results []SearchResult, Status IndexStatus }`
IndexStatus: `{ Built bool }`
ScoredChunk: `{ Range string, Score float64 }`
ChunkResult: `{ Path string, Range string, Content string, Index int }` — a chunk with its content and position in the file's chunk list
MultiSearchResult: `{ Strategy string, Results []SearchResult }` — one strategy's top-k results from SearchMulti

ScoreFunc: `func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64`
SearchOption: `func(*searchConfig)` — functional option pattern
Built-in options: `WithCoverage()` (default), `WithDensity()`, `WithOverlap()`, `WithScoring(fn ScoreFunc)`, `WithVerify()` (post-filter: re-chunk file using stored strategy, tokenize query into terms — split on spaces, quoted strings as single terms — verify each term is a case-insensitive substring of the chunk content; eliminates trigram false positives), `WithTrigramFilter(fn TrigramFilter)` (caller-supplied trigram selection), `WithProximityRerank(topN int)` (post-filter: rerank top-N by query term proximity in chunk text)
Built-in score functions: `ScoreOverlap` (matching trigram count), `ScoreBM25(idf, avgdl)` (returns ScoreFunc closure)

## Chunk filtering

`ChunkFilter` receives the `CRecord` for a candidate chunk. Called during candidate evaluation — after T record intersection, before scoring. The C record is already loaded on the hot path (needed for per-trigram counts), so filtering adds a conditional check on data already in memory.

The CRecord carries unexported `db` and `txn` fields — the chunk is inherently tied to the transaction that read it. `Txn()` and `DB()` accessors expose the context for power-user filters. `FileRecord(fileid)` is a convenience method for the common case.

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

## Overlap (OR semantics)

"How many of my query trigrams appear in this chunk?"

Count of matching query trigrams, no normalization. The simplest fuzzy score — more matches = better. Useful when any partial match is interesting and the caller wants to rank by breadth of overlap rather than precision.

```go
func ScoreOverlap(queryTrigrams []uint32, chunkCounts map[uint32]int, _ int) float64
```

Fits `ScoreFunc` signature directly. Pure function, no extra state.

## BM25

Standard term frequency / inverse document frequency scoring. Uses per-trigram TF from the chunk's C record and corpus-wide IDF from T record value lengths.

BM25 needs IDF data that isn't available through the `ScoreFunc` signature. Solution: a closure factory that captures IDF and average document length, returning a `ScoreFunc`.

```go
func ScoreBM25(idf map[uint32]float64, avgTokenCount float64) ScoreFunc
```

The caller pre-computes IDF from T record value lengths and average token count from I record counters, then passes the returned closure as a `ScoreFunc`. No signature change needed.

### BM25 formula

For each query trigram t in the chunk:
- `tf(t)` = trigram count in the chunk (from C record)
- `idf(t) = ln((N - df(t) + 0.5) / (df(t) + 0.5) + 1)` where N = total chunks, df(t) = T record value length
- `score += idf(t) * (tf(t) * (k1 + 1)) / (tf(t) + k1 * (1 - b + b * dl/avgdl))`
- `k1 = 1.2`, `b = 0.75` (standard defaults)
- `dl` = chunk token count, `avgdl` = average chunk token count across corpus

### BM25 helper

```go
func (db *DB) BM25Func(queryTrigrams []uint32) (ScoreFunc, error)
```

Reads T records for per-trigram document frequencies, reads I record counters for total chunks and total tokens, computes IDF map and avgdl, returns a `ScoreBM25` closure. Convenience for callers who don't need custom IDF computation.

### I record counters for BM25

Two I record counters maintained atomically during AddFile, RemoveFile, and AppendChunks:
- `totalTokens`: sum of all chunk token counts across the corpus
- `totalChunks`: total number of unique chunks

Average document length: `avgdl = totalTokens / totalChunks`.

Updated in the same write transaction as other record changes — one extra `Get` + `Put` per counter, no additional I/O round-trips.

## Proximity reranking

Position-aware reranking for multi-term queries. Takes top-N results from the primary scorer, re-chunks each file to recover text, finds query term positions in the chunk content, and computes a proximity bonus based on how close the terms appear to each other.

```go
func WithProximityRerank(topN int) SearchOption
```

Proximity is a post-filter, not a primary scorer — it needs chunk text that isn't in the index. Applied after scoring and before final sort. Works with `Search`, `SearchMulti`, and `ScoreFile`.

The proximity bonus is computed as: for each pair of query terms found in the chunk, measure the minimum token distance. Score adjustment = `1 / (1 + minSpan)` where minSpan is the smallest window (in tokens) containing all query terms. Chunks where terms appear closer together get a higher adjustment.

# Multi-Strategy Search

`SearchMulti` runs one query through multiple scoring strategies in a single LMDB read transaction, sharing candidate collection. The candidate set (trigram intersection, T record reads, C record reads, chunk filter application) is computed once; only scoring diverges.

```go
type MultiSearchResult struct {
    Strategy string
    Results  []SearchResult
}

func (db *DB) SearchMulti(query string, strategies map[string]ScoreFunc,
    k int, opts ...SearchOption) ([]MultiSearchResult, error)
```

- `strategies`: map of name → ScoreFunc. Each strategy scores the same candidate set independently.
- `k`: number of top results to keep per strategy. Same k for all strategies.
- `opts`: shared SearchOptions (TrigramFilter, ChunkFilter, verify, regex filters) applied once during candidate collection.
- Returns one `MultiSearchResult` per strategy, each containing that strategy's top-k results sorted by score descending.

The same chunk can appear in results from multiple strategies. No deduplication — the caller handles merge and can use cross-strategy agreement as a confidence signal.

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

# Append Detection Support

For append-only files (e.g. JSONL conversation logs), ark wants to detect that a file change was an append and skip full reindex. microfts2 provides the primitives; ark implements the detection logic.

## FileLength in F record

The F record stores `fileLength` (int64): the file size in bytes at index time. `AddFile` and `Reindex` set this from the file content they already read. Ark reads this to hash only the prefix up to the stored length, detecting whether a change was purely an append.

## AppendChunks API

Add chunks to an existing file without full reindex.

```go
func (db *DB) AppendChunks(fileid uint64, content []byte, strategy string) error
```

Chunks `content` using the named strategy, adds the resulting chunks and trigrams to the existing file's records. The `content` parameter is only the new bytes (the appended portion), not the full file.

Updates the F record: new ContentHash, ModTime, FileLength, appended chunk entries, merged token bag. Does NOT touch existing chunks or trigrams — they remain valid.

For each new chunk: hash content, check H record for dedup. New chunks get C records, T/W record updates. Existing chunks (dedup hit) just add fileid to C record. F record gets new (chunkid, location) entries appended.

## Chunker offset support

When `AppendChunks` passes content to a `ChunkFunc`, the content starts at an arbitrary byte offset in the original file, not byte 0. For line-based chunkers, this means line numbering must account for lines already processed.

`AppendChunks` passes a base line number to line-based chunkers so that Range labels (e.g. "51-60") are absolute, not relative to the appended slice. The mechanism: `ChunkFunc` signature is unchanged; `AppendChunks` counts newlines in a prefix window or accepts a base line count from the caller, then adjusts the Range values after chunking.

Suggestion: `AppendChunks` accepts an optional base line number. When zero, ranges are used as-is (for non-line-based chunkers). When non-zero, line-based ranges are offset by that amount.

# Ark Integration

microfts2 and microvec share the same LMDB environment when used together in ark. LMDB does not allow two env handles on the same database path within one process, so the first library opened provides the env to the second.

## MaxDBs

LMDB requires `SetMaxDBs` before opening the environment. microfts2 uses 1 named subdatabase. When sharing the environment with other libraries (e.g. microvec), the host process needs a higher limit. `Options.MaxDBs` sets this, defaulting to 2.

## Env accessor

`Env()` returns the underlying `*lmdb.Env`. The host process opens microfts2 first, gets the env, and passes it to microvec.

## Fileid surfacing

`AddFile` and `Reindex` return the fileid (uint64) alongside the error. The fileid is already computed internally — it just needs to be returned. microvec keys its embedding records by fileid.

## FileInfo lookup

`FileInfoByID(fileid)` resolves a fileid to its `FRecord`. microvec search returns `(fileid, chunknum, score)` — the CLI needs to resolve these to human-readable output using this method. Wraps the F record read in a read transaction.

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

Per-query T record reads: look up each query trigram's document frequency from T record value length. Typically 3-10 LMDB reads per query.

The total chunk count is derived from the database (sum of file chunk counts from F records, or maintained as a counter).

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

1. Look up the file's F record to get its ordered chunk list (chunkid + location pairs)
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

# Per-Query Chunk Cache

`ChunkCache` avoids redundant file reads and re-chunking during search result processing. Created at the start of a query, discarded when done. No LRU, no eviction — the working set of a single query is bounded.

```go
func (db *DB) NewChunkCache() *ChunkCache
```

## API

```go
// Drop-in replacement for DB.GetChunks — same signature, cached.
func (cc *ChunkCache) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)

// Single chunk text by range label. Returns cached content if available.
func (cc *ChunkCache) ChunkText(fpath, rangeLabel string) ([]byte, bool)
```

## Caching strategy

- **First access to a file:** resolve path → fileid via N records (one View txn), read F record for chunk list and strategy, read file from disk into `data`, resolve Chunker.
- **Lazy chunking (ChunkText path):** run `Chunker.Chunks()` but stop as soon as the target range is found. Each chunk encountered along the way is deep-copied and stored at its index. A `byRange` map indexes range label → position. Subsequent requests for already-seen ranges are a map lookup.
- **Full chunking (GetChunks path):** run `Chunker.Chunks()` to completion, fill every slot. Subsequent `ChunkText` calls on any range in the file are map lookups.
- **Copy semantics:** the cache deep-copies Range, Content, and Attrs on store. Downstream consumers get stable references.

## Lifecycle

- Created with `db.NewChunkCache()`.
- No invalidation — the file state is assumed stable for the duration of a query.
- Goes away when the caller drops the reference (normal GC).

# Bracket Chunker

A configurable chunker that groups program text into chunks based on bracket structure. Table-driven — adding a new language means adding a config entry, not code. Works for Go, Java, C, JavaScript, Lisp, nginx, Pascal, Julia, Bourne shell, and other bracket-delimited or word-delimited languages. Pascal and shell work because `begin`/`end` and `do`/`done` are brackets even though they don't look like traditional ones.

## Token types

The chunker scans content into a stream of tokens. Each token type is configurable per language:

- **comment**: `//...`, `/* ... */`, `#...`, `--...`, etc. Comment syntax varies by language. Comments inside strings are not comments. Nesting rules are per-language (most don't nest).
- **string**: `"..."`, `'...'`, `` `...` ``, `[[...]]`, etc. Escape characters are configurable. Strings inside comments are not strings.
- **whitespace**: contiguous runs of space, newline, tab, carriage return, form feed. Always recognized, not configurable.
- **bracket**: `(`, `)`, `{`, `}`, `<!--`, `-->`, `begin`, `end`, etc. Multi-character and word brackets are supported. Multi-bracket groups are allowed: `if`..`then`..`else`..`end`, `while`..`do`..`done`. Each group defines its opener(s), separator(s), and closer.
- **text**: any other contiguous non-whitespace characters.

## Chunk definition

A chunk is a **group** or a **paragraph**:

- **Group**: line-oriented. A group starts at the line containing an open bracket (not inside a comment or string) and continues line by line until all brackets are closed. Depth is tracked across all bracket types — `func f() {` is one group start because the parens open and close mid-line but the brace keeps depth above zero at end of line. Groups on a single line (e.g. `f()`) are not chunks. Leading comment/text lines immediately before the group (no blank line separating them) attach to the group.
- **Paragraph**: a sequence of lines not inside a group, terminated by a blank line or the start of a group. Top-level text between groups.

Range labels are `startline-endline` (1-based), consistent with other chunkers.

## Language configuration

```go
// BracketLang defines the lexical rules for one language.
type BracketLang struct {
    LineComments  []string       // e.g. "//", "#", "--"
    BlockComments [][2]string    // e.g. {{"/*", "*/"}, {"<!--", "-->"}}
    StringDelims  []StringDelim  // e.g. {`"`, `\`}, {`'`, `\`}, {"`", ""}
    Brackets      []BracketGroup // open/separator/close sets
}

// StringDelim defines a string delimiter and its escape character.
type StringDelim struct {
    Open   string // opening delimiter
    Close  string // closing delimiter (same as Open for symmetric quotes)
    Escape string // escape character (empty = no escaping, e.g. Go raw strings)
}

// BracketGroup defines one set of matching brackets.
// Separators are mid-group markers (e.g. "else" between "if"/"end").
type BracketGroup struct {
    Open       []string // openers: e.g. ["{"], ["if","while","for"]
    Separators []string // optional: e.g. ["else","elif","then"]
    Close      []string // closers: e.g. ["}"], ["end","done","fi"]
}
```

Built-in language configs are provided as package-level variables (e.g. `LangGo`, `LangC`, `LangPython`). Users can construct custom `BracketLang` values.

## Library API

```go
// BracketChunker returns a Chunker for the given language config.
func BracketChunker(lang BracketLang) Chunker
```

Returns a full `Chunker` (both `Chunks` and `ChunkText`).

## CLI

```
microfts chunk-bracket -lang <name> <file>
```

Output: one chunk per stdout line as `range\tcontent` (tab-separated), matching the external chunker protocol. Available language names come from the built-in configs.

# Indent Chunker

A chunker for languages where indentation defines scope. Similar to the bracket chunker — groups and paragraphs — but scope is determined by indentation level rather than bracket characters.

## Languages

Python, YAML, and potentially other indentation-scoped formats (Haskell, CoffeeScript, Nim, Makefiles).

## Token types

Same as bracket chunker (comment, string, whitespace, text) minus brackets. Comment and string syntax is still configurable per language, using the same `BracketLang` structure (with empty `Brackets`).

## Scope detection

- **Indent increase**: a line indented further than the previous non-blank line opens a new scope.
- **Dedent**: a line at a lower indentation level than the current scope closes the scope.
- **Tabs vs spaces**: configurable per language (tab width for column counting). Mixed indentation uses the configured tab width.

## Chunk definition

- **Group**: a line that introduces a deeper indentation level (the "header" line), plus all following lines at that deeper level or deeper, until dedent. Leading comment lines attach to the group (same rule as bracket chunker).
- **Paragraph**: consecutive lines at the same indentation level (the top level or between groups), terminated by a blank line or the start of a group.

Range labels are `startline-endline` (1-based).

## Library API

```go
// IndentChunker returns a Chunker for indentation-scoped languages.
// tabWidth controls how tabs are counted for indentation level (0 = tabs are one column).
func IndentChunker(lang BracketLang, tabWidth int) Chunker
```

Reuses `BracketLang` for comment/string config (Brackets field ignored).

## CLI

```
microfts chunk-indent -lang <name> [-tabwidth N] <file>
```

Output: same `range\tcontent` format.

