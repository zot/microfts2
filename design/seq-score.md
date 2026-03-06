# Sequence: Score File
**Requirements:** R96, R97, R98, R103

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
 |  read A record (packed sorted trigrams)  |
 |  build map for O(1) active lookup       |
 |  filter query trigrams to active set    |
 |  if none: return empty []ScoredChunk     |
 |                                          |
 |  for each chunk of fileid:               |
 |    scan R record or index entries        |
 |      to build chunkCounts map            |
 |    score = fn(queryTrigrams,             |
 |      chunkCounts, chunkTokenCount)       |
 |    read start/end lines from FileInfo    |
 |    append ScoredChunk{start, end, score} |
 |                                          |
 |  return []ScoredChunk                    |
```

ScoredChunk struct: StartLine int, EndLine int, Score float64
CLI prints: `filepath:startline-endline\tscore`
