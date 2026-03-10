# Sequence: Search
**Requirements:** R30, R31, R32, R33, R34, R35, R82, R83, R84, R85, R87, R88, R89, R99, R103, R104, R105, R106, R107, R108, R124, R125, R126, R127, R132, R134, R135, R136, R137, R140, R141, R142, R143, R144, R178, R179, R180, R181, R182, R183, R184, R185, R186, R187, R188, R189, R190, R191, R196

Participants: DB, Trigrams

## Literal Search (Search)

```
DB                                        Trigrams
 |                                          |
 |  resolve scoring function from opts      |
 |  (default: coverage)                     |
 |                                          |
 |  trim query whitespace                   |
 |  parse query into terms:                 |
 |    parseQueryTerms(query)                |
 |    (space-split, "quoted" = single term) |
 |                                          |
 |  for each term:                          |
 |-- Trigrams(term) ----------------------> |
 | <-- termTrigrams[] (char-internal skip)  |
 |                                          |
 |  union all term trigrams into            |
 |    queryTrigrams[] (deduplicated)        |
 |                                          |
 |  select query trigrams via filter:       |
 |    for each query trigram:               |
 |      point-read C[tri:3] for count       |
 |    get total chunk count from DB         |
 |    call filter([]TrigramCount, total)    |
 |    (default: FilterAll — use all)        |
 |  if none: return empty results           |
 |                                          |
 |  for each term's selected trigrams:      |
 |    scan index DB range per trigram       |
 |    intersect within term                 |
 |  intersect candidate sets across terms   |
 |  accumulate chunkCounts map per chunk    |
 |                                          |
 |  for each (fileid, chunknum) in result:  |
 |    look up FileInfo from N record        |
 |    (filename, chunkRanges,               |
 |     chunkTokenCount)                     |
 |    score = scoreFunc(queryTrigrams,      |
 |      chunkCounts, chunkTokenCount)       |
 |                                          |
 |  if WithVerify:                          |
 |    for each result:                      |
 |      re-chunk file using stored strategy |
 |      find chunk by range match           |
 |      lowercase chunk content             |
 |      for each term:                      |
 |        if term not found as substring:   |
 |          discard result                  |
 |                                          |
 |  if regex filters or except-regex:       |
 |    compile all patterns (error on fail)  |
 |    for each result:                      |
 |      re-chunk file (cached per path)     |
 |      find chunk by range match           |
 |      for each regex filter (AND):        |
 |        if !regex.Match(content): discard |
 |      for each except-regex (subtract):   |
 |        if regex.Match(content): discard  |
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
 |  compile pattern with regexp.Compile
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
 |  verify (always):
 |    for each result:
 |      re-chunk file using stored strategy
 |      find chunk by range match
 |      if !regex.Match(content): discard
 |
 |  if regex filters or except-regex:
 |    compile all patterns (error on fail)
 |    for each result:
 |      re-chunk file (cached per path)
 |      for each regex filter (AND):
 |        if !regex.Match(content): discard
 |      for each except-regex (subtract):
 |        if regex.Match(content): discard
 |
 |  sort by score descending
 |  return *SearchResults{Results, Status}
```

SearchResult struct: Path string, Range string, Score float64
SearchResults struct: Results []SearchResult, Status IndexStatus
ScoreFunc type: func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64
CLI prints: `filepath:range`
CLI `-regex` flag selects SearchRegex path
CLI `-score coverage|density` selects scoring strategy
