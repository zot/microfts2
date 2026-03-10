# Requirements

## Feature: Core
**Source:** specs/main.md

- **R1:** Go CLI command, also usable as a Go library
- **R2:** LMDB-backed storage using named subdatabases

## Feature: Raw Byte Trigrams
**Source:** specs/main.md

- **R3:** Raw byte trigrams — every byte is its own value, no character set mapping
- **R4:** Whitespace bytes (space, tab, newline, carriage return) are word boundaries; runs collapse
- **R5:** All non-whitespace bytes are indexed; UTF-8 multibyte characters produce cross-boundary byte trigrams
- **R6:** 8 bits per byte, 24 bits per trigram, 16M possible trigrams
- **R7:** ~~removed: A record replaced by dynamic TrigramFilter~~
- **R8:** C records: sparse individual LMDB records `C[trigram:3] → count:8`, one per non-zero trigram
- **R9:** Case-insensitive mode: `bytes.ToLower()` on input before trigram extraction
- **R111:** AddFile checks each chunk's Content for valid UTF-8 (utf8.Valid); the raw file may be binary — the chunker produces UTF-8 text
- **R112:** Character-internal byte trigrams are skipped: a 3-byte window entirely within one multibyte character is not emitted
- **R113:** 3-byte characters (CJK): 1 internal trigram skipped per character; 4-byte characters (emoji): 2 internal trigrams skipped
- **R114:** 2-byte and ASCII characters produce no internal trigrams; behavior unchanged for these

## Feature: Byte Aliases
**Source:** specs/main.md

- **R45:** Byte aliases map input bytes to replacement bytes before trigram extraction
- **R46:** Aliases stored in I record as aliases object
- **R47:** Applied before trigram extraction (e.g. newline → `^` for line-start matching)
- **R115:** Both source and target bytes in aliases must be ASCII (< 0x80) — aliasing UTF-8 continuation or leading bytes would corrupt multibyte characters and break character-internal trigram skipping

## Feature: Chunking Strategies
**Source:** specs/main.md

- **R10:** Chunking strategies are configurable and added/removed dynamically (external commands or Go functions)
- **R11:** Each strategy is a name mapped to an external command: `[cmd] [filename]` outputs `range\tcontent` lines
- **R12:** Each file tracks which chunking strategy was used to index it
- **R13:** Files can be reindexed with a different strategy to allow migration
- **R116:** `AddStrategyFunc(name, fn ChunkFunc)` registers a Go function as a chunking strategy; `ChunkFunc` type: `func(path string, content []byte, yield func(Chunk) bool) error` — generator pattern
- **R117:** Func strategies are in-memory only — not persisted to I record cmd field (re-register on Open); I record stores name with empty cmd
- **R118:** When AddFile/Reindex uses a func strategy, call the function directly instead of exec
- **R119:** Built-in chunkers (chunk-lines, chunk-lines-overlap, chunk-words-overlap, chunk-markdown) register as func strategies

## Feature: Two-Tree Storage
**Source:** specs/main.md

- **R14:** Content DB stores file metadata, trigram frequency counts, and settings
- **R15:** Index DB stores the trigram-to-chunk inverted index with occurrence counts, maintained incrementally

## Feature: Content DB Records
**Source:** specs/main.md

- **R16:** `C` records: sparse `C[trigram:3] → count:8`, only non-zero trigrams stored
- **R17:** `I` record: JSON database settings (chunking strategies, case-insensitive flag, aliases, search cutoff)
- **R19:** `N` records: `[fileid:8]` → JSON with chunk ranges (opaque strings), token counts, and chunking strategy name; `chunkRanges` array index = chunk number (0-based), preserving chunker emission order; `chunkTokenCounts` is parallel
- **R20:** `F` records: filename → fileid mapping using key chains for names exceeding 511 bytes

## Feature: Index DB Records
**Source:** specs/main.md

- **R21:** Forward index entries: `[trigram:3][count:2 desc][fileid:8][chunknum:8]` as keys (empty values); count stored as `0xFFFF - count` for descending sort, capped at 65535
- **R102:** Reverse index entries: `R[fileid:8]` → packed `[chunknum:8][numTrigrams:2][[trigram:3][count:2]]...` groups; one record per file, used for removal

## Feature: Data-in-Key Pattern
**Source:** specs/main.md

- **R22:** Store data in keys using lexical sort for range queries
- **R23:** Key ranges: `[key]...[key+1]` spans all items for a key
- **R24:** Sets represented as `[key][info] → empty value`

## Feature: Key Chains
**Source:** specs/main.md

- **R25:** Filenames exceeding LMDB's 511-byte key limit use multiple chained keys
- **R26:** `F` records use name-part byte to chain: part 255 indicates final segment, value holds fileid

## Feature: Full Trigram Index
**Source:** specs/main.md

- **R27:** Index DB contains entries for ALL trigrams present in the content, not a frequency-selected subset
- **R28:** ~~removed: searchCutoff and A record replaced by dynamic TrigramFilter~~

## Feature: Adding Files
**Source:** specs/main.md

- **R29:** Adding a file: create `F` record (assigns fileid), call chunker generator (yields Range+Content per chunk), compute trigrams on Content, create `N` record (with chunk ranges and token counts), update C records (per-trigram, insert/increment), write forward and reverse index entries

## Feature: Searching
**Source:** specs/main.md

- **R30:** If the index DB does not exist, compute it before searching
- **R31:** Literal search: trim whitespace, parse query into terms via parseQueryTerms, extract trigrams per term (not whole query), intersect per-term candidate sets, select via TrigramFilter (default: FilterAll)
- **R32:** Literal search: for each active query trigram, scan index by trigram prefix (ignoring count field) to collect candidate (fileid, chunknum) pairs; intersect candidate sets across all active query trigrams; collect per-trigram counts from index keys for surviving candidates
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
- **R50:** All CLI commands require `-db` flag; shared optional flags `-content-db`, `-index-db`

## Feature: Library API
**Source:** specs/main.md

- **R51:** `Create`/`Open`/`Close`/`Settings` lifecycle functions
- **R52:** `AddFile`/`RemoveFile`/`Reindex` content management methods
- **R53:** `Search` accepts variadic `SearchOption` and returns `*SearchResults` with Results slice and IndexStatus
- **R54:** ~~removed: BuildIndex replaced by dynamic TrigramFilter~~
- **R55:** `AddStrategy`/`RemoveStrategy` for runtime strategy management
- **R56:** `Options` struct configures creation (CaseInsensitive, Aliases) and opening (ContentDBName, IndexDBName)

## Feature: Built-in Chunking Strategies
**Source:** specs/main.md

- **R57:** Built-in chunkers are registered as func strategies; also available as CLI subcommands outputting `range\tcontent` lines
- **R58:** `chunk-lines`: every line is a chunk; range is `N-N` (line number); content is the line text
- **R59:** `chunk-lines-overlap`: fixed-size line windows with configurable lines per chunk (default 50) and overlap (default 10); range is `startline-endline`
- **R60:** `chunk-words-overlap`: fixed-size word windows with configurable words per chunk (default 200), overlap (default 50), and word pattern (default `\S+`); range is `startline-endline`
- **R61:** Word pattern is a configurable regexp used to count and locate word boundaries
- **R62:** Range for all built-in text chunkers is `startline-endline` (1-based, inclusive); content is the text of those lines

## Feature: Subdatabase Names
**Source:** specs/main.md

- **R40:** Content and index subdatabase names are parameters with defaults `ftscontent` and `ftsindex`
- **R41:** Settable via CLI flags and library API
- **R42:** Not stored in the I record — required to open the databases

## Feature: Staleness Detection
**Source:** specs/main.md

- **R63:** N record JSON stores file modification time (Unix nanoseconds) and content hash (SHA-256) at index time
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

## Feature: A Record (Removed)
**Source:** specs/main.md

- **R75:** ~~removed: A record replaced by dynamic TrigramFilter~~
- **R76:** ~~removed: BuildIndex replaced by dynamic TrigramFilter~~
- **R77:** `AddFile`/`Reindex` write forward and reverse index entries for all of the file's trigrams
- **R78:** `RemoveFile` reads the file's reverse index entry to find and delete its forward index entries

## Feature: Incremental Index Update
**Source:** specs/main.md

- **R79:** `AddFile` always writes forward and reverse index entries (index is always maintained)
- **R80:** `RemoveFile` uses the reverse index entry to delete forward entries, then deletes the reverse entry
- **R81:** Index is always maintained incrementally — no separate "index exists" check needed

## Feature: Regex Search
**Source:** specs/main.md

- **R82:** `SearchRegex(pattern string, opts ...SearchOption)` searches using a Go regexp pattern against the full trigram index
- **R83:** `IndexStatus` struct: `Built bool`
- **R84:** Trigram query extracted from regex AST as boolean AND/OR expression, using rsc's approach (github.com/google/codesearch/regexp)
- **R85:** `Search` returns `*SearchResults` containing both results and IndexStatus
- **R86:** `RefreshStale` returns `([]FileStatus, error)` — no IndexStatus
- **R87:** AND nodes in the trigram query intersect candidate sets; OR nodes union them
- **R88:** Regex search queries the full index (no trigram filtering)
- **R127:** Regex search always verifies: re-chunk file using stored strategy, run compiled regex against chunk content, discard non-matches (trigram query is a superset filter)
- **R89:** CLI `search -regex` flag switches to regex mode

## Feature: Ark Integration
**Source:** specs/main.md

- **R91:** `Env()` method returns the underlying `*lmdb.Env` for sharing with other libraries in the same process
- **R92:** `AddFile` returns `(uint64, error)` — the fileid alongside the error
- **R93:** `Reindex` returns `(uint64, error)` — the fileid alongside the error
- **R94:** `FileInfoByID(fileid uint64)` returns `(FileInfo, error)` — resolves fileid to filename, chunk ranges, strategy, modTime, contentHash
- **R95:** `FileInfo` is the exported struct type matching the N record JSON fields (ChunkRanges, ChunkTokenCounts, etc.)
- **R96:** `ScoreFile(query, fpath string, fn ScoreFunc)` returns `([]ScoredChunk, error)` — per-chunk scores using the given scoring function
- **R97:** Coverage score = matching selected trigrams / total selected query trigrams, per chunk (default strategy)
- **R98:** `ScoredChunk` struct: `Range string, Score float64`
- **R99:** `SearchResult` gains `Score float64` field — per-chunk score in the general search path
- **R100:** CLI `score` subcommand: `microfts score -db <path> [-score coverage|density] <query> <file>...` — output `filepath:range\tscore`
- **R120:** `AddFileWithContent(fpath, strategy string)` returns `(uint64, []byte, error)` — fileid + file content already read for trigram extraction
- **R121:** `ReindexWithContent(fpath, strategy string)` returns `(uint64, []byte, error)` — fileid + file content already read for trigram extraction
- **R122:** Original `AddFile`/`Reindex` signatures unchanged (no breaking change); WithContent variants avoid a redundant file read in ark's hot path

## Feature: Scoring Strategies
**Source:** specs/main.md

- **R103:** `ScoreFunc` type: `func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64`
- **R104:** `SearchOption` functional option type for configuring search behavior
- **R105:** `WithCoverage()` option: score = matching selected trigrams / total selected query trigrams (default)
- **R106:** `WithDensity()` option: token-density scoring for long queries — tokenize on spaces, token strength = min trigram count, score = sum strengths / chunk token count
- **R107:** `WithScoring(fn ScoreFunc)` option: use a custom scoring function
- **R108:** CLI `search -score coverage|density` flag selects scoring strategy (default: coverage)
- **R109:** N record stores `chunkRanges` (opaque strings from chunker) and `chunkTokenCounts` array — token count per chunk, computed from chunk Content during AddFile
- **R110:** (inferred) AddFile computes trigram counts per chunk (map[uint32]int) from chunk Content, not raw file bytes

## Feature: MaxDBs Option
**Source:** specs/main.md

- **R101:** `Options.MaxDBs` sets the LMDB max named databases; defaults to 2; used by both `Create` and `Open`

## Feature: C Record BigEndian
**Source:** specs/main.md

- **R123:** C record values (`count:8`) use big-endian encoding, consistent with all other integer fields in LMDB keys and values

## Feature: WithVerify Post-filter
**Source:** specs/main.md

- **R124:** `WithVerify()` search option: after trigram intersection, re-chunk the file using stored strategy to recover chunk content, verify each query term appears as a case-insensitive substring; discard chunks that fail
- **R125:** Query tokenization for verify: split on spaces; double-quoted strings are a single term with quotes stripped (e.g. `"hello world" foo` → terms `hello world`, `foo`)
- **R126:** CLI `-verify` flag on search command passes `WithVerify()` to the library

## Feature: Chunk Struct and Generator Contract
**Source:** specs/main.md

- **R128:** `Chunk` struct: `Range []byte` (opaque, string semantics) + `Content []byte` (text to trigram-index); both are reusable buffers — caller must copy before next yield
- **R129:** Chunkers are deterministic: same file produces same chunks with same ranges
- **R130:** Chunkers serve dual purpose: indexing (produce chunks) and extraction (re-chunk to recover content by range match)
- **R131:** External command chunkers output `range\tcontent` per line to stdout; a wrapper ChunkFunc parses this into Chunk yields
- **R132:** `SearchResult` struct: `Path string, Range string, Score float64` (replaces StartLine/EndLine)
- **R133:** `ScoredChunk` struct: `Range string, Score float64` (replaces StartLine/EndLine)
- **R134:** Verify and regex verification re-chunk the file using stored strategy, match by range to find chunk content

## Feature: Dynamic Trigram Filtering
**Source:** specs/main.md

- **R135:** `TrigramCount` struct: `Trigram uint32, Count int` — pairs a trigram code with its corpus document frequency
- **R136:** `TrigramFilter` type: `func(trigrams []TrigramCount, totalChunks int) []TrigramCount` — caller-supplied function deciding which query trigrams to search with
- **R137:** `WithTrigramFilter(fn TrigramFilter)` search option: look up each query trigram's C record count, call the filter, use the returned subset
- **R138:** ~~removed: no backward compat needed — default is FilterAll~~
- **R139:** `WithTrigramFilter` applies to both `Search` and `ScoreFile`
- **R140:** `FilterAll` stock filter: returns all trigrams unmodified (disables filtering)
- **R141:** `FilterByRatio(maxRatio float64)` stock filter: skips trigrams appearing in more than `maxRatio` of total chunks
- **R142:** `FilterBestN(n int)` stock filter: keeps the N trigrams with the lowest document frequency
- **R143:** Trigram counts retrieved via per-query C record point reads (typically 3-10 LMDB reads per query)
- **R144:** Total chunk count derived from the database for use by filter functions
- **R145:** ~~removed: A record and BuildIndex fully removed~~

## Feature: FileLength in N Record
**Source:** specs/main.md

- **R146:** N record JSON stores `fileLength` (int64): file size in bytes at index time
- **R147:** `AddFile` and `Reindex` set `fileLength` from the content they already read
- **R148:** `FileInfo` struct gains `FileLength int64` field
- **R149:** (inferred) Existing N records without `fileLength` decode as zero — no migration needed, JSON omitempty handles it

## Feature: AppendChunks API
**Source:** specs/main.md

- **R150:** `AppendChunks(fileid uint64, content []byte, strategy string, opts ...AppendOption) error` adds chunks to an existing file without full reindex
- **R151:** `content` parameter is only the appended bytes, not the full file
- **R152:** Chunks `content` using the named strategy's `ChunkFunc`
- **R153:** Chunk numbering continues from the file's existing chunk count
- **R154:** Forward index entries are written for new chunks only; existing entries are not touched
- **R155:** R record is replaced with a new packed record containing both old and new chunk data
- **R156:** C records are incremented for the new chunks' trigrams
- **R157:** N record is updated: appended ChunkRanges, appended ChunkTokenCounts, new ContentHash, ModTime, FileLength
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
- **R198:** Target chunk identified by exact string match of range label against the file's `chunkRanges` array
- **R199:** Neighbor window: `max(0, targetIndex - before)` to `min(len-1, targetIndex + after)`, inclusive
- **R200:** Chunk content recovered by re-chunking the file from disk using its stored chunking strategy
- **R201:** `ChunkResult` struct: `Path string, Range string, Content string, Index int` — index is 0-based position in the file's chunk list
- **R202:** Returns chunks in positional order (ascending index)
- **R203:** Error if file not in database, target range not found, file missing from disk, or chunking strategy not registered
- **R204:** CLI `chunks` subcommand: `microfts chunks -db <path> [-before N] [-after N] <file> <range>` — JSONL output with `path`, `range`, `content`, `index` fields
- **R205:** `-before` and `-after` default to 0 (target chunk only)
- **R206:** (inferred) Expansion unit is chunks (strategy-agnostic), not lines or bytes — range labels are opaque
