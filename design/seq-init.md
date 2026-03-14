# Sequence: Initialize Database
**Requirements:** R2, R17, R39, R40, R101, R218, R219, R220

Participants: CLI, DB

```
CLI                         DB
 |                           |
 |-- init(opts) -----------> |
 |                           |
 |                           |  create LMDB env at path
 |                           |  SetMaxDBs(opts.maxDBs)
 |                           |  open subdatabase (default "fts")
 |                           |  write I records (data-in-key):
 |                           |    I["caseInsensitive"] = "true"/"false"
 |                           |    I["alias:\n"] = "^"  (per alias)
 |                           |    I["strategy:chunk-lines"] = ""
 |                           |    (one record per setting)
 |                           |
 | <-- ok ------------------ |
```
