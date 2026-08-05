# microfts2 — Spec Index

A dynamic trigram index, written in Go. CLI command, structured so it can also
be used as a library.

Root index for `specs/`. Entries are **pointers, not copies** — the named spec
stays canonical, this file only says where to look. Read this first to orient,
then open only the specs it points you to.

## Systems

### Foundations
- [`overview.md`](overview.md) — what microfts2 is.
- [`trigrams.md`](trigrams.md) — trigram representation: raw byte trigrams,
  whitespace and word boundaries, case folding, byte aliases, UTF-8 validation,
  character-internal trigram skipping, 24-bit trigram codes.
- [`storage.md`](storage.md) — record layout. Single bbolt bucket with
  prefix-distinguished records, data-in-key pattern, key chains, chunk
  deduplication, the C/F/I/N/T/W/H record types and their encodings, the full
  trigram index, and Attrs on retrieval.

### Chunking
- [`chunking.md`](chunking.md) — the `Chunker` interface contract, the `Chunk`
  and `Pair` types, strategy registration, built-in text chunkers
  (`chunk-lines`, `-overlap` variants, `chunk-markdown`), the `FileChunker` and
  `RandomAccessChunker` interfaces, dispatch rules, and the shared chunker
  helpers.
- [`chunk-bracket.md`](chunk-bracket.md) — bracket chunker: `BracketLang`
  lexical rules, bracket modes, language configuration.
- [`chunk-indent.md`](chunk-indent.md) — indent chunker: indentation-scoped
  chunking, reusing `BracketLang` for comment and string rules.

### Indexing
- [`indexing.md`](indexing.md) — adding, removing, and reindexing files;
  duplicate guard; staleness detection; the chunk-processor, remove, and
  reindex callbacks.
- [`append.md`](append.md) — `AppendChunks`, the `AppendAwareChunker`
  interface, chunker offset support, and the `ErrAppendBoundary` guard.

### Search and retrieval
- [`search.md`](search.md) — literal and regex search, scoring strategies
  (coverage, density, overlap, BM25), dynamic trigram filtering, multi-regex
  post-filters, chunk filters, proximity reranking, multi-strategy search, and
  loose (OR-semantics) search.
- [`fuzzy-trigram.md`](fuzzy-trigram.md) — typo-tolerant search by trigram
  OR-union, and search escalation.
- [`retrieval.md`](retrieval.md) — chunk context retrieval: `GetChunks` with
  before/after windows.
- [`chunk-cache.md`](chunk-cache.md) — per-query cache for file content and
  chunked data.

### Documents and hosting
- [`overlay.md`](overlay.md) — `tmp://` in-memory documents: lifecycle, search
  integration, fileid filtering, corpus counters, and the indexed-chunk
  callback on overlay paths.
- [`api.md`](api.md) — the public Go surface: lifecycle, options, ark
  integration and the shared-`*bbolt.DB` handle, `ChunkerMetadata`, record
  counts, fileid↔path mapping, search cache, partial F-record unmarshal, copy
  and cache invalidation.
- [`cli.md`](cli.md) — CLI subcommands and flags.

## Summary specs

None yet. Note that CLI flags introduced by a feature are documented in that
feature's own spec, so `cli.md` is not a complete inventory — a CLI summary
spec is the obvious candidate if that becomes a question people keep asking.
If one is added, register it here and pin it in a persistent list: normal
per-feature anchoring does not maintain summary specs.

## Themes

Cross-cutting invariants, stated once so contradictions between specs become
visible.

### The index is a lens, not a vault
Files stay where they are, owned by the user. Chunk *text* is never stored in
the index — retrieval re-chunks the original file on disk using the stored
strategy to recover a chunk's content. A C record carries hash, content length,
trigrams, tokens, attrs, and fileids; not the bytes. Touches `storage.md`,
`retrieval.md`, `chunk-cache.md`.

### Chunk identity is content identity
A chunk's identity is a SHA-256 over its content, so identical content in
different files dedups to one chunkid. Reindex is therefore a content diff:
unchanged content keeps its chunkid, so chunkid-keyed external state (an
embedding keyed by chunkid, say) survives an edit. The overlay uses the same
mechanism. Anything that would change a chunkid for unchanged content breaks a
downstream consumer. Touches `storage.md`, `indexing.md`, `overlay.md`.

### The index is full-text, verbatim
Trigram and token indexes are built from chunk content exactly as given —
microfts2 strips and rewrites nothing. A caller wanting spans excluded from its
own *embeddings* does that on its own side. The retired `ContentTransform` hook
is the standing counterexample: don't strip the full-text index for an
embedding-axis concern. Touches `storage.md`, `chunking.md`.

## Migrations

Completed migrations live in `specs/migrations/complete/`, numbered in landing
order. None in flight.

- [`complete/001-lmdb-to-bbolt.md`](migrations/complete/001-lmdb-to-bbolt.md) —
  LMDB → bbolt store port (v0.4.0).
