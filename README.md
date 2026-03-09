# microfts2

A dynamic trigram index backed by LMDB, written in Go. Usable as a CLI tool or as a library.

microfts2 indexes files into raw byte trigrams (24-bit, 16M possible) organized by chunks, then maintains an inverted index for fast substring search. The index is maintained incrementally — every add/remove updates it immediately.

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
# Create a database
microfts init -db myindex

# Register a chunking strategy (any command that outputs range\tcontent lines)
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

Every byte is its own value — no character set mapping. Three consecutive bytes form a 24-bit trigram. UTF-8 multibyte characters produce cross-boundary byte trigrams; character-internal trigrams are skipped.

Search computes the query's trigrams, optionally filters them via a caller-supplied `TrigramFilter`, intersects posting lists from the inverted index, and returns matching file/chunk locations.

**Two-tree design:** Content and index live in separate LMDB subdatabases. The content DB stores trigram frequency counts (sparse C records), file metadata, and settings. The index DB stores the inverted trigram-to-chunk mapping, maintained incrementally on every add/remove.

**Dynamic trigram filtering:** Query trigram selection is handled at search time via `TrigramFilter` functions. Stock filters include `FilterAll` (use all trigrams), `FilterByRatio` (skip high-frequency trigrams), and `FilterBestN` (keep N most selective). Callers can supply custom filters.

**Staleness detection:** Each indexed file records its modification time and SHA-256 hash. Checking mod time first avoids hashing unchanged files. The `-r` flag refreshes stale files before any command.

## CLI Reference

All commands require `-db <path>`. Optional: `-content-db`, `-index-db` for custom subdatabase names.

| Command | Description |
|---------|-------------|
| `init` | Create a new database. `-case-insensitive`, `-aliases` |
| `add` | Add files. `-strategy <name>` |
| `search` | Search for text. `-regex`, `-score coverage\|density`, `-verify` |
| `delete` | Remove files from the database |
| `reindex` | Re-chunk files with a different strategy. `-strategy <name>` |
| `score` | Score named files against a query. `-score coverage\|density` |
| `stale` | List stale and missing files |
| `strategy add\|remove\|list` | Manage chunking strategies |
| `chunk-lines` | Built-in chunker: one chunk per line |
| `chunk-lines-overlap` | Built-in chunker: overlapping line windows. `-lines`, `-overlap` |
| `chunk-words-overlap` | Built-in chunker: overlapping word windows. `-words`, `-overlap`, `-pattern` |

**Global flag:** `-r` refreshes all stale files before running the command. Usable standalone (`microfts -r -db path`) or combined (`microfts search -r -db path query`).

## Library API

```go
import "microfts2"

// Lifecycle
db, err := microfts.Create(path, microfts.Options{})
db, err := microfts.Open(path, microfts.Options{})
db.Close()

// Content
fileid, err := db.AddFile(filepath, strategyName)
db.RemoveFile(filepath)
fileid, err := db.Reindex(filepath, strategyName)

// Search
results, err := db.Search("query", microfts.WithTrigramFilter(microfts.FilterAll))
results, err := db.SearchRegex("pattern")

// Scoring
chunks, err := db.ScoreFile("query", filepath, microfts.CoverageScore)

// Staleness
status, err := db.CheckFile(filepath)    // FileStatus{Path, Status, FileID, Strategy}
statuses, err := db.StaleFiles()         // all indexed files
refreshed, err := db.RefreshStale("")    // reindex stale files

// Strategies
db.AddStrategy(name, command)
db.AddStrategyFunc(name, fn)
db.RemoveStrategy(name)
```

## Chunking Strategies

A chunking strategy is either an external command or a Go function that takes a file and produces chunks. Each chunk has an opaque range label and text content to index.

External chunker example using `awk`:

```sh
microfts strategy add -db myindex -name awk-lines \
  -cmd "awk 'BEGIN{pos=0} {pos+=length(\$0)+1; print pos}'"
```

## License

MIT
