# microfts2 — Spec Index

Root index for `specs/`. Entries are **pointers, not copies** — the named
spec stays canonical, this file only says where to look. Read this first to
orient; open only the specs it points you to.

`specs/main.md` is a monolith (~1700 lines) covering most systems, so its
entries below name the section headings that own each area rather than
separate files.

## Systems

### Storage layout
Single bbolt bucket, prefix-distinguished records, chunk dedup by content hash.
- `main.md` — *Single Bucket with Chunk Deduplication*, *data-in-key pattern
  using lexical sort*, *Key Chains*, *Record structs*, *TxnHolder interface*,
  *Buckets*, *FileLength in F record*, *Partial F Record Unmarshal*

### Chunking
Strategy registration, the text-chunker interface quartet, and the built-in
chunkers.
- `main.md` — *configurable chunking strategies*, *chunk-lines*,
  *chunk-lines-overlap*, *chunk-words-overlap*, *chunk-markdown*,
  *FileChunker interface*, *RandomAccessChunker interface*,
  *AppendAwareChunker (boundary-aware appends)*, *Dispatch rules*,
  *Registration*, *Chunker offset support*, *ChunkerMetadata*,
  *UTF-8 validation*, *Removal of ChunkTexter*
- `main.md` — bracket chunker: *Token types*, *Chunk definition*,
  *Bracket modes*, *Language configuration*
- `main.md` — indent chunker: *Languages*, *Scope detection*

### Search and scoring
Trigram selection, candidate intersection, post-filters, scoring strategies.
- `main.md` — *Search types*, *Trigram filtering*, *Chunk filtering*,
  *Coverage (default)*, *Density*, *Overlap (OR semantics)*, *BM25*,
  *Proximity reranking*
- `fuzzy-trigram.md` — typo-tolerant search by trigram OR-union: *How it
  works*, *Why it catches typos*, *Search escalation*

### Overlay (`tmp://` documents)
In-memory document store that never touches the bbolt file.
- `main.md` — *URI Scheme* through *Corpus Counters* (fileid allocation,
  add/update/append/remove, search integration, thread safety)

### Indexing and callbacks
Adding and refreshing files, append semantics, per-chunk callbacks.
- `main.md` — *Append options*, *AppendChunks API*, *IndexOption type*,
  *Option constructors*, *Affected methods*, *Callback behavior*

### Library API and introspection
The public Go surface and the host-facing accessors.
- `main.md` — *Library API*, *DB accessor*, *Fileid surfacing*,
  *FileInfo lookup*, *Introspection*, *FileIDPaths*, *Copy*,
  *InvalidateCaches*, *Search Cache*

### Chunk cache
Per-query cache for file content and chunked data.
- `main.md` — *API*, *cachedFile shape*, *GetChunks behavior*,
  *Caching strategy*, *Lifecycle*

## Summary specs

None yet. If one is added (CLI inventory, record-format layout, public API
surface), register it here and pin it in a persistent list so it gets kept in
sync — normal per-feature anchoring does not maintain summary specs.

## Themes

Cross-cutting invariants, stated once so contradictions between sections
become visible.

### The index is a lens, not a vault
Files stay where they are, owned by the user. Chunk *text* is never stored in
the index — retrieval re-chunks the original file on disk using the stored
strategy to recover a chunk's content (`main.md`, *verify* post-filter and the
`-verify` CLI description). A C record carries hash, content length, trigrams,
tokens, attrs, and fileids; not the bytes.

### Chunk identity is content identity
A chunk's identity is a SHA-256 over its content, so identical content in
different files dedups to one chunkid (`main.md`, *Single Bucket with Chunk
Deduplication*). Reindex is therefore a content diff: unchanged content keeps
its chunkid so chunkid-keyed external state survives an edit (`main.md`,
reindex paragraph). The overlay uses the same mechanism. Anything that would
change a chunkid for unchanged content breaks a downstream consumer.

### The index is full-text, verbatim
Trigram and token indexes are built from chunk content exactly as given —
microfts2 strips and rewrites nothing (`main.md`, dedup identity paragraph). A
caller wanting spans excluded from its *embeddings* does that on its own side.
The retired `ContentTransform` hook is the standing counterexample: don't
strip the full-text index for an embedding-axis concern.

## Migrations

Completed migrations live in `specs/migrations/complete/`, numbered in landing
order. None in flight.

- `complete/001-lmdb-to-bbolt.md` — LMDB → bbolt store port (v0.4.0)
