# Sequence: Add File
**Requirements:** R10, R11, R12, R18, R19, R20, R25, R26, R29

Participants: DB, Chunker, CharSet, Bitset, KeyChain

```
DB                      Chunker       CharSet      Bitset     KeyChain
 |                        |              |            |           |
 |-- Run(cmd, path) ----> |              |            |           |
 | <-- offsets[] -------- |              |            |           |
 |                                       |            |           |
 |-- Encode(filename) -----------------------------------------> |
 | <-- F key/value pairs ----------------------------------------|
 |  store F records, assign fileid       |            |           |
 |                                       |            |           |
 |  for each chunk (offsets[i]..offsets[i+1]):        |           |
 |    read chunk text from file          |            |           |
 |-- Trigrams(text) ----------------->   |            |           |
 | <-- []uint32 ----------------------   |            |           |
 |                                       |            |           |
 |    new Bitset                                      |           |
 |    Set each trigram --------------------------->   |           |
 |    store T record: key=[T,fileid,chunknum]         |           |
 |                    val=bitset.Bytes()              |           |
 |    update C counts: increment for each set bit     |           |
 |                                                    |           |
 |  store N record: key=[N,fileid]                    |           |
 |    val=JSON{chunkOffsets, chunkingStrategy}         |           |
 |                                                    |           |
 |  drop index DB (stale)                             |           |
```
