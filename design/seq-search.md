# Sequence: Search
**Requirements:** R30, R31, R32, R33, R34, R35, R82, R83, R84, R85, R87, R88, R89, R99, R103, R104, R105, R106, R107, R108, R124, R125, R126, R127

Participants: DB, Trigrams

## Literal Search (Search)

```
DB                                        Trigrams
 |                                          |
 |  resolve scoring function from opts      |
 |  (default: coverage)                     |
 |                                          |
 |-- Trigrams(query) ---------------------> |
 | <-- queryTrigrams[] (char-internal skip) |
 |                                          |
 |  read A record (packed sorted trigrams)  |
 |  build map for O(1) active lookup       |
 |  filter query trigrams to active set    |
 |  if none: return empty results           |
 |                                          |
 |  for each active query trigram:          |
 |    scan index DB range:                  |
 |      [trigram,0,0,0]..[trigram+1,0,0,0]  |
 |    collect (fileid, chunknum, count)     |
 |                                          |
 |  intersect candidate sets by            |
 |    (fileid, chunknum)                    |
 |  accumulate chunkCounts map per chunk    |
 |                                          |
 |  for each (fileid, chunknum) in result:  |
 |    look up FileInfo from N record        |
 |    (filename, chunk start/end lines,     |
 |     chunkTokenCount)                     |
 |    score = scoreFunc(queryTrigrams,      |
 |      chunkCounts, chunkTokenCount)       |
 |                                          |
 |  if WithVerify:                          |
 |    tokenize query into terms             |
 |    (space-split, "quoted" = single term) |
 |    for each result:                      |
 |      read chunk text from disk           |
 |        (FileInfo.ChunkOffsets + file)    |
 |      lowercase chunk text                |
 |      for each term:                      |
 |        if term not found as substring:   |
 |          discard result                  |
 |                                          |
 |  sort by score descending                |
 |  return *SearchResults{Results, Status}  |
```

## Regex Search (SearchRegex)

```
DB
 |
 |  resolve scoring function from opts
 |  (default: coverage)
 |
 |  parse pattern with regexp/syntax
 |  extract trigram query from AST:
 |    boolean AND/OR expression of trigrams
 |    (rsc codesearch approach)
 |
 |  evaluate trigram query against full index:
 |    AND nodes: intersect candidate sets
 |    OR nodes: union candidate sets
 |    leaf trigram: scan index DB range
 |      [trigram,0,0,0]..[trigram+1,0,0,0]
 |    collect (fileid, chunknum, count) per scan
 |
 |  for each (fileid, chunknum) in result:
 |    look up FileInfo from N record
 |    score = scoreFunc(queryTrigrams,
 |      chunkCounts, chunkTokenCount)
 |
 |  verify (always):                       |
 |    compile pattern with regexp.Compile   |
 |    for each result:                      |
 |      read chunk text from disk           |
 |      if !regex.Match(chunk): discard     |
 |
 |  sort by score descending
 |  return *SearchResults{Results, Status}
```

SearchResult struct: Path string, StartLine int, EndLine int, Score float64
SearchResults struct: Results []SearchResult, Status IndexStatus
ScoreFunc type: func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64
CLI prints: `filepath:startline-endline`
CLI `-regex` flag selects SearchRegex path
CLI `-score coverage|density` selects scoring strategy
