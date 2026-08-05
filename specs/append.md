# Append Detection Support

For append-only files (e.g. JSONL conversation logs), ark wants to detect that a file change was an append and skip full reindex. microfts2 provides the primitives; ark implements the detection logic.

## FileLength in F record

The F record stores `fileLength` (int64): the file size in bytes at index time. `AddFile` and `Reindex` set this from the file content they already read. Ark reads this to hash only the prefix up to the stored length, detecting whether a change was purely an append.

## AppendChunks API

Add chunks to an existing file without full reindex.

```go
func (db *DB) AppendChunks(fileid uint64, content []byte, strategy string) error
```

Chunks `content` using the named strategy, adds the resulting chunks and trigrams to the existing file's records. The `content` parameter is only the new bytes (the appended portion), not the full file.

Updates the F record: new ContentHash, ModTime, FileLength, appended chunk entries, merged token bag. Does NOT touch existing chunks or trigrams — they remain valid.

For each new chunk: hash content, check H record for dedup. New chunks get C records, T/W record updates. Existing chunks (dedup hit) just add fileid to C record. F record gets new (chunkid, location) entries appended.

## Chunker offset support

When `AppendChunks` passes content to a `ChunkFunc`, the content starts at an arbitrary byte offset in the original file, not byte 0. For line-based chunkers, this means line numbering must account for lines already processed.

`AppendChunks` passes a base line number to line-based chunkers so that Range labels (e.g. "51-60") are absolute, not relative to the appended slice. The mechanism: `ChunkFunc` signature is unchanged; `AppendChunks` counts newlines in a prefix window or accepts a base line count from the caller, then adjusts the Range values after chunking.

Suggestion: `AppendChunks` accepts an optional base line number. When zero, ranges are used as-is (for non-line-based chunkers). When non-zero, line-based ranges are offset by that amount.

## AppendAwareChunker (boundary-aware appends)

The default `AppendChunks` flow chunks the appended bytes alone and concatenates the resulting chunks onto the F record's chunk list. That's correct for chunkers whose boundaries don't depend on what came before — e.g. `chunk-lines-overlap` is purely positional. It is *wrong* for chunkers whose final chunk may need to be reconsidered when new content arrives:

- `chunk-lines`: an existing tail like `"hello"` (no trailing newline) should merge with appended `" world\n"` into one chunk.
- markdown: an existing open paragraph should continue when the appended content is more non-blank lines, not start a fresh chunk.
- bracket / indent: the depth/indent level at end of file determines what new content does — sibling, child, or close.

For tree-structured chunkers, the final chunk at end-of-file is always either a leaf or an inner node that has absorbed a leaf. Both shapes are local. The chunker doesn't need arbitrary parser state — it needs to know which shape the last chunk is and its structural position (heading level, indent depth, etc). That information fits in the per-occurrence `Locator` byte string the chunker already writes.

Optional interface:

```go
type AppendAwareChunker interface {
    // lastLocator: locator of the last existing chunk (nil if file has no chunks yet).
    // newBytes: content being appended.
    // Yields chunks that replace the trailing region (zero or more existing
    // chunks dropped, one or more new chunks emitted).
    // replacedLast=true means the last existing chunk is being replaced — the
    // append flow drops its F-record entry and decrements the chunkid's fileid
    // count in its C record (cascading orphan cleanup as in RemoveFile).
    AppendChunks(
        path        string,
        lastLocator []byte,
        newBytes    []byte,
        yield       func(Chunk) bool,
    ) (replacedLast bool, err error)
}
```

Chunkers that don't implement `AppendAwareChunker` get the default behavior: `AppendChunks` chunks `newBytes` alone and appends without any boundary fixup. Built-in tree-structured chunkers (line, markdown, bracket, indent) implement the interface to handle their boundary cases correctly.

The chunker uses `lastLocator` as its resume state. Because the locator is per-occurrence (stored in the F record, not in the dedup-shared C record), it correctly reflects this file's tail regardless of where else the chunk's content appears.

### Shared resume helper

Built-in text chunkers whose chunks already carry byte-range locators share one resume protocol — read the file, re-chunk from the previous last chunk's start byte through end-of-file, and compare the first emitted chunk's byte range to the previous tail to decide drop-or-replace. That protocol is consolidated into one internal helper, `appendByRechunkResume(path, lastLocator, newBytes, chunk, yield)`, that takes the chunker's content-based `Chunks` function as a parameter. `LineChunker`, `MarkdownChunker`, and `BracketChunker` all use it.

A parallel helper, `fileChunksByRead(path, old, chunk, yield)`, gives text chunkers their `FileChunker` implementation: read the file, compute the SHA-256, skip yielding when the supplied old hash matches (the staleness short-circuit), and otherwise delegate to the chunker's content-based `Chunks` function.

### Default-path silent-no-op guard

The default path is only safe when the chunker's chunk boundaries don't depend on what came before — e.g. fixed-byte or pure-positional chunkers. Any boundary-sensitive chunker — line-bounded, bracket-bounded, indent-bounded — can produce zero chunks from an appended slice whose bytes don't form complete boundaries on their own. Without a guard, that silently drops the appended bytes from the index and leaves the F record stale; the next reconcile feeds the same partial tail in and the cycle repeats indefinitely.

`AppendChunks` therefore returns `ErrAppendBoundary` when the chunker is not an `AppendAwareChunker` and produces zero chunks from non-empty `content`. The error wraps the chunker strategy name and the byte count of the dropped append so the caller can fall through to `Reindex` (or an equivalent full-refresh) instead of silently losing data.

Callers detect this with `errors.Is(err, ErrAppendBoundary)`. Empty `content` is still a successful no-op — the error only fires when there were bytes to chunk and the chunker had nothing to say about them.

### Drop-and-replace cleanup

When `replacedLast` is true, `AppendChunks`:
1. Drops the last entry from the F record's chunk list.
2. Reads the dropped chunk's C record, decrements this fileid's count (or removes the fileid entry if count reaches 0).
3. If the C record's fileids list is now empty, deletes the C record, removes the chunkid from each T record (by trigram) and W record (by token), and deletes the H record. Same cascade as `RemoveFile`'s per-chunk path — this logic is consolidated into a shared internal helper.

A chunker may, in principle, replace more than just the last chunk. The current spec scope is single-last-chunk replacement; a future extension could allow replacing the last K chunks if a use case appears.
