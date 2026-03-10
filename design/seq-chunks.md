# Sequence: Chunk Context Retrieval
**Requirements:** R197, R198, R199, R200, R201, R202, R203, R204, R205, R206

Participants: CLI, DB

## GetChunks

```
CLI                                       DB
 |                                         |
 |  parse flags: -db, -before, -after      |
 |  positional: <file> <range>             |
 |                                         |
 |-- GetChunks(fpath, range, before, after) ->
 |                                         |
 |                            View txn:    |
 |                            look up F record -> fileid
 |                            read N record -> FileInfo
 |                            (chunkRanges, strategy)
 |                                         |
 |                            find targetRange in chunkRanges
 |                            (exact string match, linear scan)
 |                            if not found: return error
 |                                         |
 |                            compute window:
 |                              lo = max(0, targetIdx - before)
 |                              hi = min(len-1, targetIdx + after)
 |                                         |
 |                            resolve ChunkFunc for strategy
 |                            if nil: return error
 |                                         |
 |                            read file from disk
 |                            if err: return error
 |                                         |
 |                            re-chunk file with ChunkFunc
 |                            collect chunks[lo..hi] with content
 |                            (copy content, match by index)
 |                                         |
 | <-- []ChunkResult (in positional order)  |
 |                                         |
 |  for each ChunkResult:                  |
 |    emit JSONL: {path, range,            |
 |                 content, index}          |
```

ChunkResult struct: Path string, Range string, Content string, Index int
CLI output: one JSON object per line (JSONL)
