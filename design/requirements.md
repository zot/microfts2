# Requirements

## Feature: Core
**Source:** specs/main.md

- **R1:** Go CLI command, also usable as a Go library
- **R2:** LMDB-backed storage using a single named subdatabase with prefix-distinguished records

## Feature: Raw Byte Trigrams
**Source:** specs/main.md

- **R3:** Raw byte trigrams — every byte is its own value, no character set mapping
- **R4:** Whitespace bytes (space, tab, newline, carriage return) are word boundaries; runs collapse
- **R5:** All non-whitespace bytes are indexed; UTF-8 multibyte characters produce cross-boundary byte trigrams
- **R6:** 8 bits per byte, 24 bits per trigram, 16M possible trigrams
- **R7:** ~~removed: A record replaced by dynamic TrigramFilter~~
- **R8:** ~~removed: old per-trigram C records replaced by T records and per-chunk C records~~
- **R9:** Case-insensitive mode: `bytes.ToLower()` on input before trigram extraction
- **R111:** AddFile checks each chunk's Content for valid UTF-8 (utf8.Valid); the raw file may be binary — the chunker produces UTF-8 text
- **R112:** Character-internal byte trigrams are skipped: a 3-byte window entirely within one multibyte character is not emitted
- **R113:** 3-byte characters (CJK): 1 internal trigram skipped per character; 4-byte characters (emoji): 2 internal trigrams skipped
- **R114:** 2-byte and ASCII characters produce no internal trigrams; behavior unchanged for these

## Feature: Byte Aliases
**Source:** specs/main.md

- **R45:** Byte aliases map input bytes to replacement bytes before trigram extraction
- **R46:** Aliases stored in I records using data-in-key pattern (one record per alias)
- **R47:** Applied before trigram extraction (e.g. newline → `^` for line-start matching)
- **R115:** Both source and target bytes in aliases must be ASCII (< 0x80) — aliasing UTF-8 continuation or leading bytes would corrupt multibyte characters and break character-internal trigram skipping

## Feature: Chunking Strategies
**Source:** specs/main.md

- **R10:** Chunking strategies are configurable and added/removed dynamically (external commands or Go functions)
- **R11:** Each strategy is a name mapped to an external command: `[cmd] [filename]` outputs `range\tcontent` lines
- **R12:** Each file tracks which chunking strategy was used to index it (stored in F record)
- **R13:** Files can be reindexed with a different strategy to allow migration
- **R116:** `Chunker` interface: `Chunks(path, content, yield) error` (producer) + `ChunkText(path, content, rangeLabel) ([]byte, bool)` (retriever)
- **R291:** `ChunkFunc` type preserved: `func(path string, content []byte, yield func(Chunk) bool) error` — convenience type
- **R292:** `FuncChunker` adapter wraps a `ChunkFunc` into a `Chunker`; `Chunks` delegates to fn; `ChunkText` re-runs fn and returns the first chunk matching the range label
- **R293:** `AddChunker(name string, c Chunker) error` registers a Chunker as a strategy — in-memory only, must re-register on Open
- **R294:** `AddStrategyFunc(name, fn ChunkFunc)` convenience: wraps fn in `FuncChunker`, calls `AddChunker`
- **R117:** Func/Chunker strategies are in-memory only — not persisted to I record cmd field (re-register on Open); I record stores name with empty value
- **R118:** When AddFile/Reindex uses a Chunker strategy, call the Chunker directly instead of exec
- **R119:** Built-in chunkers (chunk-lines, chunk-lines-overlap, chunk-words-overlap, chunk-markdown) register as func strategies

## Feature: Single Subdatabase
**Source:** specs/main.md

- **R14:** ~~removed: two-tree content/index split replaced by single subdatabase~~
- **R15:** ~~removed: two-tree content/index split replaced by single subdatabase~~
- **R218:** All records live in one LMDB named subdatabase, distinguished by prefix byte (I, H, C, F, N, T, W)
- **R219:** Subdatabase name is a parameter: default `fts`, settable via CLI (`-db-name`) and library (`Options.DBName`)

## Feature: Encoding Conventions
**Source:** specs/main.md

- **R220:** Integer fields use varint encoding (Go `binary.PutUvarint` / `binary.ReadUvarint`)
- **R221:** Trigram fields are fixed 3 bytes (24-bit); hash fields are fixed 32 bytes (SHA-256)
- **R222:** Strings are length-prefixed (varint length + bytes), except the final field in a key can use remaining bytes

## Feature: Chunk Deduplication
**Source:** specs/main.md

- **R223:** Chunks are deduplicated by content hash (SHA-256) — same content = same chunkid across files
- **R224:** `H` records: `H[hash:32] → chunkid:varint` — content hash to chunkid lookup
- **R225:** During AddFile, each chunk's content is hashed; if H record exists, the chunk is a dedup hit (add fileid to existing C record, skip T/W updates)
- **R226:** If H record does not exist, allocate new chunkid, create H record, create C record, update T and W records

## Feature: C Records (Per-Chunk)
**Source:** specs/main.md

- **R16:** ~~removed: old per-trigram C records replaced by per-chunk C records~~
- **R227:** `C` records: `C[chunkid:varint] → hash:32 + packed trigrams + packed tokens + packed attrs + packed fileids`
- **R228:** C record trigrams: `[n-trigrams:varint] [[trigram:3] [count:varint]]...` — per-chunk trigram counts
- **R229:** C record tokens: `[n-tokens:varint] [[count:varint] [token:str]]...` — per-chunk token counts
- **R230:** C record attrs: `[n-attrs:varint] [[key:bytes] [value:bytes]]...` — optional key-value pairs from chunker Attrs (e.g. timestamp, role); opaque to microfts2
- **R231:** C record fileids: `[n-fileids:varint] [fileid:varint]...` — list of files containing this chunk
- **R232:** C record is self-describing — all data needed for search, scoring, filtering, and removal in one read

## Feature: F Records (Per-File)
**Source:** specs/main.md

- **R233:** `F` records: `F[fileid:varint] → metadata + names + chunks + token bag`
- **R234:** F record metadata: `[modTime:8] [contentHash:32] [fileLength:varint] [strategy:str]` — staleness detection and chunking strategy
- **R235:** F record names: `[filecount:varint] [name:str]...` — multiple names for duplicate/copied files sharing a fileid
- **R236:** F record chunks: `[chunkcount:varint] [[chunkid:varint] [location:str]]...` — ordered chunk list with opaque range labels from chunker
- **R237:** F record token bag: `[tokencount:varint] [[token:str] [count:varint]]` — aggregated union of all chunk tokens with summed counts, for file-level scoring

## Feature: I Records (Config)
**Source:** specs/main.md

- **R17:** `I` records use data-in-key pattern: `I[name:str] = [value:str] → empty` — each setting independently readable and writable
- **R19:** ~~removed: N record JSON replaced by F record struct~~

## Feature: N Records (Name Lookup)
**Source:** specs/main.md

- **R20:** `N` records: filename → fileid mapping using key chains for names exceeding 511 bytes
- **R25:** Filenames exceeding LMDB's 511-byte key limit use multiple chained N keys
- **R26:** `N` records use chain-byte to chain: `N[0-254][name:str] → empty` for prefix, `N[255][name:str] → [[full-name:str] [fileid:varint]]...` for final segment with full filename and fileid

## Feature: T Records (Trigram Inverted Index)
**Source:** specs/main.md

- **R21:** ~~removed: forward index entries replaced by T records~~
- **R238:** `T` records: `T[trigram:3] → [chunkid:varint]...` — packed list of chunkids per trigram
- **R239:** Corpus trigram document frequency derived from T record value length — no separate count records needed
- **R240:** One T record per distinct trigram; entry count proportional to vocabulary, not trigram-chunk pairs

## Feature: W Records (Token Inverted Index)
**Source:** specs/main.md

- **R241:** `W` records: `W[token-hash:4] → [chunkid:varint]...` — packed list of chunkids per token hash
- **R242:** Token IDF derived from W record value length — same structure as T records
- **R243:** Provides exact token-level inverse document frequency for BM25 scoring

## Feature: Record Structs
**Source:** specs/main.md

- **R95:** ~~removed: FileInfo struct replaced by FRecord~~
- **R102:** ~~removed: R record reverse index replaced by C record fileid list~~
- **R244:** `CRecord` struct: `ChunkID uint64, Hash [32]byte, Trigrams []TrigramEntry, Tokens []TokenEntry, Attrs []Pair, FileIDs []uint64`
- **R245:** `FRecord` struct: `FileID uint64, ModTime int64, ContentHash [32]byte, FileLength int64, Strategy string, Names []string, Chunks []FileChunkEntry, Tokens []TokenEntry`
- **R246:** `TRecord` struct: `Trigram uint32, ChunkIDs []uint64`
- **R247:** `WRecord` struct: `TokenHash uint32, ChunkIDs []uint64`
- **R248:** `HRecord` struct: `Hash [32]byte, ChunkID uint64`
- **R249:** `TrigramEntry` struct: `Trigram uint32, Count int`
- **R250:** `TokenEntry` struct: `Token string, Count int`
- **R251:** `FileChunkEntry` struct: `ChunkID uint64, Location string`
- **R252:** Each record struct has `Marshal` and `Unmarshal` methods for LMDB encode/decode

## Feature: TxnHolder Interface
**Source:** specs/main.md

- **R264:** `TxnHolder` interface: `Txn() *lmdb.Txn` — any value carrying an LMDB transaction
- **R265:** CRecord implements `TxnHolder` via its `Txn()` accessor; internal DB methods accept `TxnHolder` instead of raw `*lmdb.Txn`
- **R266:** `CRecord.FileRecord(fileid)` passes self as `TxnHolder` to internal read methods — no txn extraction needed
- **R267:** (inferred) `txnWrap` struct wraps raw `*lmdb.Txn` from View/Update blocks into a `TxnHolder`

## Feature: Data-in-Key Pattern
**Source:** specs/main.md

- **R22:** Store data in keys using lexical sort for range queries
- **R23:** Key ranges: `[key]...[key+1]` spans all items for a key
- **R24:** Sets represented as `[key][info] → empty value`

## Feature: Full Trigram Index
**Source:** specs/main.md

- **R27:** T records contain entries for ALL trigrams present in the content, not a frequency-selected subset
- **R28:** ~~removed: searchCutoff and A record replaced by dynamic TrigramFilter~~

## Feature: Adding Files
**Source:** specs/main.md

- **R29:** Adding a file: check for existing N records (dedup guard), allocate fileid, create N/F records, call chunker (yields Range+Content per chunk), for each chunk: hash content, check H record for dedup, create/update C records, batch T/W record updates
- **R253:** Batch T/W updates: accumulate all chunkids per trigram/token across the file's chunks, then one read-modify-write per affected T/W record
- **R110:** AddFile computes trigram counts per chunk from chunk Content, not raw file bytes

## Feature: Removing Files
**Source:** specs/main.md

- **R254:** Removing a file: read F record to get chunk list, for each chunkid remove fileid from C record, if C has no remaining fileids delete C/H records and remove chunkid from T/W records, delete F and N records

## Feature: Searching
**Source:** specs/main.md

- **R30:** ~~removed: no separate index DB existence check — index is always maintained~~
- **R31:** Literal search: trim whitespace, parse query into terms via parseQueryTerms, extract trigrams per term (not whole query), intersect per-term candidate sets, select via TrigramFilter (default: FilterAll)
- **R32:** Literal search: for each selected query trigram, read T record to get candidate chunkid lists; intersect candidate sets across all selected trigrams; for each surviving chunkid, read C record to get per-trigram counts and fileids; resolve chunkid → (filepath, range) via C record fileids → F record chunk list
- **R33:** Results scored using the selected scoring function (coverage or density), sorted by score descending
- **R34:** CLI output: one result per line, `filepath:range`
- **R35:** Library returns struct slices with file path, range string, score

## Feature: Index Computation
**Source:** specs/main.md

- **R36:** ~~removed: BuildIndex replaced by dynamic TrigramFilter~~

## Feature: CLI Commands
**Source:** specs/main.md

- **R37:** CLI `delete` command removes files from the database
- **R38:** CLI `reindex` command re-chunks files with a different strategy
- **R39:** CLI `init` command creates a new database with case-insensitive and alias options
- **R48:** ~~removed: build-index CLI command removed with A record~~
- **R49:** CLI `strategy` subcommands: `add`, `remove`, `list` for managing chunking strategies
- **R50:** All CLI commands require `-db` flag; optional shared flag `-db-name` (subdatabase name, default `fts`)

## Feature: Library API
**Source:** specs/main.md

- **R51:** `Create`/`Open`/`Close`/`Settings` lifecycle functions
- **R268:** `Version() (string, error)` returns the DB format version string from the I record; read-only transaction
- **R52:** `AddFile`/`RemoveFile`/`Reindex` content management methods
- **R53:** `Search` accepts variadic `SearchOption` and returns `*SearchResults` with Results slice and IndexStatus
- **R54:** ~~removed: BuildIndex replaced by dynamic TrigramFilter~~
- **R55:** `AddStrategy`/`RemoveStrategy` for runtime strategy management
- **R56:** `Options` struct configures creation (CaseInsensitive, Aliases) and opening (DBName, MaxDBs)

## Feature: Built-in Chunking Strategies
**Source:** specs/main.md

- **R57:** Built-in chunkers are registered as func strategies; also available as CLI subcommands outputting `range\tcontent` lines
- **R58:** `chunk-lines`: every line is a chunk; range is `N-N` (line number); content is the line text
- **R59:** `chunk-lines-overlap`: fixed-size line windows with configurable lines per chunk (default 50) and overlap (default 10); range is `startline-endline`
- **R60:** `chunk-words-overlap`: fixed-size word windows with configurable words per chunk (default 200), overlap (default 50), and word pattern (default `\S+`); range is `startline-endline`
- **R61:** Word pattern is a configurable regexp used to count and locate word boundaries
- **R62:** Range for all built-in text chunkers is `startline-endline` (1-based, inclusive); content is the text of those lines

## Feature: Subdatabase Name
**Source:** specs/main.md

- **R40:** Subdatabase name is a parameter with default `fts`
- **R41:** Settable via CLI flag (`-db-name`) and library API (`Options.DBName`)
- **R42:** Not stored in the I record — required to open the database

## Feature: Staleness Detection
**Source:** specs/main.md

- **R63:** F record stores file modification time (Unix nanoseconds) and content hash (SHA-256) at index time
- **R64:** A file is stale when its mod time differs from stored AND its content hash differs from stored
- **R65:** A file is missing when it no longer exists on disk
- **R66:** Mod time is checked first; if it matches, the file is fresh without hashing
- **R67:** `CheckFile` checks a single file's staleness status
- **R68:** `StaleFiles` returns status of all indexed files (fresh, stale, missing)
- **R69:** `RefreshStale` reindexes all stale files using their existing strategy (or a given override). Missing files are skipped.
- **R70:** CLI `stale` subcommand lists stale and missing files as `status\tpath`
- **R71:** CLI `-r` global flag refreshes stale files before executing any subcommand
- **R72:** `-r` without a subcommand refreshes and exits, printing refreshed files
- **R73:** `-r` combined with a subcommand (e.g. `search`) refreshes first, then runs the subcommand
- **R74:** Missing files are reported by `-r` but not deleted

## Feature: Incremental Index Update
**Source:** specs/main.md

- **R75:** ~~removed: A record replaced by dynamic TrigramFilter~~
- **R76:** ~~removed: BuildIndex replaced by dynamic TrigramFilter~~
- **R77:** `AddFile`/`Reindex` create/update C, T, and W records for all of the file's chunks and trigrams
- **R78:** `RemoveFile` reads the F record chunk list to find chunkids, removes fileid from C records, and cleans up T/W/H records for orphaned chunks
- **R79:** `AddFile` always maintains the index incrementally (T/W/C records updated in the same transaction)
- **R80:** `RemoveFile` uses C record fileid lists to determine orphaned chunks; orphaned chunks have their T/W/H entries deleted
- **R81:** Index is always maintained incrementally — no separate "index exists" check needed

## Feature: Regex Search
**Source:** specs/main.md

- **R82:** `SearchRegex(pattern string, opts ...SearchOption)` searches using a Go regexp pattern against the full trigram index
- **R83:** ~~removed: IndexStatus.Built vestige of old build-index step~~
- **R84:** Trigram query extracted from regex AST as boolean AND/OR expression, using rsc's approach (github.com/google/codesearch/regexp)
- **R85:** `Search` returns `*SearchResults` containing both results and IndexStatus
- **R86:** `RefreshStale` returns `([]FileStatus, error)` — no IndexStatus
- **R87:** AND nodes in the trigram query intersect candidate chunkid sets; OR nodes union them
- **R88:** Regex search queries T records directly (no trigram filtering)
- **R127:** Regex search always verifies: re-chunk file using stored strategy, run compiled regex against chunk content, discard non-matches (trigram query is a superset filter)
- **R89:** CLI `search -regex` flag switches to regex mode

## Feature: Ark Integration
**Source:** specs/main.md

- **R91:** `Env()` method returns the underlying `*lmdb.Env` for sharing with other libraries in the same process
- **R92:** `AddFile` returns `(uint64, error)` — the fileid alongside the error
- **R93:** `Reindex` returns `(uint64, error)` — the fileid alongside the error
- **R94:** `FileInfoByID(fileid uint64)` returns `(FRecord, error)` — resolves fileid to its F record data
- **R96:** `ScoreFile(query, fpath string, fn ScoreFunc, opts ...SearchOption)` returns `([]ScoredChunk, error)` — per-chunk scores using the given scoring function
- **R97:** Coverage score = matching selected trigrams / total selected query trigrams, per chunk (default strategy)
- **R98:** `ScoredChunk` struct: `Range string, Score float64`
- **R99:** `SearchResult` struct: `Path string, Range string, Score float64` (plus unexported `chunkID uint64` and `chunk []byte` — see R490, R491)
- **R100:** CLI `score` subcommand: `microfts score -db <path> [-score coverage|density] <query> <file>...` — output `filepath:range\tscore`
- **R120:** `AddFileWithContent(fpath, strategy string)` returns `(uint64, []byte, error)` — fileid + file content already read for trigram extraction
- **R121:** `ReindexWithContent(fpath, strategy string)` returns `(uint64, []byte, error)` — fileid + file content already read for trigram extraction
- **R122:** Original `AddFile`/`Reindex` signatures unchanged; WithContent variants avoid a redundant file read in ark's hot path

## Feature: Scoring Strategies
**Source:** specs/main.md

- **R103:** `ScoreFunc` type: `func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64`
- **R104:** `SearchOption` functional option type for configuring search behavior
- **R105:** `WithCoverage()` option: score = matching selected trigrams / total selected query trigrams (default)
- **R106:** `WithDensity()` option: token-density scoring for long queries — tokenize on spaces, token strength = min trigram count, score = sum strengths / chunk token count
- **R107:** `WithScoring(fn ScoreFunc)` option: use a custom scoring function
- **R108:** CLI `search -score coverage|density` flag selects scoring strategy (default: coverage)
- **R109:** ~~removed: N record chunkRanges/chunkTokenCounts replaced by F record chunk list and C record token counts~~
- **R110:** (inferred) AddFile computes trigram counts per chunk (map[uint32]int) from chunk Content, not raw file bytes

## Feature: MaxDBs Option
**Source:** specs/main.md

- **R101:** `Options.MaxDBs` sets the LMDB max named databases; defaults to 2; used by both `Create` and `Open`

## Feature: Encoding
**Source:** specs/main.md

- **R123:** ~~removed: old C record BigEndian replaced by varint encoding~~

## Feature: WithVerify Post-filter
**Source:** specs/main.md

- **R124:** `WithVerify()` search option: after trigram intersection, re-chunk the file using stored strategy to recover chunk content, verify each query term appears as a case-insensitive substring; discard chunks that fail
- **R125:** Query tokenization for verify: split on spaces; double-quoted strings are a single term with quotes stripped (e.g. `"hello world" foo` → terms `hello world`, `foo`)
- **R126:** CLI `-verify` flag on search command passes `WithVerify()` to the library

## Feature: Chunk Struct and Generator Contract
**Source:** specs/main.md

- **R128:** `Chunk` struct: `Range []byte` (opaque, string semantics) + `Content []byte` (text to trigram-index) + `Attrs []Pair` (optional per-chunk metadata, nil by default); Range and Content are reusable buffers — caller must copy before next yield
- **R295:** `Pair` struct: `Key []byte, Value []byte` — opaque key-value pair; allows duplicate keys; mirrors DB wire format
- **R296:** Chunk.Attrs are opaque to microfts2 — stored in C records and exposed to ChunkFilters; the DB never interprets attr keys or values
- **R129:** Chunkers are deterministic: same file produces same chunks with same ranges
- **R130:** Chunkers serve dual purpose via `Chunker` interface: `Chunks` method for indexing, `ChunkText` method for extraction; `ChunkText` may be optimized for targeted retrieval
- **R131:** External command chunkers output `range\tcontent` per line to stdout; `RunChunkerFunc` wraps the command as a Chunker (FuncChunker-like behavior)
- **R132:** `SearchResult` struct: `Path string, Range string, Score float64`
- **R133:** `ScoredChunk` struct: `Range string, Score float64`
- **R134:** Verify and regex verification re-chunk the file using stored strategy, match by range to find chunk content

## Feature: Dynamic Trigram Filtering
**Source:** specs/main.md

- **R135:** `TrigramCount` struct: `Trigram uint32, Count int` — pairs a trigram code with its corpus document frequency
- **R136:** `TrigramFilter` type: `func(trigrams []TrigramCount, totalChunks int) []TrigramCount` — caller-supplied function deciding which query trigrams to search with
- **R137:** `WithTrigramFilter(fn TrigramFilter)` search option: look up each query trigram's document frequency from T record value length, call the filter, use the returned subset
- **R138:** ~~removed: no backward compat needed — default is FilterAll~~
- **R139:** `WithTrigramFilter` applies to both `Search` and `ScoreFile`
- **R140:** `FilterAll` stock filter: returns all trigrams unmodified (disables filtering)
- **R141:** `FilterByRatio(maxRatio float64)` stock filter: skips trigrams appearing in more than `maxRatio` of total chunks
- **R142:** `FilterBestN(n int)` stock filter: keeps the N trigrams with the lowest document frequency
- **R143:** Trigram document frequencies retrieved via per-query T record reads (typically 3-10 LMDB reads per query)
- **R144:** Total chunk count derived from the database (sum of file chunk counts from F records, or maintained as a counter)
- **R145:** ~~removed: A record and BuildIndex fully removed~~

## Feature: FileLength in F Record
**Source:** specs/main.md

- **R146:** F record stores `fileLength` (varint): file size in bytes at index time
- **R147:** `AddFile` and `Reindex` set `fileLength` from the content they already read
- **R148:** ~~removed: FileInfo struct replaced by FRecord~~
- **R149:** ~~removed: N record migration not needed — F records are the new format~~

## Feature: AppendChunks API
**Source:** specs/main.md

- **R150:** `AppendChunks(fileid uint64, content []byte, strategy string, opts ...AppendOption) error` adds chunks to an existing file without full reindex
- **R151:** `content` parameter is only the appended bytes, not the full file
- **R152:** Chunks `content` using the named strategy's `ChunkFunc`
- **R153:** New chunk numbering continues from the file's existing chunk count in the F record
- **R154:** ~~removed: forward index replaced by T records~~
- **R155:** ~~removed: R record replaced by C record fileid list~~
- **R156:** For each new chunk: hash content, check H for dedup, create/update C records, batch T/W updates
- **R157:** F record updated: appended chunk entries (chunkid + location), merged token bag, new ContentHash, ModTime, FileLength
- **R158:** `AppendOption` functional option type: `func(*appendConfig)`
- **R159:** `WithContentHash(hash string)` option: full-file SHA-256, caller pre-computed
- **R160:** `WithModTime(t int64)` option: Unix nanoseconds for the updated file
- **R161:** `WithFileLength(n int64)` option: full file size after append
- **R162:** `WithBaseLine(n int)` option: 1-based line number offset for line-based chunker ranges; 0 means no adjustment
- **R163:** (inferred) `AppendChunks` validates that fileid exists; returns error if not found
- **R164:** (inferred) `AppendChunks` runs in a single LMDB write transaction — all updates are atomic

## Feature: Chunker Offset Support
**Source:** specs/main.md

- **R165:** When `WithBaseLine(n)` is non-zero, `AppendChunks` adjusts line-based Range values by adding the base line offset after chunking
- **R166:** Range adjustment is string-level: parse "start-end", add base, re-format — works for any chunker producing `N-N` ranges
- **R167:** When `WithBaseLine` is zero or unset, ranges are used as-is from the chunker — correct for non-line-based chunkers
- **R168:** (inferred) ChunkFunc signature is unchanged — offset handling is AppendChunks' responsibility, not the chunker's

## Feature: Markdown Chunker
**Source:** specs/main.md

- **R169:** `chunk-markdown`: paragraph-based splitting for markdown files; exported as `MarkdownChunkFunc`
- **R170:** A heading line (`#`...) always starts a new chunk
- **R171:** A heading and its following paragraph (up to the next blank line or heading) form one chunk
- **R172:** Consecutive blank lines collapse to a single boundary
- **R173:** Non-heading text between boundaries is one chunk
- **R174:** Range is `startline-endline` (1-based, inclusive); content is the raw text of those lines, excluding boundary blank lines
- **R177:** Blank lines are boundaries only — not included in any chunk's content; gaps between chunks are expected
- **R175:** CLI subcommand `microfts chunk-markdown <file>` outputs `range\tcontent` per chunk
- **R176:** Registered as a built-in func strategy alongside other built-in chunkers
- **R465:** Fenced code blocks (opening `` ``` `` or `~~~`, with optional info string) suppress blank-line splitting — all lines from fence open through matching close belong to the current chunk
- **R466:** A fence opening does not start a new chunk — it continues the current paragraph/chunk
- **R467:** Blank lines inside a fenced code block are not chunk boundaries
- **R468:** Fence matching: closing fence is a line starting with the same character (`` ` `` or `~`) repeated at least as many times as the opening, with no other non-whitespace content

## Feature: Per-token Trigram Generation
**Source:** specs/main.md

- **R178:** Literal search trims leading and trailing whitespace from the query before trigram extraction
- **R179:** Literal search parses query into terms using `parseQueryTerms` before trigram extraction — unquoted words split on spaces, quoted phrases as single terms
- **R180:** Trigrams are extracted per term, not from the whole query as one byte sequence — prevents cross-boundary trigrams between unrelated words
- **R181:** Candidate set is the intersection of all terms' trigram candidate sets — a chunk must match all terms
- **R182:** (inferred) Query term order does not affect search results — "daneel olivaw" and "olivaw daneel" return the same set

## Feature: Multi-Regex Post-Filtering
**Source:** specs/main.md

- **R183:** `WithRegexFilter(patterns ...string) SearchOption` adds AND post-filters — every pattern must match chunk content for the chunk to be kept
- **R184:** `WithExceptRegex(patterns ...string) SearchOption` adds subtract post-filters — any match rejects the chunk
- **R185:** Multiple calls to `WithRegexFilter` or `WithExceptRegex` accumulate patterns
- **R186:** Patterns stored as strings in searchConfig; compiled to `*regexp.Regexp` at start of Search/SearchRegex; compilation failure returned as error
- **R187:** Filtering operates on chunk content recovered by re-chunking the file (same mechanism as WithVerify)
- **R188:** Filters apply after trigram candidate selection and scoring, before final sort
- **R189:** Filters apply to both `Search` and `SearchRegex`
- **R190:** When combined with `SearchRegex`, the primary regex drives trigram extraction; regex filters and except-regex filters are independent post-filters
- **R191:** Filtering uses the existing `filterResults` helper with a combined match function
- **R192:** CLI `-filter-regex` flag is repeatable — each adds an AND regex filter
- **R193:** CLI `-except-regex` flag is repeatable — each adds a subtract regex filter
- **R194:** CLI repeatable flags implemented via custom `flag.Value` type for string slice accumulation
- **R195:** (inferred) Both CLI flags work with literal and regex search modes
- **R196:** (inferred) When no regex filters or except-regex filters are supplied, behavior is unchanged from current

## Feature: Chunk Context Retrieval
**Source:** specs/main.md

- **R197:** `GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)` retrieves the target chunk and up to N positional neighbors before and after
- **R198:** Target chunk identified by exact string match of range label against the F record's chunk list (location field)
- **R199:** Neighbor window: `max(0, targetIndex - before)` to `min(len-1, targetIndex + after)`, inclusive
- **R200:** Chunk content recovered via `Chunker.ChunkText` or by re-running `Chunker.Chunks` for windowed retrieval (GetChunks)
- **R201:** `ChunkResult` struct: `Path string, Range string, Content string, Index int` — index is 0-based position in the file's chunk list
- **R202:** Returns chunks in positional order (ascending index)
- **R203:** Error if file not in database, target range not found, file missing from disk, or chunking strategy not registered
- **R204:** CLI `chunks` subcommand: `microfts chunks -db <path> [-before N] [-after N] <file> <range>` — JSONL output with `path`, `range`, `content`, `index` fields
- **R205:** `-before` and `-after` default to 0 (target chunk only)
- **R206:** (inferred) Expansion unit is chunks (strategy-agnostic), not lines or bytes — range labels are opaque

## Feature: Composable --contains and --regex
**Source:** specs/main.md

- **R207:** CLI `--contains` string flag provides an explicit FTS text query for the `search` subcommand
- **R208:** When `--contains` is used with `--regex`, `Search(containsText)` is called with the positional-arg regex pattern added as a `WithRegexFilter` post-filter
- **R209:** When `--regex` is used alone (no `--contains`), positional args are the regex pattern → `SearchRegex` (unchanged behavior)
- **R210:** When `--contains` is used alone, it is the FTS query → `Search` (no positional args required)
- **R211:** When neither `--contains` nor `--regex` is set, positional args are the FTS query → `Search` (unchanged behavior)
- **R212:** (inferred) Error if no query is determinable — no positional args and no `--contains`

## Feature: AddFile Duplicate Guard
**Source:** specs/main.md

- **R213:** `addFileInTxn` checks for existing N records (via `FinalKey` lookup) before allocating a new fileid
- **R214:** If the file is already indexed, `AddFile` and `AddFileWithContent` return `ErrAlreadyIndexed`
- **R215:** `ErrAlreadyIndexed` is a sentinel error: `var ErrAlreadyIndexed = errors.New("file already indexed")`
- **R216:** Callers check with `errors.Is(err, ErrAlreadyIndexed)` and use `Reindex` or `AppendChunks` instead
- **R217:** (inferred) The guard is a check, not a policy — no automatic reindex or append behavior

## Feature: Chunk Filtering
**Source:** specs/main.md

- **R255:** `ChunkFilter` type: `func(chunk CRecord) bool` — receives full C record during candidate evaluation
- **R256:** `WithChunkFilter(fn ChunkFilter) SearchOption` — called after T record intersection, before scoring; C record already loaded on hot path, zero extra I/O
- **R257:** Multiple `WithChunkFilter` calls accumulate with AND semantics
- **R258:** `WithAfter(t time.Time)` — sugar over ChunkFilter; keeps chunks with `timestamp` attr >= t; falls back to file mod time from F record if no timestamp attr
- **R259:** `WithBefore(t time.Time)` — sugar over ChunkFilter; keeps chunks with `timestamp` attr < t; same fallback
- **R260:** ChunkFilter applies to `Search`, `SearchRegex`, and `ScoreFile`

## Feature: File-Level Token Bag
**Source:** specs/main.md

- **R261:** F record aggregated token bag is the union of all chunk tokens with summed counts
- **R262:** Token bag maintained incrementally: AddFile/Reindex rebuilds, AppendChunks merges new chunk tokens
- **R263:** Enables file-level scoring and pre-filtering without reading every chunk's C record

## Feature: Overlap Scoring
**Source:** specs/main.md

- **R269:** `ScoreOverlap` score function: count of matching query trigrams, no normalization (OR semantics)
- **R270:** Fits `ScoreFunc` signature directly — pure function, no extra state
- **R271:** `WithOverlap()` search option: sugar for `WithScoring(ScoreOverlap)`

## Feature: BM25 Scoring
**Source:** specs/main.md

- **R272:** `ScoreBM25(idf map[uint32]float64, avgTokenCount float64) ScoreFunc` — closure factory capturing IDF and avgdl
- **R273:** BM25 formula: `idf(t) * (tf * (k1+1)) / (tf + k1 * (1 - b + b * dl/avgdl))` with k1=1.2, b=0.75
- **R274:** `BM25Func(queryTrigrams []uint32) (ScoreFunc, error)` — convenience helper that reads T records for IDF, I record counters for avgdl, returns a ScoreBM25 closure
- **R275:** I record counter `totalTokens`: sum of all chunk token counts, maintained atomically during AddFile/RemoveFile/AppendChunks
- **R276:** I record counter `totalChunks`: total number of unique chunks, maintained atomically during AddFile/RemoveFile/AppendChunks
- **R277:** `avgdl = totalTokens / totalChunks` — average chunk token count across corpus
- **R278:** (inferred) IDF per trigram: `df(t)` derived from T record value length; `N` from totalChunks I record counter

## Feature: Proximity Reranking
**Source:** specs/main.md

- **R279:** `WithProximityRerank(topN int) SearchOption` — post-filter that reranks top-N results by query term proximity in chunk text
- **R280:** Proximity bonus: `1 / (1 + minSpan)` where minSpan is the smallest token window containing all query terms
- **R281:** Re-chunks file to recover chunk text (same mechanism as WithVerify)
- **R282:** Applied after scoring, before final sort; works with Search, SearchMulti, and ScoreFile

## Feature: Multi-Strategy Search
**Source:** specs/main.md

- **R283:** `SearchMulti(query string, strategies map[string]ScoreFunc, k int, opts ...SearchOption) ([]MultiSearchResult, error)`
- **R284:** Candidate collection (trigram intersection, T record reads, C record reads, chunk filters) computed once in a single View transaction
- **R285:** Each strategy scores the same candidate set independently, keeping its own top-k sorted by score descending
- **R286:** `MultiSearchResult` struct: `Strategy string, Results []SearchResult`
- **R287:** Same k for all strategies
- **R288:** No deduplication — same chunk can appear in results from multiple strategies; caller handles merge
- **R289:** Shared SearchOptions (TrigramFilter, ChunkFilter, verify, regex filters) applied once during candidate collection
- **R290:** (inferred) Post-filters (verify, regex, proximity rerank) applied per strategy's result set after scoring

## Feature: Per-Query Chunk Cache
**Source:** specs/main.md

- **R297:** `ChunkCache` struct: per-query cache for file content and chunked data — avoids redundant file reads and re-chunking
- **R298:** `NewChunkCache() *ChunkCache` factory method on DB
- **R299:** `ChunkCache.GetChunks(fpath, targetRange, before, after)` — same signature as `DB.GetChunks`, cached
- **R300:** `ChunkCache.ChunkText(fpath, rangeLabel)` — single chunk text by range label, cached
- **R301:** First file access resolves path → fileid via N records (View txn), reads F record, reads file from disk, resolves Chunker
- **R302:** Lazy chunking: `Chunker.Chunks()` stops at target range; all chunks encountered along the way are stored; `byRange` map indexes range label → position
- **R303:** Full chunking: `GetChunks` runs `Chunker.Chunks()` to completion, fills every slot; subsequent `ChunkText` calls are map lookups
- **R304:** Cache deep-copies Range, Content, and Attrs from yielded chunks — downstream consumers get stable references
- **R305:** No LRU, no eviction, no invalidation — per-query lifecycle, discarded when caller drops reference
- **R306:** (inferred) `ChunkCache` holds a reference to `*DB` for N record lookups, F record reads, and Chunker resolution
- **R486:** `WithChunkCache(cc *ChunkCache) SearchOption` — optional cross-search cache; `Retrieve` checks ChunkCache before rechunking from disk, enabling file-read reuse across multiple searches in a session
- **R490:** `SearchResult` carries unexported `chunkID uint64` — set during `scoreAndResolve` from the candidate's chunkID; provides dedup key for chunk content
- **R491:** `SearchResult` carries unexported `chunk []byte` — lazily populated by `Retrieve`; once set, subsequent `Retrieve` calls return immediately
- **R492:** `Retrieve(r *SearchResult) []byte` method on `*searchConfig` — returns chunk content. Check order: `r.chunk` (instant) → chunkID dedup cache → ChunkCache (auto-created if no external cache provided). Stores on both `r.chunk` and chunkID cache. ChunkCache handles file-level caching and tmp:// overlay paths
- **R493:** Post-filters (`verifyResults`, `verifyResultsRegex`, `applyRegexPostFilters`, `proximityRerank`) use `Retrieve` instead of `filterResults`+`rechunkForVerify`. `filterResults`, `rechunkForVerify`, `rechunkForVerifyTmp`, `rechunkContent`, `fileStrategy` are removed
- **R494:** (inferred) Within a search, Retrieve deduplicates by chunkID via a `map[uint64][]byte` on searchConfig — same chunk content across multiple results is retrieved once

## Feature: searchConfig as Search Pipeline Receiver
**Source:** specs/main.md

- **R487:** `searchConfig` embeds `*DB` — search pipeline functions become methods on `*searchConfig`
- **R488:** Search entry points (`Search`, `SearchRegex`, `SearchMulti`, `ScoreFile`, `SearchFuzzy`) build a `searchConfig` then dispatch to its methods
- **R489:** (inferred) Pure structural refactor — no behavior change, no new functionality beyond method receiver conversion

## Feature: Bracket Chunker
**Source:** specs/main.md

- **R307:** `BracketLang` struct: configurable lexical rules per language — line comments, block comments, string delimiters, bracket groups
- **R308:** `StringDelim` struct: `Open`, `Close`, `Escape` strings — supports asymmetric delimiters (e.g. `[[`/`]]`) and escapeless raw strings (empty Escape)
- **R309:** `BracketGroup` struct: `Open []string`, `Separators []string`, `Close []string` — handles word brackets like `if`/`then`/`else`/`end`
- **R310:** Token types: comment, string, whitespace, bracket, text — scanner classifies each token
- **R311:** Comments inside strings are not comments; strings inside comments are not strings
- **R312:** Whitespace tokens: contiguous runs of space, newline, tab, carriage return, form feed — always recognized, not configurable
- **R313:** Bracket tokens: single-character (`{`, `}`), multi-character (`<!--`, `-->`), or word (`begin`, `end`) — all configurable via BracketGroup
- **R314:** Multi-bracket groups: a single BracketGroup can have multiple openers, separators, and closers (e.g. `if`/`while`/`for` all open, `end` closes)
- **R315:** Text tokens: any contiguous non-whitespace characters that are not comment, string, or bracket
- **R316:** Chunk type — group: line-oriented; starts at the line containing an open bracket (not inside comment or string), continues line by line until all brackets are closed (depth returns to 0 at end of line); unified depth across all bracket types; single-line groups are not chunks
- **R317:** Leading comment/text lines immediately before an open bracket (no blank line separating) attach to the group's chunk
- **R318:** Chunk type — paragraph: sequence of lines not inside a group, terminated by blank line or start of a group
- **R319:** Range labels are `startline-endline` (1-based), consistent with other chunkers
- **R320:** `BracketChunker(lang BracketLang) Chunker` — returns a full Chunker (both Chunks and ChunkText)
- **R321:** Built-in language configs as package-level variables: `LangGo`, `LangC`, `LangJava`, `LangJS`, `LangLisp`, `LangNginx`, `LangPascal`, `LangShell`
- **R322:** Table-driven: adding a new language means adding a config entry, not code
- **R323:** CLI subcommand: `microfts chunk-bracket -lang <name> <file>` — outputs `range\tcontent` lines
- **R324:** (inferred) ChunkText seeks to the target range without scanning the entire file — justified by the chunker's complexity

## Feature: Indent Chunker
**Source:** specs/main.md

- **R325:** Reuses `BracketLang` for comment/string configuration (Brackets field ignored)
- **R326:** Scope detection: a line indented further than the previous non-blank line opens a new scope
- **R327:** Dedent: a line at lower indentation than the current scope closes the scope
- **R328:** Tab width configurable per invocation — controls how tabs count for column calculation; 0 means one column per tab
- **R329:** Chunk type — group: the header line (introducing deeper indentation) plus all following lines at that level or deeper, until dedent
- **R330:** Leading comment lines attach to the group (same rule as bracket chunker)
- **R331:** Chunk type — paragraph: consecutive lines at the same indentation level between groups, terminated by blank line or group start
- **R332:** Range labels are `startline-endline` (1-based)
- **R333:** `IndentChunker(lang BracketLang, tabWidth int) Chunker` — returns a full Chunker
- **R334:** CLI subcommand: `microfts chunk-indent -lang <name> [-tabwidth N] <file>` — outputs `range\tcontent` lines
- **R335:** (inferred) Comment and string handling required to avoid false scope detection inside literals

## Feature: Fuzzy Search
**Source:** specs/main.md

- **R336:** `WithLoose() SearchOption` — enables OR semantics at the term level for candidate collection
- **R337:** Fuzzy candidate set is the union of all terms' trigram candidate sets (a chunk matches if it contains any term's trigrams)
- **R338:** Within each term, trigram intersection is still AND (all trigrams of the term must match the chunk)
- **R339:** Default fuzzy scoring: score = (terms matched) / (total query terms), range [0.0, 1.0]
- **R340:** A term matches a chunk if its trigram set intersects the chunk's trigram counts (all trigram counts > 0)
- **R341:** Results sorted by score descending; custom `ScoreFunc` via `WithScoring` overrides the default fuzzy scoring
- **R342:** Composable with all existing search options: `WithVerify`, `WithRegexFilter`, `WithExceptRegex`, `WithChunkFilter`, `WithTrigramFilter`, `WithProximityRerank`
- **R343:** Works with `SearchMulti` — fuzzy candidate collection shared, per-strategy scoring independent
- **R344:** CLI flag: `microfts search -db <path> -fuzzy <query>` — composable with `-verify`, `-filter-regex`, `-except-regex`, `-score`
- **R345:** (inferred) When `WithLoose` is combined with `WithScoring`, the custom ScoreFunc receives the full query trigram set (union of all terms); the default loose term-match scoring is bypassed

## Feature: Fileid Filtering
**Source:** specs/main.md

- **R346:** `WithOnly(ids map[uint64]struct{}) SearchOption` — keep candidate chunks only if at least one of their fileids is in the set
- **R347:** `WithExcept(ids map[uint64]struct{}) SearchOption` — discard candidate chunks if any of their fileids is in the set
- **R348:** Both apply during candidate evaluation (same phase as ChunkFilter)

## Feature: Temporary Documents (tmp:// Overlay)
**Source:** specs/main.md

- **R349:** In-memory overlay on `*DB` holds tmp:// documents alongside the LMDB index — never touches LMDB
- **R350:** Temporary document paths use `tmp://` URI scheme (e.g. `tmp://abc123/scoring-notes`); path is opaque to microfts2
- **R351:** Temporary fileids count down from `math.MaxUint64` — structural guarantee against collision with LMDB fileids (which count up from 1)
- **R352:** Temporary chunkids count down from a separate counter starting at `math.MaxUint64` — same structural separation
- **R353:** Overlay holds equivalent of C, F, T, W, H records in Go maps — per-chunk data, per-file data, trigram index, token index, hash-to-chunkid lookup
- **R354:** Chunk deduplication within the overlay using SHA-256 hash — same mechanism as LMDB
- **R355:** No cross-deduplication between overlay and LMDB (separate chunkid spaces)
- **R356:** Overlay lifecycle tied to `*DB` handle — created on first use, destroyed on `Close()` or process exit
- **R357:** Individual tmp:// documents can be removed explicitly
- **R358:** `AddTmpFile(path, strategy string, content []byte) (uint64, error)` — chunks content, stores in overlay, returns fileid (counting down)
- **R359:** `AddTmpFile` requires registered chunking strategy; content must be valid UTF-8
- **R360:** `AddTmpFile` returns `ErrAlreadyIndexed` if path is already in the overlay
- **R361:** `UpdateTmpFile(path, strategy string, content []byte) error` — replaces content of existing tmp:// document; removes old chunks, adds new ones
- **R362:** `UpdateTmpFile` is atomic from caller's perspective — document is never absent from search during update
- **R363:** `UpdateTmpFile` returns error if path not found in overlay
- **R364:** `RemoveTmpFile(path string) error` — removes document and all orphaned chunks from overlay
- **R365:** `RemoveTmpFile` returns error if path not found
- **R366:** Search always includes overlay — candidates collected from both LMDB and overlay, merged and sorted by score
- **R367:** Overlay participates in all search modes: `Search`, `SearchRegex`, `SearchMulti`, `ScoreFile`
- **R368:** All `SearchOption`s apply uniformly to overlay candidates — `WithChunkFilter`, `WithVerify`, `WithTrigramFilter`, etc.
- **R369:** `TmpFileIDs() map[uint64]struct{}` — returns set of all current tmp:// fileids for use with `WithExcept`
- **R370:** `GetChunks` and `ChunkCache` work with tmp:// documents — retrieval reads from overlay's stored content rather than disk
- **R371:** Overlay stores original content bytes for each document (needed for chunk retrieval and verify)
- **R372:** Thread safety: concurrent reads allowed, writes (add/update/remove) serialized — `sync.RWMutex`
- **R373:** Overlay maintains its own `totalChunks` and `totalTokens` counters
- **R374:** BM25 and corpus-level computations sum LMDB counters and overlay counters for true corpus size
- **R375:** No CLI changes — tmp:// is library-only; ark CLI handles exposure
- **R376:** `WithNoTmp() SearchOption` — skips overlay entirely during search; no overlay lock acquired, no allocation
- **R377:** `HasTmp() bool` — returns true if the overlay has any tmp:// documents; no allocation
- **R378:** `TmpContent(path string) (*bytes.Reader, error)` — returns a reader over the raw stored content of a tmp:// document; no copy; error if not found

### AppendTmpFile (overlay append)

- **R428:** `AppendTmpFile(path, strategy string, content []byte, opts ...AppendOption) (uint64, error)` — shell `>>` semantics for tmp:// documents
- **R429:** Content must be valid UTF-8; error if not
- **R430:** `content` is only the appended bytes, not the full document
- **R431:** If path not found in overlay: auto-create via `addFile` (create-if-absent), return new fileid
- **R432:** If path found: strategy must match stored strategy; error on mismatch
- **R433:** Chunks appended content using the named strategy; adds resulting chunks to the file's chunk list
- **R434:** Existing chunks untouched — append only extends the chunk list
- **R435:** Chunk deduplication within the overlay applies to new chunks (via `dedupOrCreateChunk`)
- **R436:** File-level token bag merged with tokens from new chunks
- **R437:** Stored content bytes extended: `ofile.content = append(ofile.content, content...)`
- **R438:** (inferred) `adjustRange(rangeStr string, baseLine int) (string, error)` — shifts line range strings by baseLine offset
- **R439:** `WithBaseLine(n int)` option: 1-based line offset applied to chunk ranges via `adjustRange` so line numbers are absolute
- **R440:** Chunking happens outside the write lock — RLock to read file state, chunk outside lock, Lock to mutate
- **R441:** Double-check after acquiring write lock: if file was removed between RLock and Lock, return error
- **R442:** Empty chunk result from chunking is a no-op — returns fileid, nil

## Feature: Bigram Index
**Source:** specs/bigram.md

- **R379:** Bigram indexing is on by default; `--no-bigrams` flag at `init` disables it
- **R380:** Bigram enabled/disabled setting stored in an I record; checked at index time to gate B record writes and C record bigram section
- **R381:** DB format version bumped from v2 to v3 for C record bigram extension
- **R382:** Bigram extraction uses the same byte-level approach as trigrams: raw bytes, whitespace boundaries collapsed, case-insensitive when DB is case-insensitive
- **R383:** Character-internal bigrams (both bytes inside a single multibyte character) are skipped — same principle as trigram character-internal skipping
- **R384:** Word-boundary padding: each token gets a leading `_` and trailing `_` before bigram extraction (e.g. "cat" -> `_c`, `ca`, `at`, `t_`)
- **R385:** Byte aliases apply before bigram extraction, same as trigrams
- **R386:** `B` records: `B[bigram:2] → [chunkid:varint]...` — packed list of chunkids per bigram, same format as T records but 2-byte key
- **R387:** One B record per distinct bigram; document frequency derived from B record value length (same as T records)
- **R388:** C record bigram section (when enabled): `[n-bigrams:varint] [[bigram:2] [count:varint]]...` — appended after existing trigram counts
- **R389:** I record bigram flag determines whether C record marshal/unmarshal includes the bigram section
- **R390:** Bigram B record updates coalesced alongside T/W updates during AddFile — one read-modify-write per unique bigram across all chunks
- **R391:** For single-strategy `Search`, bigrams are scoring-only — candidates come from trigram intersection
- **R392:** `ScoreBigramOverlap` score function: matching query bigrams / total query bigrams per chunk; fits `ScoreFunc` signature
- **R393:** `WithBigramOverlap() SearchOption` — sugar for `WithScoring(ScoreBigramOverlap)`
- **R394:** Score function extracts bigrams from the query at call time using the same extraction as indexing
- **R395:** B records exist for DF lookups (future BM25-style bigram scoring)
- **R396:** CLI `init -db <path> --no-bigrams` — create DB without bigram index
- **R397:** CLI `search -db <path> -score bigram <query>` — search using bigram overlap scoring
- **R398:** Overlay (tmp://) includes bigram data when DB has bigrams enabled: chunks store bigram counts, overlay maintains B-record equivalent maps
- **R399:** `searchOverlay` includes bigram data in overlay candidates when bigrams enabled
- **R400:** RemoveFile updates B records alongside T/W records — same orphan-cleanup logic for B record chunkid removal
- **R401:** Reindex updates B records alongside T/W records
- **R402:** AppendChunks includes bigram extraction and B record updates when bigrams enabled
- **R403:** (inferred) `BRecord` struct: `Bigram uint16, ChunkIDs []uint64` — marshal/unmarshal same pattern as TRecord
- **R404:** (inferred) `CRecord` gains `Bigrams []BigramEntry` field; `BigramEntry` struct: `Bigram uint16, Count int`
- **R405:** (inferred) `extractBigrams` function: takes normalized byte slice, returns `map[uint16]int` of bigram counts with word-boundary padding
- **R406:** (inferred) Bigram extraction reuses `CharSet.normalize` for case folding and alias application before extracting 2-byte windows
- **R407:** `SearchStrategy` struct: `{Score ScoreFunc, UseBigrams bool}` — wraps a ScoreFunc with metadata about what candidate data it needs
- **R408:** `SearchMulti` accepts `map[string]SearchStrategy` instead of `map[string]ScoreFunc`
- **R409:** When any strategy in the map has `UseBigrams = true`, `collectCandidates` populates bigram counts for all candidates
- **R410:** `scoreAndResolve` passes bigram counts (instead of trigram counts) to strategies with `UseBigrams = true`
- **R411:** `StrategyFunc(fn ScoreFunc) SearchStrategy` — wraps a plain ScoreFunc with `UseBigrams = false`
- **R412:** `StrategyBigramOverlap(queryBigrams map[uint16]int) SearchStrategy` — returns `SearchStrategy{Score: ScoreBigramOverlap(queryBigrams), UseBigrams: true}`
- **R413:** `SearchMulti` candidate expansion: when any strategy has `UseBigrams`, collect additional candidates via trigram OR-union (union of T record posting lists for query trigrams) — surviving trigrams catch typo queries that AND-intersection misses
- **R414:** OR-union candidate chunkIDs merged into trigram-intersection candidate set before `collectCandidates` — all strategies see the merged set
- **R415:** `collectTrigramUnion` helper: reads T records for query trigrams, returns union of posting lists (OR semantics). Called from `SearchMulti` only
- **R416:** Overlay candidate expansion: when `expandCandidates` is true, overlay `searchOverlay` unions its trigram index entries for all active trigrams into the candidate set
- ~~**R417:** (inferred) Bigram OR-union may produce a larger candidate set than trigram intersection — no cap initially, measure and add filtering if needed~~

## Feature: Fuzzy Trigram Search
**Source:** specs/fuzzy-trigram.md

- **R418:** `SearchFuzzy(query string, k int, opts ...SearchOption) (*SearchResults, error)` — fast typo-tolerant search method on DB
- **R419:** Phase 1: extract trigrams from query, read T record posting lists, count how many posting lists each chunkID appears in — select top-k by count
- **R420:** Phase 2: read C records for top-k candidates only, re-score using ScoreCoverage (actual trigram counts), re-sort by accurate score
- **R421:** `k` parameter required — no unbounded fuzzy search
- **R422:** Returns `*SearchResults` (same type as Search) for API compatibility
- **R423:** Post-filter options apply to top-k: WithChunkFilter, WithRegexFilter, WithExceptRegex, WithProximityRerank
- **R424:** WithVerify, WithScoring, WithTrigramFilter, WithLoose are ignored (not applicable to posting-list scoring)
- **R425:** Overlay (tmp://) documents participate: overlay trigram maps OR-unioned into the tally alongside LMDB T records
- **R426:** CLI `-fuzzy` flag on search command calls `SearchFuzzy`. CLI `-loose` flag calls `Search` with `WithLoose()`. Default k = 20 for fuzzy
- **R427:** (inferred) `collectTrigramUnion` reused from SearchMulti candidate expansion for T record OR-union

## Feature: Record Counts
**Source:** specs/main.md

- **R443:** `RecordCounts() (map[byte]RecordStats, error)` — method on DB returning per-prefix statistics
- **R444:** Opens a read-only LMDB transaction, iterates all keys in the subdatabase, accumulates per-prefix stats
- **R445:** `RecordStats` struct: `Count int64`, `KeyBytes int64`, `ValueBytes int64` — aggregate totals per prefix byte

## Feature: TrigramFilter totalChunks source
**Source:** specs/main.md

- **R446:** `applyTrigramFilter` must read totalChunks from the I counter, not scan F records
- **R447:** (inferred) Include overlay chunk count in totalChunks passed to TrigramFilter, same pattern as BM25Func

## Feature: FileID–Path Mapping
**Source:** specs/main.md

- **R448:** `FileIDPaths() (map[uint64]string, error)` — method on DB returning fileid→path for all indexed files
- **R449:** Lazily loaded on first call: scans F records with `UnmarshalFHeader`, caches result
- **R450:** Incrementally maintained: AddFile inserts, RemoveFile deletes, Reindex removes+adds
- **R454:** (inferred) Cache is valid because microfts2 owns its subdatabase — dbi is unexported, no external writes
- **R455:** `pathToID` reverse cache (path→fileid) built alongside `pathCache`, used by `lookupFileByPath` to skip N record lookup

## Feature: Search Cache
**Source:** specs/main.md

- **R456:** `NewSearchCache() func()` — enables FRecord caching on DB, returns cleanup function that clears the cache
- **R457:** `readFRecord` checks `frecordCache` before LMDB read; caches result on miss
- **R458:** (inferred) Cache is keyed by fileid; same fileid returns same FRecord without re-read

## Feature: Partial F Record Unmarshal
**Source:** specs/main.md

- **R451:** `UnmarshalFHeader(data)` decodes ModTime, ContentHash, FileLength, Strategy, and Names from F record value
- **R452:** Stops before Chunks and Tokens — does not allocate or decode those arrays
- **R453:** `StaleFiles` uses `UnmarshalFHeader` instead of `UnmarshalFValue`

## Feature: DB Copy and Cache Invalidation
**Source:** specs/main.md

- **R459:** `Copy() *DB` — shallow copy sharing LMDB env, overlay, and chunker registry; caches nil
- **R460:** Copy shares `env`, `dbi`, `dbName`, `settings`, `trigrams`, `overlay`, `chunkers`
- **R461:** Copy sets `pathCache`, `pathToID`, `frecordCache` to nil — lazy reload from LMDB
- **R462:** (inferred) Copy does not copy `overlayOnce` — overlay pointer is shared directly, already initialized
- **R463:** `InvalidateCaches()` — nils `pathCache`, `pathToID`, `frecordCache` on the receiver
- **R464:** `InvalidateCaches` does NOT reset `overlayOnce`

## Feature: Chunk Processor Callback
**Source:** specs/main.md

- **R469:** `ChunkCallback` type: `func(chunkText string)` — receives clean chunk text during indexing
- **R470:** `WithChunkCallback(fn ChunkCallback) IndexOption` — supplies callback for indexing methods
- **R471:** `WithAppendChunkCallback(fn ChunkCallback) AppendOption` — supplies callback for append methods
- **R472:** `IndexOption` type: `func(*indexConfig)` — functional option for indexing methods, parallel to SearchOption and AppendOption
- **R473:** Callback fires once per chunk, in chunk order, after UTF-8 validation, before hashing/trigram extraction
- **R474:** Callback receives `string(chunk.Content)` — a copy, safe to retain
- **R475:** Nil callback (no option supplied) is a no-op — zero overhead on existing path
- **R476:** Callback errors are not propagated — observation only, never aborts indexing
- **R477:** `AddFile` gains `...IndexOption` variadic parameter
- **R478:** `AddFileWithContent` gains `...IndexOption` variadic parameter
- **R479:** `RefreshStale` gains `...IndexOption` variadic parameter
- **R480:** `AddTmpFile` gains `...IndexOption` variadic parameter
- **R481:** `UpdateTmpFile` gains `...IndexOption` variadic parameter
- **R482:** `AppendChunks` accepts `WithAppendChunkCallback` via existing `...AppendOption`
- **R483:** `AppendTmpFile` accepts `WithAppendChunkCallback` via existing `...AppendOption`
- **R484:** (inferred) Backward compatible — existing callers pass zero IndexOption args; append signatures unchanged
- **R485:** (inferred) `collectChunks` and overlay `collectChunksFromContent` are the injection points for the callback
