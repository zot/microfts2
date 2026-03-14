# Sequence: Staleness Check and Refresh
**Requirements:** R63, R64, R65, R66, R67, R68, R69, R234

Participants: DB

```
DB
 |
 |  === CheckFile(fpath) ===
 |
 |  look up fileid from N records
 |  read F record -> FRecord (has ModTime, ContentHash)
 |  classifyFile(info):
 |    stat file on disk
 |      if file missing: return "missing"
 |    if disk mod time == stored mod time:
 |      return "fresh"
 |    compute SHA-256 of file contents
 |    if hash == stored hash:
 |      update F record mod time (touch)
 |      return "fresh"  (mod time changed, content same)
 |    return "stale"
 |
 |  === StaleFiles() ===
 |
 |  scan all F records (prefix 'F')
 |  for each FRecord:
 |    classifyFile(info)
 |    collect FileStatus
 |  return []FileStatus
 |
 |  === RefreshStale(strategy) ===
 |
 |  call StaleFiles()
 |  for each stale file:
 |    determine strategy: param if non-empty, else file's existing strategy
 |    Reindex(file.Path, strategy)
 |    collect into refreshed list
 |  return (refreshed []FileStatus, nil)
```
