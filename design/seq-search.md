# Sequence: Search
**Requirements:** R30, R31, R32, R33, R34, R35

Participants: DB, CharSet

```
DB                                        CharSet
 |                                          |
 |  if index DB does not exist:             |
 |    BuildIndex (see seq-build-index.md)   |
 |                                          |
 |-- Trigrams(query) ---------------------> |
 | <-- queryTrigrams[] -------------------- |
 |                                          |
 |  filter to active trigrams only          |
 |  if no active trigrams: return all chunks (degenerate case)
 |                                          |
 |  for each active query trigram:          |
 |    scan index DB range:                  |
 |      key=[trigram,0,0]..[trigram+1,0,0]  |
 |    collect set of (fileid, chunknum)     |
 |                                          |
 |  intersect all sets                      |
 |                                          |
 |  for each (fileid, chunknum) in result:  |
 |    look up filename from F records       |
 |    look up chunk offsets from N record   |
 |    compute start/end lines               |
 |                                          |
 |  sort by filename, then chunknum         |
 |  return []SearchResult                   |
```

SearchResult struct: Path string, StartLine int, EndLine int
CLI prints: `filepath:startline-endline`
