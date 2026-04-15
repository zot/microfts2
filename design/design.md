# microfts2

Dynamic LMDB trigram index. Go library with CLI.

## Cross-cutting Concerns

### LMDB Transactions
All DB operations use LMDB transactions. Reads use read-only txns. Writes use read-write txns. LMDB supports one writer at a time; concurrent readers are fine.

### Key and Value Encoding
Integer fields use varint encoding (`binary.PutUvarint` / `binary.ReadUvarint`). Trigram fields are fixed 3 bytes (24-bit). Hash fields are fixed 32 bytes (SHA-256). Strings are length-prefixed (varint length + bytes), except the final field in a key can use remaining bytes. Record structs (CRecord, FRecord, TRecord, WRecord, HRecord) handle marshal/unmarshal.

### Error Handling
Go idiomatic error returns. CLI prints to stderr and exits non-zero.

## Artifacts

### CRC Cards
- [x] crc-DB.md → `db.go`
- [x] crc-CharSet.md → `charset.go`
- [x] crc-Bitset.md → `bitset.go`
- [x] crc-Chunker.md → `chunker.go`
- [x] crc-KeyChain.md → `keychain.go`
- [x] crc-CLI.md → `cmd/microfts/main.go`
- [x] crc-ChunkCache.md → `cache.go`

- [x] crc-BracketChunker.md → `bracket_chunker.go`
- [x] crc-IndentChunker.md → `indent_chunker.go`
- [x] crc-Overlay.md → `overlay.go`

### Sequences
- [x] seq-init.md → `db.go`
- [x] seq-add.md → `db.go`, `chunker.go`, `charset.go`, `keychain.go`
- [x] seq-search.md → `db.go`, `charset.go`
- [x] seq-score.md → `db.go`, `charset.go`
- [x] seq-stale.md → `db.go`, `cmd/microfts/main.go`
- [x] seq-append.md → `db.go`
- [x] seq-chunks.md → `db.go`, `cmd/microfts/main.go`
- [x] seq-search-multi.md → `db.go`
- [x] seq-cache.md → `cache.go`
- [x] seq-bracket-chunk.md → `bracket_chunker.go`
- [x] seq-indent-chunk.md → `indent_chunker.go`
- [x] seq-fuzzy-search.md → `db.go`
- [x] seq-tmp-add.md → `overlay.go`, `db.go`
- [x] seq-tmp-search.md → `overlay.go`, `db.go`
- [x] seq-fuzzy-trigram.md → `db.go`, `cmd/microfts/main.go`
- [x] seq-chunker-dispatch.md → `chunker.go`, `db.go`, `cache.go`

### Test Designs
- [x] test-CharSet.md → `charset_test.go`
- [x] test-Bitset.md → `bitset_test.go`
- [x] test-DB.md → `db_test.go`
- [x] test-Chunker.md → `chunker_test.go`
- [x] test-Overlay.md → `overlay_test.go`

## Gaps

- [x] O1: Missing test: TestDBReindex (test-DB.md 'reindex with different strategy')
- [x] O2: Missing test: TestDBLongFilename (test-DB.md 'key chain for long filename')
- [x] O3: No unit tests for keychain.go (EncodeFilename, DecodeFilename, FinalKey)
- [ ] A1: No unit tests for chunker.go — shells out to external commands, integration-only
- [ ] A2: Requirement numbering non-sequential — cosmetic, not renumbering to avoid breaking all CRC refs
- [ ] O4: No test for density scoring (WithDensity search option)
- [x] O5: ~~R record roundtrip — R records removed in LMDB reorganization~~
- [x] O6: No test for CharSet.TrigramCounts
- [x] O7: ~~sparse C record encode/decode — old C records removed in LMDB reorganization~~
- [x] O8: Packed trigram functions removed (A record eliminated)
- [ ] A3: Removed requirements uncovered: R7, R8, R14, R15, R16, R19, R21, R28, R30, R36, R48, R54, R75, R76, R83, R95, R102, R109, R123, R138, R145, R148, R149, R154, R155 — old two-tree layout, forward/reverse index, per-trigram C records, N record JSON
- [ ] A4: Bigram index removed — R379-R412 no longer implemented. SearchFuzzy (trigram OR-union) handles typo-tolerant search. Bigrams were slow (2.5s on 74K chunks) and fat (1.7x index size). Version reverted to "2"
- [ ] O9: No test for WRecord encode/decode roundtrip
- [x] O10: No test for WithAfter/WithBefore date filtering (needs chunker producing Attrs with timestamp)
- [x] O11: Implementation: db.go needs full rewrite for new record layout (single subdatabase, chunk dedup, record structs, T/W records, ChunkFilter)
- [ ] O12: SearchOption enumeration not fully anchored in requirements — WithOnly and WithExcept exist in code without spec/requirement coverage; audit all SearchOptions against requirements
- [ ] O13: ChunkFilter on overlay candidates lacks LMDB transaction context — filters using Txn() or FileRecord() will get zero values on tmp:// chunks
- [x] O14: R417: Bigram OR-union candidate set size unbounded — monitor performance on large corpora, add filtering if needed
- [x] O15: resolveChunkText and chunkTextByRangeFile are defined but not yet called — will be needed when a FileChunker is registered