# Sequence: Chunk Cache
**Requirements:** R297, R298, R299, R300, R301, R302, R303, R304, R305, R306

Participants: Caller, ChunkCache, DB, Chunker

## ChunkText (lazy path)

```
Caller                ChunkCache              DB              Chunker
 |                       |                     |                 |
 |-- ChunkText(path, range) ->                 |                 |
 |                       |                     |                 |
 |                  check files[path]          |                 |
 |                  if nil:                    |                 |
 |                    ensureFile(path):        |                 |
 |                  -- lookupFileByPath -----> |                 |
 |                  <-- fileid, FRecord ------ |                 |
 |                  -- resolveChunker -------> |                 |
 |                  <-- Chunker -------------- |                 |
 |                    os.ReadFile(path)        |                 |
 |                    allocate sparse slice    |                 |
 |                                             |                 |
 |                  check byRange[range]       |                 |
 |                  if hit:                    |                 |
 |                    return cached content    |                 |
 |                                             |                 |
 |                  if not complete:           |                 |
 |                    chunkUntil(cf, range):   |                 |
 |                  -- Chunks(path, data, yield) ------------>  |
 |                       for each Chunk:       |                 |
 |                         deep-copy, store    |                 |
 |                         byRange[range]=idx  |                 |
 |                         if range matches:   |                 |
 |                           stop early        |                 |
 |                  <-- content, true -------- |                 |
 |                                             |                 |
 | <-- content, true                           |                 |
```

## GetChunks (full path)

```
Caller                ChunkCache              DB              Chunker
 |                       |                     |                 |
 |-- GetChunks(path, range, before, after) ->  |                 |
 |                       |                     |                 |
 |                  ensureFile (if needed)     |                 |
 |                  (same as above)            |                 |
 |                                             |                 |
 |                  if not complete:           |                 |
 |                    chunkFull(cf):           |                 |
 |                  -- Chunks(path, data, yield) ------------>  |
 |                       for each Chunk:       |                 |
 |                         deep-copy, store    |                 |
 |                         byRange[range]=idx  |                 |
 |                    complete = true          |                 |
 |                                             |                 |
 |                  find targetIdx via byRange |                 |
 |                  compute window [lo..hi]    |                 |
 |                  build []ChunkResult        |                 |
 |                                             |                 |
 | <-- []ChunkResult                           |                 |
```
