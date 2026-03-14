# DB
**Requirements:** R1, R2, R17, R20, R25, R26, R27, R29, R33, R35, R37, R38, R39, R40, R41, R42, R22, R23, R24, R32, R51, R52, R53, R55, R56, R63, R64, R65, R66, R67, R68, R69, R77, R78, R79, R80, R81, R82, R84, R85, R86, R87, R88, R91, R92, R93, R94, R96, R97, R98, R99, R101, R103, R104, R105, R106, R107, R110, R111, R112, R115, R116, R117, R118, R119, R120, R121, R122, R124, R125, R127, R128, R129, R130, R132, R134, R135, R136, R137, R139, R140, R141, R142, R143, R144, R146, R147, R150, R151, R152, R153, R156, R157, R158, R159, R160, R161, R162, R163, R164, R165, R166, R167, R168, R176, R178, R179, R180, R181, R182, R183, R184, R185, R186, R187, R188, R189, R190, R191, R196, R197, R198, R199, R200, R201, R202, R203, R206, R213, R214, R215, R216, R217, R218, R219, R220, R221, R222, R223, R224, R225, R226, R227, R228, R229, R230, R231, R232, R233, R234, R235, R236, R237, R238, R239, R240, R241, R242, R243, R244, R245, R246, R247, R248, R249, R250, R251, R252, R253, R254, R255, R256, R257, R258, R259, R260, R261, R262, R263, R264, R265, R266, R267

Main database handle. Manages LMDB environment with a single named subdatabase. All records (I, H, C, F, N, T, W) are prefix-distinguished in one B-tree. Chunks are deduplicated by content hash. Provides the public library API.

## Knows
- env: LMDB environment handle
- dbi: subdatabase handle (single)
- dbName: subdatabase name (default "fts")
- settings: loaded from I records (chunking strategies, caseInsensitive, aliases)
- trigrams: Trigrams instance configured from settings (case insensitivity, byte aliases)
- funcStrategies: map[string]ChunkFunc — in-memory Go function strategies
- Record structs: CRecord, FRecord, TRecord, WRecord, HRecord — typed encode/decode
- Supporting types: TrigramEntry, TokenEntry, FileChunkEntry
- TrigramCount: exported struct {Trigram uint32, Count int}
- TrigramFilter: exported function type deciding which query trigrams to search with
- ChunkFilter: exported function type `func(chunk CRecord) bool` — predicate on full chunk data
- stock filters: FilterAll, FilterByRatio, FilterBestN
- ChunkResult: exported struct {Path, Range, Content string, Index int}

## Does
- Create(path, opts): create new database, validate aliases (ASCII-only), set MaxDBs from opts (default 2), write I records with settings using data-in-key pattern
- Open(path, opts): open existing database, set MaxDBs from opts (default 2), load settings from I records
- Close(): close LMDB environment
- Env(): return underlying *lmdb.Env for sharing with other libraries in-process
- AddFile(path, strategy): check for existing N records via FinalKey — return ErrAlreadyIndexed if present. Allocate fileid, create N/F records. Call chunker (yields Range+Content per chunk). For each chunk: hash content, check H record for dedup — if hit, add fileid to existing C record; if new, allocate chunkid, create H/C records. Batch T/W updates across all chunks. Update F record with chunk entries and token bag. Returns (fileid, error)
- AddFileWithContent(path, strategy): like AddFile but also returns the raw file content. Returns (fileid, []byte, error)
- ErrAlreadyIndexed: sentinel error — caller checks with errors.Is and uses Reindex or AppendChunks instead
- RemoveFile(path): read F record to get chunk list, for each chunkid: read C record, remove fileid — if no fileids remain, delete C/H records and remove chunkid from T/W records. Delete F and N records
- Search(query, opts): extract trigrams, select via TrigramFilter (default FilterAll) using T record value lengths for DF. Read T records for candidate chunkids, intersect sets. Read C records for surviving candidates — apply ChunkFilter if present, then score using ScoreFunc. If WithVerify/regex filters: re-chunk file, apply post-filters via filterResults. Return SearchResults
- SearchRegex(pattern, opts): extract trigram query from regex AST (rsc approach), evaluate boolean query against T records, apply ChunkFilter, score, then always verify — re-chunk file, run regex, discard non-matches. Apply regex/except-regex post-filters if present. Return SearchResults
- ScoreFile(query, fpath, fn, opts): extract trigrams, select via TrigramFilter, compute per-chunk scores for one file's chunks using given ScoreFunc. Apply ChunkFilter if present. Returns []ScoredChunk
- Reindex(path, strategy): remove old records (via RemoveFile path), re-add with new strategy. Returns (fileid, error)
- ReindexWithContent(path, strategy): like Reindex but also returns the file content. Returns (fileid, []byte, error)
- FileInfoByID(fileid): read F record for fileid, return FRecord. Wraps in a View txn
- CheckFile(path): stat + hash to determine fresh/stale/missing using F record metadata
- StaleFiles(): scan F records, classify each, return []FileStatus
- RefreshStale(strategy): reindex all stale files, return ([]FileStatus, error)
- AddStrategy(name, cmd): add chunking strategy to I records
- AddStrategyFunc(name, fn): register Go function as chunking strategy; in-memory only, I record stores name with empty value
- AppendChunks(fileid, content, strategy, opts): chunk content using strategy, for each new chunk: hash, check H for dedup, create/update C records, batch T/W updates. Update F record: append chunk entries, merge token bag, update metadata. Single LMDB write transaction. WithBaseLine adjusts line-based ranges after chunking.
- RemoveStrategy(name): remove chunking strategy from I records
- GetChunks(fpath, targetRange, before, after): look up file's F record for chunk list, find target by exact range label match (location field), compute neighbor window, re-chunk file from disk using stored strategy, return []ChunkResult in positional order

## Collaborators
- Trigrams: raw byte trigram extraction (with counts)
- Chunker: wrap external chunking commands as ChunkFunc generators
- KeyChain: encode/decode long filenames in N records

## Sequences
- seq-init.md
- seq-add.md
- seq-search.md
- seq-score.md
- seq-stale.md
- seq-append.md
- seq-chunks.md
