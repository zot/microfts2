# Sequence: Chunker Dispatch
**Requirements:** R502, R505, R513, R514, R515, R516, R517

How DB dispatches to the right chunking interface at each call site.

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

## Retrieval-time (getChunks, non-tmp)

```
DB.getChunks(fpath, targetRange, before, after)
  |
  resolveChunker(frec.Strategy) → c (any)
  |
  switch c.(type):
  |
  case FileChunker:
  |   fc.FileChunks(fpath, [32]byte{}, yield) → (_, err)
  |   (zero old = always chunk)
  |
  case Chunker:
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

## ChunkText retrieval

```
resolveChunkText(c, path, content, rangeLabel)
  |
  switch c.(type):
  |
  case ChunkTexter:
  |   c.ChunkText(path, content, rangeLabel)
  |
  case Chunker (no ChunkTexter):
  |   chunkTextByRange(c, path, content, rangeLabel)
  |   (re-run Chunks, stop at match)
  |
  case FileChunker (no ChunkTexter):
  |   chunkTextByRangeFile(fc, path, rangeLabel)
  |   (re-run fc.FileChunks(path, zero, yield), stop at match)
```

## ChunkCache dispatch

```
ChunkCache.ensureFile(fpath)
  |
  resolveChunker → c (any)
  |
  case FileChunker: data = nil (chunker reads file)
  case Chunker:     data = os.ReadFile(fpath)
  |
  chunkFull / chunkUntil:
  |
  case FileChunker: fc.FileChunks(path, [32]byte{}, yield)
  case Chunker:     c.Chunks(path, data, yield)
```
