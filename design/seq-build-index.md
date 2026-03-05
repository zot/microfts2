# Sequence: Build Index
**Requirements:** R27, R28, R36, R75, R76

Participants: DB

```
DB
 |
 |  cursor scan all C[tri:3] records in content DB
 |    collect (trigram, count) pairs
 |    (only non-zero trigrams have records)
 |
 |  sort by count ascending
 |  take bottom searchCutoff% = active trigrams
 |
 |  encode as packed sorted trigram list:
 |    [tri:3][tri:3][tri:3]...
 |  write A record
 |
 |  update I record with searchCutoff
```

Index entries are maintained incrementally by AddFile/RemoveFile.
BuildIndex only recomputes the A record from C records.
Changing cutoff only requires re-running this sequence.
