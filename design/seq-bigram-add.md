# Sequence: Bigram Add (within AddFile)
**Requirements:** R380, R382, R384, R386, R388, R389, R390, R402, R403, R404, R405

Participants: DB, CharSet

Bigram extraction and B record writes are interleaved with the existing AddFile flow. This sequence shows only the bigram-specific steps.

```
DB                                        CharSet
 |                                          |
 |  [inside addFileInTxn, after chunk yield]|
 |  check I record: bigrams enabled?        |
 |    if disabled: skip all bigram work     |
 |                                          |
 |  for each new chunk (not dedup hit):     |
 |-- BigramCounts(Content) ------------>    |
 | <-- map[uint16]int ------------------    |
 |    store bigram counts in CRecord       |
 |      (CRecord.Bigrams = []BigramEntry)  |
 |    accumulate bigrams for B batch       |
 |                                          |
 |  C record marshal includes bigram       |
 |    section: [n-bigrams:varint]          |
 |    [[bigram:2] [count:varint]]...       |
 |                                          |
 |  batch B record updates:                |
 |    for each affected bigram:            |
 |      read B[bi], append chunkids,       |
 |      write B[bi]                         |
 |    (coalesced with T/W batch writes)    |
 |                                          |
```

For dedup hits: C record already has bigram data from original indexing. No B record updates needed (chunkid already in B records).

AppendChunks follows the same pattern — bigram extraction and B record updates when bigrams enabled.

RemoveFile: for orphaned chunks, remove chunkid from B records (same loop as T/W cleanup).

Reindex: RemoveFile + AddFile, so B records handled by both paths.
