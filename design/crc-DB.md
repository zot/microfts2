# DB
**Requirements:** R1, R2, R17, R20, R25, R26, R27, R29, R33, R35, R37, R38, R39, R40, R41, R42, R22, R23, R24, R32, R51, R52, R53, R55, R56, R63, R64, R65, R66, R67, R68, R69, R77, R78, R79, R80, R81, R82, R84, R85, R86, R87, R88, R91, R92, R93, R94, R96, R97, R98, R99, R101, R103, R104, R105, R106, R107, R110, R111, R112, R115, R116, R117, R118, R119, R120, R121, R122, R124, R125, R127, R128, R129, R130, R132, R134, R135, R136, R137, R139, R140, R141, R142, R143, R144, R146, R147, R150, R151, R152, R153, R156, R157, R158, R159, R160, R161, R162, R163, R164, R165, R166, R167, R168, R176, R178, R179, R180, R181, R182, R183, R184, R185, R186, R187, R188, R189, R190, R191, R196, R197, R198, R199, R200, R201, R202, R203, R206, R213, R214, R215, R216, R217, R218, R219, R220, R221, R222, R223, R224, R225, R226, R227, R228, R229, R230, R231, R232, R233, R234, R235, R236, R237, R238, R239, R240, R241, R242, R243, R244, R245, R246, R247, R248, R249, R250, R251, R252, R253, R254, R255, R256, R257, R258, R259, R260, R261, R262, R263, R264, R265, R266, R267, R268, R269, R270, R271, R272, R273, R274, R275, R276, R277, R278, R279, R280, R281, R282, R283, R284, R285, R286, R287, R288, R289, R290, R291, R292, R293, R294, R295, R296, R298, R336, R337, R338, R339, R340, R341, R342, R343, R345, R346, R347, R348, R358, R360, R361, R363, R364, R365, R366, R367, R368, R369, R370, R374, R375, R376, R377, R378, R418, R419, R420, R421, R422, R423, R424, R425, R427, R428, R429, R430, R431, R432, R439, R442, R443, R444, R445, R446, R447, R448, R449, R450, R453, R451, R452, R454, R455, R456, R457, R458, R459, R460, R461, R462, R463, R464, R469, R470, R471, R472, R473, R474, R475, R476, R477, R478, R479, R480, R481, R482, R483, R484, R485, R486, R487, R488, R489, R490, R491, R492, R493, R494, R495, R496, R497, R498, R500, R513, R514, R515, R516, R520

Main database handle. Manages LMDB environment with a single named subdatabase. All records (I, H, C, F, N, T, W) are prefix-distinguished in one B-tree. Chunks are deduplicated by content hash. Provides the public library API.

## Knows
- env: LMDB environment handle
- dbi: subdatabase handle (single)
- dbName: subdatabase name (default "fts")
- settings: loaded from I records (chunking strategies, caseInsensitive, aliases)
- trigrams: Trigrams instance configured from settings (case insensitivity, byte aliases)
- chunkers: map[string]any — in-memory chunker strategies (each satisfies Chunker, FileChunker, or both)
- Record structs: CRecord (with ContentLen), FRecord, TRecord, WRecord, HRecord — typed encode/decode
- Supporting types: TrigramEntry, TokenEntry, FileChunkEntry
- TrigramCount: exported struct {Trigram uint32, Count int}
- TrigramFilter: exported function type deciding which query trigrams to search with
- ChunkFilter: exported function type `func(chunk CRecord) bool` — predicate on full chunk data
- stock filters: FilterAll, FilterByRatio, FilterBestN
- stock score functions: ScoreOverlap (pure), ScoreBM25 (closure factory)
- MultiSearchResult: exported struct {Strategy string, Results []SearchResult}
- ChunkResult: exported struct {Path, Range, Content string, Index int}
- overlay: *Overlay — in-memory tmp:// document store (lazily created)
- RecordStats: exported struct {Count, KeyBytes, ValueBytes int64} — per-prefix aggregate
- pathCache: map[uint64]string — cached fileid→path mapping, lazily loaded, incrementally maintained
- pathToID: map[string]uint64 — reverse cache, built alongside pathCache
- frecordCache: map[uint64]FRecord — opt-in per-batch FRecord cache, nil when inactive
- ChunkCallback: exported type `func(chunkText string)` — observation callback for indexing
- IndexOption: exported type `func(*indexConfig)` — functional option for indexing methods
- indexConfig: unexported struct holding ChunkCallback

## Does
- Create(path, opts): create new database, validate aliases (ASCII-only), set MaxDBs from opts (default 2), write I records with settings using data-in-key pattern. Write version "2" I record
- Open(path, opts): open existing database, set MaxDBs from opts (default 2), load settings from I records
- Close(): close LMDB environment
- Env(): return underlying *lmdb.Env for sharing with other libraries in-process
- Version(): read "version" I record in a View txn, return (string, error)
- AddFile(path, strategy, idxOpts): check for existing N records via FinalKey — return ErrAlreadyIndexed if present. Allocate fileid, create N/F records. Call Chunker.Chunks (yields Range+Content+Attrs per chunk). For each chunk: fire ChunkCallback if present (R473), hash content, check H record for dedup — if hit, add fileid to existing C record; if new, allocate chunkid, create H/C records (with attrs). Batch T/W updates across all chunks. Update F record with chunk entries and token bag. Returns (fileid, error)
- AddFileWithContent(path, strategy, idxOpts): like AddFile but also returns the raw file content. Returns (fileid, []byte, error)
- ErrAlreadyIndexed: sentinel error — caller checks with errors.Is and uses Reindex or AppendChunks instead
- RemoveFile(path): read F record to get chunk list, for each chunkid: read C record, remove fileid — if no fileids remain, delete C/H records and remove chunkid from T/W records. Delete F and N records
- searchConfig: embeds *DB, built by search entry points. The entire search pipeline (candidate collection, overlay merge, scoring, post-filtering, reranking) runs as methods on *searchConfig. Fields: scoreFunc, onlyIDs, exceptIDs, verify, trigramFilter, regexFilters, exceptRegexFilters, chunkFilters, proximityTopN, loose, noTmp, chunkCache
- SearchResult: Path, Range, Score (exported); chunkID, chunk (unexported). chunkID set by scoreAndResolve; chunk lazily populated by Retrieve
- WithChunkCache(cc *ChunkCache): SearchOption that stores a *ChunkCache on searchConfig — Retrieve checks it before rechunking from disk (cross-search file-read reuse)
- Retrieve(r *SearchResult): method on *searchConfig — returns chunk content. Check order: r.chunk (instant) → chunkCache.ChunkText(path, range) if present → rechunk from disk. Stores on r.chunk. Post-filters use Retrieve instead of filterResults+rechunkForVerify
- Search(query, opts): build searchConfig, extract trigrams, select via TrigramFilter (default FilterAll) using T record value lengths for DF. Read T records for candidate chunkids, intersect sets. Read C records for surviving candidates — apply ChunkFilter if present, then score using ScoreFunc. Post-filters (verify, regex, proximity) use Retrieve for chunk text. Return SearchResults
- SearchFuzzy(query, k, opts): two-phase typo-tolerant search. Phase 1: extract trigrams, read T record posting lists, tally chunkID appearances across posting lists, select top-k. Phase 2: read C records for top-k only, re-score with ScoreCoverage using actual trigram counts, resolve to paths. Apply post-filters (ChunkFilter, regex, proximity). Returns *SearchResults (R418-R425, R427)
- SearchRegex(pattern, opts): extract trigram query from regex AST (rsc approach), evaluate boolean query against T records, apply ChunkFilter, score, then always verify — re-chunk file, run regex, discard non-matches. Apply regex/except-regex post-filters if present. Return SearchResults
- ScoreFile(query, fpath, fn, opts): extract trigrams, select via TrigramFilter, compute per-chunk scores for one file's chunks using given ScoreFunc. Apply ChunkFilter if present. Returns []ScoredChunk
- Reindex(path, strategy): remove old records (via RemoveFile path), re-add with new strategy. Returns (fileid, error)
- ReindexWithContent(path, strategy): like Reindex but also returns the file content. Returns (fileid, []byte, error)
- FileInfoByID(fileid): read F record for fileid, return FRecord. Wraps in a View txn
- ChunkContentLens(fileid): read F record chunk list, read each C record's ContentLen, return []int in chunk-list order. Single View txn. Overlay-aware (R500, R501)
- CheckFile(path): stat + hash to determine fresh/stale/missing using F record metadata
- StaleFiles(): scan F records, classify each, return []FileStatus
- RefreshStale(strategy, idxOpts): reindex all stale files, pass idxOpts through to reindex. Return ([]FileStatus, error)
- AddStrategy(name, cmd): add chunking strategy to I records
- AddChunker(name, c): register a chunker (Chunker, FileChunker, or both) as a strategy; validates at least one interface is satisfied. In-memory only, I record stores name with empty value
- AddStrategyFunc(name, fn): convenience — wraps fn in FuncChunker, calls AddChunker
- AppendChunks(fileid, content, strategy, opts): chunk content using strategy, for each new chunk: fire ChunkCallback if WithAppendChunkCallback present (R482), hash, check H for dedup, create/update C records. Batch T/W updates. Update F record: append chunk entries, merge token bag, update metadata. Single LMDB write transaction. WithBaseLine adjusts line-based ranges after chunking.
- RemoveStrategy(name): remove chunking strategy from I records
- GetChunks(fpath, targetRange, before, after): look up file's F record for chunk list, find target by exact range label match (location field), compute neighbor window. Dispatch: if FileChunker, call FileChunker.FileChunks(path, zero, yield); if Chunker, os.ReadFile + Chunker.Chunks. Return []ChunkResult in positional order
- SearchMulti(query, strategies map[string]ScoreFunc, k, opts): single View txn — collect candidates once (trigram intersection, T/C record reads, chunk filters). Score with each ScoreFunc, keep top-k per strategy, apply post-filters per result set. Returns []MultiSearchResult
- BM25Func(queryTrigrams): read T records for per-trigram DF, read I record counters (totalTokens, totalChunks), compute IDF map and avgdl, return ScoreBM25 closure. Convenience for callers
- I record counter maintenance: AddFile/RemoveFile/AppendChunks atomically update totalTokens and totalChunks counters in the same write transaction
- AddTmpFile(path, strategy, content, idxOpts): delegate to overlay.addFile with ChunkCallback, return (fileid, error)
- AppendTmpFile(path, strategy, content, opts): delegate to overlay.appendFile with ChunkCallback from WithAppendChunkCallback, return (fileid, error). Shell >> semantics: create if absent, append if exists
- UpdateTmpFile(path, strategy, content, idxOpts): delegate to overlay.updateFile with ChunkCallback, return error
- RemoveTmpFile(path): delegate to overlay.removeFile, return error
- TmpFileIDs(): delegate to overlay.tmpFileIDs, return map[uint64]struct{}
- Search/SearchRegex/SearchMulti/ScoreFile: collect candidates from both LMDB and overlay, merge, apply filters and scoring uniformly
- GetChunks: detect tmp:// paths and route to overlay's stored content instead of disk
- BM25Func: sum LMDB and overlay counters for true corpus size
- RecordCounts(): open read-only txn, iterate all keys in subdatabase, accumulate count/key bytes/value bytes per prefix. Return map[byte]RecordStats
- FileIDPaths(): return cached fileid→path map. Lazy load on first call (F record scan with UnmarshalFHeader). AddFile/RemoveFile/Reindex update cache incrementally
- StaleFiles: uses UnmarshalFHeader (not UnmarshalFValue) — skips Chunks and Tokens
- NewSearchCache(): set frecordCache, return cleanup func that nils it. readFRecord checks cache before LMDB
- Copy(): return shallow copy sharing env, dbi, dbName, settings, trigrams, overlay, chunkers. Caches (pathCache, pathToID, frecordCache) set to nil. overlayOnce not copied — overlay pointer shared directly
- InvalidateCaches(): nil pathCache, pathToID, frecordCache. Does not reset overlayOnce
- WithChunkCallback(fn): return IndexOption that sets ChunkCallback in indexConfig (R470)
- WithAppendChunkCallback(fn): return AppendOption that sets ChunkCallback in appendConfig (R471)

## Collaborators
- Trigrams: raw byte trigram extraction (with counts)
- Chunker/FileChunker/ChunkTexter: chunking interfaces — dispatch by capability
- KeyChain: encode/decode long filenames in N records
- Overlay: in-memory tmp:// document storage and search

## Sequences
- seq-init.md
- seq-add.md
- seq-search.md
- seq-score.md
- seq-stale.md
- seq-append.md
- seq-chunks.md
- seq-search-multi.md
- seq-tmp-add.md
- seq-tmp-search.md
- seq-fuzzy-trigram.md
