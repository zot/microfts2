# Sequence: Score File
**Requirements:** R96-98, R103, R133, R135-137, R139, R143, R144, R238, R239, R244, R255, R256, R260, R775, R777

Participants: DB, Trigrams

```
DB                                        Trigrams
 |                                          |
 |  look up fileid from N records           |
 |  read F record -> FRecord                |
 |  (chunk list, strategy)                  |
 |                                          |
 |-- Trigrams(query) ---------------------> |
 | <-- queryTrigrams[] -------------------- |
 |                                          |
 |  select query trigrams via filter:       |
 |    for each query trigram:               |
 |      decode T[tri]: live postings -> DF  |
 |    get total chunk count from DB         |
 |    call filter([]TrigramCount, total)    |
 |    (default: FilterAll — use all)        |
 |  if none: return empty []ScoredChunk     |
 |                                          |
 |  for each chunkid in file's chunk list:  |
 |    read C record -> CRecord              |
 |    apply ChunkFilters (AND) if present   |
 |    build chunkCounts map from CRecord    |
 |    tokenCount from CRecord tokens        |
 |    score = fn(queryTrigrams,             |
 |      chunkCounts, tokenCount)            |
 |    read location from FRecord chunks     |
 |    append ScoredChunk{range, score}      |
 |                                          |
 |  return []ScoredChunk                    |
```

ScoredChunk struct: Range string, Score float64
CLI prints: `filepath:range\tscore`
