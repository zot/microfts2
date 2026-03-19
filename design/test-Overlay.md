# Test Design: Overlay
**Source:** crc-Overlay.md

## Test: add and search tmp file
**Purpose:** Verify tmp:// documents are searchable alongside LMDB documents
**Input:** DB with one disk file indexed, then AddTmpFile with different content
**Expected:** Search returns results from both disk and tmp:// documents
**Refs:** crc-Overlay.md, crc-DB.md, seq-tmp-search.md

## Test: add tmp file duplicate guard
**Purpose:** AddTmpFile returns ErrAlreadyIndexed for duplicate paths
**Input:** AddTmpFile twice with same path
**Expected:** Second call returns ErrAlreadyIndexed
**Refs:** crc-Overlay.md, seq-tmp-add.md

## Test: update tmp file
**Purpose:** UpdateTmpFile replaces content and search reflects changes
**Input:** AddTmpFile, search for old content, UpdateTmpFile with new content, search for both
**Expected:** Old content no longer found, new content found
**Refs:** crc-Overlay.md, seq-tmp-add.md

## Test: update tmp file not found
**Purpose:** UpdateTmpFile returns error for nonexistent path
**Input:** UpdateTmpFile on path never added
**Expected:** Error returned
**Refs:** crc-Overlay.md

## Test: remove tmp file
**Purpose:** RemoveTmpFile removes document from search
**Input:** AddTmpFile, verify searchable, RemoveTmpFile, search again
**Expected:** No results after removal
**Refs:** crc-Overlay.md, seq-tmp-add.md

## Test: remove tmp file not found
**Purpose:** RemoveTmpFile returns error for nonexistent path
**Input:** RemoveTmpFile on path never added
**Expected:** Error returned
**Refs:** crc-Overlay.md

## Test: tmp fileid counting down
**Purpose:** Temporary fileids count down from MaxUint64
**Input:** AddTmpFile twice
**Expected:** First fileid is MaxUint64, second is MaxUint64-1
**Refs:** crc-Overlay.md

## Test: TmpFileIDs returns overlay fileids
**Purpose:** TmpFileIDs returns the set of all tmp:// fileids
**Input:** AddTmpFile twice, call TmpFileIDs
**Expected:** Returned set contains both fileids
**Refs:** crc-Overlay.md, crc-DB.md

## Test: WithExcept excludes tmp results
**Purpose:** WithExcept(TmpFileIDs()) filters out tmp:// results
**Input:** DB with disk file and tmp file, search with WithExcept(TmpFileIDs())
**Expected:** Only disk file results returned
**Refs:** crc-Overlay.md, crc-DB.md, seq-tmp-search.md

## Test: GetChunks with tmp path
**Purpose:** GetChunks works with tmp:// documents using stored content
**Input:** AddTmpFile, GetChunks on the tmp:// path
**Expected:** Returns chunk content from overlay's stored bytes
**Refs:** crc-Overlay.md, crc-DB.md, seq-tmp-search.md

## Test: chunk dedup within overlay
**Purpose:** Identical content in two tmp:// files shares a chunkid
**Input:** AddTmpFile with same content under two different paths
**Expected:** Both files indexed, internal chunkid shared (verify via search — same chunk appears with both paths)
**Refs:** crc-Overlay.md

## Test: BM25 includes overlay counters
**Purpose:** BM25Func sums LMDB and overlay counters for corpus size
**Input:** DB with disk files, add tmp file, call BM25Func
**Expected:** Corpus size reflects both disk and tmp chunks
**Refs:** crc-Overlay.md, crc-DB.md, seq-tmp-search.md

## Test: overlay survives Close
**Purpose:** Overlay is destroyed on DB Close
**Input:** AddTmpFile, Close, reopen, search for tmp content
**Expected:** No tmp results after reopen (overlay is gone)
**Refs:** crc-Overlay.md
