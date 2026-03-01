# microfts2

Dynamic LMDB trigram index. Go library with CLI.

## Cross-cutting Concerns

### LMDB Transactions
All DB operations use LMDB transactions. Reads use read-only txns. Writes use read-write txns. LMDB supports one writer at a time; concurrent readers are fine.

### Key Encoding
Fixed-size integer fields in LMDB keys use big-endian encoding for correct lexical sort. Fileid and chunknum are 8-byte big-endian uint64. Trigram values are 3-byte big-endian (18 bits in 24 bits).

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

### Sequences
- [x] seq-init.md → `db.go`
- [x] seq-add.md → `db.go`, `chunker.go`, `charset.go`, `bitset.go`, `keychain.go`
- [x] seq-search.md → `db.go`, `charset.go`
- [x] seq-build-index.md → `db.go`, `bitset.go`

### Test Designs
- [x] test-CharSet.md → `charset_test.go`
- [x] test-Bitset.md → `bitset_test.go`
- [x] test-DB.md → `db_test.go`

## Gaps

- [x] O1: Missing test: TestDBReindex (test-DB.md 'reindex with different strategy')
- [x] O2: Missing test: TestDBLongFilename (test-DB.md 'key chain for long filename')
- [x] O3: No unit tests for keychain.go (EncodeFilename, DecodeFilename, FinalKey)
- [ ] A1: No unit tests for chunker.go — shells out to external commands, integration-only
- [ ] A2: Requirement numbering non-sequential (R45-R47 inserted between R9 and R10) — cosmetic, not renumbering to avoid breaking all CRC refs