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
**Input:** add files with chunks "@status: open task", "@status: completed task". Search "task" with WithExceptRegex("@status:.*completed")
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

## Test: Copy can perform LMDB reads
**Purpose:** the copy is functional for indexing
**Input:** create DB, add a file, call Copy(), use copy to read F records
**Expected:** copy can open View txns and read records from the shared env
**Refs:** crc-DB.md, R459, R460

## Test: InvalidateCaches clears caches
**Purpose:** InvalidateCaches nils all three caches
**Input:** create DB, add a file, call FileIDPaths (populates caches), call InvalidateCaches()
**Expected:** pathCache == nil, pathToID == nil, frecordCache == nil. Next FileIDPaths call re-populates from LMDB
**Refs:** crc-DB.md, R463, R464
