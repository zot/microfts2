# microfts2

A dynamic trigram index backed by LMDB, written in Go. Usable as a CLI tool or as a library.

microfts2 indexes files into trigram bitsets organized by chunks, then builds an inverted index for fast substring search. The index can be dropped and rebuilt from stored content without re-reading source files.

## Install

```sh
go install microfts/cmd/microfts@latest
```

Or clone and build:

```sh
git clone https://github.com/zot/microfts2.git
cd microfts2
go build ./cmd/microfts
```

## Quick Start

```sh
# Create a database with default character set
microfts init -db myindex

# Register a chunking strategy (any command that outputs byte offsets)
microfts strategy add -db myindex -name lines -cmd "microfts chunk-lines"

# Add files
microfts add -db myindex -strategy lines src/*.go

# Search
microfts search -db myindex "func Open"
# output: src/db.go:198-260

# Check for stale files
microfts stale -db myindex

# Refresh stale files and search in one step
microfts search -r -db myindex "func Open"
```

## How It Works

Each character maps to 6 bits (configurable character set, up to 63 characters plus space). Three consecutive characters form an 18-bit trigram. Each file chunk gets a 32KB bitset recording which trigrams appear in it.

Search computes the query's trigrams, intersects posting lists from the inverted index, and returns matching file/chunk locations.

**Two-tree design:** Content and index live in separate LMDB subdatabases. The content DB stores trigram bitsets, file metadata, and settings. The index DB stores the inverted trigram-to-chunk mapping and can be dropped and rebuilt at any time from the content DB alone.

**Active trigram cutoff:** Not all trigrams are useful for search — very common ones (like `the`) match too many chunks to be selective. The cutoff percentile (default 50) controls which trigrams are "active" in the index: only trigrams in the bottom N% by frequency get indexed. A lower cutoff means fewer, more selective trigrams and a smaller index; a higher cutoff means more trigrams indexed and broader coverage. Use `build-index -cutoff <percentile>` to change it — no need to re-add files, since the content DB retains the full trigram bitsets. Subsequent searches use the last-built cutoff.

**Staleness detection:** Each indexed file records its modification time and SHA-256 hash. Checking mod time first avoids hashing unchanged files. The `-r` flag refreshes stale files before any command.

## CLI Reference

All commands require `-db <path>`. Optional: `-content-db`, `-index-db` for custom subdatabase names.

| Command | Description |
|---------|-------------|
| `init` | Create a new database. `-charset`, `-case-insensitive`, `-aliases` |
| `add` | Add files. `-strategy <name>` |
| `search` | Search for text. Builds index on demand. Output: `path:start-end` |
| `delete` | Remove files from the database |
| `reindex` | Re-chunk files with a different strategy. `-strategy <name>` |
| `build-index` | Rebuild the index. `-cutoff <percentile>` (default 50) |
| `stale` | List stale and missing files |
| `strategy add\|remove\|list` | Manage chunking strategies |
| `chunk-lines` | Built-in chunker: one chunk per line |
| `chunk-lines-overlap` | Built-in chunker: overlapping line windows. `-lines`, `-overlap` |
| `chunk-words-overlap` | Built-in chunker: overlapping word windows. `-words`, `-overlap`, `-pattern` |

**Global flag:** `-r` refreshes all stale files before running the command. Usable standalone (`microfts -r -db path`) or combined (`microfts search -r -db path query`).

## Library API

```go
import "microfts"

// Lifecycle
db, err := microfts.Create(path, microfts.Options{CharSet: "abc..."})
db, err := microfts.Open(path, microfts.Options{})
db.Close()

// Content
db.AddFile(filepath, strategyName)
db.RemoveFile(filepath)
db.Reindex(filepath, strategyName)

// Search
results, err := db.Search("query")  // []SearchResult{Path, StartLine, EndLine}

// Index
db.BuildIndex(cutoffPercentile)

// Staleness
status, err := db.CheckFile(filepath)    // FileStatus{Path, Status, FileID, Strategy}
statuses, err := db.StaleFiles()         // all indexed files
refreshed, err := db.RefreshStale("")    // reindex stale files

// Strategies
db.AddStrategy(name, command)
db.RemoveStrategy(name)
```

## Chunking Strategies

A chunking strategy is any command that takes a filename argument and prints byte offsets to stdout, one per line. Three built-in chunkers are included as subcommands.

External chunker example using `awk`:

```sh
microfts strategy add -db myindex -name awk-lines \
  -cmd "awk 'BEGIN{pos=0} {pos+=length(\$0)+1; print pos}'"
```

## License

MIT
