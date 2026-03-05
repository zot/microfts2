# DB
**Requirements:** R1, R2, R14, R15, R16, R17, R19, R20, R21, R27, R28, R29, R30, R33, R35, R36, R37, R38, R39, R40, R41, R42, R22, R23, R24, R32, R51, R52, R53, R54, R55, R56, R63, R64, R65, R66, R67, R68, R69, R75, R76, R77, R78, R79, R80, R81, R82, R83, R84, R85, R86, R87, R88, R91, R92, R93, R94, R95, R96, R97, R98, R99, R101, R102, R103, R104, R105, R106, R107, R109, R110, R8

Main database handle. Manages LMDB environment with two named subdatabases (content and index). Provides the public library API.

## Knows
- env: LMDB environment handle
- contentDBI: content subdatabase handle
- indexDBI: index subdatabase handle
- settings: loaded from I record (character set, chunking strategies, searchCutoff)
- activeTrigrams: sorted []uint32 loaded from A record (bottom searchCutoff% for literal query filtering)
- FileInfo.ModTime: Unix nanoseconds at index time
- FileInfo.ContentHash: SHA-256 hex string at index time
- charSet: CharSet instance configured from settings
- contentName: subdatabase name (default "ftscontent")
- indexName: subdatabase name (default "ftsindex")

## Does
- Create(path, opts): create new database, set MaxDBs from opts (default 2), write I record with settings (no C initialization needed — sparse)
- Open(path, opts): open existing database, set MaxDBs from opts (default 2), load settings from I record, load A record if present (decode packed trigram list)
- Close(): close LMDB environment
- Env(): return underlying *lmdb.Env for sharing with other libraries in-process
- AddFile(path, strategy): chunk file, compute trigram counts per chunk, store F/N records, update C counts, write forward + reverse index entries. Returns (fileid, error)
- RemoveFile(path): read R record to find file's index entries, delete forward entries, delete R record, remove F/N records, update C counts
- Search(query, opts): extract trigrams, filter to active set, intersect posting lists, score using ScoreFunc, return SearchResults with IndexStatus
- SearchRegex(pattern, opts): extract trigram query from regex AST (rsc approach), evaluate boolean query against full index, score results, return SearchResults with IndexStatus
- ScoreFile(query, fpath, fn): extract trigrams, compute per-chunk scores for one file's index entries using given ScoreFunc. Returns []ScoredChunk
- BuildIndex(cutoff): cursor scan all C[tri:3] records, sort by count, take bottom cutoff% → write A record as packed sorted trigram list (index entries maintained incrementally, not rebuilt)
- Reindex(path, strategy): remove old records + index entries, re-add with new strategy + new index entries. Returns (fileid, error)
- FileInfoByID(fileid): read N record for fileid, return FileInfo. Wraps readFileInfo in a View txn
- CheckFile(path): stat + hash to determine fresh/stale/missing
- StaleFiles(): scan N records, classify each, return []FileStatus
- RefreshStale(strategy): reindex all stale files, return ([]FileStatus, error)
- AddStrategy(name, cmd): add chunking strategy to I record
- RemoveStrategy(name): remove chunking strategy from I record

## Collaborators
- CharSet: trigram extraction from text (with counts)
- Chunker: execute external chunking commands
- KeyChain: encode/decode long filenames in F records

## Sequences
- seq-init.md
- seq-add.md
- seq-search.md
- seq-score.md
- seq-build-index.md
- seq-stale.md
