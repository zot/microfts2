# Sequence: Build Index
**Requirements:** R27, R28, R36, R43, R44

Participants: DB, Bitset

```
DB                                       Bitset
 |                                         |
 |  read C record (2MB trigram counts)     |
 |  sort trigrams by count                 |
 |  compute cutoff: trigrams below         |
 |    percentile = active trigrams         |
 |  store active trigrams + cutoff         |
 |    in I record                          |
 |                                         |
 |  drop index DB if exists                |
 |  create index DB                        |
 |                                         |
 |  for each T record in content DB:       |
 |    key = [T, fileid, chunknum]          |
 |    val = bitset bytes                   |
 |-- FromBytes(val) ---------------------> |
 | <-- bitset ----------------------------- |
 |                                         |
 |-- ForEach(fn) ------------------------> |
 |    fn receives each set trigram         |
 |    if trigram is active:                |
 |      write index entry:                |
 |        key=[trigram,fileid,chunknum]    |
 |        val=empty                        |
 |                                         |
 |  index DB complete                      |
```

No file I/O required — rebuilds entirely from content DB.
Changing cutoff only requires re-running this sequence.
