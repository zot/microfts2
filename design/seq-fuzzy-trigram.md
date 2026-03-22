# Sequence: SearchFuzzy (Fuzzy Trigram Search)
**Requirements:** R418, R419, R420, R421, R422, R425, R427

Participants: DB, LMDB, Overlay

Fast typo-tolerant search. Scores from posting lists — no C record reads until path resolution of top-k.

```
Caller               DB                      LMDB
  |                   |                        |
  |--SearchFuzzy()--->|                        |
  |                   |                        |
  |                   |  parse query, extract trigrams     |
  |                   |                        |
  |                   |  -- posting-list tally (R419) --   |
  |                   |                        |
  |                   |  for each query trigram:           |
  |                   |--read T record-------->|
  |                   |<--chunkid list---------|
  |                   |    for each chunkid:               |
  |                   |      tally[chunkid]++              |
  |                   |                        |
  |                   |  -- overlay tally (R425) --        |
  |                   |                        |
  |                   |  for each query trigram:           |
  |                   |    union overlay trigram map        |
  |                   |    tally[chunkid]++                |
  |                   |                        |
  |                   |  -- top-k selection (R421) --      |
  |                   |                        |
  |                   |  select top-k by tally count       |
  |                   |                        |
  |                   |  -- phase 2: re-score (R420) --    |
  |                   |                        |
  |                   |--read C records (k)--->|
  |                   |<--CRecord data---------|
  |                   |  re-score with ScoreCoverage       |
  |                   |  (actual trigram counts)            |
  |                   |  re-sort by accurate score         |
  |                   |                        |
  |                   |  -- resolve winners --             |
  |                   |                        |
  |                   |--read F records------->|
  |                   |<--FRecord data---------|
  |                   |  map chunkid → (path, range)       |
  |                   |                        |
  |                   |  -- post-filters (R423) --         |
  |                   |                        |
  |                   |  apply ChunkFilter, regex filters  |
  |                   |  apply proximity rerank            |
  |                   |  sort by score descending          |
  |                   |                        |
  |<--*SearchResults--|                        |
```
