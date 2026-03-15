# Sequence: SearchMulti
**Requirements:** R283, R284, R285, R286, R287, R288, R289, R290

## SearchMulti(query, strategies, k, opts)

```
Caller               DB                      LMDB
  |                   |                        |
  |--SearchMulti()--->|                        |
  |                   |--env.View()----------->|
  |                   |                        |
  |                   |  -- candidate collection (once) --
  |                   |                        |
  |                   |  parse query, extract trigrams     |
  |                   |  apply TrigramFilter               |
  |                   |--read T records------->|
  |                   |<--chunkid lists--------|
  |                   |  intersect per-term sets           |
  |                   |--read C records------->|
  |                   |<--CRecord data---------|
  |                   |  apply ChunkFilters                |
  |                   |  build candidateChunk slice        |
  |                   |                        |
  |                   |  -- scoring (per strategy) --      |
  |                   |                        |
  |                   |  for each strategy:                |
  |                   |    score all candidates            |
  |                   |    keep top-k by score             |
  |                   |    resolve chunkid->(path, range)  |
  |                   |                        |
  |                   |<--end View-------------|
  |                   |                        |
  |                   |  -- post-filters (per strategy) -- |
  |                   |                        |
  |                   |  for each strategy's results:      |
  |                   |    apply verify if set             |
  |                   |    apply regex filters             |
  |                   |    apply proximity rerank          |
  |                   |    sort by score descending        |
  |                   |                        |
  |<--[]MultiSearchResult                      |
```

## Internal refactor

`scoreAndResolve` splits into:
1. `collectCandidates(th, chunkIDs, cfg) []candidateChunk` — C record reads + chunk filters
2. `scoreAndResolve(th, candidates, active, scoreFunc, cfg) []SearchResult` — score + resolve

`Search` calls both in sequence (no behavior change).
`SearchMulti` calls `collectCandidates` once, then `scoreAndResolve` per strategy.
