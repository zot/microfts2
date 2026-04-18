# ChunkCache
**Requirements:** R297, R298, R299, R300, R301, R302, R303, R304, R305, R306, R370, R486, R514

Per-query cache for file content and chunked data. Avoids redundant file reads and re-chunking when processing search results. Lazily chunks files on first access, caches all encountered chunks for subsequent lookups.

## Knows
- db: *DB reference for N record lookups, F record reads, Chunker resolution
- files: map[string]*cachedFile — keyed by resolved absolute path
- cachedFile: data []byte (file content, nil for FileChunker), chunker any (Chunker or FileChunker), chunks []cachedChunk (sparse), byRange map[string]int, complete bool

## Does
- NewChunkCache(): create cache with DB reference
- GetChunks(fpath, targetRange, before, after): resolve file (if not cached), run chunker to completion (filling all slots), return window of ChunkResults — same contract as DB.GetChunks
- ChunkText(fpath, rangeLabel): resolve file (if not cached), check byRange for hit, if miss run chunker lazily (stop at target), return content
- ensureFile(fpath): resolve path → fileid via DB (N records), read F record, resolve chunker. Dispatch: if FileChunker, data stays nil (chunker reads file); if Chunker, os.ReadFile. Allocate sparse chunk slice from F record chunk count
- chunkFull(cf): dispatch to Chunker.Chunks or FileChunker.FileChunks(path, zero, yield), deep-copy and store all chunks, set complete=true
- chunkUntil(cf, rangeLabel): dispatch to Chunker.Chunks or FileChunker.FileChunks, deep-copy and store each chunk, stop when target found

## Collaborators
- DB: N record path→fileid lookup, F record reads, Chunker resolution
- Chunker/FileChunker: Chunks/FileChunks method for content extraction

## Sequences
- seq-cache.md
