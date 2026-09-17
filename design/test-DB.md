# Test Design: DB
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
**Pulled:** 2026-09-17 — rang (expected ErrAppendTailMoved, got <nil>)

## Test: PrepareAppend opens no write transaction
**Purpose:** append compute is transaction-free (R685)
**Input:** add a file; snapshot RecordCounts; call PrepareAppend(path, lastLocator, content, strategy) and discard the handle; snapshot again
**Expected:** RecordCounts unchanged across the call
**Refs:** crc-DB.md, seq-prepare-store.md#4, R685
**Code:** db_test.go
**Fire alarm:** make PrepareAppend open a write txn and touch a counter — the post-call RecordCounts differ; assertion red
**Inject:** db.go:PrepareAppend
**Pulled:** 2026-09-17 — rang (RecordCounts 22→23 after PrepareAppend)

## Test: a PreparedFile is single-use
**Purpose:** storing a handle twice is rejected rather than silently corrupting (R679)
**Input:** p := PrepareFile(path, strategy); StorePrepared(p) succeeds; call StorePrepared(p) again
**Expected:** the second store returns a non-nil error (handle already consumed); it does not write a second, malformed file
**Refs:** crc-DB.md, seq-prepare-store.md#2, R679
**Code:** db_test.go
**Fire alarm:** remove the consumed check in storePrepared — without it the second store hits ErrAlreadyIndexed (masking the guard), so the test asserts errors.Is(err, errPreparedConsumed) specifically and goes red on ErrAlreadyIndexed
**Inject:** db.go:storePrepared
**Pulled:** 2026-09-17 — rang (got "file already indexed", want errPreparedConsumed; test strengthened to the specific sentinel during this pull)
