# Sequence: Add File
**Requirements:** R10-12, R20, R25, R26, R29, R77, R79, R81, R92, R110, R111, R116, R118, R120-122, R128-131, R146, R147, R213-215, R223-231, R233-238, R240, R241, R244, R245, R249-251, R253, R261, R262, R469, R470, R472-478, R485, R735, R739

Entry flow: `AddFile` now runs as `PrepareFile` (compute, no txn) then `StorePrepared`
(this store) — see seq-prepare-store.md. The steps below are the store phase; the file
read and chunking happen in the compute phase.

Participants: DB, Chunker, Trigrams, KeyChain

```
DB                      Chunker       Trigrams      KeyChain
 |                        |              |            |
 |  [in addFileInTxn]                    |            |
 |  check finalKey(FilenameKey(fpath))   |            |
 |    via N records (R739)               |            |
 |  if exists: return (0, ErrAlreadyIndexed)         |
 |                                       |            |
 |  stat file (mod time before read)     |            |
 |  read file, compute SHA-256           |            |
 |                                       |            |
 |  allocate fileid                      |            |
 |-- Encode(filename) --------------------------------> |
 | <-- N key/value pairs --------------------------------|
 |  store N records (filename key chain) |            |
 |                                       |            |
 |  resolve Chunker for strategy:        |            |
 |    if chunkers[name] exists:          |            |
 |      c = chunkers[name]              |            |
 |    else:                              |            |
 |-- RunChunkerFunc(cmd) -> c ---------> |            |
 |                                       |            |
 |  apply IndexOptions (extract callback) |            |
 |                                       |            |
 |  call c.Chunks(path, content, yield): |            |
 |    for each yielded Chunk{Range, Content, Attrs}:  |
 |      copy Range as string             |            |
 |      validate UTF-8 on Content        |            |
 |      if callback != nil:              |            |
 |        callback(string(Content)) [R473]            |
 |      compute SHA-256 of Content       |            |
 |-- TrigramCounts(Content) -------->    |            |
 | <-- map[uint32]int ---------------    |            |
 |      tokenize Content, count tokens   |            |
 |      copy Attrs ([]Pair)              |            |
 |                                       |            |
 |      check H[hash] for dedup:         |            |
 |        if exists -> chunkid (dedup hit):           |
 |          read C record, add fileid    |            |
 |          write updated C record       |            |
 |        if not found -> new chunk:     |            |
 |          allocate chunkid             |            |
 |          create H record: H[hash] -> chunkid      |
 |          create C record: C[chunkid] ->           |
 |            hash, trigrams, tokens,    |            |
 |            attrs, [fileid]            |            |
 |          accumulate trigrams for T batch          |
 |          accumulate tokens for W batch            |
 |                                       |            |
 |      collect (chunkid, location) for F record     |
 |      merge tokens into file-level bag |            |
 |                                       |            |
 |  batch T record updates:              |            |
 |    for each affected trigram:         |            |
 |      read T[tri], append chunkids,   |            |
 |      write T[tri]                     |            |
 |                                       |            |
 |  batch W record updates:              |            |
 |    for each affected token hash:      |            |
 |      read W[hash], append chunkids,  |            |
 |      write W[hash]                    |            |
 |                                       |            |
 |  create F record: F[fileid] ->        |            |
 |    modTime, contentHash, fileLength,  |            |
 |    strategy, names, chunks, tokenBag  |            |
 |                                       |            |
 |  return (fileid, nil)                 |            |
 |  WithContent: return (fileid, content, nil)       |
```
