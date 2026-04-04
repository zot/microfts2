# Sequence: Add File
**Requirements:** R10, R11, R12, R20, R25, R26, R29, R77, R79, R81, R92, R110, R111, R116, R118, R120, R121, R122, R128, R129, R130, R131, R146, R147, R213, R214, R215, R223, R224, R225, R226, R227, R228, R229, R230, R231, R233, R234, R235, R236, R237, R238, R240, R241, R244, R245, R249, R250, R251, R252, R253, R261, R262, R469, R470, R472, R473, R474, R475, R476, R477, R478, R485

Participants: DB, Chunker, Trigrams, KeyChain

```
DB                      Chunker       Trigrams      KeyChain
 |                        |              |            |
 |  [in addFileInTxn]                    |            |
 |  check FinalKey(fpath) via N records  |            |
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
