# Sequence: Initialize Database
**Requirements:** R2, R3, R8, R16, R17, R39, R40

Participants: CLI, DB, CharSet

```
CLI                         DB                          CharSet
 |                           |                            |
 |-- init(charset, opts) --> |                            |
 |                           |-- New(chars, caseIns) ---> |
 |                           | <-- charSet instance ----- |
 |                           |                            |
 |                           |  create LMDB env at path
 |                           |  open content subdatabase
 |                           |  write I record:
 |                           |    characterSet, caseInsensitive,
 |                           |    chunkingStrategies: {},
 |                           |    activeTrigramCutoff: default,
 |                           |    activeTrigrams: []
 |                           |  write C record: 2MB zeroed
 |                           |                            |
 | <-- ok ------------------ |                            |
```
