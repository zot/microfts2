# Sequence: Add/Update/Remove Temporary Document
**Requirements:** R349, R351, R352, R354, R355, R358, R359, R360, R361, R362, R363, R364, R365, R371, R372

## AddTmpFile

```
Caller              DB                    Overlay               Chunker
  |                  |                       |                      |
  |--AddTmpFile----->|                       |                      |
  |  (path,strategy, |                       |                      |
  |   content)       |                       |                      |
  |                  |--addFile------------>|                      |
  |                  |  (path,strategy,     |                      |
  |                  |   content,db)        |                      |
  |                  |                      |--WLock               |
  |                  |                      |--check path exists   |
  |                  |                      |  (ErrAlreadyIndexed) |
  |                  |                      |--allocate fileID     |
  |                  |                      |  (MaxUint64--)       |
  |                  |                      |--resolve chunker---->|
  |                  |                      |                      |
  |                  |                      |--Chunks(path,content)|
  |                  |                      |<---yield chunk-------|
  |                  |                      |  for each chunk:     |
  |                  |                      |    hash content      |
  |                  |                      |    check hashes map  |
  |                  |                      |    if new:           |
  |                  |                      |      alloc chunkID-- |
  |                  |                      |      store chunk     |
  |                  |                      |      update trigrams |
  |                  |                      |      update tokens   |
  |                  |                      |      store hash      |
  |                  |                      |    if dedup:         |
  |                  |                      |      add fileID to   |
  |                  |                      |      existing chunk  |
  |                  |                      |                      |
  |                  |                      |--store overlayFile   |
  |                  |                      |  (content,chunks,    |
  |                  |                      |   token bag)         |
  |                  |                      |--update counters     |
  |                  |                      |--WUnlock             |
  |                  |<--fileid-------------|                      |
  |<--(fileid,nil)---|                       |                      |
```

## UpdateTmpFile

```
Caller              DB                    Overlay
  |                  |                       |
  |--UpdateTmpFile-->|                       |
  |  (path,strategy, |                       |
  |   content)       |                       |
  |                  |--updateFile---------->|
  |                  |                       |--WLock
  |                  |                       |--find by path (err if missing)
  |                  |                       |--remove old chunks + index entries
  |                  |                       |--add new (same as addFile body)
  |                  |                       |--WUnlock
  |                  |<--nil-----------------|
  |<--nil------------|                       |
```

## RemoveTmpFile

```
Caller              DB                    Overlay
  |                  |                       |
  |--RemoveTmpFile-->|                       |
  |  (path)          |                       |
  |                  |--removeFile---------->|
  |                  |                       |--WLock
  |                  |                       |--find by path (err if missing)
  |                  |                       |--for each chunkid:
  |                  |                       |    remove fileID from chunk
  |                  |                       |    if orphaned:
  |                  |                       |      delete chunk
  |                  |                       |      clean trigram map
  |                  |                       |      clean token map
  |                  |                       |      delete hash entry
  |                  |                       |--delete overlayFile
  |                  |                       |--update counters
  |                  |                       |--WUnlock
  |                  |<--nil-----------------|
  |<--nil------------|                       |
```
