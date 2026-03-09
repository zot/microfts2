# Sequence: Score File
**Requirements:** R96, R97, R98, R103, R133, R135, R136, R137, R139, R143, R144

Participants: DB, Trigrams

```
DB                                        Trigrams
 |                                          |
 |  look up fileid from F records           |
 |  read FileInfo from N record             |
 |                                          |
 |-- Trigrams(query) ---------------------> |
 | <-- queryTrigrams[] -------------------- |
 |                                          |
 |  select query trigrams via filter:       |
 |    for each query trigram:               |
 |      point-read C[tri:3] for count       |
 |    get total chunk count from DB         |
 |    call filter([]TrigramCount, total)    |
 |    (default: FilterAll — use all)        |
 |  if none: return empty []ScoredChunk     |
 |                                          |
 |  for each chunk of fileid:               |
 |    scan R record or index entries        |
 |      to build chunkCounts map            |
 |    score = fn(queryTrigrams,             |
 |      chunkCounts, chunkTokenCount)       |
 |    read range from FileInfo.ChunkRanges  |
 |    append ScoredChunk{range, score}      |
 |                                          |
 |  return []ScoredChunk                    |
```

ScoredChunk struct: Range string, Score float64
CLI prints: `filepath:range\tscore`
