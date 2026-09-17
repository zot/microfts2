# Sequence: Prepare / Store (Two-Phase Compute-Store Split)
**Requirements:** R676, R677, R678, R679, R680, R681, R682, R683, R684, R685, R686, R687, R688, R689

Participants: DB, Chunker, Trigrams

The compute entries (`Prepare*`) open no bbolt transaction; the store entries (`Store*`,
`ReindexPrepared`, `AppendPrepared`) open exactly one write transaction. A `*PreparedFile`
or `*PreparedAppend` is the only value that crosses between them. The store internals — H
dedup, C/T/W/F record work, orphan cascade — are unchanged; see seq-add.md and
seq-append.md. This sequence shows only the split boundary and what each side owns.

Diagram 1 — Compute a file, transaction-free (`PrepareFile` / `PrepareContent`):

```
1. PrepareFile(fpath, strategy, opts) / PrepareContent(fpath, strategy, content, opts)
  1.1. resolve chunker for strategy from the in-memory registry — no txn (R678)
  1.2. obtain bytes: PrepareFile reads fpath from disk; PrepareContent takes the caller's content (R676, R677)
  1.3. run chunker; per yielded chunk: validate UTF-8, fire WithChunkCallback (R683), SHA-256 hash, TrigramCounts, tokenize -> collectedChunk (R676)
  1.4. assemble opaque *PreparedFile{chunks, modTime, contentHash, fileLength}, unconsumed; return having opened no bbolt txn (R676, R678, R679)
```

Diagram 2 — Store a fresh add, one write txn (`StorePrepared`):

```
2. StorePrepared(p, opts)
  2.1. reject if p is already consumed (R679)
  2.2. open bbolt Update txn
  2.3. FinalKey guard -> ErrAlreadyIndexed if the path is already indexed (R680)
  2.4. allocate fileid; write N key chain; addFileInTxn stores p's chunks — H dedup/insert, C, coalesced T/W, corpus counters, F; WithIndexedChunkCallback on new chunks (R680, R683)
  2.5. mark p consumed (release chunk content); return fileid (R679, R680)
```

Diagram 3 — Store a content-diff reindex, one write txn (`ReindexPrepared`):

```
3. ReindexPrepared(p, opts)
  3.1. reject if p is already consumed (R679)
  3.2. open bbolt Update txn; look up the old F record by path
  3.3. deleteFileMeta(oldFileID, path) — old F + N only, freeing the path (R681)
  3.4. addFileInTxn stores p's chunks under a fresh fileid — unchanged content dedup-hits its surviving H record and keeps its chunkid (R681)
  3.5. dropFileChunkOccurrences(oldFrec, oldFileID) — the removal set is derived here, in-txn, from the committed F record, never supplied by the caller (R682)
  3.6. fire ReindexCallback(tx, orphaned, new) (R683); mark p consumed; return fileid (R679)
```

Diagram 4 — Compute an append, transaction-free (`PrepareAppend`):

```
4. PrepareAppend(path, lastLocator, content, strategy, opts)
  4.1. resolve chunker for strategy — no txn (R678, R685)
  4.2. dispatch: AppendAwareChunker.AppendChunks(path, lastLocator, content, yield) -> replacedLast; else plain Chunker.Chunks(path, content, yield), replacedLast=false (R685)
  4.3. per yielded chunk: validate UTF-8, fire WithAppendChunkCallback (R683), SHA-256 hash, TrigramCounts, tokenize (R685)
  4.4. ErrAppendBoundary if not append-aware and zero chunks came from non-empty content
  4.5. assemble opaque *PreparedAppend{chunks, replacedLast, assumedLastLocator=lastLocator}; return having opened no bbolt txn (R685, R686)
```

Diagram 5 — Store an append, one write txn (`AppendPrepared`):

```
5. AppendPrepared(fileid, p, opts)
  5.1. reject if p is already consumed
  5.2. open bbolt Update txn; re-read the F record for fileid
  5.3. if p.replacedLast: verify the committed last entry's locator equals p.assumedLastLocator, else return ErrAppendTailMoved (R688)
  5.4. drop-and-replace the last chunk when p.replacedLast; store p's chunks — H dedup/insert, coalesced T/W, corpus counters, F update (R687)
  5.5. mark p consumed; return nil (R687)
```
