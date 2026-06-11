# Sequence: Chunker Dispatch
**Requirements:** R502, R505, R513, R514, R515, R516, R529, R530, R542, R543, R544, R655, R656

How DB and ChunkCache dispatch to the right chunking interface at each call site.

## Index-time (collectChunks)

```
DB.collectChunks(fpath, strategy)
  |
  resolveChunker(strategy) → c (any)
  |
  switch c.(type):
  |
  case FileChunker:
  |   fc.FileChunks(fpath, oldHash, yield) → (hash, err)
  |   if hash == oldHash: skip (no yield calls)
  |   else: chunks collected from yield
  |
  case Chunker:
  |   fileModTime(fpath)
  |   os.ReadFile(fpath)
  |   c.Chunks(fpath, data, yield) → err
  |   contentHash(data) → hash
```

## Retrieval-time (DB.GetChunks, non-tmp)

```
DB.GetChunks(fpath, targetRange, before, after)
  |
  read frec via lookupFileByPath
  find targetIdx by frec.Chunks[i].Location == targetRange
  compute [lo, hi] clamped to bounds
  |
  resolveChunker(frec.Strategy) → c (any)
  |
  switch c.(type):
  |
  case RandomAccessChunker:                                       # fast path, R529
  |   var cd any                                                  # transient customData for this call
  |   data = os.ReadFile(fpath) if Chunker, else nil
  |   for i in [lo, hi]:
  |     fce = frec.Chunks[i]
  |     crec = read C record by fce.ChunkID                       # for Attrs
  |     chunk = Chunk{Range: fce.Location, Attrs: crec.Attrs}     # R526, R527
  |     ra.GetChunk(fpath, data, &cd, &chunk) → err               # R528
  |     append to results
  |
  case FileChunker:                                               # streaming fallback, R530
  |   fc.FileChunks(fpath, [32]byte{}, yield) → (_, err)
  |
  case Chunker:                                                   # streaming fallback, R530
  |   os.ReadFile(fpath)
  |   c.Chunks(fpath, data, yield) → err
```

## Overlay (AddTmpFile, UpdateTmpFile, AppendTmpFile)

```
DB.AddTmpFile(path, strategy, content)
  |
  resolveChunker(strategy) → c (any)
  |
  c, ok := c.(Chunker)
  if !ok: error — FileChunker-only cannot be used with overlay
  |
  c.Chunks(path, content, yield) → err
```

## ChunkCache dispatch

```
ChunkCache.ensureFile(fpath)
  |
  resolveChunker → c (any)
  |
  case FileChunker (no Chunker): data = nil
  case Chunker: data = os.ReadFile(fpath)
  |
  snapshot fileChunks := frec.Chunks
  build rangeIds := {fce.Location → fce.ChunkID for fce in fileChunks}
  customData := nil

ChunkCache.ChunkTextWithId(fpath, chunkID)
  |
  cf := ensureFile(fpath)
  locate i such that fileChunks[i].ChunkID == chunkID
  loc := fileChunks[i].Location
  if byRange[loc] hit: return chunks[idx].Content
  |
  switch cf.chunker.(type):
  |
  case RandomAccessChunker:                                       # fast path, R529, R542
  |   crec = read C record by chunkID
  |   chunk = Chunk{Range: loc, Attrs: crec.Attrs}
  |   ra.GetChunk(cf.path, cf.data, &cf.customData, &chunk)
  |   storeChunk(cf, chunk)
  |
  default:                                                        # streaming fallback, R543
  |   run Chunks/FileChunks from start, storeChunk each yield
  |   stop when yielded Range == loc

ChunkCache.ChunkText(fpath, rangeLabel)
  |
  cf := ensureFile(fpath)
  chunkID := cf.rangeIds[rangeLabel]
  return ChunkTextWithId(fpath, chunkID)                          # R536

ChunkCache.GetChunks(fpath, targetRange, before, after)
  |
  cf := ensureFile(fpath)
  locate targetPos in cf.fileChunks by Location == targetRange
  compute [lo, hi]
  |
  for i in [lo, hi]:
    fce = cf.fileChunks[i]
    if byRange[fce.Location] hit: use cached
    else: retrieve via fast path or streaming (same dispatch as ChunkTextWithId)
  |
  assemble []ChunkResult in positional order                      # R544
```

## Content and Attrs across index/retrieval (cross-cutting)

The trigram/token index is full-text — chunks are indexed verbatim, nothing is
stripped. The dedup hash is over the chunk's Content; native chunker Attrs are
stored in the C record at index time. Retrieval re-reads Content from the file
but reads Attrs from storage.

Index sites — content hashed and indexed verbatim; native Attrs stored:

```
collectChunks / AppendChunks / collectChunksFromContent  yield(c) -> db.indexChunk(c):
  hash = sha256(c.Content)                          # dedup identity                R656, R657
  trigrams/tokens computed on c.Content             # full-text, nothing stripped   R656
  store collectedChunk{ Content: c.Content, Attrs: c.Attrs (native), hash }
```

Retrieval sites — Content re-read from the file; Attrs read from the stored C
record via db.chunkAttrs (overlay.chunkAttrs for tmp://), R655:

```
RandomAccessChunker fast path (DB.getChunksFast, ChunkCache.retrieveFast / populateFastWindow):
  chunk.Attrs pre-filled from the stored C record
  ra.GetChunk(path, data, &customData, &chunk)      # Content is the re-read region

streaming fallback (DB.GetChunks, ChunkCache.GetChunks):
  re-chunk for Content; Attrs read from the stored C record via db.chunkAttrs(frec.Chunks, lo, hi)

tmp:// (DB.getChunksTmp):
  re-chunk ofile.content for Content; Attrs from overlay.chunkAttrs (stored overlayChunk.attrs)
```
