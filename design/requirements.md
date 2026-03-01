# Requirements

## Feature: Core
**Source:** specs/main.md

- **R1:** Go CLI command, also usable as a Go library
- **R2:** LMDB-backed storage using named subdatabases

## Feature: Character Set and Trigrams
**Source:** specs/main.md

- **R3:** Configurable character set — up to 63 characters plus space, set at initialization, immutable after
- **R4:** No spaces allowed in the character set string
- **R5:** Unmapped characters treated as space; runs of spaces collapsed
- **R6:** 6 bits per character, 18 bits per trigram, 256K possible trigrams
- **R7:** Trigram bitset per chunk: 32KB (2^18 bits = 2^15 bytes)
- **R8:** 64-bit trigram counts stored in a single 2M record
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

- **R14:** Content DB stores trigram bitsets, file metadata, and settings — this is the durable data
- **R15:** Index DB stores the inverted index (trigram → file+chunk set) — can be dropped and rebuilt from content DB

## Feature: Content DB Records
**Source:** specs/main.md

- **R16:** `C` record: 2M of 64-bit trigram counts
- **R17:** `I` record: JSON database settings (chunking strategies, case-insensitive flag, character set, active trigrams)
- **R18:** `T` records: `[fileid:8][chunknum:8]` → trigram bitset for that chunk
- **R19:** `N` records: `[fileid:8]` → JSON with chunk offsets and chunking strategy name
- **R20:** `F` records: filename → fileid mapping using key chains for names exceeding 511 bytes

## Feature: Index DB Records
**Source:** specs/main.md

- **R21:** Index entries: `[trigram:3][fileid:8][chunknum:8]` as keys in a set (empty values)

## Feature: Data-in-Key Pattern
**Source:** specs/main.md

- **R22:** Store data in keys using lexical sort for range queries
- **R23:** Key ranges: `[key]...[key+1]` spans all items for a key
- **R24:** Sets represented as `[key][info] → empty value`

## Feature: Key Chains
**Source:** specs/main.md

- **R25:** Filenames exceeding LMDB's 511-byte key limit use multiple chained keys
- **R26:** `F` records use name-part byte to chain: part 255 indicates final segment, value holds fileid

## Feature: Active Trigrams
**Source:** specs/main.md

- **R27:** Trigrams below a configurable frequency percentile cutoff are indexed (active trigrams)
- **R28:** Active trigram list and cutoff stored in the `I` record

## Feature: Adding Files
**Source:** specs/main.md

- **R29:** Adding a file: create `F` record (assigns fileid), create `T` records for each chunk, create `N` record

## Feature: Searching
**Source:** specs/main.md

- **R30:** If the index DB does not exist, compute it before searching
- **R31:** Compute trigrams for search string
- **R32:** Intersect file+chunk sets for each active trigram in the query
- **R33:** Results sorted by filename then chunk number
- **R34:** CLI output: one result per line, `filepath:startline-endline`
- **R35:** Library returns struct slices with file path, start line, end line

## Feature: Index Computation
**Source:** specs/main.md

- **R36:** For each `T` record, for each active trigram in the bitset, add an index entry to the index DB

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
- **R53:** `Search` returns `[]SearchResult` with Path, StartLine, EndLine
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

## Feature: Index Rebuild
**Source:** specs/main.md

- **R43:** Index can be rebuilt from content DB without re-reading source files
- **R44:** Rebuilding with a different cutoff only requires iterating T records

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
