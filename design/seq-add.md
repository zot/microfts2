# Sequence: Add File
**Requirements:** R10, R11, R12, R19, R20, R25, R26, R29, R77, R79, R81, R92, R102, R109, R110, R111, R116, R118, R120, R121, R122

Participants: DB, Chunker, Trigrams, KeyChain

```
DB                      Chunker       Trigrams      KeyChain
 |                        |              |            |
 |  stat file (mod time before read)     |            |
 |  read file, compute SHA-256           |            |
 |  validate UTF-8 (reject if invalid)  |            |
 |                                       |            |
 |  if funcStrategy[name] exists:        |            |
 |    offsets = fn(path, content)        |            |
 |  else:                                |            |
 |-- Run(cmd, path) ----> |              |            |
 | <-- offsets[] -------- |              |            |
 |                                       |            |
 |-- Encode(filename) --------------------------------> |
 | <-- F key/value pairs --------------------------------|
 |  store F records, assign fileid       |            |
 |                                       |            |
 |  for each chunk (offsets[i]..offsets[i+1]):        |
 |    read chunk text from file          |            |
 |-- TrigramCounts(text) ------------>   |            |
 | <-- map[uint32]int --------------->   |            |
 |                                       |            |
 |    count tokens (space-separated) for chunk        |
 |    update C records: get/put C[tri:3] per trigram  |
 |                                       |            |
 |    write forward index entries:       |            |
 |      key=[trigram, 0xFFFF-count, fileid, chunknum] |
 |      val=empty                        |            |
 |                                       |            |
 |    accumulate (chunknum, trigram, count) for R     |
 |                                       |            |
 |  write R record: key=[R, fileid]      |            |
 |    val=packed chunk groups:           |            |
 |    [chunknum:8][numTri:2][[tri:3][count:2]]...     |
 |                                       |            |
 |  store N record: key=[N, fileid]      |            |
 |    val=JSON{chunkOffsets, chunkStartLines,          |
 |      chunkEndLines, chunkTokenCounts,               |
 |      strategy, modTime, hash}         |            |
 |                                       |            |
 |  return (fileid, nil)                 |            |
 |  WithContent: return (fileid, content, nil)       |
```
