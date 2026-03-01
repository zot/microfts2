# microfts
A dynamic LMDB trigram index, written in Go. CLI command, structured so it can also be used as a library.

# features

## configurable chunking strategies

- add/remove chunking strategies dynamically
- files track their indexed chunking strategy
  - can reindex with a different strategy -- allows migration to better strategies
- configurable character set -- supports up to 63 characters plus space, unindexed character == space (runs are collapsed)
- character aliases -- map input characters to charset characters before encoding (e.g. newline → `^` for line-start matching)
  - 6 bits / character, 18 bits per trigram
  - 256K possible trigrams (2^18 = 262,144)
  - trigram bitset per chunk: 32KB (2^18 bits = 2^15 bytes)
  - 64-bit counts for all trigrams: 2MB (256K × 8 bytes)

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

LMDB supports multiple named subdatabases which can be deleted at the drop of a hat.
This lets us use one tree for the content we index (a trigram bitset) and another for the actual index,
allowing us to drop the index but keep the content data.

Subdatabase names are parameters: defaults 'ftscontent' and 'ftsindex', settable via CLI and library API.
These are not stored in the I record -- they're needed to open the databases in the first place.

- content db:
  - 'C' -> counts: 2M of trigram counts (fits evenly into blocks)
  - 'I' -> JSON -- database settings
    - chunkingStrategies: object
      - {name: cmd}: [cmd] [filename] -> list of file offsets
    - characterAliases: object -- maps input characters to charset characters (e.g. {"\n": "^"})
    - caseInsensitive: boolean
    - characterSet: string -- this is set during initialization and cannot be changed
      - a string of up to 64 characters
      - No spaces allowed
      - For punctuation, case-insensitive is recommended
    - activeTrigramCutoff: number -- frequency percentile cutoff for active trigrams (e.g. 50 means bottom 50%)
    - activeTrigrams: array[number] -- the trigrams that are actually in the index (below the cutoff)
  - 'T' [fileid: 8] [chunknum: 8] -> trigram bitset for chunk
  - 'N' [fileid: 8] -> JSON -- information about file
    - chunkOffsets: array[number] -- offsets of chunks in file
    - chunkingStrategy: string -- which chunking strategy was used for the file
  - 'F' [name part: 1] filename-prefix -> filename prefix, name part 255 indicates end of prefix (see previous)
  - 'F' 255 filename-suffix -> [fileid: 8]: file info for reindexing changed files / deleting

- index db: 
  - [trigram: 3] [fileid: 8] [chunknum: 8] -- set of fileids + chunknums for each trigram

# The process

We add a file to the database with a chosen chunking strategy:
- create the F record for the file to get its fileid
- create a T record for each chunk
- create the N record for the file

When searching for a string
- if the index does not exist, compute the index
- compute the trigrams for the string
- collect the file chunks for each active trigram (see the I record)
- output the file names and chunks, sorted by filename and chunknum
- CLI output format: one result per line, `filepath:startline-endline`
- library returns struct slices with the same information

To compute the index:
- for each T record
  - for each active trigram in the record, add an index entry

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
- `microfts search -db <path> <query>...`
  Search for text. Builds index on demand if needed. Output: `filepath:startline-endline`
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

# Library API

```go
// Lifecycle
func Create(path string, opts Options) (*DB, error)
func Open(path string, opts Options) (*DB, error)
func (db *DB) Close() error
func (db *DB) Settings() Settings

// Content
func (db *DB) AddFile(fpath, strategy string) error
func (db *DB) RemoveFile(fpath string) error
func (db *DB) Reindex(fpath, strategy string) error

// Search
func (db *DB) Search(query string) ([]SearchResult, error)

// Index
func (db *DB) BuildIndex(cutoff int) error

// Strategies
func (db *DB) AddStrategy(name, cmd string) error
func (db *DB) RemoveStrategy(name string) error
```

Options:
- CharSet, CaseInsensitive, Aliases — creation-time only
- ContentDBName, IndexDBName — defaults "ftscontent"/"ftsindex"

SearchResult: `{ Path string, StartLine int, EndLine int }`
