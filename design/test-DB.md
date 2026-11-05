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
**Expected:** search succeeds (T and C records written during add)
**Refs:** crc-DB.md, seq-search.md

## Test: remove file
**Purpose:** file deletion cleans records
**Input:** add file, remove file, search for its content
**Expected:** no results returned; C/H records deleted and D records written for orphaned chunks
**Refs:** crc-DB.md, R774

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
**Expected:** FRecord reflects updated hash, modTime, fileLength and appended chunk entries
**Refs:** crc-DB.md, seq-append.md, R769

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
**Alarm:** 1
**Fire alarm:** restore the positional match in the streaming path (take the yielded chunk at the window's position instead of the one whose Range matches); red is "4-4" returning "e\n" and "2-2" carrying "c\n" instead of Missing
**Inject:** db.go:windowMatcher.yield
**Pulled:** 2026-10-24 — rang: "4-4" got "e\n"; the tmp test also red (shared matcher)

## Test: ChunkCache GetChunks reports a stale entry as missing
**Purpose:** the cached window keeps its shape — a window entry with no cached chunk becomes a Missing result instead of being skipped
**Input:** same file and chunker swap as above; NewChunkCache().GetChunks(fpath, "3-3", 1, 1)
**Expected:** three results; "2-2" has Missing set and empty content; "3-3" and "4-4" carry "c\n" and "d\n"
**Refs:** crc-ChunkCache.md, seq-chunker-dispatch.md, R702
**Code:** db_test.go
**Alarm:** 2
**Fire alarm:** put back the `continue` on a byRange miss in ChunkCache.GetChunks; red is two results instead of three
**Inject:** cache.go:ChunkCache.GetChunks
**Pulled:** 2026-10-24 — rang: expected 3 chunks, got 2

## Test: a missing chunk keeps its stored attrs
**Purpose:** a Missing result carries the F-record chunk's stored Attrs on both the DB streaming path and ChunkCache, so a caller can still label a stale chunk
**Input:** index the 5-line file with a line chunker that tags each chunk `text=<line>`, swap in the DROP-skipping chunker; GetChunks(fpath, "3-3", 1, 1) via DB and via NewChunkCache
**Expected:** on both, the first result is Missing with Attrs `text=DROP`
**Refs:** crc-DB.md, crc-ChunkCache.md, R702
**Code:** db_test.go
**Alarm:** 3
**Fire alarm:** clear Attrs on the Missing result (DB), or omit streamAttrs from the cache's Missing result; red is "missing chunk should carry its stored attrs", got []
**Inject:** db.go:windowMatcher.results, cache.go:ChunkCache.GetChunks
**Pulled:** 2026-10-24 — rang on both sites

## Test: streaming retrieval stops once the window is found
**Purpose:** matching by range must not cost a read to end of file — the chunker is stopped once every window range has been yielded
**Input:** index a 5-line file, swap in a line chunker that counts yields; GetChunks(fpath, "2-2", 0, 0)
**Expected:** exactly 2 yields
**Refs:** crc-DB.md, seq-chunks.md, R701
**Code:** db_test.go
**Alarm:** 4
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
**Expected:** C record has one fileid remaining, search still finds the other file. Then remove the second file — C/H records deleted and a D record written for the orphaned chunk
**Refs:** crc-DB.md, R254, R231

## Test: CRecord marshal/unmarshal roundtrip
**Purpose:** record struct encode/decode
**Input:** create a CRecord with known distinctTokens, tokenTotal, attrs, fileids, marshal, unmarshal
**Expected:** all fields match after roundtrip
**Refs:** crc-DB.md, R772, R771, R735
**Code:** records_test.go

## Test: FRecord marshal/unmarshal roundtrip
**Purpose:** record struct encode/decode
**Input:** create an FRecord with known metadata, name and chunks, marshal, unmarshal
**Expected:** all fields match after roundtrip
**Refs:** crc-DB.md, R766, R735
**Code:** records_test.go

## Test: TRecord marshal/unmarshal roundtrip
**Purpose:** record struct encode/decode with varint chunkids
**Input:** create a TRecord with known chunkids, marshal, unmarshal
**Expected:** all chunkids match after roundtrip
**Refs:** crc-DB.md, R246, R735
**Code:** records_test.go

## Test: empty CRecord roundtrip
**Purpose:** a CRecord with zero token counts and no attrs or fileids survives encode/decode
**Input:** marshal and unmarshal a zero CRecord
**Expected:** zero counts and empty slices after roundtrip
**Refs:** crc-DB.md, R772, R735
**Code:** records_test.go

## Test: CRecord fileid inc/dec
**Purpose:** fileid occurrence counts are refcounted
**Input:** IncFileID 7 three times and 42 once; DecFileID 7 down to zero
**Expected:** counts 3 and 1; Dec at 3 keeps the entry; Dec at 1 removes it, leaving one entry
**Refs:** crc-DB.md, R596, R597
**Code:** records_test.go

## Test: CRecord refcount roundtrip
**Purpose:** `[fileid, count]` pairs survive encode/decode
**Input:** a CRecord with two fileids at different counts
**Expected:** both pairs match after roundtrip
**Refs:** crc-DB.md, R595
**Code:** records_test.go

## Test: FRecord locator roundtrip
**Purpose:** chunk entries keep ChunkID, Location and Locator, empty locators included
**Input:** an FRecord with three chunk entries, one with a nil locator
**Expected:** all three entries match after roundtrip
**Refs:** crc-DB.md, R251, R592
**Code:** records_test.go

## Test: byte-range locator
**Purpose:** a line-oriented chunker's byte-range locator encodes and decodes
**Input:** encode 100–250; decode it; decode a nil locator
**Expected:** 100–250 back; nil decodes with ok=false
**Refs:** crc-DB.md, R594
**Code:** records_test.go

## Test: key construction
**Purpose:** each record key has its prefix byte and expected length
**Input:** build C, F, H, I, T and W keys
**Expected:** correct prefix on each; H key 33 bytes, T key 4, W key 5; I key carries the name
**Refs:** crc-DB.md, R224, R227
**Code:** records_test.go

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
**Alarm:** 5
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
**Alarm:** 6
**Fire alarm:** make PrepareFile allocate a fileid or write the N chain (a stray write) — the post-call RecordCounts differ and the assertion goes red
**Inject:** db.go:PrepareFile
**Pulled:** 2026-09-17 — rang (RecordCounts 22→23 after PrepareFile)

## Test: ReindexPrepared preserves chunkids for unchanged content
**Purpose:** the two-phase reindex is a content diff — unchanged content keeps its chunkid, and the removal set is derived in-txn (R681, R682)
**Input:** add a 3-line file; record the chunkid of line 2. Edit line 1 only; ReindexPrepared(PrepareFile(editedPath, strategy)). Re-read line 2's chunkid
**Expected:** line 2's chunkid is unchanged (dedup hit under the fresh fileid); line 1's old chunk orphan-cascades; line 1's new content gets a fresh chunkid
**Refs:** crc-DB.md, seq-prepare-store.md#3, R681, R682, R673
**Code:** db_test.go
**Alarm:** 7
**Fire alarm:** reverse the R673 order in reindexPrepared — drop the old fileid's occurrences before storing the new chunks — so an unchanged line's chunk loses its last reference and orphans before the re-add, forcing a fresh chunkid; the chunkid-stability assertion goes red
**Inject:** db.go:reindexPrepared
@alarm-hash: af9fa204f41eead9
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved reindexPrepared: occurrences dropped before the re-add, TestReindexPreservesUnchangedChunkIDs red, `line 1 chunkid changed: was 1, now 4`; restored clean *Earlier —* 2026-11-05 — rang — old occurrences dropped before the new chunks are stored in reindexPrepared (after #219): TestReindexPreservesUnchangedChunkIDs red, "line 1 chunkid changed: was 1, now 4"; restored clean *Earlier —* 2026-10-25 — rang (line 1 chunkid 1→4, line 3 chunkid 3→6); first pulled 2026-09-17
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
**Alarm:** 8
**Fire alarm:** defeat the last-locator comparison in appendPrepared (guard it with `if false`, build-safe) — the stale append is applied against the moved tail; the errors.Is(ErrAppendTailMoved) assertion goes red (nil error)
**Inject:** db.go:appendPrepared
@alarm-hash: e51783b37c8a97bf
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved appendPrepared: last-locator check guarded with `if false`, TestAppendPreparedRefusesMovedTail red, `expected ErrAppendTailMoved, got <nil>`; restored clean *Earlier —* 2026-11-05 — rang — last-locator comparison guarded with `if false` in appendPrepared (after #219): TestAppendPreparedRefusesMovedTail red, "expected ErrAppendTailMoved, got <nil>"; restored clean *Earlier —* 2026-11-04 — rang — last-locator comparison guarded with `if false` in appendPrepared, after #218 removed the token-bag bookkeeping around it: TestAppendPreparedRefusesMovedTail red, "expected ErrAppendTailMoved, got <nil>"; restored clean *Earlier —* 2026-10-25 — rang (expected ErrAppendTailMoved, got <nil>); re-pulled after appendPrepared lost its empty-append early return
## Test: PrepareAppend opens no write transaction
**Purpose:** append compute is transaction-free (R685)
**Input:** add a file; snapshot RecordCounts; call PrepareAppend(path, lastLocator, content, strategy) and discard the handle; snapshot again
**Expected:** RecordCounts unchanged across the call
**Refs:** crc-DB.md, seq-prepare-store.md#4, R685
**Code:** db_test.go
**Alarm:** 9
**Fire alarm:** make PrepareAppend open a write txn and touch a counter — the post-call RecordCounts differ; assertion red
**Inject:** db.go:PrepareAppend
**Pulled:** 2026-09-18 — rang (RecordCounts 22→23 after an injected PrepareAppend write); re-pulled after PrepareAppend was restructured to delegate to SplitAppend (R700)

## Test: a PreparedFile is single-use
**Purpose:** storing a handle twice is rejected rather than silently corrupting (R679)
**Input:** p := PrepareFile(path, strategy); StorePrepared(p) succeeds; call StorePrepared(p) again
**Expected:** the second store returns a non-nil error (handle already consumed); it does not write a second, malformed file
**Refs:** crc-DB.md, seq-prepare-store.md#2, R679
**Code:** db_test.go
**Alarm:** 10
**Fire alarm:** remove the consumed check in storePrepared — without it the second store hits ErrAlreadyIndexed (masking the guard), so the test asserts errors.Is(err, errPreparedConsumed) specifically and goes red on ErrAlreadyIndexed
**Inject:** db.go:storePrepared
@alarm-hash: 53b3df9faa40890d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved storePrepared: consumed check removed, TestPreparedFileSingleUse red, `got file already indexed, want errPreparedConsumed`; restored clean *Earlier —* 2026-11-05 — rang — consumed check removed in storePrepared: TestPreparedFileSingleUse red, "second StorePrepared: got file already indexed, want errPreparedConsumed"; restored clean *Earlier —* 2026-10-25 — rang (got "file already indexed", want errPreparedConsumed); first pulled 2026-09-17, when the test was strengthened to the specific sentinel
## Test: SplitFile hash equals sha256 of chunk content
**Purpose:** the split owns the authoritative content hash — SplitChunk.Hash is SHA-256 over Chunk.Content, the same value the store dedups by (R690, R693)
**Input:** SplitFile a multi-chunk fixture; for each returned SplitChunk compute sha256(sc.Chunk.Content) directly and compare to sc.Hash
**Expected:** every SplitChunk.Hash equals the independently computed sha256 of its Content
**Refs:** crc-DB.md, seq-prepare-store.md#6, R690, R693
**Code:** db_test.go
**Alarm:** 11
**Fire alarm:** in splitYield, hash the chunk's Range instead of its Content — the recomputed sha256(Content) no longer matches sc.Hash and the assertion goes red
**Inject:** db.go:splitYield
**Pulled:** 2026-09-17 — rang (chunk 2 Hash != sha256(Content))

## Test: Split plus PrepareChunk plus AssembleFile equals PrepareFile
**Purpose:** the per-chunk compute assembles the same `*PreparedFile` as the fused PrepareFile, so both store to an identical index (R690, R694, R695, R696, R699)
**Input:** two DBs from the same fixture. On one: StorePrepared(PrepareFile(path, strategy)). On the other: SplitFile(path, strategy), build a PreparedEntry per SplitChunk with Grams=PrepareChunk(sc.Chunk.Content), then StorePrepared(AssembleFile(path, strategy, entries, meta)). Compare RecordCounts per prefix and a search across both
**Expected:** identical RecordCounts per prefix and identical search results
**Refs:** crc-DB.md, seq-prepare-store.md#6, seq-prepare-store.md#8, R694, R695, R696, R699
**Code:** db_test.go
**Alarm:** 12
**Fire alarm:** in the fused-only entriesFromSplits, drop the trailing entry — the fused PrepareFile then has one fewer chunk than the manual Split+PrepareChunk+AssembleFile path, so the RecordCounts/search comparison goes red. (Injecting into the shared AssembleFile moves both sides equally and does NOT ring.)
**Inject:** db.go:entriesFromSplits
**Pulled:** 2026-09-17 — rang (PrepareFile search empty vs manual path); verified this drop also rings TestSplitAppendAssembleEqualsPrepareAppend

## Test: reused grams across identical content in two files index correctly
**Purpose:** a `*PreparedChunk` is content-pure and reusable — the same grams value assembled into two files with identical content produces a correct index, with positions taken from each split (R694, R698)
**Input:** two files A and B whose content shares an identical chunk (same bytes, different byte offset / Range in each). SplitFile both. Call PrepareChunk once on the shared chunk's Content and feed that single `*PreparedChunk` into the PreparedEntry for that chunk in BOTH files' AssembleFile calls. StorePrepared both. Search for a substring of the shared chunk and for a substring unique to each file's other chunks; also confirm the shared chunk dedups to one chunkid
**Expected:** the shared content is found in both files at each file's correct Range; the shared chunk resolves to a single chunkid (dedup); each file's non-shared chunks index independently — reusing the grams did not transplant positions
**Refs:** crc-DB.md, seq-prepare-store.md#6, R694, R698
**Code:** db_test.go
**Alarm:** 13
**Fire alarm:** add a Range field to `*PreparedChunk` and have collectedFromEntries cache/read it instead of entry.Chunk.Range — the grams token reused across two files carries file A's position into file B, so B's shared chunk resolves to A's Range and the position assertion goes red
**Inject:** db.go:collectedFromEntries, db.go:PreparedChunk
**Pulled:** 2026-09-17 — rang (shared chunk missing in B: A=1 B=0)

## Test: SplitFile and PrepareChunk open no write transaction
**Purpose:** the split and per-chunk-prepare steps are transaction-free — they write no records (R690, R694)
**Input:** create DB, add one file to populate it, snapshot RecordCounts; call SplitFile(path, strategy) and PrepareChunk on one of its chunks, discarding both results; snapshot RecordCounts again
**Expected:** RecordCounts is unchanged across the calls — no fileid allocated, no records written
**Refs:** crc-DB.md, seq-prepare-store.md#6, R690, R694
**Code:** db_test.go
**Alarm:** 14
**Fire alarm:** make PrepareChunk open a bbolt Update txn and Put a per-chunk (content-keyed) record — the post-call RecordCounts differ and the assertion goes red. The injection MUST be content-keyed: AddFile now composes through PrepareChunk, so a fixed-key stray write lands during the setup AddFile (before the baseline snapshot) and is masked.
**Inject:** db.go:PrepareChunk
**Pulled:** 2026-09-17 — rang (content-keyed stray Put; RecordCounts 24->25)

## Test: SplitAppend plus PrepareChunk plus AssembleAppend equals PrepareAppend
**Purpose:** the per-chunk append assembles the same `*PreparedAppend` as the fused PrepareAppend, so both store an identical append (R692, R697, R699)
**Input:** two DBs with the same base file. On one: AppendPrepared(fileid, PrepareAppend(path, lastLocator, content, strategy)). On the other: SplitAppend(path, lastLocator, content, strategy) -> entries via PrepareChunk per chunk -> AppendPrepared(fileid, AssembleAppend(path, strategy, entries, replacedLast, lastLocator)). lastLocator read from the base file's F record on both. Compare chunk lists, RecordCounts, and search
**Expected:** identical chunk lists, RecordCounts per prefix, and search results; both carry the same replacedLast into the store phase
**Refs:** crc-DB.md, seq-prepare-store.md#7, R692, R697, R699
**Code:** db_test.go
**Alarm:** 15
**Fire alarm:** in AssembleAppend, hardcode replacedLast=false — a prepared append that should replace the tail no longer does. Guarded by the replacedLast-exercising tests (TestAppendPreparedRefusesMovedTail plus the markdown/bracket append-aware tests), NOT by this equivalence test: the line strategy never sets replacedLast, and AssembleAppend is shared by both compared paths. This test's own chunk-count equivalence is guarded by the entriesFromSplits fused-path drop (verified).
**Inject:** db.go:AssembleAppend
**Pulled:** 2026-09-17 — rang (replacedLast=false -> TestAppendPreparedRefusesMovedTail + markdown/bracket object)

## Test: SplitAppend with base line offset
**Purpose:** SplitAppend honors WithBaseLine, offsetting line-based ranges exactly as PrepareAppend does — the per-chunk append path carries the base-line offset (R700). Also guards that PrepareAppend forwards its opts to SplitAppend, since the recordSig-based equivalence tests are blind to range labels (chunkids are content hashes, so a mis-ranged chunk has the same signature)
**Input:** add a 3-line file with the "line" strategy; read the last chunk's locator from the F record; call SplitAppend(path, lastLocator, "line4\nline5\n", "line", WithBaseLine(3)), then SplitAppend with no options, then PrepareAppend(..., WithBaseLine(3)) + AppendPrepared and read the stored F record
**Expected:** with WithBaseLine(3) the two returned SplitChunk ranges are "4-4" and "5-5"; with no options they are "1-1" and "2-2" (so the option carries the offset); and after PrepareAppend+AppendPrepared the stored F-record chunk Locations are "4-4" and "5-5"
**Refs:** crc-DB.md, seq-prepare-store.md#7, R700
**Code:** db_test.go
**Alarm:** 16
**Fire alarm:** two injections, each turns a "4-4" assertion red. (a) revert SplitAppend to drop its opts — `db.splitAppend(path, lastLocator, content, strategy, nil, 0)` — so the split ignores WithBaseLine and ranges come back "1-1"/"2-2". (b) drop the `opts...` forwarding in PrepareAppend's `db.SplitAppend(path, lastLocator, content, strategy)` call — the stored F-record ranges regress to "1-1"/"2-2" while recordSig equivalence stays green
**Inject:** db.go:SplitAppend, db.go:PrepareAppend
**Pulled:** 2026-09-18 — rang: (a) SplitAppend opts dropped -> split ranges and stored ranges both 1-1/2-2 (three assertions red); (b) PrepareAppend opts... forwarding dropped -> stored ranges 1-1/2-2 red while the SplitAppend-direct assertions stayed green, confirming the forwarding seam is guarded independently

## Test: fuzzy WithOnly applies before the top-k cut
**Purpose:** a file-restricted fuzzy search finds the restricted file's match even when more than k better matches lie outside the restriction (ark fuzzy-only-after-cut)
**Input:** one in-scope file "apple banana delta" and 60 out-of-scope files "apple banana cherry wNN"; SearchFuzzy("apple banana cherry", 50, WithOnly(in-scope fileid))
**Expected:** exactly one result, the in-scope file (the post-cut restriction returned none)
**Refs:** crc-DB.md, seq-fuzzy-trigram.md, R706, R707
**Code:** db_test.go
**Alarm:** 17
**Fire alarm:** skip Phase-1 admission for disk chunks (admit every tally entry), restoring the post-cut restriction; red is zero results
**Inject:** db.go:DB.SearchFuzzy
@alarm-hash: 0a7ace2d8599f566
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved SearchFuzzy: the disk restriction bypassed before the cut, TestSearchFuzzyOnlyBeforeCut red, `expected exactly the in.txt match, got []`; restored clean *Earlier —* 2026-11-05 — rang — every tally entry admitted in SearchFuzzy phase 1 (after #219 moved the tally onto the posting reader): TestSearchFuzzyOnlyBeforeCut red, "expected exactly the in.txt match, got []"; restored clean *Earlier —* 2026-10-24 — rang: got []
## Test: fuzzy WithExcept applies before the top-k cut
**Purpose:** excepting the files holding more than k better matches leaves the remaining match reachable
**Input:** the same corpus; SearchFuzzy("apple banana cherry", 50, WithExcept(the 60 out-of-scope fileids))
**Expected:** exactly one result, the in-scope file
**Refs:** crc-DB.md, seq-fuzzy-trigram.md, R706, R707
**Code:** db_test.go
**Alarm:** 18
**Fire alarm:** same injection as the WithOnly test; red is zero results
**Inject:** db.go:DB.SearchFuzzy
@alarm-hash: 0a7ace2d8599f566
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved SearchFuzzy: the same bypass, TestSearchFuzzyExceptBeforeCut red, `expected exactly the in.txt match, got []`; restored clean *Earlier —* 2026-11-05 — rang — the same injection: TestSearchFuzzyExceptBeforeCut red, "expected exactly the in.txt match, got []"; restored clean *Earlier —* 2026-10-24 — rang: got []
## Test: fuzzy WithExcept keeps a shared chunk's other file
**Purpose:** a chunk listed in an excepted file is still admitted when it also belongs to a file outside the set, so the restriction before the cut agrees with Phase 2
**Input:** files A and B with identical content "apple banana cherry" (one deduplicated chunk); SearchFuzzy("apple banana cherry", 10, WithExcept(A))
**Expected:** one result, for B
**Refs:** crc-DB.md, R705, R707
**Code:** db_test.go
**Alarm:** 19
**Fire alarm:** make admitDisk reject every chunk in an excepted file's chunk list without reading its C record; red is zero results
**Inject:** db.go:fileRestriction.admitDisk
**Pulled:** 2026-10-24 — rang: got []

## Test: fuzzy returns tmp:// hits
**Purpose:** an overlay chunk that wins a top-k place is scored and resolved to its tmp:// path instead of vanishing in Phase 2
**Input:** a disk file "humble master of disk" and AddTmpFile("tmp://s/doc", "humble master of tmp"); SearchFuzzy("humble master", 10)
**Expected:** results include tmp://s/doc 1-1 and the disk file
**Refs:** crc-DB.md, crc-Overlay.md, seq-fuzzy-trigram.md, R709
**Code:** db_test.go
**Alarm:** 20
**Fire alarm:** drop the overlay winners in Phase 2 (resolve disk winners only), the behavior before this change; red is no tmp://s/doc result
**Inject:** db.go:DB.SearchFuzzy
@alarm-hash: 0a7ace2d8599f566
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved SearchFuzzy: tmp:// resolve dropped, TestSearchFuzzyReturnsTmpHits red, only disk.txt returned; restored clean *Earlier —* 2026-11-05 — rang — overlay winners dropped in SearchFuzzy phase 2: TestSearchFuzzyReturnsTmpHits red, no tmp://s/doc result; restored clean *Earlier —* 2026-10-24 — rang: disk.txt only
## Test: fuzzy WithOnly admits overlay chunks before the cut
**Purpose:** overlay chunks pass the same Phase-1 restriction as disk chunks, from their in-memory fileids
**Input:** one in-scope tmp doc "apple banana delta" and 60 out-of-scope tmp docs "apple banana cherry wNN"; SearchFuzzy("apple banana cherry", 50, WithOnly(in-scope tmp fileid))
**Expected:** exactly one result, the in-scope tmp doc
**Refs:** crc-Overlay.md, seq-fuzzy-trigram.md, R708
**Code:** db_test.go
**Alarm:** 21
**Fire alarm:** make overlay.admitLocked return true; red is zero results
**Inject:** overlay.go:overlay.admitLocked
**Pulled:** 2026-10-24 — rang: got []

## Test: fuzzy WithOnly minus WithExcept before the cut
**Purpose:** with both filters, Phase 1 admits only the chunks of files in only∖except — an excepted file named in WithOnly must not take a top-k place that Phase 2 would then empty
**Input:** the 1 + 60 corpus; SearchFuzzy("apple banana cherry", 50, WithOnly(all 61 fileids), WithExcept(the 60 out-of-scope fileids))
**Expected:** exactly one result, the in-scope file
**Refs:** crc-DB.md, seq-fuzzy-trigram.md, R707
**Code:** db_test.go
**Alarm:** 22
**Fire alarm:** build onlyChunks from every WithOnly file, ignoring WithExcept; red is zero results (found by probing past the list: Phase 2 still filters, so nothing else objects)
**Inject:** db.go:searchConfig.newFileRestriction
**Pulled:** 2026-10-24 — rang: got []

## Test: WithVerify is exact-search only
**Purpose:** verify confirms an exact match and must not overrule loose (any term) or fuzzy (resemblance)
**Input:** one file, line 1 "go to hell, yellow cat", line 2 "hello world"; Search("hello", WithVerify); Search("hello cat", WithLoose, WithVerify); SearchFuzzy("helo wrld", 10, WithVerify); SearchMulti("hello cat", coverage, 10, WithLoose, WithVerify)
**Expected:** exact: line 2 only (line 1 is the trigram false positive verify removes); loose: both lines; fuzzy: line 2 found; SearchMulti loose: both lines
**Refs:** crc-DB.md, seq-search.md, seq-search-multi.md, R124, R710, R424
**Code:** db_test.go
**Alarm:** 23
**Fire alarm:** drop the loose guard on verify in Search or in SearchMulti, or restore verify in SearchFuzzy; red is the loose case returning no lines, or fuzzy returning nothing
**Inject:** db.go:DB.Search, db.go:DB.SearchFuzzy, db.go:DB.SearchMulti
@alarm-hash: 97833bb1cb483f68
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved Search, SearchMulti and SearchFuzzy, all three variants: the loose guard dropped in SearchTerms and in SearchMulti, and verify restored in SearchFuzzy; TestWithVerifyExactSearchOnly red each time (`loose + verify: want both lines`, `SearchMulti loose + verify`, `fuzzy + verify: want 2-2 among typo hits`); restored clean *Earlier —* 2026-11-05 — rang — loose guard on verify dropped in Search: TestWithVerifyExactSearchOnly red, "loose + verify: want both lines (verify ignored), got []"; restored clean *Earlier —* 2026-10-24 — rang at all three sites (SearchMulti found by probing past the list: no test reached it)
## Test: chunker version stamped on index, kept on append
**Purpose:** the F record records the strategy's chunker version when a file is chunked, and an append leaves it alone
**Input:** register a VersionedChunker at version 3 and a plain chunker; AddFile one file with each; AppendChunks to the versioned file; StorePrepared a third file
**Expected:** FileInfoByID ChunkerVersion is 3, 0, 3 (after append), 3 (prepared store)
**Refs:** crc-DB.md, crc-Chunker.md, R714, R715, R716, R765
**Code:** db_test.go
**Alarm:** 24
**Fire alarm:** stop stamping in addFileInTxn; red is version 0 where 3 was expected
**Inject:** db.go:DB.addFileInTxn
@alarm-hash: 389251aeaddbe79c
**Pulled:** 2026-11-04 — rang — addFileInTxn stamps ChunkerVersion 0, after #218 changed the struct literal: TestChunkerVersionStamped red, "ChunkerVersion = 0, want 3"; restored clean *Earlier —* 2026-11-04 — rang (re-pulled for the one-name record, 2026-11-04): ChunkerVersion 0, want 3 *Earlier —* 2026-10-25 — rang: versioned AddFile ChunkerVersion = 0, want 3

## Test: chunker rule change makes files stale
**Purpose:** a version bump makes untouched files stale, and a refresh re-chunks and clears it
**Input:** a versioned chunker at version 1; AddFile; bump the chunker's version to 2 (re-register); CheckFile, StaleFiles, RefreshStale, CheckFile
**Expected:** fresh before the bump; stale after it from CheckFile and StaleFiles though the file is untouched; RefreshStale refreshes it; then fresh with ChunkerVersion 2. A lowered version (2 → 1) is stale too
**Refs:** crc-DB.md, seq-stale.md, R719
**Code:** db_test.go
**Alarm:** 25
**Fire alarm:** move the chunker-version check after the mod-time shortcut in classifyFile; red is "fresh" after the bump
**Inject:** db.go:DB.classifyFile
@alarm-hash: 34a78cc1f82ca562
**Pulled:** 2026-11-04 — rang (re-pulled for the one-name record, 2026-11-04): after the bump CheckFile said fresh, want stale *Earlier —* 2026-10-25 — rang: after bump CheckFile fresh, want stale

## Test: append refused onto old-rule chunks
**Purpose:** an append onto a file chunked under another version refuses and writes nothing
**Input:** versioned chunker at version 1; AddFile; bump to 2; AppendChunks with non-empty content; also AppendPrepared with a PreparedAppend
**Expected:** errors.Is(err, ErrChunkerVersionStale) for both; F record unchanged (same chunks)
**Refs:** crc-DB.md, seq-prepare-store.md, R727
**Code:** db_test.go
**Alarm:** 26
**Fire alarm:** remove the chunkerStale check from appendPrepared; red is a nil error and an extra chunk
**Inject:** db.go:DB.appendPrepared
@alarm-hash: e51783b37c8a97bf
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved appendPrepared: chunkerStale refusal removed, TestChunkerVersionAppendRefused red, `err = <nil>, want ErrChunkerVersionStale`; restored clean *Earlier —* 2026-11-05 — rang — chunkerStale check removed from appendPrepared: TestChunkerVersionAppendRefused red, "err = <nil>, want ErrChunkerVersionStale"; restored clean *Earlier —* 2026-11-04 — rang — chunkerStale check disabled in appendPrepared: TestChunkerVersionAppendRefused red, "err = <nil>, want ErrChunkerVersionStale"; restored clean *Earlier —* 2026-10-25 — rang: AppendChunks err = nil, want ErrChunkerVersionStale; re-pulled after the empty-append early return was removed, rang again
## Test: an append with no chunks is refused onto old-rule chunks
**Purpose:** the version check is not skipped when the append yields no chunks — ark's chat chunker indexes nothing from a tool-call-only tail, and that append must still refuse on a version-stale file
**Input:** versioned chunker at version 1; AddFile; bump to 2; AppendPrepared with an empty PreparedAppend (AssembleAppend with no entries, no replace); AppendChunks with empty content
**Expected:** errors.Is(err, ErrChunkerVersionStale) for both; the F record's FileLength is unchanged
**Refs:** crc-DB.md, seq-prepare-store.md, seq-append.md, R727
**Code:** db_test.go
**Alarm:** 27
**Fire alarm:** restore the early return in appendPrepared for an append with no chunks and no replace; red is a nil error
**Inject:** db.go:DB.appendPrepared
@alarm-hash: e51783b37c8a97bf
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved appendPrepared: an early return for an append with no chunks, TestChunkerVersionEmptyAppendRefused red, `err = <nil>, want ErrChunkerVersionStale`; restored clean *Earlier —* 2026-11-05 — rang — early return restored for an append with no chunks and no replace: TestChunkerVersionEmptyAppendRefused red, nil error; restored clean *Earlier —* 2026-11-04 — rang — early return for an append with no chunks and no replace: TestChunkerVersionEmptyAppendRefused red, "AppendPrepared with no chunks: err = <nil>, want ErrChunkerVersionStale"; restored clean *Earlier —* 2026-10-25 — rang: AppendPrepared with no chunks err = nil, want ErrChunkerVersionStale
## Test: an append with no chunks records the file's new state
**Purpose:** an append-aware chunker that indexes nothing from the new bytes still moves the F record's length, hash and modtime forward, so the file does not read stale by content and the next scan does not repeat the append from the old length
**Input:** an append-aware chunker whose AppendChunks yields nothing; AddFile; append bytes to the file on disk; AppendChunks with those bytes and WithModTime, WithContentHash, WithFileLength set to the file's new state; CheckFile
**Expected:** nil error; chunk list unchanged; FileLength, ContentHash and ModTime equal the new values; CheckFile reports fresh
**Refs:** crc-DB.md, seq-prepare-store.md, seq-append.md, R726, R728
**Code:** db_test.go
**Alarm:** 28
**Fire alarm:** skip the F record metadata update when the append has no chunks; red is the old FileLength and CheckFile stale
**Inject:** db.go:DB.appendPrepared
@alarm-hash: e51783b37c8a97bf
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved appendPrepared: return before the F record update when there are no chunks, TestAppendNoChunksRecordsFileState red, `length 11 (want 21)`; restored clean *Earlier —* 2026-11-05 — rang — F record metadata update skipped for an append with no chunks: TestAppendNoChunksRecordsFileState red, "length 11 (want 21)"; restored clean *Earlier —* 2026-11-04 — rang — F record metadata update skipped when the append has no chunks: TestAppendNoChunksRecordsFileState red, "F record not moved forward: length 11 (want 21)"; restored clean *Earlier —* 2026-10-25 — rang: F record not moved forward, length 11 want 21
## Test: Open refuses another format version
**Purpose:** a database in an older layout is refused rather than mis-read
**Input:** Create a DB, overwrite its version I record with "3", close, Open
**Expected:** Open errors and the error names "3"; a fresh Create reports Version() "7"
**Refs:** crc-DB.md, R797, R798
**Code:** db_test.go
**Alarm:** 29
**Fire alarm:** delete the version check in Open; red is Open succeeding
**Inject:** db.go:Open
@alarm-hash: bd57c86c552aa771
**Pulled:** 2026-11-05 — rang — version check in Open disabled (format "6"): TestOpenRefusesOtherFormat red, "Open accepted format version 4"; restored clean *Earlier —* 2026-11-04 — rang — version check in Open disabled: TestOpenRefusesOtherFormat red, "Open accepted format version 4" (the message read 3 until the test text was corrected the same day); restored clean *Earlier —* 2026-11-04 — rang (re-pulled for format 4, the test now writing "3"): Open accepted format version 3 *Earlier —* 2026-11-04 — rang (re-pulled for format 4, the test now writing "3"): Open accepted the older format *Earlier —* 2026-10-25 — rang: Open accepted format version 2

## Test: Open's format refusal is typed
**Purpose:** R751: a caller can recognize the refusal with `errors.As` and read the stored and expected versions from it
**Input:** the same database as the test above (version I record overwritten with "2"), Open
**Expected:** `errors.As(err, *FormatVersionError)` holds, with Stored "2" and Want the current format
**Refs:** crc-DB.md, R751
**Code:** db_test.go
**Alarm:** 38
**Fire alarm:** go back to the untyped error the check returned before, `fmt.Errorf("open db %q: format version %q, want %q", ...)`; red names the error as not a `*FormatVersionError`
**Inject:** db.go:Open
@alarm-hash: bd57c86c552aa771
**Pulled:** 2026-11-05 — rang — refusal returned as the old untyped fmt.Errorf: TestOpenRefusesOtherFormat red, "Open error is not a `*FormatVersionError`"; restored clean *Earlier —* 2026-11-04 — rang — refusal returned as the old untyped fmt.Errorf: TestOpenRefusesOtherFormat red, "Open error is not a `*FormatVersionError`"; restored clean *Earlier —* 2026-11-04 — rang (re-pulled for format 4): the error was not a `*FormatVersionError` (format version "3", want "4") *Earlier —* 2026-11-04 — rang (re-pulled for format 4): the error was not a `*FormatVersionError` (format version "3", want "4") *Earlier —* 2026-11-03 — rang: `Open error is not a *FormatVersionError: *errors.errorString open db "fts": format version "2", want "3"`; restore diff empty

## Test: missing outranks chunker-version stale
**Purpose:** a deleted file reports missing even when its chunker version is behind, so RefreshStale skips it instead of reindexing a file that is gone
**Input:** versioned chunker at version 1; AddFile; bump to 2; delete the file; CheckFile; RefreshStale
**Expected:** status missing; RefreshStale returns no error
**Refs:** crc-DB.md, seq-stale.md, R719
**Code:** db_test.go
**Alarm:** 30
**Fire alarm:** move the chunkerStale check above the os.Stat in classifyFile; red is "stale" (found by probing past the list after simplification: the suite stayed green)
**Inject:** db.go:DB.classifyFile
@alarm-hash: 34a78cc1f82ca562
**Pulled:** 2026-11-04 — rang (re-pulled for the one-name record, 2026-11-04): a deleted file under a bumped chunker read stale, want missing *Earlier —* 2026-10-25 — rang: deleted file under a bumped chunker reported stale, want missing

## Test: posting rewrite drops dead postings and keeps order
**Purpose:** a rewrite drops the dead postings, keeps the survivors' order and counts, puts the new postings after them, and a record left with no live posting encodes empty (R778)
**Input:** stored postings (5,1) (7,2) (300,1) (70000,4), dead {300}, add (70001,3); separately every stored chunk dead and nothing added
**Expected:** (5,1) (7,2) (70000,4) (70001,3); the all-dead case encodes to an empty value
**Refs:** crc-DB.md, seq-prepare-store.md, R778
**Code:** db_test.go
**Alarm:** 31
**Fire alarm:** keep dead postings (skip the dead-set check in rewritePostings); red is (300,1) still listed
**Inject:** postings.go:rewritePostings
@alarm-hash: 92522156557ca907
**Pulled:** 2026-11-05 — rang — dead-set check skipped in rewritePostings: TestRewritePostingsFiltersAndAppends red, (300,1) still listed and the all-dead case 9 bytes, not empty; restored clean *Earlier —* 2026-10-25 — rang: rewritePostings = [9 5 7 70000], want [5 7 70000 9]

## Test: reindex rewrites each posting record once
**Purpose:** a reindex that replaces every chunk rewrites each affected T record once, though many new chunks share its trigrams; the orphans' dead postings leave the records it rewrites in the same pass
**Input:** a file of twelve lines, each containing "common" with distinct text after it; AddFile; rewrite every line on disk, still containing "common"; Reindex while the posting-write hook counts writes per key
**Expected:** no key written more than once; a search for an old line's text finds nothing, a new line's text finds its chunk, "common" finds all twelve new chunks
**Refs:** crc-DB.md, seq-prepare-store.md, R784, R778
**Code:** db_test.go
**Alarm:** 32
**Fire alarm:** apply the posting edits inside addFileInTxn's chunk loop, once per new chunk, instead of once at the end; red is a key written more than once
**Inject:** db.go:DB.addFileInTxn
@alarm-hash: 389251aeaddbe79c
**Pulled:** 2026-11-05 — rang — posting edits applied per new chunk in addFileInTxn: TestReindexRewritesEachPostingOnce red, posting records written 13 times, want once; restored clean *Earlier —* 2026-10-25 — rang: T records written 2 times, want once

## Test: a removal writes no posting
**Purpose:** a removal edits no T record; its orphans are dead (D records), no live posting names them, the trigram filter's document frequency no longer counts them, and a file outside the removal keeps its chunks (R774, R775)
**Input:** a file of twelve "common" lines and a keeper with one; RemoveFileWithCallback on the first while the hook counts posting writes
**Expected:** no posting write; twelve orphans, each with a D record and no live posting; the DF of "com" is 1; "common" finds only the keeper
**Refs:** crc-DB.md, R774, R775
**Code:** db_test.go
**Alarm:** 33
**Fire alarm:** skip the D record write in cascadeOrphan; red is an orphan with no D record and a live posting, and a DF of 13
**Inject:** db.go:DB.cascadeOrphan
@alarm-hash: 209f2c9b6726c294
**Pulled:** 2026-11-05 — rang — D record write skipped in cascadeOrphan: TestRemoveWritesNoPosting red, orphans with no D record and a live posting; restored clean

## Test: append drop-and-replace rewrites each posting record once
**Purpose:** an append that replaces the last chunk rewrites each affected record once: the new chunk's postings added, the dropped chunk's dead postings dropped where a record is rewritten
**Input:** tailReplaceChunker; AddFile with "shared alpha"; AppendChunks with "shared beta" while the hook counts writes per key
**Expected:** no key written more than once; "alpha" finds nothing, "beta" finds the new chunk
**Refs:** crc-DB.md, seq-prepare-store.md, seq-append.md, R784, R778
**Code:** db_test.go
**Alarm:** 34
**Fire alarm:** apply the posting edits twice in appendPrepared (since format "6" the drop adds no edits, so the old split-apply injection cannot ring); red is the new chunk's keys written twice
**Inject:** db.go:DB.appendPrepared
@alarm-hash: e51783b37c8a97bf
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved appendPrepared: posting edits applied twice, TestAppendReplaceRewritesEachPostingOnce red, each T record `written 2 times, want once`; restored clean *Earlier —* 2026-11-05 — rang — posting edits applied twice in appendPrepared: TestAppendReplaceRewritesEachPostingOnce red, posting records written 2 times, want once; restored clean *Earlier —* 2026-11-04 — rang — posting edits applied after the drop and again after the new chunks: TestAppendReplaceRewritesEachPostingOnce red, posting records written 2 times, want once; restored clean *Earlier —* 2026-10-25 — rang: "sha", "har", "are" written 2 times, want once
## Test: reindex callback sees final postings
**Purpose:** the coalesced edits are applied before ReindexCallback fires, so a callback reading T records inside the txn sees no live posting for the orphans and the new chunks present
**Input:** AddFile "alpha\n"; rewrite to "omega\n"; ReindexWithCallback whose callback checks, from tx, for a live posting of each orphan and each new chunk
**Expected:** no orphan has a live posting (it is dead); every new chunkid has one
**Refs:** crc-DB.md, seq-prepare-store.md, R785
**Code:** db_test.go
**Alarm:** 35
**Fire alarm:** fire rcb before applying the posting edits in reindexPrepared; red is a new chunk not listed
**Inject:** db.go:DB.reindexPrepared
@alarm-hash: af9fa204f41eead9
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved reindexPrepared: ReindexCallback fired before applyPostings, TestReindexCallbackSeesFinalPostings red, `callback: new chunk 2 not listed`; restored clean *Earlier —* 2026-11-05 — rang — ReindexCallback fired before the posting edits: TestReindexCallbackSeesFinalPostings red, `callback: new chunk 2 not listed`; restored clean *Earlier —* 2026-10-25 — rang: callback saw orphan 1 still listed, new chunk 2 not listed
## Test: remove with out-of-order chunkids clears every posting
Retired 2026-11-04 with format "6": a removal no longer builds a sorted set of chunkids to filter out of each record; it writes D records, and a rewrite drops dead postings by set membership, so the order of the orphans cannot matter. Its alarm (36) has no code left to inject into.

## Test: file summaries match the full records
**Purpose:** FileSummaries reports for every file the same chunk count, length, strategy and mod time as FileInfoByID, while decoding only the header and the count.
**Input:** three files of 3, 1 and 5 lines indexed with the line strategy.
**Expected:** three summaries, each equal to its FileInfoByID record on all four fields, none with zero chunks.
**Refs:** crc-DB.md
**Code:** db_test.go
**Alarm:** 37
**Fire alarm:** report the name count as the chunk count, the shape of reading the wrong field after the header; every summary disagrees with FileInfoByID on chunks and the test goes red naming the file.
**Inject:** records.go:unmarshalFSummary
@alarm-hash: c86793b2879ca5b4
**Pulled:** 2026-11-02 — rang — chunk count taken from the name count (with `_ = nChunks` so it builds; a first injection left nChunks unused, failed to build, and was discarded): "file N: summary {… ChunkCount:1 …}, FileInfoByID chunks=3"; restored clean

## Test: SearchTerms takes terms as given
**Purpose:** R752, R753: a term is used exactly as given (a quote is a character, a space is part of the term, empty terms dropped), for SearchTerms and SearchMultiTerms; Search reads the same text through the query syntax, a quoted phrase
**Input:** files holding `"hello world"` (quoted), `hello world` (bare) and the two words apart; SearchTerms with `["\"hello world\""]`, `["hello world"]`, `["hello","world"]`, `["", "hello world", ""]`, all WithVerify; SearchMultiTerms with the quoted term; Search with the quoted text as a query
**Expected:** the quoted term hits only the quoted file; the spaced term hits quoted and bare, not apart; two terms hit all three; empty terms change nothing; Search's quoted query hits quoted and bare
**Refs:** crc-DB.md, R752, R753
**Code:** search_terms_test.go
**Alarm:** 39
**Fire alarm:** make SearchTerms re-split its terms, `terms = parseQueryTerms(strings.Join(terms, " "))`, the join-and-decode #206 removes; red is the quoted term also hitting bare.txt
**Inject:** db.go:SearchTerms
@alarm-hash: 927e44d03f9e7407
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved SearchTerms: terms re-split from their joined text, TestSearchTermsTakesTermsAsGiven red (quote and space no longer part of the term), with TestSearchWithVerifyQuotedTerms and TestCFDKeysSortByID red too; restored clean *Earlier —* 2026-11-03 — rang: `quote is a character: SearchTerms(["\"hello world\""]) hit map[bare.txt:true quoted.txt:true], want [quoted.txt]`; restore clean
## Test: a batch remove writes no posting
**Purpose:** RemoveFiles edits no T record; a chunk shared only by files in the batch is orphaned exactly once; a file outside the batch keeps its chunks (R788, R756)
**Input:** three files of "common" lines, two also sharing one identical line; a keeper file with one "common" line; RemoveFiles on the three while the hook counts posting writes
**Expected:** no posting write; the callback's fileIDs are the three; the shared line's chunk is in the orphans once; "common" finds only the keeper
**Refs:** crc-DB.md, R788, R756
**Code:** db_test.go
**Alarm:** 40
**Fire alarm:** rewrite a T record per file inside RemoveFiles' loop, as eager removal did before format "6" (simulated by rewriting "com"); red is posting writes observed
**Inject:** db.go:DB.RemoveFiles
@alarm-hash: 0a9c07ac279ad13d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved RemoveFiles: a T record rewritten per file in the loop, TestRemoveFilesWritesNoPosting red, `batch removal wrote posting records: map[Tcom:3]`; restored clean *Earlier —* 2026-11-05 — rang — a T record rewritten per file in RemoveFiles: TestRemoveFilesWritesNoPosting red, `batch removal wrote posting records: map[Tcom:3]`; restored clean *Earlier —* 2026-11-04 — rang: posting records "Tmon", "Tomm" and others written 3 times, want once
## Test: a batch remove skips a path that is not indexed
**Purpose:** one path already removed (by the watcher, say) must not abort the batch (R755)
**Input:** two indexed files and one path never indexed, in one RemoveFiles call
**Expected:** no error; the callback's fileIDs are the two indexed files; neither is searchable afterwards
**Refs:** crc-DB.md, R755
**Code:** db_test.go
**Alarm:** 41
**Fire alarm:** return the lookup error instead of skipping; red is RemoveFiles failing and both files still searchable
**Inject:** db.go:DB.RemoveFiles
@alarm-hash: 0a9c07ac279ad13d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved RemoveFiles: the ErrNotIndexed skip removed, TestRemoveFilesSkipsUnindexed red, `file not found` on the never-indexed path; restored clean *Earlier —* 2026-11-05 — rang — lookup error returned instead of skipped in RemoveFiles: TestRemoveFilesSkipsUnindexed red, "RemoveFiles: file not found"; restored clean *Earlier —* 2026-11-04 — rang: RemoveFiles failed with "file not found: …/never-indexed.txt"
## Test: a failed batch callback rolls back the whole batch
**Purpose:** the callback's error aborts every removal in the batch; a nil callback is allowed; after a successful batch the path caches forget every removed path (R757, R758)
**Input:** two files; RemoveFiles with a callback returning a sentinel; then FileIDPaths (the path caches load lazily, and ark always has them loaded), RemoveFiles with nil, and AddFile of the first path again
**Expected:** the sentinel comes back and both files are still searchable; the nil batch removes both; FileIDPaths no longer lists them and CheckFile on one answers ErrNotIndexed; the re-add succeeds as a fresh file
**Refs:** crc-DB.md, R757, R758
**Code:** db_test.go
**Alarm:** 42
**Fire alarm:** skip the post-commit cache invalidation for the batch; red is FileIDPaths still listing the removed paths. (First written without loading the caches, it stayed green under this injection: a nil cache cannot go stale.)
**Inject:** db.go:DB.RemoveFiles
@alarm-hash: 0a9c07ac279ad13d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved RemoveFiles: post-commit cache invalidation removed, TestRemoveFilesCallbackRollsBack red, path cache still lists both removed files; restored clean *Earlier —* 2026-11-05 — rang — post-commit cache invalidation skipped in RemoveFiles: TestRemoveFilesCallbackRollsBack red, "path cache still lists a.txt and b.txt after the batch"; restored clean *Earlier —* 2026-11-04 — rang: path cache still lists a.txt and b.txt after the batch; CheckFile answered "read F record 1: not found", not ErrNotIndexed
## Test: the F record ends after its chunks
**Purpose:** the F record value ends after its chunk list (R765), and a value with bytes past it, which is what a format "4" record's token bag looks like read as format "5", is refused rather than decoded with the tail ignored
**Input:** an FRecord marshalled; the same bytes with one zero byte appended (a format "4" record's empty token bag)
**Expected:** the first decodes; the second returns an error
**Refs:** crc-DB.md, R765
**Code:** records_test.go
**Alarm:** 43
**Fire alarm:** drop the trailing-bytes check in unmarshalFValue (return f, nil after the chunk list whatever is left); red is the appended-byte value decoding without error
**Inject:** records.go:UnmarshalFValue
@alarm-hash: ab613b40fc444b81
**Pulled:** 2026-11-04 — rang — trailing-bytes check disabled in unmarshalFValue: TestFRecordEndsAfterItsChunks red, "a record with a token bag after its chunks decoded without error"; restored clean

## Test: T record round trip
**Purpose:** postings with counts of 1 (no count byte written) and above 1, over ids of several varint widths, decode to what was encoded (R770, R780)
**Input:** TRecord postings (1,1) (42,3) (1000,1) (999999,200); then three postings of count 1
**Expected:** the postings decode equal; the three count-1 postings take three bytes
**Refs:** crc-DB.md, R770, R780
**Code:** records_test.go
**Alarm:** 44
**Fire alarm:** always write the count (drop the count == 1 branch in appendPostings); red is the three count-1 postings taking six bytes
**Inject:** records.go:appendPostings
@alarm-hash: e5caab4b9d8a2b8d
**Pulled:** 2026-11-05 — rang — count always written in appendPostings: TestTRecordRoundtrip red, three count-1 postings took 6 bytes, want 3; restored clean

## Test: a rewrite drops dead postings
**Purpose:** an add that rewrites a T record drops the dead postings in it and leaves the D record, since other records may still hold the id (R778)
**Input:** add "common alpha", remove it, add "common beta"; read the "com" record and the dead set
**Expected:** "com" stores only the new chunk; the removed chunk still has its D record
**Refs:** crc-DB.md, R778
**Code:** postings_test.go
**Alarm:** 45
**Fire alarm:** load an empty dead set in applyPostings instead of the D records; red is the dead chunk still stored under "com"
**Inject:** postings.go:DB.applyPostings
@alarm-hash: 5e53646f65cd4f12
**Pulled:** 2026-11-05 — rang — empty dead set in applyPostings: TestRewriteDropsDeadPostings red, the rewritten "com" record still stores dead chunk 1; restored clean

## Test: Purge clears the dead set
**Purpose:** Purge drops every dead posting, deletes records left with none, clears the dead set it started with and reports it; a second Purge writes nothing (R779)
**Input:** a file of two lines (one with a trigram no other file has) and a keeper; remove the file; Purge; Purge again while the hook counts writes
**Expected:** stats name 2 dead ids and some postings and lists; no dead chunk is stored in any posting; the unique trigram's record is gone; the dead set is empty; "common" finds the keeper; the second Purge reports nothing and writes nothing
**Refs:** crc-DB.md, R779
**Code:** postings_test.go
**Alarm:** 46
**Fire alarm:** delete the wrong keys at the end of Purge (so the D records stay); red is a non-empty dead set after Purge and a second Purge that reports dead ids
**Inject:** postings.go:DB.Purge
@alarm-hash: 5a6cf7805f8d0161
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved Purge: final cleanup deleting C keys instead of D keys, TestPurge red, `dead set after Purge: map[1:{} 2:{}], want empty`; restored clean *Earlier —* 2026-11-05 — rang (second attempt; the first injection did not build) — Purge deletes the wrong D keys: TestPurge red, dead set after Purge map[1 2] and a second Purge reporting Dead 2; restored clean
## Test: scorers get their counts from the postings
**Purpose:** a chunk's count of a trigram reaches the scorers from its posting, in Search and in ScoreFile (R776, R777)
**Input:** one chunk "aaaaaa xx" ("aaa" four times, two distinct tokens); Search "aaa" with density; ScoreFile "aaa" with ScoreDensityFunc
**Expected:** both score 2 (4 / 2)
**Refs:** crc-DB.md, R776, R777
**Code:** postings_test.go
**Alarm:** 47
**Fire alarm:** make postingReader.count return 1 for any posting it finds; red is a score of 0.5 in both
**Inject:** postings.go:postingReader.count
@alarm-hash: ac988f9fb3423c5f
**Pulled:** 2026-11-05 — rang — postingReader.count returns 1: TestCountsComeFromPostings red, density 0.5 in Search and ScoreFile, want 2; restored clean

## Test: an orphan takes its token total out of the counter
**Purpose:** totalTokens rises by a new chunk's token total and falls by it when the chunk is orphaned (R774)
**Input:** add "aa aa aa bb"; read totalTokens; remove the file; read it again
**Expected:** 4, then 0
**Refs:** crc-DB.md, R774
**Code:** postings_test.go
**Alarm:** 48
**Fire alarm:** subtract the distinct-token count instead of the token total in cascadeOrphan; red is 2 left after the removal
**Inject:** db.go:DB.cascadeOrphan
@alarm-hash: 209f2c9b6726c294
**Pulled:** 2026-11-05 — rang — distinct-token count subtracted in cascadeOrphan: TestOrphanSubtractsTokenTotal red, totalTokens 2 after remove, want 0; restored clean

## Test: loose search counts every trigram of a term
**Purpose:** loose search's term scorer checks every trigram of a term, the ones the trigram filter dropped included, so their counts come from the postings too (R776, R340)
**Input:** "zqxy" in one file and "qxy" in three more; loose search "zqxy" with FilterBestN(1), which keeps the rare "zqx" and drops "qxy"
**Expected:** exactly the "zqxy" file
**Refs:** crc-DB.md, R776, R340
**Code:** postings_test.go
**Alarm:** 49
**Fire alarm:** count only the selected trigrams in loose search too (countTris = active in searchCollect); red is no result
**Inject:** db.go:searchConfig.searchCollect
@alarm-hash: de83c74a945ece32
**Pulled:** 2026-11-05 — rang — loose search counts only the selected trigrams: TestLooseCountsEveryTermTrigram red, no result; restored clean

## Test: Purge works across batches
**Purpose:** Purge rewrites T records a batch at a time, and with more records than one batch holds every one still loses its dead postings (R779). Found by injecting past the alarm list: stopping after the first batch left the suite green
**Input:** one line holding every three-letter word over a to p (4,864 T records); remove the file; Purge
**Expected:** the fixture has more T records than one batch; after Purge no T record is left
**Refs:** crc-DB.md, R779
**Code:** postings_test.go
**Alarm:** 50
**Fire alarm:** stop after the first batch (set the next key to nil in Purge's cursor loop); red is T records left after the purge
**Inject:** postings.go:DB.Purge
@alarm-hash: 5a6cf7805f8d0161
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved Purge: next key set to nil in the cursor loop, TestPurgeAcrossBatches red, `2864 T records left after purging the only file's chunk, want 0`; restored clean *Earlier —* 2026-11-05 — rang — Purge stops after its first batch: TestPurgeAcrossBatches red, 2864 T records left after the purge, want 0; restored clean. Before this test the same injection left the suite green
## Test: key ids sort as their values
**Purpose:** `AppendKeyID` writes SQLite4's order-preserving varint: byte order equals numeric order across every length boundary, and `KeyID` returns exactly what was written (R791-R793)
**Input:** every value 0-70,000, then each boundary of every length (240/241, 2287/2288, 67823/67824, 2^24-1/2^24 up to 2^56-1/2^56, and 2^64-1) with its neighbours; encode each, compare each with the previous
**Expected:** each encoding sorts strictly after the previous value's; lengths are 1, 2, 3, 4 to 9 bytes at the boundaries; `KeyID` gives back the value and the encoding's length
**Refs:** crc-DB.md, R791, R793
**Code:** records_test.go
**Alarm:** 51
**Fire alarm:** encode the 3-byte form with base 2287 instead of 2288 (an off-by-one at the boundary); red is 2288 and 2287's encodings out of order or a round trip that comes back wrong
**Inject:** records.go:AppendKeyID
@alarm-hash: d18720e24dcbdf48
**Pulled:** 2026-11-05 — 2026-11-05 — rang — 3-byte form based at 2287 instead of 2288: TestKeyIDOrder red, `KeyID(AppendKeyID(2288)) = 2289, 3; want 2288, 3`; restored clean

## Test: short and non-minimal key ids are refused
**Purpose:** `KeyID` never panics on bad input: empty and truncated encodings give n == 0, a non-minimal one gives n < 0 (R792)
**Input:** nil; each multi-byte form cut short by one byte; 241 0 (a two-byte form of 240); 250 0 0 1 (a four-byte form of 1)
**Expected:** n == 0 for the empty and short inputs, n < 0 for the two non-minimal ones, no panic
**Refs:** crc-DB.md, R792
**Code:** records_test.go
**Alarm:** 52
**Fire alarm:** drop the length check before reading the multi-byte form; red is a panic (index out of range) on a truncated input
**Inject:** records.go:KeyID
@alarm-hash: bf14d12cebe4168d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — length check before the multi-byte read removed: TestKeyIDRefusesBadInput red, truncated `fa0108`, `fb010000`, `ff01000000000000` returned n = 4, 5, 9 (want 0); no panic, since the range over the short slice stops early; restored clean

## Test: C, F and D keys sort by id
**Purpose:** C, F and D keys hold their id in the ordered varint, so a cursor over each class visits ids in numeric order, and every reader of those keys decodes them (R794)
**Input:** add 300 one-chunk files with distinct content (chunkids and fileids crossing 128 and 240); remove one so a D record exists; walk the C, F and D keys with a cursor
**Expected:** each class's decoded ids come out strictly increasing; `FileInfoByID`-style reads and search find every remaining file; the D walk decodes the removed chunk's id
**Refs:** crc-DB.md, R794
**Code:** records_test.go
**Alarm:** 53
**Fire alarm:** make makeCKey append the chunkid as LEB128 again (the format "6" key); red is the C walk out of order at chunkid 128
**Inject:** records.go:makeCKey
@alarm-hash: ba02aafa5a07858d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — makeCKey appending LEB128 (the format 6 key): TestCFDKeysSortByID red, `C key 438001 does not hold one ordered id`; restored clean

## Test: writes fill appended pages to 90%
**Purpose:** every write sets the bucket's FillPercent to 0.9, so leaves split by right-edge appends stay about 90% full rather than bbolt's default half (R796)
**Input:** add 5,000 one-chunk files, one write transaction each, in a fresh database
**Expected:** the fts bucket's leaf fill (LeafInuse over LeafPageN × page size) is at least 0.8
**Refs:** crc-DB.md, R796
**Code:** records_test.go
**Alarm:** 54
**Fire alarm:** remove the FillPercent assignment in bucket(tx); red is a leaf fill near 0.5
**Inject:** db.go:DB.bucket
@alarm-hash: fbeb2c8c379a6ec8
**Pulled:** 2026-11-05 — 2026-11-05 — rang — FillPercent assignment removed from DB.bucket: TestWritesFillAppendedPages red, `leaf fill 0.48 over 304 pages, want >= 0.8`; restored clean

