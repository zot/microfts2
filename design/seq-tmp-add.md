# Sequence: Add/Update/Append/Remove Temporary Document
**Requirements:** R349, R351, R352, R354, R355, R358, R359, R360, R361, R362, R363, R364, R365, R371, R372, R428, R429, R430, R431, R432, R433, R434, R435, R436, R437, R439, R440, R441, R442

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

## AppendTmpFile

```
Caller              DB                    Overlay               Chunker
  |                  |                       |                      |
  |--AppendTmpFile-->|                       |                      |
  |  (path,strategy, |                       |                      |
  |   content,opts)  |                       |                      |
  |                  |--appendFile---------->|                      |
  |                  |                       |--validate UTF-8      |
  |                  |                       |--RLock               |
  |                  |                       |--check path exists   |
  |                  |                       |  if not found:       |
  |                  |                       |    RUnlock            |
  |                  |                       |    delegate to        |
  |                  |                       |    addFile (R431)     |
  |                  |                       |    return fileid      |
  |                  |                       |  if found:            |
  |                  |                       |    check strategy     |
  |                  |                       |    match (R432)       |
  |                  |                       |    read fileID        |
  |                  |                       |    RUnlock            |
  |                  |                       |                      |
  |                  |                       |--resolve chunker---->|
  |                  |                       |--Chunks(content)---->|
  |                  |                       |<---collected---------|
  |                  |                       |                      |
  |                  |                       |--apply WithBaseLine   |
  |                  |                       |  adjustRange (R439)   |
  |                  |                       |                      |
  |                  |                       |--WLock               |
  |                  |                       |--re-check path (R441)|
  |                  |                       |  (error if removed)  |
  |                  |                       |  for each chunk:     |
  |                  |                       |    dedupOrCreate      |
  |                  |                       |    append to chunks  |
  |                  |                       |    merge token bag   |
  |                  |                       |--extend content (R437)|
  |                  |                       |--WUnlock             |
  |                  |<--fileid-------------|                      |
  |<--(fileid,nil)---|                       |                      |
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
