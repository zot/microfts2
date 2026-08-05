# Chunker Interface

## configurable chunking strategies

- add/remove chunking strategies dynamically (external commands or Go functions)
  - external: `AddStrategy(name, cmd)` — command is persisted in I record
  - function: `AddChunker(name, c)` — in-memory only, must re-register on Open
  - `Chunker` interface with two methods:
    - `Chunks(path string, content []byte, yield func(Chunk) bool) error` — producer: yields chunks for indexing
    - `ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool)` — retriever: extracts a single chunk's content by its range label
  - `Chunk` struct: `{ Range []byte, Locator []byte, Content []byte, Attrs []Pair }`
    - Range, Locator, and Content are reusable buffers — the caller must copy before the next yield
    - Range has string semantics: opaque to microfts2, meaningful to the chunker and the user. Used for CLI/human display.
    - Locator is opaque bytes, chunker-defined, used by the chunker for fast random-access retrieval and append-merge. Per-occurrence (lives in F record), not per-content. nil if the chunker doesn't need it.
    - Content is the text to be trigram-indexed for this chunk
    - Attrs is optional per-chunk metadata (e.g. timestamp, role). Opaque to microfts2 — stored in C records and exposed to ChunkFilters. Per-content (lives on the C record). nil means no attrs.
  - `Pair` type: `{ Key []byte, Value []byte }` — opaque key-value pair. Allows duplicate keys. Mirrors the DB wire format.
  - `ChunkFunc` type preserved for convenience: `func(path string, content []byte, yield func(Chunk) bool) error`
  - `FuncChunker` adapter wraps a bare `ChunkFunc` into a `Chunker`:
    - `Chunks` delegates to the wrapped function
    - `ChunkText` re-runs the wrapped function and returns the first chunk whose Range matches the label
  - `AddStrategyFunc(name, fn)` convenience: wraps fn in FuncChunker, calls AddChunker
  - when AddFile/Reindex uses a func strategy, calls the Chunker directly (no exec)
  - I record stores name with empty cmd for func strategies (marks as registered)
  - built-in chunkers (chunk-lines, chunk-lines-overlap, chunk-words-overlap) register as func strategies
- chunkers serve two purposes via the Chunker interface:
  - indexing (Chunks method): produce chunks with content to trigram-index, a range label, and optional attrs
  - extraction (ChunkText method): given the same file, retrieve a specific chunk's content by its range label — may be optimized (e.g. a markdown chunker can jump to the right heading without full scan)
  - the range is an opaque string label: for text chunkers it's "startline-endline", for other formats it's whatever the chunker needs (e.g. "sheet1:A1-B20", "slides/3:para/7")
  - chunkers must be deterministic: same file produces same chunks with same ranges
- files track their indexed chunking strategy
  - can reindex with a different strategy -- allows migration to better strategies

# Built-in Chunking Strategies

The binary includes built-in chunkers registered as func strategies. They can also be invoked as CLI subcommands (`microfts chunk-* <file>`) outputting `range\tcontent` lines to stdout.

For all built-in text chunkers, the range is `startline-endline` (1-based, inclusive) and the content is the raw text of those lines. This means CLI search output like `filepath:3-17` is the same format as before.

## chunk-lines

Break file at line boundaries.

`microfts chunk-lines <file>`

Every line is a chunk. Range: `N-N` (single line number). Content: the line text.

Exported as `microfts2.LineChunkFunc` for direct use as a `ChunkFunc`. `microfts2.LineChunker{}` is a value-type wrapper that implements the full text-chunker quartet: `Chunker`, `FileChunker` (standard read-and-delegate), `RandomAccessChunker` (line-range fast path), and `AppendAwareChunker` (re-chunks from the previous last chunk's byte start through end-of-file, so a trailing partial line that gets completed by an append correctly merges into one chunk instead of stranding a fragment).

## chunk-lines-overlap

Fixed-size line windows with overlap.

`microfts chunk-lines-overlap [-lines N] [-overlap M] <file>`

- `-lines`: lines per chunk (default 50)
- `-overlap`: lines of overlap between consecutive chunks (default 10)

Each chunk starts `lines - overlap` lines after the previous chunk's start. Range: `startline-endline`. Content: the text of those lines.

## chunk-words-overlap

Fixed-size word windows with overlap. Good for vector databases and hybrid search.

`microfts chunk-words-overlap [-words N] [-overlap M] [-pattern P] <file>`

- `-words`: words per chunk (default 200)
- `-overlap`: words of overlap between consecutive chunks (default 50)
- `-pattern`: regexp defining a "word" (default `\S+`)

Each chunk starts `words - overlap` words after the previous chunk's start. Range: `startline-endline` (lines spanning the word window). Content: the text of those lines.

## chunk-markdown

Paragraph-based splitting for markdown files.

`microfts chunk-markdown <file>`

Splits on blank lines and heading transitions:
- A heading line (`#`...) always starts a new chunk
- A heading and its following paragraph (up to the next blank line or heading) form one chunk
- Consecutive blank lines collapse to a single boundary
- Non-heading text between boundaries is one chunk
- Blank lines are boundaries only — they are not included in any chunk's content
- Gaps between chunks are expected; each chunk's range notes its precise position in the file
- Fenced code blocks (opening `` ``` `` or `~~~`, with optional info string) suppress blank-line splitting — all lines from the opening fence through the matching closing fence are part of the current chunk, not a new one
- A fence opening does not start a new chunk — it continues the current paragraph/chunk
- Blank lines inside a fenced code block are not boundaries
- Fence matching: a closing fence is a line starting with the same character (`` ` `` or `~`) repeated at least as many times as the opening fence, with no other non-whitespace content

Headline merging: a heading absorbs following tag-only chunks and one content chunk into a single merged chunk.
- A tag line is any line whose first character is `@`
- After a heading chunk is emitted internally (heading + any non-blank, non-tag continuation lines up to a blank line or heading), the chunker looks ahead
- If the next chunk consists entirely of tag lines (every line starts with `@`), it is merged into the heading chunk. This repeats for consecutive tag-only chunks.
- After all tag-only chunks (if any), the next chunk is also merged if it is a non-heading chunk (paragraph, fenced code block, table, etc.)
- Blank lines between the heading, tag chunks, and the absorbed content chunk become internal to the merged chunk — they are included in the merged chunk's content
- The merged chunk's range spans from the heading's start line to the last line of the final absorbed chunk
- If no tag-only or content chunks follow the heading (e.g. two headings in a row, or heading at end of file), the heading chunk is emitted as-is

Range: `startline-endline` (1-based, inclusive). Content: the raw text of those lines. For merged heading chunks, internal blank lines are included. For non-merged chunks, boundary blank lines are excluded.

Exported as `microfts2.MarkdownChunkFunc` for direct use as a `ChunkFunc` (wraps into a Chunker via FuncChunker when registered). `microfts2.MarkdownChunker{}` is a value-type wrapper that implements the full text-chunker quartet: `Chunker`, `FileChunker` (standard read-and-delegate), `RandomAccessChunker` (line-range fast path), and `AppendAwareChunker` (re-chunks from the previous last chunk's byte start through end-of-file).

# FileChunker Interface

Binary formats (PDF, images, archives) cannot be meaningfully pre-read as `[]byte` and passed to a chunker. The chunker needs to open the file itself using format-specific libraries. `FileChunker` is a separate interface for this access pattern.

## FileChunker interface

An optional interface for chunkers that read files directly:

```go
type FileChunker interface {
    FileChunks(path string, old [32]byte, yield func(Chunk) bool) ([32]byte, error)
}
```

The method is named `FileChunks` (not `Chunks`) so that a single Go type can implement both `Chunker` and `FileChunker` — the two interfaces have different signatures and would otherwise collide on the method name.

- The chunker opens and reads the file from `path` using whatever library it needs
- It computes the SHA-256 hash of the file content
- If `old` is non-zero and matches the computed hash, the chunker may skip chunking entirely (yield is never called) and return the hash — this avoids redundant computation when the file hasn't changed
- Returns the content hash and any error
- Returns zero hash `[32]byte{}` to signal "no content" (file missing, unreadable, or empty)
- Chunk content yielded via the callback must be valid UTF-8, same as `Chunker` — the raw file may be binary but chunk text is always UTF-8

A `FileChunker` does NOT need to implement `Chunker`. It is a separate interface for a separate access pattern. A chunker may implement both if it handles both content-based and file-based paths (e.g., a PDF chunker that can also accept pre-read bytes from the overlay).

## Dispatch rules

microfts2 checks which interfaces a registered chunker implements and dispatches accordingly:

### Index-time (collectChunks, reindexCore)

1. If `FileChunker`: call `FileChunker.FileChunks(path, oldHash, yield)`. Skip `os.ReadFile`. The hash comes back from the chunker. If hash matches old, chunking was skipped — no work needed.
2. If `Chunker`: existing path — `os.ReadFile`, pass content to `Chunker.Chunks`, compute hash separately.

### Retrieval-time (getChunks, ChunkCache)

1. If `FileChunker`: call `FileChunker.FileChunks(path, [32]byte{}, yield)`. Zero old hash means "always chunk, don't skip." No `os.ReadFile` by microfts2.
2. If `Chunker`: existing path — `os.ReadFile`, pass content.

### Content-in-hand (overlay: AddTmpFile, UpdateTmpFile, AppendTmpFile)

1. Always call `Chunker.Chunks(path, content, yield)` — content is provided, not on disk.
2. If the chunker only implements `FileChunker` and not `Chunker`, this is an error — file-only chunkers cannot be used with the tmp:// overlay.

## Registration

`AddChunker(name, c)` accepts any value. At registration time, microfts2 checks which interfaces the value satisfies (`Chunker`, `FileChunker`, `RandomAccessChunker`) and stores the appropriate capabilities. A chunker must implement at least one of `Chunker` or `FileChunker`.

## UTF-8 validation

The UTF-8 validation in `collectChunks` (checking each yielded chunk's Content) applies regardless of which interface produced the chunks. Binary-format chunkers are responsible for extracting text — microfts2 verifies the result is valid UTF-8.

# Random-Access Chunk Retrieval

The default retrieval path (`ChunkCache.ChunkText`, `DB.GetChunks`) is a linear scan: the chunker runs from the start of the file, yielding each chunk in order, until the target range is found. For small files this is fine. For a 600-page PDF whose target chunk is on the last page, walking from the top is wasteful.

`RandomAccessChunker` is an optional chunker interface that skips the scan — given a range label, the chunker extracts that chunk directly, using a caller-provided scratch pointer for per-file state (e.g. a line-offset table or page index).

## RandomAccessChunker interface

```go
type RandomAccessChunker interface {
    GetChunk(path string, data []byte, customData *any, chunk *Chunk) error
}
```

- `path`: file path being retrieved from
- `data`: pre-read content (nil when the wrapping chunker is `FileChunker`-only — the random-access chunker must read the file itself)
- `customData`: pointer to a per-file scratch slot. First call: `*customData == nil`. The chunker lazily populates it (e.g. build a line-offset table) and reuses across subsequent calls. The slot's lifetime is tied to the enclosing `cachedFile` (or the single `DB.GetChunks` invocation) — it garbage collects when the owner goes away.
- `chunk`: pointer to a Chunk with `Range` pre-filled (from the F record's Location) and `Attrs` pre-filled (from the C record's stored attributes). The chunker must fill `Content`. It may replace `Attrs` with its own derivation if it wants; otherwise the stored Attrs remain authoritative. The chunker may also *consume* stored Attrs as retrieval hints — e.g. a stored "page-offset" Attr lets a PDF chunker jump directly.

## Dispatch

`ChunkCache.ChunkTextWithId`, `ChunkCache.GetChunks`, and `DB.GetChunks` check whether the resolved chunker is a `RandomAccessChunker`. If yes, they take the fast path. If no, they fall back to the existing streaming path (`Chunker.Chunks` / `FileChunker.FileChunks` yielding from the start).

## Built-in chunkers

All four built-in chunkers (`LineChunk`, `MarkdownChunkFunc`, `BracketChunker`, `IndentChunker`) produce line-range labels as output. Their fast path is identical: `customData` stores a `[]int` line-offset table, incrementally extended as needed. Given `"startLine-endLine"`, the chunker slices `data[offsets[startLine-1]:offsets[endLine]]`. No depth or indent state is needed because the stored range label already identifies the byte region.

## Removal of ChunkTexter

The earlier `ChunkTexter` interface — content-only retrieval via `ChunkText(path, content, rangeLabel) ([]byte, bool)` — is removed. It was defined but never dispatched to by any production code path. `RandomAccessChunker` supersedes it entirely: the new interface returns a full Chunk (not just content), supports per-file scratch state, and uses stored Attrs as authoritative metadata.
