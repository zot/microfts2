# Sequence: Bigram Search (scoring path)
**Requirements:** R391, R392, R393, R394, R395, R397

Participants: DB, CharSet

Bigram scoring is a ScoreFunc alternative. Candidates still come from trigram intersection (unchanged). This sequence shows only the bigram-specific scoring path.

```
DB                                        CharSet
 |                                          |
 |  [Search called with WithBigramOverlap]  |
 |                                          |
 |  (candidate collection unchanged:        |
 |   trigram extraction, T record reads,    |
 |   intersection, C record reads)          |
 |                                          |
 |  score function is ScoreBigramOverlap:   |
 |    receives queryTrigrams, chunkCounts,  |
 |    chunkTokenCount (standard ScoreFunc)  |
 |                                          |
 |  ScoreBigramOverlap internally:          |
 |-- BigramCounts(query) ----------------> |
 | <-- queryBigrams map[uint16]int -------- |
 |    for each queryBigram:                 |
 |      if chunkBigrams[bigram] > 0: match |
 |    score = matchCount / len(queryBigrams)|
 |                                          |
 |  (rest of search unchanged:              |
 |   verify, regex filters, sort)           |
 |                                          |
```

Note: ScoreBigramOverlap does NOT fit the existing ScoreFunc signature
(which operates on trigram data). It needs access to the C record's
bigram counts. Two options:
1. Wrap as a closure that receives the CRecord directly
2. Extend ScoreFunc signature

Option 1 preferred: ScoreBigramOverlap is a factory that returns a
closure. The closure captures the query bigrams and is called with
CRecord during candidate evaluation (same phase where CRecord is
already loaded).

CLI: `-score bigram` selects WithBigramOverlap.
