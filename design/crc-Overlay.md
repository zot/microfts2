# Overlay
**Requirements:** R349, R350, R351, R352, R353, R354, R355, R356, R357, R358, R359, R360, R361, R362, R363, R364, R365, R366, R369, R371, R372, R373

In-memory overlay holding tmp:// documents alongside the LMDB index. Mirrors the LMDB record structure (C, F, T, W, H equivalents) in Go maps. Fileids and chunkids count down from MaxUint64 to structurally avoid collision with LMDB ids. Thread-safe: concurrent reads, serialized writes.

## Knows
- mu: sync.RWMutex
- nextFileID: uint64 — counts down from math.MaxUint64
- nextChunkID: uint64 — counts down from math.MaxUint64
- files: map[string]*overlayFile — keyed by tmp:// path
- filesByID: map[uint64]*overlayFile — keyed by fileid
- chunks: map[uint64]*overlayChunk — keyed by chunkid, equivalent to C records
- trigrams: map[uint32]map[uint64]struct{} — trigram → chunkid set, equivalent to T records
- tokens: map[uint32]map[uint64]struct{} — token hash → chunkid set, equivalent to W records
- hashes: map[[32]byte]uint64 — content hash → chunkid, equivalent to H records
- totalChunks: int — overlay chunk count
- totalTokens: int — sum of overlay chunk token counts

## overlayFile (unexported)
- fileID: uint64
- path: string
- content: []byte — original document bytes (for chunk retrieval)
- strategy: string
- chunks: []FileChunkEntry — ordered chunk list with locations
- tokens: []TokenEntry — aggregated token bag

## overlayChunk (unexported)
- chunkID: uint64
- hash: [32]byte
- trigrams: []TrigramEntry
- tokens: []TokenEntry
- attrs: []Pair
- fileIDs: []uint64

## Does
- addFile(path, strategy, content, db): validate UTF-8, check for duplicate path (ErrAlreadyIndexed), allocate fileid (decrement), chunk content via db's chunker registry, for each chunk: hash, check overlay hashes for dedup, allocate chunkid if new (decrement), build overlayChunk, update trigram/token maps. Build overlayFile with chunk list and token bag. Update counters. Returns fileid
- updateFile(path, strategy, content, db): find existing file by path (error if missing), remove old file data, add new — no moment where path is absent from the overlay (hold write lock across both operations)
- removeFile(path): find by path (error if missing), for each chunkid: remove fileid from overlayChunk, if no fileids remain delete chunk and clean trigram/token/hash maps. Delete file records. Update counters
- searchCandidates(queryTrigrams): RLock, intersect trigram maps to produce candidate chunkid set, return overlayChunks for candidates. Mirrors DB's T record intersection
- lookupChunk(chunkid): RLock, return overlayChunk by id
- lookupFile(fileid): RLock, return overlayFile by id
- lookupFileByPath(path): RLock, return overlayFile by path
- tmpFileIDs(): RLock, return map[uint64]struct{} of all overlay fileids
- counters(): RLock, return totalChunks, totalTokens
- chunkContent(fileid, rangeLabel, db): return chunk text from stored content using db's chunker

## Collaborators
- DB: chunker resolution, merged search, corpus counter summation

## Sequences
- seq-tmp-add.md
- seq-tmp-search.md
