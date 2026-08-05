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

- Integer fields use varint encoding (Go binary.PutUvarint / binary.ReadUvarint)
- Trigram fields are fixed 3 bytes (24-bit)
- Hash fields are fixed 32 bytes (SHA-256)
- Strings are length-prefixed (varint length + bytes), except the final field in a key can use remaining bytes (computed from record length)

### Record types

| Prefix | Key                    | Value                                 | Purpose                                                          |
|--------|------------------------|---------------------------------------|------------------------------------------------------------------|
| I      | name: str              | empty (value encoded in key)          | Config settings, data-in-key pattern                             |
| H      | hash: 32               | chunkid: varint                       | Content hash → chunkid lookup                                    |
| C      | chunkid: varint        | hash + contentLen + trigrams + tokens + fileids | Per-chunk: all analysis data                           |
| F      | fileid: varint         | metadata + names + chunks + token bag | Per-file: staleness info, ordered chunk list, file-level scoring |
| N      | chain-byte + name: str | (varies by chain-byte)                | Filename → fileid mapping via key chains                         |
| T      | trigram: 3             | chunkid: varint...                    | Trigram inverted index                                           |
| W      | token-hash: 4          | chunkid: varint...                    | Token inverted index for IDF                                     |

### Record details

- `I` [name: str] = [value: str] -> empty
  Config record using data-in-key pattern. Each setting is independently readable and writable — no JSON parse/serialize cycle.

- `H` [hash: 32] -> [chunkid: varint]
  Content hash to chunkid lookup. Used during AddFile to detect duplicate chunks.

- `C` [chunkid: varint] -> [hash: 32] [contentLen: varint] [n-trigrams: varint] [[trigram: 3] [count: varint]]... [n-tokens: varint] [[count: varint] [token: str]]... [n-attrs: varint] [[key: bytes] [value: bytes]]... [n-fileids: varint] [[fileid: varint] [count: varint]]...
  Per-chunk record. Contains everything known about the chunk: content hash, byte length of the chunk content, packed trigram+count pairs, packed token+count pairs, optional attributes (opaque key-value pairs from chunker Attrs), and the list of files containing this chunk with per-file occurrence counts. Self-describing — all data needed for search, scoring, filtering, and removal. The `[fileid, count]` pair shape parallels the trigram/token shape: count is the number of times this chunk occurs in that file. Add-occurrence increments the count (or inserts with count=1 if absent); drop-occurrence decrements (count=0 removes the fileid entry; empty fileids list cascades the orphan cleanup). Date filtering reads the `timestamp` attr directly from C during candidate evaluation — zero extra reads. Content length enables corpus-wide chunk size statistics without re-reading files from disk. `ChunkContentLens(fileid)` returns the content lengths for all of a file's chunks in chunk-list order, by reading the F record's chunk list and each C record's contentLen field.

- `F` [fileid: varint] -> [modTime: 8] [contentHash: 32] [fileLength: varint] [strategy: str] [filecount: varint] [name: str]... [chunkcount: varint] [[chunkid: varint] [location: bytes] [locator: bytes]]... [tokencount: varint] [[token: str] [count: varint]]
  Per-file record. Stores file metadata (mod time as Unix nanos, SHA-256 content hash, file length, chunking strategy name). Multiple names handle duplicate/copied files mapping to the same fileid. Ordered chunk list with two per-occurrence opaque labels: `location` (the chunker's Range — used for CLI/human display) and `locator` (the chunker's Locator — used for fast random-access retrieval and append-merge resume). Both are length-prefixed byte strings so they can be empty. Aggregated token bag (union of all chunk tokens with summed counts) for file-level scoring without reading every chunk's C record.

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
| Chunk  | C record: contentLen                           | Chunk size statistics without disk I/O |
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

bbolt mmap pressure scales with B-tree entry count, not data volume. Packing per-trigram data into T record values (one entry per distinct trigram) and per-chunk data into C record values (one entry per unique chunk) keeps the entry count low while the data volume stays comparable.

# Full Trigram Index

The T records contain entries for ALL trigrams present in the content. This makes the index complete and usable for both literal and regex search.

Trigram selection for queries is handled dynamically via `TrigramFilter` functions supplied at search time. This allows callers to adapt filtering strategy per query rather than relying on a static global cutoff.

The index is maintained incrementally on add/remove. If the database is lost, files must be re-added from disk.

# Chunk dedup and Attrs on retrieval

A chunk's dedup identity is a SHA-256 hash over its `Content`. Chunks with identical content dedup to a single chunkid (shared across files via the H record); chunks with differing content receive distinct chunkids. The trigram and token indexes are built from the chunk's content verbatim — microfts2's index is **full-text**, so whatever a chunk literally contains is searchable. microfts2 indexes content as given; it does not strip or rewrite it. (A caller that wants certain spans excluded from its own *embeddings* — e.g. ark's inline `@tag: value` lines, which skew embedding vectors but not full-text search — does that stripping on its own side; the trigram index keeps the text intact.)

A chunk may carry `Attrs` — opaque per-chunk key/value metadata a chunker emits (e.g. a PDF chunker's page rectangles, a chat-jsonl chunker's role/timestamp). `Attrs` are stored in the C record at index time. On retrieval, microfts2 re-reads `Content` from the file (so retrieval reflects the file's current bytes) but reads `Attrs` from the stored C record — on every path: `GetChunks` (streaming and `RandomAccessChunker` fast path), `ChunkCache`, and `tmp://`. So native chunker `Attrs` are surfaced consistently, never dropped on the streaming or `tmp://` paths.
