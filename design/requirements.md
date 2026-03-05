# Requirements

## Feature: Core
**Source:** specs/main.md

- **R1:** Go CLI command, also usable as a Go library
- **R2:** LMDB-backed storage using named subdatabases

## Feature: Character Set and Trigrams
**Source:** specs/main.md

- **R3:** Configurable character set — up to 255 characters plus space, set at initialization, immutable after
- **R4:** No spaces allowed in the character set string
- **R5:** Unmapped characters treated as space; runs of spaces collapsed
- **R6:** 8 bits per character, 24 bits per trigram, 16M possible trigrams
- **R7:** A record: sparse packed sorted trigram list (3 bytes per trigram, only active trigrams stored)
- **R8:** C records: sparse individual LMDB records `C[trigram:3] → count:8`, one per non-zero trigram
- **R9:** Case-insensitive mode (recommended for punctuation character sets)

## Feature: Character Aliases
**Source:** specs/main.md

- **R45:** Character aliases map input characters to charset characters before encoding
- **R46:** Aliases stored in I record as characterAliases object
- **R47:** Applied during Encode before charset lookup (e.g. newline → `^` for line-start matching)

## Feature: Chunking Strategies
**Source:** specs/main.md

- **R10:** Chunking strategies are configurable and added/removed dynamically
- **R11:** Each strategy is a name mapped to an external command: `[cmd] [filename]` returns a list of file offsets
- **R12:** Each file tracks which chunking strategy was used to index it
- **R13:** Files can be reindexed with a different strategy to allow migration

## Feature: Two-Tree Storage
**Source:** specs/main.md

- **R14:** Content DB stores file metadata, trigram frequency counts, active set, and settings
- **R15:** Index DB stores the trigram-to-chunk inverted index with occurrence counts, maintained incrementally

## Feature: Content DB Records
**Source:** specs/main.md

- **R16:** `C` records: sparse `C[trigram:3] → count:8`, only non-zero trigrams stored
- **R17:** `I` record: JSON database settings (chunking strategies, case-insensitive flag, character set, active trigrams)
- **R19:** `N` records: `[fileid:8]` → JSON with chunk offsets, token counts, and chunking strategy name
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
- **R28:** searchCutoff stored in the I record; active trigrams (bottom searchCutoff% by frequency) stored as a sparse packed sorted trigram list in the A record for literal query filtering

## Feature: Adding Files
**Source:** specs/main.md

- **R29:** Adding a file: create `F` record (assigns fileid), chunk and compute trigram counts, create `N` record (with token counts), update C records (per-trigram, insert/increment), write forward and reverse index entries

## Feature: Searching
**Source:** specs/main.md

- **R30:** If the index DB does not exist, compute it before searching
- **R31:** Literal search: compute trigrams for search string, filter to active set (bottom searchCutoff%)
- **R32:** Literal search: intersect file+chunk sets for each active query trigram
- **R33:** Results sorted by filename then chunk number
- **R34:** CLI output: one result per line, `filepath:startline-endline`
- **R35:** Library returns struct slices with file path, start line, end line

## Feature: Index Computation
**Source:** specs/main.md

- **R36:** Build-index: cursor scan all C records, sort by count, take bottom searchCutoff% → write A record as packed sorted trigram list; index entries are maintained incrementally, build-index only recomputes the active set

## Feature: CLI Commands
**Source:** specs/main.md

- **R37:** CLI `delete` command removes files from the database
- **R38:** CLI `reindex` command re-chunks files with a different strategy
- **R39:** CLI `init` command creates a new database with charset, case-insensitive, and alias options
- **R48:** CLI `build-index` command explicitly builds/rebuilds the index with configurable cutoff
- **R49:** CLI `strategy` subcommands: `add`, `remove`, `list` for managing chunking strategies
- **R50:** All CLI commands require `-db` flag; shared optional flags `-content-db`, `-index-db`

## Feature: Library API
**Source:** specs/main.md

- **R51:** `Create`/`Open`/`Close`/`Settings` lifecycle functions
- **R52:** `AddFile`/`RemoveFile`/`Reindex` content management methods
- **R53:** `Search` accepts variadic `SearchOption` and returns `*SearchResults` with Results slice and IndexStatus
- **R54:** `BuildIndex` accepts cutoff percentile parameter
- **R55:** `AddStrategy`/`RemoveStrategy` for runtime strategy management
- **R56:** `Options` struct configures creation (CharSet, CaseInsensitive, Aliases) and opening (ContentDBName, IndexDBName)

## Feature: Built-in Chunking Strategies
**Source:** specs/main.md

- **R57:** Built-in chunkers are CLI subcommands that output byte offsets to stdout, same interface as external chunkers
- **R58:** `chunk-lines`: every line is a chunk (byte offset after each newline)
- **R59:** `chunk-lines-overlap`: fixed-size line windows with configurable lines per chunk (default 50) and overlap (default 10)
- **R60:** `chunk-words-overlap`: fixed-size word windows with configurable words per chunk (default 200), overlap (default 50), and word pattern (default `\S+`)
- **R61:** Word pattern is a configurable regexp used to count and locate word boundaries
- **R62:** Chunk boundaries output as byte offsets at the start of each window

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

## Feature: A Record (Active Trigram Set)
**Source:** specs/main.md

- **R75:** `A` record in content DB: sparse packed sorted trigram list (3 bytes each) of bottom searchCutoff% trigrams by frequency
- **R76:** `BuildIndex` cursor-scans all C records, sorts by count, writes A record as packed sorted trigram list
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
- **R88:** Regex search queries the full index, not filtered to active set
- **R89:** CLI `search -regex` flag switches to regex mode

## Feature: Ark Integration
**Source:** specs/main.md

- **R91:** `Env()` method returns the underlying `*lmdb.Env` for sharing with other libraries in the same process
- **R92:** `AddFile` returns `(uint64, error)` — the fileid alongside the error
- **R93:** `Reindex` returns `(uint64, error)` — the fileid alongside the error
- **R94:** `FileInfoByID(fileid uint64)` returns `(FileInfo, error)` — resolves fileid to filename, chunk offsets, line numbers, strategy, modTime, contentHash
- **R95:** `FileInfo` is the exported struct type matching the N record JSON fields (including `ChunkTokenCounts`)
- **R96:** `ScoreFile(query, fpath string, fn ScoreFunc)` returns `([]ScoredChunk, error)` — per-chunk scores using the given scoring function
- **R97:** Coverage score = matching active trigrams / total active query trigrams, per chunk (default strategy)
- **R98:** `ScoredChunk` struct: `StartLine int, EndLine int, Score float64`
- **R99:** `SearchResult` gains `Score float64` field — per-chunk score in the general search path
- **R100:** CLI `score` subcommand: `microfts score -db <path> <query> <file>...` — output `filepath:startline-endline\tscore`

## Feature: Scoring Strategies
**Source:** specs/main.md

- **R103:** `ScoreFunc` type: `func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64`
- **R104:** `SearchOption` functional option type for configuring search behavior
- **R105:** `WithCoverage()` option: score = matching active trigrams / total active query trigrams (default)
- **R106:** `WithDensity()` option: token-density scoring for long queries — tokenize on spaces, token strength = min trigram count, score = sum strengths / chunk token count
- **R107:** `WithScoring(fn ScoreFunc)` option: use a custom scoring function
- **R108:** CLI `search -score coverage|density` flag selects scoring strategy (default: coverage)
- **R109:** N record stores `chunkTokenCounts` array — token count per chunk, computed during AddFile
- **R110:** (inferred) AddFile computes trigram counts per chunk (map[uint32]int) instead of presence-only bitsets

## Feature: MaxDBs Option
**Source:** specs/main.md

- **R101:** `Options.MaxDBs` sets the LMDB max named databases; defaults to 2; used by both `Create` and `Open`
