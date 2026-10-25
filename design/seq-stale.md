# Sequence: Staleness Check and Refresh
**Requirements:** R63-69, R717, R719

Participants: DB

```
DB
 |
 |  === CheckFile(fpath) ===
 |
 |  look up fileid from N records
 |  read F record -> FRecord (has ModTime, ContentHash, Strategy, ChunkerVersion)
 |  db.classifyFile(info):
 |    stat file on disk
 |      if file missing: return "missing"
 |    if info.ChunkerVersion != chunkerVersion(info.Strategy):
 |      return "stale"   (chunker rules changed; content may be untouched — R719)
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
 |    db.classifyFile(info)
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
