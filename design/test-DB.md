# Test Design: DB
**Source:** crc-DB.md

## Test: create and open
**Purpose:** database lifecycle
**Input:** Create with charset "abcdefghijklmnopqrstuvwxyz0123456789", Close, Open
**Expected:** settings preserved across close/open, I record matches, C record is 2MB
**Refs:** crc-DB.md, seq-init.md

## Test: add file and search
**Purpose:** end-to-end add and retrieval
**Input:** create DB, add a file with known content, search for a substring
**Expected:** search returns correct file path and line range
**Refs:** crc-DB.md, seq-add.md, seq-search.md

## Test: search builds index on demand
**Purpose:** lazy index construction
**Input:** add a file, search without explicit BuildIndex
**Expected:** search succeeds (index built automatically)
**Refs:** crc-DB.md, seq-search.md, seq-build-index.md

## Test: remove file
**Purpose:** file deletion cleans records
**Input:** add file, remove file, search for its content
**Expected:** no results returned
**Refs:** crc-DB.md

## Test: reindex with different strategy
**Purpose:** strategy migration
**Input:** add file with strategy A, reindex with strategy B
**Expected:** N record reflects new strategy, chunks updated
**Refs:** crc-DB.md

## Test: rebuild index with different cutoff
**Purpose:** cutoff change without re-reading files
**Input:** add files, BuildIndex(50), BuildIndex(30)
**Expected:** active trigram set changes, search still returns correct results
**Refs:** crc-DB.md, seq-build-index.md

## Test: key chain for long filename
**Purpose:** filenames exceeding 511 bytes
**Input:** add file with 600-byte path
**Expected:** file added successfully, searchable, filename recoverable
**Refs:** crc-DB.md, crc-KeyChain.md

## Test: custom subdatabase names
**Purpose:** configurable DB names
**Input:** Create with contentName="myc", indexName="myi"
**Expected:** databases created with custom names, operations work normally
**Refs:** crc-DB.md
