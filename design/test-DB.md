# Test Design: DB
**Requirements:** R701-703, R705-710, R726-728
**Source:** crc-DB.md

## Test: create and open
**Purpose:** database lifecycle
**Input:** Create with case-insensitive, Close, Open
**Expected:** settings preserved across close/open, I records match
**Refs:** crc-DB.md, seq-init.md

## Test: add file and search
**Purpose:** end-to-end add and retrieval
**Input:** create DB, add a file with known content, search for a substring
**Expected:** search returns correct file path and line range
**Refs:** crc-DB.md, seq-add.md, seq-search.md

## Test: search works immediately after add
**Purpose:** index maintained incrementally
**Input:** add a file, search without any extra steps
**Expected:** search succeeds (T/W/C records written during add)
**Refs:** crc-DB.md, seq-search.md

## Test: remove file
**Purpose:** file deletion cleans records
**Input:** add file, remove file, search for its content
**Expected:** no results returned; C/H/T/W records cleaned for orphaned chunks
**Refs:** crc-DB.md, R254

## Test: reindex with different strategy
**Purpose:** strategy migration
**Input:** add file with strategy A, reindex with strategy B
**Expected:** F record reflects new strategy, chunks updated
**Refs:** crc-DB.md

## Test: key chain for long filename
**Purpose:** filenames exceeding 511 bytes
**Input:** add file with 600-byte path
**Expected:** file added successfully, searchable, filename recoverable
**Refs:** crc-DB.md, crc-KeyChain.md

## Test: custom subdatabase name
**Purpose:** configurable DB name
**Input:** Create with DBName="mydb"
**Expected:** database created with custom name, operations work normally
**Refs:** crc-DB.md, R219

## Test: FileLength stored on add
**Purpose:** FileLength in F record
**Input:** add a file, read FRecord via FileInfoByID
**Expected:** FileLength matches actual file size
**Refs:** crc-DB.md, seq-add.md, R146

## Test: append chunks
**Purpose:** incremental chunk addition
**Input:** add a 3-line file, then AppendChunks with 2 more lines
**Expected:** file has 5 chunks total, new chunks searchable, old chunks still intact
**Refs:** crc-DB.md, seq-append.md

## Test: append chunks with base line offset
**Purpose:** chunker offset support
**Input:** add a 3-line file, then AppendChunks with 2 lines and WithBaseLine(3)
**Expected:** new chunk ranges are "4-4" and "5-5" (not "1-1" and "2-2")
**Refs:** crc-DB.md, seq-append.md

## Test: append chunks updates F record metadata
**Purpose:** F record metadata after append
**Input:** add file, AppendChunks with WithContentHash, WithModTime, WithFileLength
**Expected:** FRecord reflects updated hash, modTime, fileLength, appended chunk entries and merged token bag
**Refs:** crc-DB.md, seq-append.md, R157

## Test: append chunks invalid fileid
**Purpose:** error on nonexistent fileid
**Input:** AppendChunks with fileid that doesn't exist
**Expected:** returns error
**Refs:** crc-DB.md, seq-append.md

## Test: per-token trigram search order independence
**Purpose:** query word order does not affect results
**Input:** add files containing "daneel olivaw", search "daneel olivaw" and "olivaw daneel"
**Expected:** both queries return the same result set
**Refs:** crc-DB.md, seq-search.md, R180, R181, R182

## Test: quoted phrase trigrams preserve adjacency
**Purpose:** quoted phrases generate cross-boundary trigrams
**Input:** add file with "hello world" and "hello other world", search `"hello world"`
**Expected:** quoted search matches only the file with adjacent "hello world"
**Refs:** crc-DB.md, seq-search.md, R179, R180

## Test: trailing whitespace trimmed
**Purpose:** trailing space does not add spurious trigrams
**Input:** add file with "daneel", search "daneel" and "daneel " (trailing space)
**Expected:** both return the same results
**Refs:** crc-DB.md, seq-search.md, R178

## Test: regex filter AND
**Purpose:** WithRegexFilter keeps only chunks matching all patterns
**Input:** add files with chunks "alpha beta", "alpha gamma", "alpha beta gamma". Search "alpha" with WithRegexFilter("beta", "gamma")
**Expected:** only "alpha beta gamma" chunk survives — must match both "beta" AND "gamma"
**Refs:** crc-DB.md, seq-search.md, R183, R185, R188, R189

## Test: except-regex subtract
**Purpose:** WithExceptRegex rejects chunks matching any pattern
**Input:** add files with chunks "@status: open task", "@status: completed task". Search "task" with `WithExceptRegex("@status:.*completed")`
**Expected:** only "@status: open task" survives — "completed" chunk is subtracted
**Refs:** crc-DB.md, seq-search.md, R184, R188, R189

## Test: regex filter with SearchRegex
**Purpose:** post-filters work on regex search too
**Input:** add files with chunks "alpha beta", "alpha gamma". SearchRegex("alpha") with WithExceptRegex("gamma")
**Expected:** only "alpha beta" survives
**Refs:** crc-DB.md, seq-search.md, R189, R190

## Test: regex filter bad pattern returns error
**Purpose:** compilation failure is a normal error
**Input:** Search "test" with WithRegexFilter("[invalid")
**Expected:** returns non-nil error
**Refs:** crc-DB.md, R186

## Test: regex filter combined with verify
**Purpose:** verify and regex post-filters both apply
**Input:** add file with "alpha beta gamma". Search "alpha" with WithVerify() and WithExceptRegex("gamma")
**Expected:** no results — verify passes but except-regex rejects
**Refs:** crc-DB.md, seq-search.md, R188

## Test: get chunks target only
**Purpose:** retrieve a single chunk by range label
**Input:** add a multi-line file with LineChunkFunc, GetChunks(fpath, "3-3", 0, 0)
**Expected:** returns 1 ChunkResult with correct path, range "3-3", content matching line 3, index 2
**Refs:** crc-DB.md, seq-chunks.md, R197, R198, R201

## Test: get chunks with neighbors
**Purpose:** retrieve target plus positional neighbors
**Input:** add a 5-line file, GetChunks(fpath, "3-3", 1, 1)
**Expected:** returns 3 ChunkResults with indices 1,2,3 (ranges "2-2","3-3","4-4"), in order
**Refs:** crc-DB.md, seq-chunks.md, R197, R199, R202

## Test: get chunks window clamped at boundaries
**Purpose:** before/after clamped to file bounds
**Input:** add a 5-line file, GetChunks(fpath, "1-1", 3, 0)
**Expected:** returns 1 ChunkResult (index 0) — can't go before first chunk
**Refs:** crc-DB.md, seq-chunks.md, R199

## Test: get chunks range not found
**Purpose:** error on missing range label
**Input:** add a file, GetChunks(fpath, "999-999", 0, 0)
**Expected:** returns error
**Refs:** crc-DB.md, seq-chunks.md, R203

## Test: get chunks after a chunker rule change matches by range
**Purpose:** a window entry gets the re-chunked chunk with its own range, never the one at its position; an entry the new rules no longer produce is reported missing rather than substituted or dropped
**Input:** index a 5-line file ("a","DROP","c","d","e") with the line chunker, then re-register the strategy with a chunker that skips the DROP line; GetChunks(fpath, "4-4", 0, 0) and GetChunks(fpath, "3-3", 1, 1)
**Expected:** "4-4" returns content "d\n" at index 3 (the positional match returned "e\n"); the window around "3-3" returns three results in F-record order — "2-2" with Missing set, empty content, index 1; "3-3" "c\n"; "4-4" "d\n"
**Refs:** crc-DB.md, seq-chunks.md, R701, R702, R703
**Code:** db_test.go
**Fire alarm:** restore the positional match in the streaming path (take the yielded chunk at the window's position instead of the one whose Range matches); red is "4-4" returning "e\n" and "2-2" carrying "c\n" instead of Missing
**Inject:** db.go:windowMatcher.yield
**Pulled:** 2026-10-24 — rang: "4-4" got "e\n"; the tmp test also red (shared matcher)

## Test: ChunkCache GetChunks reports a stale entry as missing
**Purpose:** the cached window keeps its shape — a window entry with no cached chunk becomes a Missing result instead of being skipped
**Input:** same file and chunker swap as above; NewChunkCache().GetChunks(fpath, "3-3", 1, 1)
**Expected:** three results; "2-2" has Missing set and empty content; "3-3" and "4-4" carry "c\n" and "d\n"
**Refs:** crc-ChunkCache.md, seq-chunker-dispatch.md, R702
**Code:** db_test.go
**Fire alarm:** put back the `continue` on a byRange miss in ChunkCache.GetChunks; red is two results instead of three
**Inject:** cache.go:ChunkCache.GetChunks
**Pulled:** 2026-10-24 — rang: expected 3 chunks, got 2

## Test: a missing chunk keeps its stored attrs
**Purpose:** a Missing result carries the F-record chunk's stored Attrs on both the DB streaming path and ChunkCache, so a caller can still label a stale chunk
**Input:** index the 5-line file with a line chunker that tags each chunk `text=<line>`, swap in the DROP-skipping chunker; GetChunks(fpath, "3-3", 1, 1) via DB and via NewChunkCache
**Expected:** on both, the first result is Missing with Attrs `text=DROP`
**Refs:** crc-DB.md, crc-ChunkCache.md, R702
**Code:** db_test.go
**Fire alarm:** clear Attrs on the Missing result (DB), or omit streamAttrs from the cache's Missing result; red is "missing chunk should carry its stored attrs", got []
**Inject:** db.go:windowMatcher.results, cache.go:ChunkCache.GetChunks
**Pulled:** 2026-10-24 — rang on both sites

## Test: streaming retrieval stops once the window is found
**Purpose:** matching by range must not cost a read to end of file — the chunker is stopped once every window range has been yielded
**Input:** index a 5-line file, swap in a line chunker that counts yields; GetChunks(fpath, "2-2", 0, 0)
**Expected:** exactly 2 yields
**Refs:** crc-DB.md, seq-chunks.md, R701
**Code:** db_test.go
**Fire alarm:** make windowMatcher.yield always return true; red is 5 yields instead of 2
**Inject:** db.go:windowMatcher.yield
**Pulled:** 2026-10-24 — rang: got 5

## Test: get chunks file not in database
**Purpose:** error on unknown file
**Input:** GetChunks("nonexistent.txt", "1-1", 0, 0)
**Expected:** returns error
**Refs:** crc-DB.md, seq-chunks.md, R203

## Test: add file already indexed returns ErrAlreadyIndexed
**Purpose:** dedup guard prevents duplicate fileids
**Input:** add a file, then AddFile same path again
**Expected:** second AddFile returns ErrAlreadyIndexed (errors.Is), file still searchable with original results (no duplication)
**Refs:** crc-DB.md, seq-add.md, R213, R214, R215, R216

## Test: chunk deduplication across files
**Purpose:** same chunk content in two files produces one C record
**Input:** create two files with identical line "hello world", add both
**Expected:** H record maps to one chunkid, C record has two fileids, search returns both files, T record has chunkid once
**Refs:** crc-DB.md, seq-add.md, R223, R224, R225

## Test: chunk dedup removal cleans orphaned records
**Purpose:** removing one file with shared chunks leaves the other intact
**Input:** add two files sharing a chunk, remove one
**Expected:** C record has one fileid remaining, search still finds the other file. Then remove the second file — C/H/T/W records deleted for orphaned chunk
**Refs:** crc-DB.md, R254, R231

## Test: CRecord marshal/unmarshal roundtrip
**Purpose:** record struct encode/decode
**Input:** create a CRecord with known trigrams, tokens, attrs, fileids, marshal, unmarshal
**Expected:** all fields match after roundtrip
**Refs:** crc-DB.md, R244, R252

## Test: FRecord marshal/unmarshal roundtrip
**Purpose:** record struct encode/decode
**Input:** create an FRecord with known metadata, names, chunks, token bag, marshal, unmarshal
**Expected:** all fields match after roundtrip
**Refs:** crc-DB.md, R245, R252

## Test: TRecord marshal/unmarshal roundtrip
**Purpose:** record struct encode/decode with varint chunkids
**Input:** create a TRecord with known chunkids, marshal, unmarshal
**Expected:** all chunkids match after roundtrip
**Refs:** crc-DB.md, R246, R252

## Test: I record data-in-key pattern
**Purpose:** settings stored as individual records
**Input:** create DB with case-insensitive and aliases, close, re-open, read settings
**Expected:** each setting readable independently; matches original values
**Refs:** crc-DB.md, seq-init.md, R17

## Test: chunk filter basic
**Purpose:** WithChunkFilter filters candidates before scoring
**Input:** add files with different content, search with a ChunkFilter that rejects chunks containing a specific fileid
**Expected:** results from rejected fileid are absent
**Refs:** crc-DB.md, seq-search.md, R255, R256

## Test: chunk filter AND accumulation
**Purpose:** multiple WithChunkFilter calls combine with AND
**Input:** search with two ChunkFilters, one that allows chunkids < 100, one that allows even chunkids
**Expected:** only even chunkids < 100 survive
**Refs:** crc-DB.md, R257

## Test: FilterByRatio on a one-chunk index
**Purpose:** ratio filtering cannot discriminate below two chunks, so the filter returns every trigram rather than dropping them all — and it does so independently of the cost clause
**Input:** call `FilterByRatio(0.50, 0)` directly with one TrigramCount of Count 1 and totalChunks 1; also drive it end-to-end via Search with WithTrigramFilter on a DB holding a single chunk. `minCount` is 0 deliberately, so the cost clause cannot be what rescues the trigram
**Expected:** the direct call returns the trigram unmodified; the search finds the chunk instead of returning zero results
**Refs:** crc-DB.md, seq-search.md, R674

## Test: FilterByRatio still discriminates at low ratios
**Purpose:** the degenerate-case guard must not become a floor — a rare trigram below the ratio is still skipped on a real corpus
**Input:** call `FilterByRatio(0.0, 0)` and `FilterByRatio(0.01, 0)` directly with a TrigramCount of Count 1 and totalChunks 50
**Expected:** both return empty — the trigram exceeds the ratio and is skipped, unaffected by the totalChunks < 2 guard
**Refs:** crc-DB.md, seq-search.md, R141, R674

## Test: FilterByRatio rescues cheap postings on a small corpus
**Purpose:** the cost clause is what distinguishes this rule from pure ratio filtering — a posting list too small to be worth avoiding is kept even though it dominates the corpus
**Input:** call `FilterByRatio(0.50, 32)` directly with a TrigramCount of Count 6 and totalChunks 10 (threshold 5, so the ratio clause alone would skip it)
**Expected:** the trigram is kept — it exceeds the ratio but has fewer than 32 postings, so it fails the cost clause and is not skipped
**Refs:** crc-DB.md, seq-search.md, R141, R675

## Test: FilterByRatio cost clause is inert above its reach
**Purpose:** `minCount` must not change verdicts once the corpus exceeds `minCount / maxRatio`, or it would be a second threshold rather than a floor on scan cost
**Input:** call `FilterByRatio(0.50, 32)` at totalChunks 200 — above the reach bound of 64 — across Counts 20, 50 and 150, which straddle both `minCount` and the threshold of 100
**Expected:** every verdict matches `FilterByRatio(0.50, 0)` on the same input (keep, keep, skip). The middle count is load-bearing: a cost clause applied in the wrong direction, skipping because `count >= minCount` rather than rescuing because `count < minCount`, diverges only there
**Refs:** crc-DB.md, seq-search.md, R675

## Test: FilterByRatio with minCount 0 or 1 is pure ratio filtering
**Purpose:** the generalization must be lossless — the old one-parameter semantics stays reachable inside the new signature
**Input:** call `FilterByRatio(0.50, 0)` and `FilterByRatio(0.50, 1)` across a table of (Count, totalChunks) pairs spanning both sides of the threshold
**Expected:** both agree with the pure ratio rule `count <= int(totalChunks × maxRatio)` on every pair, since a trigram past the threshold already has at least one posting
**Refs:** crc-DB.md, seq-search.md, R675

## Test: file-level token bag
**Purpose:** F record token bag is aggregated from chunks
**Input:** add a multi-line file, read FRecord
**Expected:** token bag contains all tokens from all chunks with summed counts
**Refs:** crc-DB.md, R237, R261

## Test: ScoreOverlap
**Purpose:** overlap scoring counts matching trigrams without normalization
**Input:** add files with varying overlap with query, search with WithOverlap()
**Expected:** scores are raw matching trigram counts (float64), higher for more overlap
**Refs:** crc-DB.md, R269, R270, R271

## Test: SearchMulti returns per-strategy results
**Purpose:** multi-strategy search collects candidates once, scores N ways
**Input:** add files, SearchMulti with coverage and overlap strategies, k=2
**Expected:** returns two MultiSearchResult entries (one per strategy), each with up to 2 results, same candidates may appear in both
**Refs:** crc-DB.md, seq-search-multi.md, R283, R285, R286, R287, R288

## Test: SearchMulti shared filters
**Purpose:** ChunkFilter and TrigramFilter applied once, shared across strategies
**Input:** SearchMulti with a ChunkFilter that rejects some chunks
**Expected:** rejected chunks absent from all strategies' results
**Refs:** crc-DB.md, seq-search-multi.md, R284, R289

## Test: BM25Func returns valid ScoreFunc
**Purpose:** BM25 convenience helper
**Input:** add files, call BM25Func with query trigrams, use returned ScoreFunc in Search
**Expected:** returns non-nil ScoreFunc, search produces scored results
**Refs:** crc-DB.md, R272, R274

## Test: I record counters maintained on add/remove
**Purpose:** totalTokens and totalChunks counters
**Input:** add file (N chunks, M total tokens), check counters. Remove file, check again.
**Expected:** after add: totalChunks = N, totalTokens = M. After remove: both 0.
**Refs:** crc-DB.md, R275, R276

## Test: I record counters maintained on append
**Purpose:** AppendChunks updates counters
**Input:** add file, then AppendChunks. Check totalChunks and totalTokens.
**Expected:** counters reflect the sum of original + appended chunks and tokens
**Refs:** crc-DB.md, R275, R276

## Test: WithProximityRerank reorders results
**Purpose:** proximity reranking adjusts scores by term closeness
**Input:** add chunks: one with "alpha beta" adjacent, one with "alpha ... many words ... beta". Search "alpha beta" with WithProximityRerank(10)
**Expected:** adjacent chunk ranks higher than distant chunk
**Refs:** crc-DB.md, R279, R280, R281, R282

## Test: Copy shares env but has nil caches
**Purpose:** Copy() returns a DB with shared env and nil caches
**Input:** create DB, add a file (populates pathCache), call Copy()
**Expected:** copy.env == original.env, copy.overlay == original.overlay, copy.chunkers == original.chunkers, copy.pathCache == nil, copy.pathToID == nil, copy.frecordCache == nil
**Refs:** crc-DB.md, R459, R460, R461, R462

## Test: Copy can perform index reads
**Purpose:** the copy is functional for indexing
**Input:** create DB, add a file, call Copy(), use copy to read F records
**Expected:** copy can open View txns and read records from the shared index
**Refs:** crc-DB.md, R459, R460

## Test: InvalidateCaches clears caches
**Purpose:** InvalidateCaches nils all three caches
**Input:** create DB, add a file, call FileIDPaths (populates caches), call InvalidateCaches()
**Expected:** pathCache == nil, pathToID == nil, frecordCache == nil. Next FileIDPaths call re-populates from the index
**Refs:** crc-DB.md, R463, R464

## Test: AddFile with ChunkCallback
**Purpose:** callback receives clean chunk text during AddFile
**Input:** create DB, add a multi-chunk file with WithChunkCallback that appends texts to a slice
**Expected:** slice contains one entry per chunk, in chunk order, matching chunk content
**Refs:** crc-DB.md, seq-add.md, R469, R470, R473, R474, R477

## Test: AddFile without ChunkCallback
**Purpose:** nil callback has zero overhead
**Input:** create DB, add file with no IndexOption
**Expected:** file added successfully, same as before (backward compatible)
**Refs:** crc-DB.md, R475, R484

## Test: AddFileWithContent with ChunkCallback
**Purpose:** callback works on WithContent variant
**Input:** create DB, add file with WithChunkCallback, capture texts
**Expected:** callback fires, content also returned as second value
**Refs:** crc-DB.md, R478

## Test: AppendChunks with WithAppendChunkCallback
**Purpose:** callback fires for appended chunks only
**Input:** create DB, add file (3 chunks), append content (2 chunks) with WithAppendChunkCallback
**Expected:** callback slice has exactly 2 entries matching appended chunk content
**Refs:** crc-DB.md, seq-append.md, R471, R482

## Test: RefreshStale with ChunkCallback
**Purpose:** callback fires during stale file reindex
**Input:** create DB, add file, modify file on disk, call RefreshStale with WithChunkCallback
**Expected:** callback fires for each chunk of the reindexed file
**Refs:** crc-DB.md, R479

## Test: PrepareFile plus StorePrepared equals AddFile
**Purpose:** the two-phase add produces the same index as the fused AddFile (R676, R680, R684)
**Input:** two DBs from the same fixture; on one call AddFile(path, strategy); on the other call StorePrepared(PrepareFile(path, strategy)). Compare RecordCounts per prefix and a search across both
**Expected:** identical RecordCounts per prefix and identical search results
**Refs:** crc-DB.md, seq-prepare-store.md#1, seq-prepare-store.md#2, R676, R680, R684
**Code:** db_test.go
**Fire alarm:** in StorePrepared, drop the last prepared chunk before addFileInTxn — the split DB has one fewer C/F chunk than the AddFile DB and the RecordCounts comparison goes red
**Inject:** db.go:StorePrepared
**Pulled:** 2026-09-17 — rang (AddFile=[45 393 585] vs split=[32 312 457])

## Test: PrepareContent plus StorePrepared indexes caller bytes
**Purpose:** compute from caller-supplied bytes, not disk (R677)
**Input:** call StorePrepared(PrepareContent(name, strategy, []byte(content))) where the content is not present on disk at name; search for a substring
**Expected:** the content is indexed and searchable although no file was read from disk
**Refs:** crc-DB.md, seq-prepare-store.md#1, R677
**Code:** db_test.go

## Test: PrepareFile opens no write transaction
**Purpose:** compute is transaction-free — it writes no records (R678)
**Input:** create DB, add one file to populate it, snapshot RecordCounts; call PrepareFile(path, strategy) and discard the handle; snapshot RecordCounts again
**Expected:** RecordCounts is unchanged across the PrepareFile call — no fileid allocated, no records written
**Refs:** crc-DB.md, seq-prepare-store.md#1, R678
**Code:** db_test.go
**Fire alarm:** make PrepareFile allocate a fileid or write the N chain (a stray write) — the post-call RecordCounts differ and the assertion goes red
**Inject:** db.go:PrepareFile
**Pulled:** 2026-09-17 — rang (RecordCounts 22→23 after PrepareFile)

## Test: ReindexPrepared preserves chunkids for unchanged content
**Purpose:** the two-phase reindex is a content diff — unchanged content keeps its chunkid, and the removal set is derived in-txn (R681, R682)
**Input:** add a 3-line file; record the chunkid of line 2. Edit line 1 only; ReindexPrepared(PrepareFile(editedPath, strategy)). Re-read line 2's chunkid
**Expected:** line 2's chunkid is unchanged (dedup hit under the fresh fileid); line 1's old chunk orphan-cascades; line 1's new content gets a fresh chunkid
**Refs:** crc-DB.md, seq-prepare-store.md#3, R681, R682, R673
**Code:** db_test.go
**Fire alarm:** reverse the R673 order in reindexPrepared — drop the old fileid's occurrences before storing the new chunks — so an unchanged line's chunk loses its last reference and orphans before the re-add, forcing a fresh chunkid; the chunkid-stability assertion goes red
**Inject:** db.go:reindexPrepared
**Pulled:** 2026-09-17 — rang (line 1 chunkid 1→4, line 3 chunkid 3→6)

## Test: PrepareAppend plus AppendPrepared equals AppendChunks
**Purpose:** the two-phase append produces the same index as the fused AppendChunks (R685, R687, R689)
**Input:** two DBs with the same base file; on one call AppendChunks(fileid, content, strategy); on the other call AppendPrepared(fileid, PrepareAppend(path, lastLocator, content, strategy)) with lastLocator read from the base file's F record. Compare RecordCounts and search
**Expected:** identical chunk lists, RecordCounts, and search results
**Refs:** crc-DB.md, seq-prepare-store.md#4, seq-prepare-store.md#5, R685, R687, R689
**Code:** db_test.go

## Test: AppendPrepared refuses a moved tail
**Purpose:** the tail-locator guard rejects a prepared append computed against a stale tail (R688)
**Input:** add a file with a line strategy whose last line has no trailing newline (so an append replaces it). PrepareAppend against the current last locator. Before storing, mutate the file's tail with a real AppendChunks (moving the tail). Then call AppendPrepared with the stale handle
**Expected:** AppendPrepared returns ErrAppendTailMoved (errors.Is); the F record is left intact — the stale drop-and-replace is not applied
**Refs:** crc-DB.md, seq-prepare-store.md#5, R688
**Code:** db_test.go
**Fire alarm:** defeat the last-locator comparison in appendPrepared (guard it with `if false`, build-safe) — the stale append is applied against the moved tail; the errors.Is(ErrAppendTailMoved) assertion goes red (nil error)
**Inject:** db.go:appendPrepared
**Pulled:** 2026-10-25 — rang (expected ErrAppendTailMoved, got <nil>); re-pulled after appendPrepared lost its empty-append early return

## Test: PrepareAppend opens no write transaction
**Purpose:** append compute is transaction-free (R685)
**Input:** add a file; snapshot RecordCounts; call PrepareAppend(path, lastLocator, content, strategy) and discard the handle; snapshot again
**Expected:** RecordCounts unchanged across the call
**Refs:** crc-DB.md, seq-prepare-store.md#4, R685
**Code:** db_test.go
**Fire alarm:** make PrepareAppend open a write txn and touch a counter — the post-call RecordCounts differ; assertion red
**Inject:** db.go:PrepareAppend
**Pulled:** 2026-09-18 — rang (RecordCounts 22→23 after an injected PrepareAppend write); re-pulled after PrepareAppend was restructured to delegate to SplitAppend (R700)

## Test: a PreparedFile is single-use
**Purpose:** storing a handle twice is rejected rather than silently corrupting (R679)
**Input:** p := PrepareFile(path, strategy); StorePrepared(p) succeeds; call StorePrepared(p) again
**Expected:** the second store returns a non-nil error (handle already consumed); it does not write a second, malformed file
**Refs:** crc-DB.md, seq-prepare-store.md#2, R679
**Code:** db_test.go
**Fire alarm:** remove the consumed check in storePrepared — without it the second store hits ErrAlreadyIndexed (masking the guard), so the test asserts errors.Is(err, errPreparedConsumed) specifically and goes red on ErrAlreadyIndexed
**Inject:** db.go:storePrepared
**Pulled:** 2026-09-17 — rang (got "file already indexed", want errPreparedConsumed; test strengthened to the specific sentinel during this pull)

## Test: SplitFile hash equals sha256 of chunk content
**Purpose:** the split owns the authoritative content hash — SplitChunk.Hash is SHA-256 over Chunk.Content, the same value the store dedups by (R690, R693)
**Input:** SplitFile a multi-chunk fixture; for each returned SplitChunk compute sha256(sc.Chunk.Content) directly and compare to sc.Hash
**Expected:** every SplitChunk.Hash equals the independently computed sha256 of its Content
**Refs:** crc-DB.md, seq-prepare-store.md#6, R690, R693
**Code:** db_test.go
**Fire alarm:** in splitYield, hash the chunk's Range instead of its Content — the recomputed sha256(Content) no longer matches sc.Hash and the assertion goes red
**Inject:** db.go:splitYield
**Pulled:** 2026-09-17 — rang (chunk 2 Hash != sha256(Content))

## Test: Split plus PrepareChunk plus AssembleFile equals PrepareFile
**Purpose:** the per-chunk compute assembles the same `*PreparedFile` as the fused PrepareFile, so both store to an identical index (R690, R694, R695, R696, R699)
**Input:** two DBs from the same fixture. On one: StorePrepared(PrepareFile(path, strategy)). On the other: SplitFile(path, strategy), build a PreparedEntry per SplitChunk with Grams=PrepareChunk(sc.Chunk.Content), then StorePrepared(AssembleFile(path, strategy, entries, meta)). Compare RecordCounts per prefix and a search across both
**Expected:** identical RecordCounts per prefix and identical search results
**Refs:** crc-DB.md, seq-prepare-store.md#6, seq-prepare-store.md#8, R694, R695, R696, R699
**Code:** db_test.go
**Fire alarm:** in the fused-only entriesFromSplits, drop the trailing entry — the fused PrepareFile then has one fewer chunk than the manual Split+PrepareChunk+AssembleFile path, so the RecordCounts/search comparison goes red. (Injecting into the shared AssembleFile moves both sides equally and does NOT ring.)
**Inject:** db.go:entriesFromSplits
**Pulled:** 2026-09-17 — rang (PrepareFile search empty vs manual path); verified this drop also rings TestSplitAppendAssembleEqualsPrepareAppend

## Test: reused grams across identical content in two files index correctly
**Purpose:** a `*PreparedChunk` is content-pure and reusable — the same grams value assembled into two files with identical content produces a correct index, with positions taken from each split (R694, R698)
**Input:** two files A and B whose content shares an identical chunk (same bytes, different byte offset / Range in each). SplitFile both. Call PrepareChunk once on the shared chunk's Content and feed that single `*PreparedChunk` into the PreparedEntry for that chunk in BOTH files' AssembleFile calls. StorePrepared both. Search for a substring of the shared chunk and for a substring unique to each file's other chunks; also confirm the shared chunk dedups to one chunkid
**Expected:** the shared content is found in both files at each file's correct Range; the shared chunk resolves to a single chunkid (dedup); each file's non-shared chunks index independently — reusing the grams did not transplant positions
**Refs:** crc-DB.md, seq-prepare-store.md#6, R694, R698
**Code:** db_test.go
**Fire alarm:** add a Range field to `*PreparedChunk` and have collectedFromEntries cache/read it instead of entry.Chunk.Range — the grams token reused across two files carries file A's position into file B, so B's shared chunk resolves to A's Range and the position assertion goes red
**Inject:** db.go:collectedFromEntries, db.go:PreparedChunk
**Pulled:** 2026-09-17 — rang (shared chunk missing in B: A=1 B=0)

## Test: SplitFile and PrepareChunk open no write transaction
**Purpose:** the split and per-chunk-prepare steps are transaction-free — they write no records (R690, R694)
**Input:** create DB, add one file to populate it, snapshot RecordCounts; call SplitFile(path, strategy) and PrepareChunk on one of its chunks, discarding both results; snapshot RecordCounts again
**Expected:** RecordCounts is unchanged across the calls — no fileid allocated, no records written
**Refs:** crc-DB.md, seq-prepare-store.md#6, R690, R694
**Code:** db_test.go
**Fire alarm:** make PrepareChunk open a bbolt Update txn and Put a per-chunk (content-keyed) record — the post-call RecordCounts differ and the assertion goes red. The injection MUST be content-keyed: AddFile now composes through PrepareChunk, so a fixed-key stray write lands during the setup AddFile (before the baseline snapshot) and is masked.
**Inject:** db.go:PrepareChunk
**Pulled:** 2026-09-17 — rang (content-keyed stray Put; RecordCounts 24->25)

## Test: SplitAppend plus PrepareChunk plus AssembleAppend equals PrepareAppend
**Purpose:** the per-chunk append assembles the same `*PreparedAppend` as the fused PrepareAppend, so both store an identical append (R692, R697, R699)
**Input:** two DBs with the same base file. On one: AppendPrepared(fileid, PrepareAppend(path, lastLocator, content, strategy)). On the other: SplitAppend(path, lastLocator, content, strategy) -> entries via PrepareChunk per chunk -> AppendPrepared(fileid, AssembleAppend(path, strategy, entries, replacedLast, lastLocator)). lastLocator read from the base file's F record on both. Compare chunk lists, RecordCounts, and search
**Expected:** identical chunk lists, RecordCounts per prefix, and search results; both carry the same replacedLast into the store phase
**Refs:** crc-DB.md, seq-prepare-store.md#7, R692, R697, R699
**Code:** db_test.go
**Fire alarm:** in AssembleAppend, hardcode replacedLast=false — a prepared append that should replace the tail no longer does. Guarded by the replacedLast-exercising tests (TestAppendPreparedRefusesMovedTail plus the markdown/bracket append-aware tests), NOT by this equivalence test: the line strategy never sets replacedLast, and AssembleAppend is shared by both compared paths. This test's own chunk-count equivalence is guarded by the entriesFromSplits fused-path drop (verified).
**Inject:** db.go:AssembleAppend
**Pulled:** 2026-09-17 — rang (replacedLast=false -> TestAppendPreparedRefusesMovedTail + markdown/bracket object)

## Test: SplitAppend with base line offset
**Purpose:** SplitAppend honors WithBaseLine, offsetting line-based ranges exactly as PrepareAppend does — the per-chunk append path carries the base-line offset (R700). Also guards that PrepareAppend forwards its opts to SplitAppend, since the recordSig-based equivalence tests are blind to range labels (chunkids are content hashes, so a mis-ranged chunk has the same signature)
**Input:** add a 3-line file with the "line" strategy; read the last chunk's locator from the F record; call SplitAppend(path, lastLocator, "line4\nline5\n", "line", WithBaseLine(3)), then SplitAppend with no options, then PrepareAppend(..., WithBaseLine(3)) + AppendPrepared and read the stored F record
**Expected:** with WithBaseLine(3) the two returned SplitChunk ranges are "4-4" and "5-5"; with no options they are "1-1" and "2-2" (so the option carries the offset); and after PrepareAppend+AppendPrepared the stored F-record chunk Locations are "4-4" and "5-5"
**Refs:** crc-DB.md, seq-prepare-store.md#7, R700
**Code:** db_test.go
**Fire alarm:** two injections, each turns a "4-4" assertion red. (a) revert SplitAppend to drop its opts — `db.splitAppend(path, lastLocator, content, strategy, nil, 0)` — so the split ignores WithBaseLine and ranges come back "1-1"/"2-2". (b) drop the `opts...` forwarding in PrepareAppend's `db.SplitAppend(path, lastLocator, content, strategy)` call — the stored F-record ranges regress to "1-1"/"2-2" while recordSig equivalence stays green
**Inject:** db.go:SplitAppend, db.go:PrepareAppend
**Pulled:** 2026-09-18 — rang: (a) SplitAppend opts dropped -> split ranges and stored ranges both 1-1/2-2 (three assertions red); (b) PrepareAppend opts... forwarding dropped -> stored ranges 1-1/2-2 red while the SplitAppend-direct assertions stayed green, confirming the forwarding seam is guarded independently

## Test: fuzzy WithOnly applies before the top-k cut
**Purpose:** a file-restricted fuzzy search finds the restricted file's match even when more than k better matches lie outside the restriction (ark fuzzy-only-after-cut)
**Input:** one in-scope file "apple banana delta" and 60 out-of-scope files "apple banana cherry wNN"; SearchFuzzy("apple banana cherry", 50, WithOnly(in-scope fileid))
**Expected:** exactly one result, the in-scope file (the post-cut restriction returned none)
**Refs:** crc-DB.md, seq-fuzzy-trigram.md, R706, R707
**Code:** db_test.go
**Fire alarm:** skip Phase-1 admission for disk chunks (admit every tally entry), restoring the post-cut restriction; red is zero results
**Inject:** db.go:DB.SearchFuzzy
**Pulled:** 2026-10-24 — rang: got []

## Test: fuzzy WithExcept applies before the top-k cut
**Purpose:** excepting the files holding more than k better matches leaves the remaining match reachable
**Input:** the same corpus; SearchFuzzy("apple banana cherry", 50, WithExcept(the 60 out-of-scope fileids))
**Expected:** exactly one result, the in-scope file
**Refs:** crc-DB.md, seq-fuzzy-trigram.md, R706, R707
**Code:** db_test.go
**Fire alarm:** same injection as the WithOnly test; red is zero results
**Inject:** db.go:DB.SearchFuzzy
**Pulled:** 2026-10-24 — rang: got []

## Test: fuzzy WithExcept keeps a shared chunk's other file
**Purpose:** a chunk listed in an excepted file is still admitted when it also belongs to a file outside the set, so the restriction before the cut agrees with Phase 2
**Input:** files A and B with identical content "apple banana cherry" (one deduplicated chunk); SearchFuzzy("apple banana cherry", 10, WithExcept(A))
**Expected:** one result, for B
**Refs:** crc-DB.md, R705, R707
**Code:** db_test.go
**Fire alarm:** make admitDisk reject every chunk in an excepted file's chunk list without reading its C record; red is zero results
**Inject:** db.go:fileRestriction.admitDisk
**Pulled:** 2026-10-24 — rang: got []

## Test: fuzzy returns tmp:// hits
**Purpose:** an overlay chunk that wins a top-k place is scored and resolved to its tmp:// path instead of vanishing in Phase 2
**Input:** a disk file "humble master of disk" and AddTmpFile("tmp://s/doc", "humble master of tmp"); SearchFuzzy("humble master", 10)
**Expected:** results include tmp://s/doc 1-1 and the disk file
**Refs:** crc-DB.md, crc-Overlay.md, seq-fuzzy-trigram.md, R709
**Code:** db_test.go
**Fire alarm:** drop the overlay winners in Phase 2 (resolve disk winners only), the behavior before this change; red is no tmp://s/doc result
**Inject:** db.go:DB.SearchFuzzy
**Pulled:** 2026-10-24 — rang: disk.txt only

## Test: fuzzy WithOnly admits overlay chunks before the cut
**Purpose:** overlay chunks pass the same Phase-1 restriction as disk chunks, from their in-memory fileids
**Input:** one in-scope tmp doc "apple banana delta" and 60 out-of-scope tmp docs "apple banana cherry wNN"; SearchFuzzy("apple banana cherry", 50, WithOnly(in-scope tmp fileid))
**Expected:** exactly one result, the in-scope tmp doc
**Refs:** crc-Overlay.md, seq-fuzzy-trigram.md, R708
**Code:** db_test.go
**Fire alarm:** make overlay.admitLocked return true; red is zero results
**Inject:** overlay.go:overlay.admitLocked
**Pulled:** 2026-10-24 — rang: got []

## Test: fuzzy WithOnly minus WithExcept before the cut
**Purpose:** with both filters, Phase 1 admits only the chunks of files in only∖except — an excepted file named in WithOnly must not take a top-k place that Phase 2 would then empty
**Input:** the 1 + 60 corpus; SearchFuzzy("apple banana cherry", 50, WithOnly(all 61 fileids), WithExcept(the 60 out-of-scope fileids))
**Expected:** exactly one result, the in-scope file
**Refs:** crc-DB.md, seq-fuzzy-trigram.md, R707
**Code:** db_test.go
**Fire alarm:** build onlyChunks from every WithOnly file, ignoring WithExcept; red is zero results (found by probing past the list: Phase 2 still filters, so nothing else objects)
**Inject:** db.go:searchConfig.newFileRestriction
**Pulled:** 2026-10-24 — rang: got []

## Test: WithVerify is exact-search only
**Purpose:** verify confirms an exact match and must not overrule loose (any term) or fuzzy (resemblance)
**Input:** one file, line 1 "go to hell, yellow cat", line 2 "hello world"; Search("hello", WithVerify); Search("hello cat", WithLoose, WithVerify); SearchFuzzy("helo wrld", 10, WithVerify); SearchMulti("hello cat", coverage, 10, WithLoose, WithVerify)
**Expected:** exact: line 2 only (line 1 is the trigram false positive verify removes); loose: both lines; fuzzy: line 2 found; SearchMulti loose: both lines
**Refs:** crc-DB.md, seq-search.md, seq-search-multi.md, R124, R710, R424
**Code:** db_test.go
**Fire alarm:** drop the loose guard on verify in Search or in SearchMulti, or restore verify in SearchFuzzy; red is the loose case returning no lines, or fuzzy returning nothing
**Inject:** db.go:DB.Search, db.go:DB.SearchFuzzy, db.go:DB.SearchMulti
**Pulled:** 2026-10-24 — rang at all three sites (SearchMulti found by probing past the list: no test reached it)

## Test: chunker version stamped on index, kept on append
**Purpose:** the F record records the strategy's chunker version when a file is chunked, and an append leaves it alone
**Input:** register a VersionedChunker at version 3 and a plain chunker; AddFile one file with each; AppendChunks to the versioned file; StorePrepared a third file
**Expected:** FileInfoByID ChunkerVersion is 3, 0, 3 (after append), 3 (prepared store)
**Refs:** crc-DB.md, crc-Chunker.md, R714, R715, R716, R717
**Code:** db_test.go
**Fire alarm:** stop stamping in addFileInTxn; red is version 0 where 3 was expected
**Inject:** db.go:DB.addFileInTxn
**Pulled:** 2026-10-25 — rang: versioned AddFile ChunkerVersion = 0, want 3

## Test: chunker rule change makes files stale
**Purpose:** a version bump makes untouched files stale, and a refresh re-chunks and clears it
**Input:** a versioned chunker at version 1; AddFile; bump the chunker's version to 2 (re-register); CheckFile, StaleFiles, RefreshStale, CheckFile
**Expected:** fresh before the bump; stale after it from CheckFile and StaleFiles though the file is untouched; RefreshStale refreshes it; then fresh with ChunkerVersion 2. A lowered version (2 → 1) is stale too
**Refs:** crc-DB.md, seq-stale.md, R719
**Code:** db_test.go
**Fire alarm:** move the chunker-version check after the mod-time shortcut in classifyFile; red is "fresh" after the bump
**Inject:** db.go:DB.classifyFile
**Pulled:** 2026-10-25 — rang: after bump CheckFile fresh, want stale

## Test: append refused onto old-rule chunks
**Purpose:** an append onto a file chunked under another version refuses and writes nothing
**Input:** versioned chunker at version 1; AddFile; bump to 2; AppendChunks with non-empty content; also AppendPrepared with a PreparedAppend
**Expected:** errors.Is(err, ErrChunkerVersionStale) for both; F record unchanged (same chunks)
**Refs:** crc-DB.md, seq-prepare-store.md, R727
**Code:** db_test.go
**Fire alarm:** remove the chunkerStale check from appendPrepared; red is a nil error and an extra chunk
**Inject:** db.go:DB.appendPrepared
**Pulled:** 2026-10-25 — rang: AppendChunks err = nil, want ErrChunkerVersionStale; re-pulled after the empty-append early return was removed, rang again

## Test: an append with no chunks is refused onto old-rule chunks
**Purpose:** the version check is not skipped when the append yields no chunks — ark's chat chunker indexes nothing from a tool-call-only tail, and that append must still refuse on a version-stale file
**Input:** versioned chunker at version 1; AddFile; bump to 2; AppendPrepared with an empty PreparedAppend (AssembleAppend with no entries, no replace); AppendChunks with empty content
**Expected:** errors.Is(err, ErrChunkerVersionStale) for both; the F record's FileLength is unchanged
**Refs:** crc-DB.md, seq-prepare-store.md, seq-append.md, R727
**Code:** db_test.go
**Fire alarm:** restore the early return in appendPrepared for an append with no chunks and no replace; red is a nil error
**Inject:** db.go:DB.appendPrepared
**Pulled:** 2026-10-25 — rang: AppendPrepared with no chunks err = nil, want ErrChunkerVersionStale

## Test: an append with no chunks records the file's new state
**Purpose:** an append-aware chunker that indexes nothing from the new bytes still moves the F record's length, hash and modtime forward, so the file does not read stale by content and the next scan does not repeat the append from the old length
**Input:** an append-aware chunker whose AppendChunks yields nothing; AddFile; append bytes to the file on disk; AppendChunks with those bytes and WithModTime, WithContentHash, WithFileLength set to the file's new state; CheckFile
**Expected:** nil error; chunk list unchanged; FileLength, ContentHash and ModTime equal the new values; CheckFile reports fresh
**Refs:** crc-DB.md, seq-prepare-store.md, seq-append.md, R726, R728
**Code:** db_test.go
**Fire alarm:** skip the F record metadata update when the append has no chunks; red is the old FileLength and CheckFile stale
**Inject:** db.go:DB.appendPrepared
**Pulled:** 2026-10-25 — rang: F record not moved forward, length 11 want 21

## Test: Open refuses another format version
**Purpose:** a database in an older layout is refused rather than mis-read
**Input:** Create a DB, overwrite its version I record with "2", close, Open
**Expected:** Open errors and the error names "2"; a fresh Create reports Version() "3"
**Refs:** crc-DB.md, R718, R721
**Code:** db_test.go
**Fire alarm:** delete the version check in Open; red is Open succeeding
**Inject:** db.go:Open
**Pulled:** 2026-10-25 — rang: Open accepted format version 2

## Test: missing outranks chunker-version stale
**Purpose:** a deleted file reports missing even when its chunker version is behind, so RefreshStale skips it instead of reindexing a file that is gone
**Input:** versioned chunker at version 1; AddFile; bump to 2; delete the file; CheckFile; RefreshStale
**Expected:** status missing; RefreshStale returns no error
**Refs:** crc-DB.md, seq-stale.md, R719
**Code:** db_test.go
**Fire alarm:** move the chunkerStale check above the os.Stat in classifyFile; red is "stale" (found by probing past the list after simplification: the suite stayed green)
**Inject:** db.go:DB.classifyFile
**Pulled:** 2026-10-25 — rang: deleted file under a bumped chunker reported stale, want missing
