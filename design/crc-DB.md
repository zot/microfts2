# DB
**Requirements:** R1, R2, R14, R15, R16, R17, R19, R20, R21, R27, R29, R30, R33, R35, R37, R38, R39, R40, R41, R42, R22, R23, R24, R32, R51, R52, R53, R55, R56, R63, R64, R65, R66, R67, R68, R69, R77, R78, R79, R80, R81, R82, R83, R84, R85, R86, R87, R88, R91, R92, R93, R94, R95, R96, R97, R98, R99, R101, R102, R103, R104, R105, R106, R107, R109, R110, R8, R111, R112, R115, R116, R117, R118, R119, R120, R121, R122, R123, R124, R125, R127, R128, R129, R130, R132, R134, R135, R136, R137, R139, R140, R141, R142, R143, R144, R146, R147, R148, R149, R150, R151, R152, R153, R154, R155, R156, R157, R158, R159, R160, R161, R162, R163, R164, R165, R166, R167, R168, R176, R178, R179, R180, R181, R182, R183, R184, R185, R186, R187, R188, R189, R190, R191, R196, R197, R198, R199, R200, R201, R202, R203, R206, R213, R214, R215, R216, R217

Main database handle. Manages LMDB environment with two named subdatabases (content and index). Provides the public library API.

## Knows
- env: LMDB environment handle
- contentDBI: content subdatabase handle
- indexDBI: index subdatabase handle
- settings: loaded from I record (chunking strategies, caseInsensitive, aliases)
- FileInfo.ModTime: Unix nanoseconds at index time
- FileInfo.ContentHash: SHA-256 hex string at index time
- FileInfo.FileLength: int64, file size in bytes at index time
- trigrams: Trigrams instance configured from settings (case insensitivity, byte aliases)
- contentName: subdatabase name (default "ftscontent")
- indexName: subdatabase name (default "ftsindex")
- funcStrategies: map[string]ChunkFunc — in-memory Go function strategies
- TrigramCount: exported struct {Trigram uint32, Count int} — trigram code paired with corpus document frequency
- TrigramFilter: exported function type deciding which query trigrams to search with
- stock filters: FilterAll, FilterByRatio, FilterBestN — shipped as package-level functions
- ChunkResult: exported struct {Path string, Range string, Content string, Index int} — a chunk with content and position

## Does
- Create(path, opts): create new database, validate aliases (ASCII-only via Trigrams.ValidateAliases), set MaxDBs from opts (default 2), write I record with settings (no C initialization needed — sparse)
- Open(path, opts): open existing database, set MaxDBs from opts (default 2), load settings from I record
- Close(): close LMDB environment
- Env(): return underlying *lmdb.Env for sharing with other libraries in-process
- AddFile(path, strategy): check for existing F records via FinalKey — return ErrAlreadyIndexed if present. Call chunker generator (yields Range+Content per chunk), compute trigrams on Content, check UTF-8 on Content, store F/N records (with chunkRanges), update C counts, write forward + reverse index entries. Returns (fileid, error)
- AddFileWithContent(path, strategy): like AddFile but also returns the raw file content. Returns (fileid, []byte, error)
- ErrAlreadyIndexed: sentinel error — caller checks with errors.Is and uses Reindex or AppendChunks instead
- RemoveFile(path): read R record to find file's index entries, delete forward entries, delete R record, remove F/N records, update C counts
- Search(query, opts): extract trigrams, select query trigrams via TrigramFilter (default FilterAll), intersect posting lists, score using ScoreFunc. If WithVerify: re-chunk file using stored strategy, match by range to recover content, tokenize query into terms (space-split, quoted phrases as single terms), discard chunks where any term is absent (case-insensitive substring check). If regex filters/except-regex: compile patterns, re-chunk, apply AND/subtract post-filters via filterResults. Return SearchResults with IndexStatus
- SearchRegex(pattern, opts): extract trigram query from regex AST (rsc approach), evaluate boolean query against full index, score results, then always verify — re-chunk file using stored strategy, run compiled regex against chunk content, discard non-matches. Then apply regex filters/except-regex post-filters if present. Return SearchResults with IndexStatus
- ScoreFile(query, fpath, fn, opts): extract trigrams, select query trigrams via TrigramFilter (default FilterAll), compute per-chunk scores for one file's index entries using given ScoreFunc. Returns []ScoredChunk
- Reindex(path, strategy): remove old records + index entries, re-add with new strategy + new index entries. Returns (fileid, error)
- ReindexWithContent(path, strategy): like Reindex but also returns the file content. Returns (fileid, []byte, error)
- FileInfoByID(fileid): read N record for fileid, return FileInfo. Wraps readFileInfo in a View txn
- CheckFile(path): stat + hash to determine fresh/stale/missing
- StaleFiles(): scan N records, classify each, return []FileStatus
- RefreshStale(strategy): reindex all stale files, return ([]FileStatus, error)
- AddStrategy(name, cmd): add chunking strategy to I record
- AddStrategyFunc(name, fn): register Go function as chunking strategy; in-memory only, I record stores name with empty cmd
- AppendChunks(fileid, content, strategy, opts): chunk content using strategy, add new chunks to existing file. Continue chunk numbering, write forward index + update R record + increment C counts + update N record. Single LMDB write transaction. WithBaseLine adjusts line-based ranges after chunking.
- RemoveStrategy(name): remove chunking strategy from I record
- GetChunks(fpath, targetRange, before, after): look up file's N record for chunkRanges, find target by exact range label match, compute neighbor window, re-chunk file from disk using stored strategy, return []ChunkResult in positional order

## Collaborators
- Trigrams: raw byte trigram extraction (with counts)
- Chunker: wrap external chunking commands as ChunkFunc generators
- KeyChain: encode/decode long filenames in F records

## Sequences
- seq-init.md
- seq-add.md
- seq-search.md
- seq-score.md
- seq-stale.md
- seq-append.md
- seq-chunks.md
