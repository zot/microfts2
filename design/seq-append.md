# Sequence: Append Chunks
**Requirements:** R146, R147, R150, R151, R152, R153, R156, R157, R158, R159, R160, R161, R162, R163, R164, R165, R166, R167, R168, R223, R224, R225, R226, R236, R237, R253, R261, R262, R471, R482, R601, R602, R603, R604, R605, R606, R607, R608, R609, R623, R624, R625

Participants: DB, Trigrams, Chunker

```
DB                                          Trigrams       Chunker
 |                                            |               |
 |  View txn: read F record for fileid        |               |
 |    -> existing FRecord                     |               |
 |    -> existingChunkCount                   |               |
 |    -> read last entry's locator (or nil)   |               |
 |    -> error if fileid not found            |               |
 |                                            |               |
 |  resolve Chunker for strategy              |               |
 |                                            |               |
 |  extract ChunkCallback from opts            |               |
 |                                            |               |
 |  if Chunker implements AppendAwareChunker:  |               |
 |--- AppendChunks(path, lastLocator, --------|------------>  |
 |    newBytes, yield)                         |              |
 |  <-- yields {Range, Locator, Content, Attrs}|<-----------  |
 |  <-- (replacedLast, err)                    |<-----------  |
 |                                             |              |
 |  else (default behavior):                   |              |
 |--- Chunks(path, newBytes, yield) -----------|------------> |
 |  <-- yields {Range, Locator, Content, Attrs}|<-----------  |
 |    replacedLast = false                     |              |
 |    if zero chunks and len(content) > 0:     |              |
 |      return ErrAppendBoundary  [R623,R624]  |              |
 |    if len(content) == 0: return nil [R625]  |              |
 |                                             |              |
 |  for each yielded Chunk:                    |              |
 |    copy Range, Locator (as bytes)            |              |
 |    validate UTF-8 on Content                |              |
 |    if callback != nil:                      |              |
 |      callback(string(Content)) [R482]       |              |
 |    compute SHA-256 of Content               |              |
 |--- TrigramCounts(Content) -----------------> |              |
 |  <-- map[uint32]int ------------------------|              |
 |    tokenize Content, count tokens           |              |
 |    copy Attrs ([]Pair)                      |              |
 |                                             |              |
 |  if baseLine > 0 and Locator empty:         |              |
 |    for each new chunk range:                |              |
 |      parse "start-end"                      |              |
 |      add baseLine to start and end          |              |
 |      re-format as "start-end"               |              |
 |                                             |              |
 |  Update txn (single, atomic):               |              |
 |    if replacedLast:                         |              |
 |      droppedChunkID = F.lastEntry.chunkid   |              |
 |      drop F's last entry                    |              |
 |      dropChunkOccurrence(droppedChunkID,    |              |
 |                          fileid)            |              |
 |        decrement fileid count in C          |              |
 |        if count==0: remove fileid entry     |              |
 |        if fileids list empty: cascade       |              |
 |          delete C, prune T/W, delete H      |              |
 |                                             |              |
 |    for each new chunk:                      |              |
 |      check H[hash] for dedup:               |              |
 |        if hit: increment fileid count in C  |              |
 |          (insert with count=1 if absent)    |              |
 |        if new: allocate chunkid,            |              |
 |          create H, C records                |              |
 |          accumulate for T/W batch           |              |
 |      collect (chunkid, location, locator)   |              |
 |      merge tokens into file bag             |              |
 |                                             |              |
 |    batch T record updates                   |              |
 |    batch W record updates                   |              |
 |                                             |              |
 |    update F record:                         |              |
 |      append chunk entries (with locator)    |              |
 |      merge token bag                        |              |
 |      set contentHash (from opt)             |              |
 |      set modTime (from opt)                 |              |
 |      set fileLength (from opt)              |              |
 |                                             |              |
 |  return nil                                 |              |
```
