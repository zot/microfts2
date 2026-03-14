# Sequence: Append Chunks
**Requirements:** R146, R147, R150, R151, R152, R153, R156, R157, R158, R159, R160, R161, R162, R163, R164, R165, R166, R167, R168, R223, R224, R225, R226, R236, R237, R253, R261, R262

Participants: DB, Trigrams

```
DB                                    Trigrams
 |                                      |
 |  View txn: read F record for fileid  |
 |    -> existing FRecord               |
 |    -> existingChunkCount             |
 |    -> error if fileid not found      |
 |                                      |
 |  resolve ChunkFunc for strategy      |
 |                                      |
 |  call fn(path, content, yield):      |
 |    for each yielded Chunk:           |
 |      copy Range as string            |
 |      validate UTF-8 on Content       |
 |      compute SHA-256 of Content      |
 |-- TrigramCounts(Content) ----------> |
 | <-- map[uint32]int ----------------- |
 |      tokenize Content, count tokens  |
 |      extract attrs (if HasAttrs)     |
 |                                      |
 |  if baseLine > 0:                    |
 |    for each new chunk range:         |
 |      parse "start-end"              |
 |      add baseLine to start and end  |
 |      re-format as "start-end"       |
 |                                      |
 |  Update txn (single, atomic):        |
 |    for each new chunk:               |
 |      check H[hash] for dedup:        |
 |        if hit: add fileid to C       |
 |        if new: allocate chunkid,     |
 |          create H, C records         |
 |          accumulate for T/W batch    |
 |      collect (chunkid, location)     |
 |      merge tokens into file bag      |
 |                                      |
 |    batch T record updates            |
 |    batch W record updates            |
 |                                      |
 |    update F record:                  |
 |      append chunk entries            |
 |      merge token bag                 |
 |      set contentHash (from opt)      |
 |      set modTime (from opt)          |
 |      set fileLength (from opt)       |
 |                                      |
 |  return nil                          |
```
