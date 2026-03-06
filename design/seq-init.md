# Sequence: Initialize Database
**Requirements:** R2, R8, R16, R17, R39, R40, R101

Participants: CLI, DB

```
CLI                         DB
 |                           |
 |-- init(opts) -----------> |
 |                           |
 |                           |  create LMDB env at path
 |                           |  SetMaxDBs(opts.maxDBs)
 |                           |  open content subdatabase
 |                           |  write I record:
 |                           |    caseInsensitive, aliases,
 |                           |    chunkingStrategies: {},
 |                           |    activeTrigramCutoff: default
 |                           |
 | <-- ok ------------------ |
```
