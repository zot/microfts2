# Sequence: Add File
**Requirements:** R10, R11, R12, R19, R20, R25, R26, R29, R77, R79, R81, R92, R102, R109, R110, R111, R116, R118, R120, R121, R122, R128, R129, R130, R131, R146, R147, R213, R214, R215

Participants: DB, Chunker, Trigrams, KeyChain

```
DB                      Chunker       Trigrams      KeyChain
 |                        |              |            |
 |  [in addFileInTxn]                    |            |
 |  check FinalKey(fpath) in content DB  |            |
 |  if exists: return (0, ErrAlreadyIndexed)         |
 |                                       |            |
 |  stat file (mod time before read)     |            |
 |  read file, compute SHA-256           |            |
 |                                       |            |
 |  resolve ChunkFunc for strategy:      |            |
 |    if funcStrategy[name] exists:      |            |
 |      fn = funcStrategy[name]          |            |
 |    else:                              |            |
 |-- RunChunkerFunc(cmd) -> fn --------> |            |
 |                                       |            |
 |  call fn(path, content, yield):       |            |
 |    for each yielded Chunk{Range, Content}:         |
 |      copy Range as string             |            |
 |      validate UTF-8 on Content        |            |
 |-- TrigramCounts(Content) -------->    |            |
 | <-- map[uint32]int ---------------    |            |
 |      count tokens on Content          |            |
 |      store range, triCounts, tokens   |            |
 |                                       |            |
 |-- Encode(filename) --------------------------------> |
 | <-- F key/value pairs --------------------------------|
 |  store F records, assign fileid       |            |
 |                                       |            |
 |  for each chunk:                      |            |
 |    update C records: get/put C[tri:3] per trigram  |
 |    write forward index entries:       |            |
 |      key=[trigram, 0xFFFF-count, fileid, chunknum] |
 |      val=empty                        |            |
 |    accumulate (chunknum, trigram, count) for R     |
 |                                       |            |
 |  write R record: key=[R, fileid]      |            |
 |    val=packed chunk groups:           |            |
 |    [chunknum:8][numTri:2][[tri:3][count:2]]...     |
 |                                       |            |
 |  store N record: key=[N, fileid]      |            |
 |    val=JSON{chunkRanges,              |            |
 |      chunkTokenCounts, strategy,      |            |
 |      modTime, hash, fileLength}       |            |
 |                                       |            |
 |  return (fileid, nil)                 |            |
 |  WithContent: return (fileid, content, nil)       |
```
