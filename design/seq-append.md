# Sequence: Append Chunks
**Requirements:** R146, R147, R148, R150, R151, R152, R153, R154, R155, R156, R157, R158, R159, R160, R161, R162, R163, R164, R165, R166, R167, R168

Participants: DB, Trigrams

```
DB                                    Trigrams
 |                                      |
 |  View txn: read N record for fileid  |
 |    -> existing FileInfo              |
 |    -> existingChunkCount             |
 |    -> error if fileid not found      |
 |                                      |
 |  View txn: read R record for fileid  |
 |    -> existingRData (packed bytes)   |
 |                                      |
 |  resolve ChunkFunc for strategy      |
 |                                      |
 |  call fn(path, content, yield):      |
 |    for each yielded Chunk:           |
 |      copy Range as string            |
 |      validate UTF-8 on Content       |
 |-- TrigramCounts(Content) ----------> |
 | <-- map[uint32]int ----------------- |
 |      count tokens on Content         |
 |      store range, triCounts, tokens  |
 |                                      |
 |  if baseLine > 0:                    |
 |    for each new chunk range:         |
 |      parse "start-end"              |
 |      add baseLine to start and end  |
 |      re-format as "start-end"       |
 |                                      |
 |  Update txn (single, atomic):        |
 |    for each new chunk (i):           |
 |      chunknum = existingChunkCount+i |
 |      update C records: incr C[tri]   |
 |      write forward index entries:    |
 |        key=[tri, 0xFFFF-count,       |
 |             fileid, chunknum]        |
 |      accumulate for new R data       |
 |                                      |
 |    replace R record:                 |
 |      val = existingRData +           |
 |            new packed chunk groups   |
 |                                      |
 |    update N record:                  |
 |      append to chunkRanges           |
 |      append to chunkTokenCounts      |
 |      set contentHash (from opt)      |
 |      set modTime (from opt)          |
 |      set fileLength (from opt)       |
 |                                      |
 |  return nil                          |
```
