# Sequence: Search with Overlay
**Requirements:** R366, R367, R368, R369, R370, R372, R374

## Search (merged LMDB + overlay)

```
Caller              DB                    Overlay               LMDB
  |                  |                       |                      |
  |--Search--------->|                       |                      |
  |  (query, opts)   |                       |                      |
  |                  |--extract trigrams     |                      |
  |                  |--apply TrigramFilter  |                      |
  |                  |                       |                      |
  |                  |  --- LMDB candidates ---                     |
  |                  |--View txn------------>|                      |
  |                  |  read T records       |                      |
  |                  |  intersect chunkids   |                      |
  |                  |  read C records       |                      |
  |                  |  apply ChunkFilter    |                      |
  |                  |  apply WithOnly/Except|                      |
  |                  |  score candidates     |                      |
  |                  |<--lmdb candidates-----|                      |
  |                  |                       |                      |
  |                  |  --- Overlay candidates ---                  |
  |                  |--searchCandidates---->|                      |
  |                  |  (queryTrigrams)      |                      |
  |                  |                       |--RLock               |
  |                  |                       |--intersect trigram   |
  |                  |                       |  maps → chunkid set  |
  |                  |                       |--return chunks       |
  |                  |                       |--RUnlock             |
  |                  |<--overlay candidates--|                      |
  |                  |                       |                      |
  |                  |--apply ChunkFilter    |                      |
  |                  |  to overlay candidates|                      |
  |                  |--apply WithOnly/Except|                      |
  |                  |--score overlay cands  |                      |
  |                  |                       |                      |
  |                  |  --- Merge ---        |                      |
  |                  |--merge all candidates |                      |
  |                  |--sort by score desc   |                      |
  |                  |                       |                      |
  |                  |  --- Post-filters ---  |                      |
  |                  |--if verify/regex:     |                      |
  |                  |  for lmdb results:    |                      |
  |                  |    re-chunk from disk  |                      |
  |                  |  for overlay results: |                      |
  |                  |    chunk from stored   |                      |
  |                  |    content            |                      |
  |                  |                       |                      |
  |<--SearchResults--|                       |                      |
```

## TmpFileIDs (for exclusion)

```
Caller              DB                    Overlay
  |                  |                       |
  |--TmpFileIDs----->|                       |
  |                  |--tmpFileIDs---------->|
  |                  |                       |--RLock
  |                  |                       |--copy fileID set
  |                  |                       |--RUnlock
  |                  |<--map[uint64]struct{}--|
  |<--ids------------|                       |
```

## BM25 with overlay counters

```
Caller              DB                    Overlay               LMDB
  |                  |                       |                      |
  |--BM25Func------->|                       |                      |
  |  (queryTrigrams) |                       |                      |
  |                  |--read T records------>|                      |
  |                  |  (per-trigram DF)     |                      |
  |                  |--read I counters----->|                      |
  |                  |  (totalChunks,Tokens) |                      |
  |                  |--counters()---------->|                      |
  |                  |                       |--return overlay      |
  |                  |                       |  totalChunks,Tokens  |
  |                  |--sum LMDB + overlay   |                      |
  |                  |  for avgdl            |                      |
  |                  |--build ScoreBM25      |                      |
  |<--ScoreFunc------|                       |                      |
```

## GetChunks / ChunkCache with overlay

```
Caller              DB/ChunkCache         Overlay
  |                  |                       |
  |--GetChunks------>|                       |
  |  (path,range,    |                       |
  |   before,after)  |                       |
  |                  |--is tmp:// path?      |
  |                  |  yes:                 |
  |                  |--lookupFileByPath---->|
  |                  |                       |--return overlayFile
  |                  |--chunkContent-------->|
  |                  |  (use stored content, |
  |                  |   db's chunker)       |
  |                  |<--[]ChunkResult-------|
  |                  |                       |
  |                  |  no (disk file):      |
  |                  |  existing path via    |
  |                  |  LMDB N/F records     |
  |                  |                       |
  |<--[]ChunkResult--|                       |
```
