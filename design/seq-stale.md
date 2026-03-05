# Sequence: Staleness Check and Refresh
**Requirements:** R63, R64, R65, R66, R67, R68, R69

Participants: DB

```
DB
 |
 |  === CheckFile(fpath) ===
 |
 |  look up fileid from F records
 |  read N record -> FileInfo (has ModTime, ContentHash)
 |  classifyFile(info):
 |    stat file on disk
 |      if file missing: return "missing"
 |    if disk mod time == stored mod time:
 |      return "fresh"
 |    compute SHA-256 of file contents
 |    if hash == stored hash:
 |      return "fresh"  (mod time changed, content same)
 |    return "stale"
 |
 |  === StaleFiles() ===
 |
 |  scan all N records (prefix 'N')
 |  for each FileInfo:
 |    classifyFile(info)
 |    collect FileStatus
 |  return []FileStatus
 |
 |  === RefreshStale(strategy) ===
 |
 |  call StaleFiles()
 |  for each stale file:
 |    determine strategy: param if non-empty, else file's existing strategy
 |    Reindex(file.Path, strategy)  [incremental index update]
 |    collect into refreshed list
 |  return (refreshed []FileStatus, nil)
```
