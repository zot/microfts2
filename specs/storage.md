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

bbolt allows up to 32768 bytes (32 KB) per key. Long filenames (F records below) use multiple keys, chained at a legacy 511-byte threshold — retained from the original design and still correct.

## Single Bucket with Chunk Deduplication

All records live in one bbolt bucket, distinguished by prefix byte. Chunks are deduplicated by content hash — the same chunk content appearing in multiple files is stored once.

Bucket name is a parameter: default 'fts', settable via CLI and library API.
Not stored in the I record — needed to open the database in the first place.

### Why one tree

One B-tree instead of two halves the bbolt page overhead and simplifies transactions (no cross-bucket coordination).

### Why chunk deduplication

Overlapping chunking strategies produce shared content across adjacent windows. Files with common boilerplate share chunks. Deduplication means shared content is indexed once — fewer C records, fewer T record entries, smaller mmap.

### Encoding conventions

- Integer fields in values use varint encoding (Go binary.PutUvarint / binary.ReadUvarint)
- Ids in keys (the chunkid in C and D keys, the fileid in F keys) use the order-preserving varint, `AppendKeyID` / `KeyID` (SQLite4's varint, format `"7"`): byte order is numeric order, so a new id appends at the tree's right edge instead of splitting a page in the middle. The first byte gives the length: 0-240 in one byte; 241-248 begin two bytes (240 to 2287); 249 begins three (2288 to 67823); 250-255 are followed by 3 to 8 big-endian bytes. Only the shortest encoding is valid
- Every write sets the bucket's `FillPercent` to 0.9: a page split by right-edge appends keeps that fill, where bbolt's default 0.5 would leave it half empty
- Trigram fields are fixed 3 bytes (24-bit)
- Hash fields are fixed 32 bytes (SHA-256)
- Strings are length-prefixed (varint length + bytes), except the final field in a key can use remaining bytes (computed from record length)

### Record types

| Prefix | Key                    | Value                                              | Purpose                                                 |
|--------|------------------------|----------------------------------------------------|---------------------------------------------------------|
| I      | name: str              | empty (value encoded in key)                       | Config settings, data-in-key pattern                    |
| H      | hash: 32               | chunkid: varint                                    | Content hash → chunkid lookup                           |
| C      | chunkid: ordered varint | hash + contentLen + token counts + attrs + fileids | Per-chunk: identity, length, metadata, files            |
| F      | fileid: ordered varint  | metadata + name + chunks                           | Per-file: staleness info, ordered chunk list            |
| N      | chain-byte + name: str | (varies by chain-byte)                             | Filename → fileid mapping via key chains                |
| T      | trigram: 3             | postings: chunkid delta + count...                 | Trigram inverted index, with each chunk's count         |
| D      | chunkid: ordered varint | empty                                              | Dead set: chunks removed but still in some posting list |

### Record details

- `I` [name: str] = [value: str] -> empty
  Config record using data-in-key pattern. Each setting is independently readable and writable — no JSON parse/serialize cycle. The `version` setting holds the DB format version, currently `"7"` (ids in C, F and D keys are order-preserving varints; `"6"` put the counts in the postings and took the lists out of C records; `"5"` dropped the F record's file-level token bag; `"4"` made its name list one name; `"3"` added `chunkerVersion`); `Open` refuses any other value.

- `H` [hash: 32] -> [chunkid: varint]
  Content hash to chunkid lookup. Used during AddFile to detect duplicate chunks.

- `C` [chunkid: ordered varint] -> [hash: 32] [contentLen: varint] [distinctTokens: varint] [tokenTotal: varint] [n-attrs: varint] [[key: bytes] [value: bytes]]... [n-fileids: varint] [[fileid: varint] [count: varint]]...
  Per-chunk record: content hash, byte length of the chunk content, the number of distinct tokens in the chunk (the chunk length scoring normalizes by) and their summed count (what the `totalTokens` corpus counter adds and subtracts), optional attributes (opaque key-value pairs from chunker Attrs), and the list of files containing this chunk with per-file occurrence counts. It carries no trigram or token list: a chunk's per-trigram counts live in the T postings (format `"6"`), which is all any scorer reads. The `[fileid, count]` count is the number of times this chunk occurs in that file. Add-occurrence increments the count (or inserts with count=1 if absent); drop-occurrence decrements (count=0 removes the fileid entry; empty fileids list cascades the orphan cleanup). Date filtering reads the `timestamp` attr directly from C during candidate evaluation — zero extra reads. Content length enables corpus-wide chunk size statistics without re-reading files from disk. `ChunkContentLens(fileid)` returns the content lengths for all of a file's chunks in chunk-list order, by reading the F record's chunk list and each C record's contentLen field.

- `F` [fileid: ordered varint] -> [modTime: 8] [contentHash: 32] [fileLength: varint] [strategy: str] [chunkerVersion: varint] [name: str] [chunkcount: varint] [[chunkid: varint] [location: bytes] [locator: bytes]]...
  Per-file record. Stores file metadata (mod time as Unix nanos, SHA-256 content hash, file length, chunking strategy name, and the version of that strategy's chunker when the file was chunked — see the VersionedChunker section of `chunking.md`), and the file's one name. A copy of a file is a record of its own; deduplication is per chunk, the C record listing every fileid that holds the chunk. Ordered chunk list with two per-occurrence opaque labels: `location` (the chunker's Range — used for CLI/human display) and `locator` (the chunker's Locator — used for fast random-access retrieval and append-merge resume). Both are length-prefixed byte strings so they can be empty. The record ends after the chunk list: it once carried a file-level token bag, written and kept current but never read, and dropped in format `"5"` (it was 94.7% of every F record's bytes in ark's index).

- `N` [0-254] [name: str] -> empty — filename prefix chain key
- `N` [255] [name: str] -> [[full-name: str] [fileid: varint]]... — final chain key; value has full filename + fileid
  The key's name is the filename key (see filename-case.md): the path folded to upper case when the database was created with `CaseInsensitiveFilenames`, otherwise the path itself. The value's full name is always the path as given.

- `T` [trigram: 3] -> [[(delta << 1) | hasCount: uvarint] [count: uvarint, only when hasCount]]...
  Trigram inverted index. Value is the trigram's postings in ascending chunkid order: each chunkid delta-coded against the previous one (the first against 0), its low bit saying whether a count follows; a posting with no count has count 1 (most do). The count is how many times the trigram occurs in that chunk. A trigram's document frequency is its number of live postings: postings whose chunkid is in the dead set are skipped by every reader. One entry per distinct trigram rather than one per trigram-chunk pair — the primary source of mmap reduction.

- `D` [chunkid: ordered varint] -> empty
  The dead set. When a chunk's last occurrence goes, its C and H records are deleted and its D record written; no posting is edited. Its chunkid stays in the T postings until a write rewrites each list, dropping dead postings as it goes, or until `Purge` rewrites them all and clears the D records it started with. Chunkids come from a counter and are never reused, so a dead posting can only name a chunk that no longer exists.

There are no W records (token postings) since format `"6"`: they were written on every add and removal and never read.

### Data at three levels

| Level  | Source                                         | Use                                   |
|--------|------------------------------------------------|---------------------------------------|
| Chunk  | T postings: the chunk's count of each trigram  | Per-chunk TF, density and BM25 scoring |
| Chunk  | C record: contentLen, distinctTokens           | Chunk size statistics without disk I/O, scoring length |
| Chunk  | C record: attrs (e.g. timestamp, role)         | Date filtering, metadata-aware search |
| Corpus | T record: live postings = trigram DF           | Trigram IDF, BM25                     |

### Estimated entry counts (ark scale: 74K chunks, 2K files, 500K distinct trigrams)

| Record type | Estimated entries |
|-------------|-------------------|
| T (trigram → postings) | ~500K |
| C (per-chunk data) | ≤74K (fewer with dedup) |
| H (hash → chunkid) | ≤74K |
| D (dead chunkids) | until the next purge |
| F (per-file data) | ~2K |
| N (name lookup) | ~2K |
| I (config) | ~10 |
| **Total** | **~630K** |

bbolt mmap pressure scales with B-tree entry count, not data volume. Packing per-trigram data into T record values (one entry per distinct trigram) and per-chunk data into C record values (one entry per unique chunk) keeps the entry count low while the data volume stays comparable.

# Full Trigram Index

The T records contain entries for ALL trigrams present in the content. This makes the index complete and usable for both literal and regex search.

Trigram selection for queries is handled dynamically via `TrigramFilter` functions supplied at search time. This allows callers to adapt filtering strategy per query rather than relying on a static global cutoff.

The index is maintained incrementally on add/remove: additions append postings, removals add to the dead set, and dead postings leave a list when it is next rewritten or purged. If the database is lost, files must be re-added from disk.

# Chunk dedup and Attrs on retrieval

A chunk's dedup identity is a SHA-256 hash over its `Content`. Chunks with identical content dedup to a single chunkid (shared across files via the H record); chunks with differing content receive distinct chunkids. The trigram index is built from the chunk's content verbatim — microfts2's index is **full-text**, so whatever a chunk literally contains is searchable. microfts2 indexes content as given; it does not strip or rewrite it. (A caller that wants certain spans excluded from its own *embeddings* — e.g. ark's inline `@tag: value` lines, which skew embedding vectors but not full-text search — does that stripping on its own side; the trigram index keeps the text intact.)

A chunk may carry `Attrs` — opaque per-chunk key/value metadata a chunker emits (e.g. a PDF chunker's page rectangles, a chat-jsonl chunker's role/timestamp). `Attrs` are stored in the C record at index time. On retrieval, microfts2 re-reads `Content` from the file (so retrieval reflects the file's current bytes) but reads `Attrs` from the stored C record — on every path: `GetChunks` (streaming and `RandomAccessChunker` fast path), `ChunkCache`, and `tmp://`. So native chunker `Attrs` are surfaced consistently, never dropped on the streaming or `tmp://` paths.
