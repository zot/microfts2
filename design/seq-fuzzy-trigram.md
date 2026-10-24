# Sequence: SearchFuzzy (Fuzzy Trigram Search)
**Requirements:** R418-422, R425, R427, R706-709

Participants: DB, index, Overlay

Fast typo-tolerant search. Scores from posting lists — no C record reads until path resolution of top-k, except to settle whether a chunk in an excepted file belongs to another file.

```
Caller               DB                      index
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
  |                   |    tmpTally[chunkid]++             |
  |                   |                        |
  |                   |  -- file restriction (R706) --     |
  |                   |                        |
  |                   |  if WithOnly or WithExcept:        |
  |                   |--read F records of restricted files->
  |                   |  drop disk entries admitDisk rejects (R707)
  |                   |    (C record read only for a chunk in an excepted file)
  |                   |  drop overlay entries admitLocked rejects (R708)
  |                   |                        |
  |                   |  -- top-k selection (R421) --      |
  |                   |                        |
  |                   |  select top-k by tally count, disk and overlay together
  |                   |                        |
  |                   |  -- phase 2: re-score (R420) --    |
  |                   |                        |
  |                   |--read C records (disk winners)--->|
  |                   |<--CRecord data---------|
  |                   |  re-score with ScoreCoverage       |
  |                   |  (actual trigram counts)            |
  |                   |                        |
  |                   |  -- resolve winners --             |
  |                   |                        |
  |                   |--read F records------->|
  |                   |<--FRecord data---------|
  |                   |  map chunkid → (path, range)       |
  |                   |                        |
  |                   |  overlay winners: Overlay.resolve(ids, ScoreCoverage) (R709)
  |                   |    in-memory record → score → tmp:// (path, range)
  |                   |                        |
  |                   |  -- post-filters (R423) --         |
  |                   |                        |
  |                   |  apply ChunkFilter, regex filters  |
  |                   |  apply proximity rerank            |
  |                   |  sort by score descending          |
  |                   |                        |
  |<--*SearchResults--|                        |
```
