# ChunkCache
**Requirements:** R297, R298, R299, R300, R301, R302, R303, R304, R305, R306, R370, R486, R514, R529, R535, R536, R537, R538, R539, R540, R541, R542, R543, R544, R545

Per-query cache for file content and chunked data. Avoids redundant file reads and re-chunking when processing search results. Dispatches to RandomAccessChunker fast path when available; otherwise streams chunks from the start, caching what's encountered.

## Knows
- db: *DB reference for N record lookups, F record reads, C record reads, Chunker resolution
- files: map[string]*cachedFile — keyed by resolved absolute path
- cachedFile:
  - path, data []byte (nil for FileChunker-only), chunker any (Chunker, FileChunker, and/or RandomAccessChunker)
  - fileChunks []FileChunkEntry — positional chunk list from frec.Chunks
  - rangeIds map[string]uint64 — Location→ChunkID, O(1) lookup
  - chunks []cachedChunk — access-order (chronological); positional meaning via fileChunks + byRange, not slice index
  - byRange map[string]int — Location → index into chunks, primary cache lookup
  - customData any — per-file scratch for RandomAccessChunker; nil until first GetChunk call
  - complete bool — true when streaming path has exhausted the file

## Does
- NewChunkCache(): create cache with DB reference
- GetChunks(fpath, targetRange, before, after): resolve file via ensureFile; locate targetPos in fileChunks; compute window [lo, hi]; for each i in window: resolve fileChunks[i].Location via byRange on hit, else retrieve (fast path via GetChunk + C record, or streaming fallback); assemble []ChunkResult in positional order
- ChunkTextWithId(fpath, chunkID): resolve file; find positional entry in fileChunks by ChunkID match; check byRange for cached content; on miss, read C record, pre-fill Chunk (Range from fileChunks[i].Location, Attrs from C record), dispatch to GetChunk (fast path) or streaming (chunkUntil by range); return content
- ChunkText(fpath, rangeLabel): resolve file; look up chunkID := rangeIds[rangeLabel]; delegate to ChunkTextWithId
- ensureFile(fpath): lookup path → fileid via DB (N records), read F record, snapshot fileChunks, build rangeIds from fileChunks, resolve chunker. For Chunker backends, os.ReadFile into data; for FileChunker-only backends, data stays nil. Allocate empty chunks slice, empty byRange map, nil customData.
- retrieveFast(cf, chunkID, loc): RandomAccessChunker path — read C record by chunkID for Attrs; pre-fill Chunk{Range: loc, Attrs: stored}; call ra.GetChunk(cf.path, cf.data, &cf.customData, &chunk); deep-copy into chunks + byRange
- retrieveStream(cf, rangeLabel): non-RandomAccessChunker fallback — run Chunker.Chunks or FileChunker.FileChunks from start, deep-copy each yielded chunk into chunks + byRange, stop when target range is found; for GetChunks-completion, run to end and set complete=true
- storeChunk(cf, chunk): deep-copy Range, Content, Attrs; append to chunks; record byRange[rangeStr] = idx

## Collaborators
- DB: N record path→fileid lookup, F record reads, C record reads (for pre-filling Attrs on fast path), Chunker resolution
- Chunker/FileChunker/RandomAccessChunker: chunking interfaces — dispatch by capability

## Sequences
- seq-cache.md
- seq-chunker-dispatch.md
