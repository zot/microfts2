# Sequence: Initialize Database
**Requirements:** R17, R39, R40, R219, R220, R660, R662, R666

Participants: CLI, DB

```
CLI                         DB
 |                           |
 |-- init(opts) -----------> |
 |                           |
 |                           |  open the index at path (bbolt.Open)
 |                           |  create the fts bucket (CreateBucketIfNotExists)
 |                           |  bucket name from opts.DBName (default "fts")
 |                           |  write I records (data-in-key):
 |                           |    I["caseInsensitive"] = "true"/"false"
 |                           |    I["alias:\n"] = "^"  (per alias)
 |                           |    I["strategy:chunk-lines"] = ""
 |                           |    (one record per setting)
 |                           |
 | <-- ok ------------------ |
```
