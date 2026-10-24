# Sequence: Chunk Context Retrieval
**Requirements:** R197-200, R202-206, R236, R701-704

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
 |                            look up N records -> fileid
 |                            read F record -> FRecord
 |                            (chunk list, strategy)
 |                                         |
 |                            find targetRange in chunk list
 |                            (exact string match on location field)
 |                            if not found: return error
 |                                         |
 |                            compute window:
 |                              lo = max(0, targetIdx - before)
 |                              hi = min(len-1, targetIdx + after)
 |                                         |
 |                            resolve Chunker for strategy
 |                            if nil: return error
 |                                         |
 |                            read file from disk
 |                            if err: return error
 |                                         |
 |                            run Chunker.Chunks on file
 |                            keep chunks whose Range is a window entry's
 |                            (match by range, never by position)
 |                            stop once every window range is found
 |                            unmatched entry -> Missing result
 |                                         |
 | <-- []ChunkResult (in positional order)  |
 |                                         |
 |  for each ChunkResult:                  |
 |    emit JSONL: {path, range,            |
 |                 content, index}          |
```

ChunkResult struct: Path string, Range string, Content string, Index int, Attrs []Pair, Missing bool
CLI output: one JSON object per line (JSONL); `missing: true` on a missing chunk
