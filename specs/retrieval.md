# Chunk Context Retrieval

Retrieve a target chunk and its positional neighbors from an indexed file. This enables "flip pages" research loops: search → hit → expand context → decide.

## How it works

1. Look up the file's F record to get its ordered chunk list (chunkid + location pairs)
2. Find the target chunk by range label match (exact string comparison)
3. Compute the window: `max(0, targetIndex - before)` to `min(len-1, targetIndex + after)`
4. Re-chunk the file using its stored chunking strategy to recover chunk content
5. Return the chunks in the window with their range labels, content, and chunk indices

The expansion unit is chunks, not lines or bytes. Range labels are opaque and strategy-dependent — the only universal coordinate is the chunk index within the file's ordered chunk list.

## Error cases

- File not in database: error
- Target range not found in chunk list: error
- File missing from disk (can't re-chunk): error
- Chunking strategy not registered: error

## Library API

```go
// ChunkResult holds a single chunk with its content and position.
type ChunkResult struct {
    Path    string
    Range   string
    Content string
    Index   int     // 0-based position in the file's chunk list
    Attrs   []Pair  // chunk metadata from the chunker (role, timestamp, etc.)
}

// GetChunks retrieves the target chunk (identified by range label) and
// up to before/after positional neighbors. Returns chunks in order.
func (db *DB) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error)
```

## CLI

```
microfts chunks -db <path> [-before N] [-after N] <file> <range>
```

Output: JSONL, one JSON object per line with `path`, `range`, `content`, `index` fields. Chunks are in positional order. `-before` and `-after` default to 0 (target only).
