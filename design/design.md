# microfts2

Dynamic trigram index, bbolt-backed (pure Go). Go library with CLI.

## Cross-cutting Concerns

### Transactions
All DB operations use bbolt transactions. Reads use `db.View` (read-only) txns; writes use `db.Update` (read-write) txns. bbolt allows one writer at a time; concurrent readers are fine. A `bbolt.Tx` spans all buckets, so the host (ark) can read its own bucket and microfts2's `fts` bucket in one atomic txn (R662).

### Key and Value Encoding
Integer fields use varint encoding (`binary.PutUvarint` / `binary.ReadUvarint`). Trigram fields are fixed 3 bytes (24-bit). Hash fields are fixed 32 bytes (SHA-256). Strings are length-prefixed (varint length + bytes), except the final field in a key can use remaining bytes. Record structs (CRecord, FRecord, TRecord, WRecord) encode with `MarshalValue` and decode with `UnmarshalCValue`/`UnmarshalFValue`/`UnmarshalTValue`/`UnmarshalWValue`; the H record is a bare chunkid varint with no struct.

### Error Handling
Go idiomatic error returns. CLI prints to stderr and exits non-zero.

## Artifacts

### CRC Cards
- [x] crc-DB.md → `db.go`, `records.go`
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
- [x] seq-fuzzy-trigram.md → `db.go`, `overlay.go`, `cmd/microfts/main.go`
- [x] seq-chunker-dispatch.md → `chunker.go`, `db.go`, `cache.go`
- [x] seq-prepare-store.md → `db.go`

### Test Designs
- [x] test-CharSet.md → `charset_test.go`
- [x] test-Bitset.md → `bitset_test.go`
- [x] test-DB.md → `db_test.go`
- [x] test-Chunker.md → `chunker_test.go`
- [x] test-Overlay.md → `overlay_test.go`
- [x] test-CLI.md → `cmd/microfts/main_test.go`
- [x] test-ChunkAttrs.md → `chunk_attrs_test.go`

## Gaps

- [x] O1: Missing test: TestDBReindex (test-DB.md 'reindex with different strategy')
- [x] O2: Missing test: TestDBLongFilename (test-DB.md 'key chain for long filename')
- [x] O3: No unit tests for keychain.go (EncodeFilename, DecodeFilename, FinalKey)
- A1: No unit tests for chunker.go — shells out to external commands, integration-only
- A2: Requirement numbering non-sequential — cosmetic, not renumbering to avoid breaking all CRC refs
- [ ] O4: No test for density scoring (WithDensity search option)
- [x] O5: ~~R record roundtrip — R records removed in LMDB reorganization~~
- [x] O6: No test for CharSet.TrigramCounts
- [x] O7: ~~sparse C record encode/decode — old C records removed in LMDB reorganization~~
- [x] O8: Packed trigram functions removed (A record eliminated)
- A3: Removed requirements uncovered: R7, R8, R14, R15, R16, R19, R21, R28, R30, R36, R48, R54, R75, R76, R83, R95, R102, R109, R123, R138, R145, R148, R149, R154, R155 — old two-tree layout, forward/reverse index, per-trigram C records, N record JSON
- A4: Bigram index removed — R379-R416 retired (T17-T54; the removal predated mini-spec retirement and was made formal 2026-06-12). SearchFuzzy (trigram OR-union) handles typo-tolerant search. Bigrams were slow (2.5s on 74K chunks) and fat (1.7x index size). DB format version reverted to "2".
- [ ] O9: No test for WRecord encode/decode roundtrip
- [x] O10: No test for WithAfter/WithBefore date filtering (needs chunker producing Attrs with timestamp)
- [x] O11: Implementation: db.go needs full rewrite for new record layout (single subdatabase, chunk dedup, record structs, T/W records, ChunkFilter)
- [ ] O12: SearchOption enumeration not fully anchored in requirements — WithOnly and WithExcept exist in code without spec/requirement coverage; audit all SearchOptions against requirements
- [ ] O13: ChunkFilter on overlay candidates lacks index transaction context — filters using Tx() or FileRecord() will get zero values on tmp:// chunks
- [x] O14: ~~R417: Bigram OR-union candidate set size unbounded — monitor performance on large corpora, add filtering if needed~~ — moot; bigram index removed (A4)
- [x] O15: ~~resolveChunkText and chunkTextByRangeFile defined but not called~~ — resolved by removing ChunkTexter entirely; RandomAccessChunker (R524) supersedes it
- [x] O16: All four built-in text chunkers (`LineChunker`, `MarkdownChunker`, `BracketChunker`, `IndentChunker`) implement `AppendAwareChunker` via the shared `appendByRechunkResume` helper, and all four implement `FileChunker` via the shared `fileChunksByRead` helper (R633, R636). The `DB.AppendChunks` silent-no-op guard (R623, `ErrAppendBoundary`) catches the case where a non-AppendAware custom chunker is used and produces no chunks for non-empty input.
- T1: R308 retired by R309 (2026-05-04 BracketLang unification: StringDelim folded into BracketGroup via Escape+AllowedInner+AllowedParent)
- [ ] O17: ChunkCache.ChunkText cannot serve tmp:// paths (lookupFileByPath is index-only — pre-existing); tmp:// retrieval goes through getChunksTmp instead, which surfaces overlay-stored Attrs (R655). If ChunkCache gains tmp:// support it must read overlay Attrs the same way.
- [ ] O18: **A heredoc body is scanned as code, so one bracket character inside it tears the enclosing group in two.** `BracketGroup.Close` is a **fixed list**, so `<<EOF` … `EOF` — whose closing delimiter is chosen at the open — cannot be expressed as a bracket group at all, and the body is therefore tokenized normally. `shell` is a registered language and heredocs are ordinary shell, so this is not a corner. Measured 2026-08-21 with `chunk-bracket -lang shell` over a nine-line script whose `greet()` wraps a heredoc, isolated against a control differing by **one character**: with no bracket character in the heredoc body the chunker returns `1-8, 10-12` and `greet()` is one chunk; adding a single `}` inside the body returns `1-4, 5-8, 10-12`, tearing the function in two with **the second chunk starting mid-string**. For a search tool the chunk is the retrieval unit, so a hit in the tail of that function returns a fragment that cannot say which function it came from. *Root shared with `O19`: the tokenizer counts a closer it never saw opened.* **The repair is not a heredoc special case** — `mini-spec`'s [carves/chunkers-over-sdom.md](../../mini-spec/carves/chunkers-over-sdom.md) Item 1 measured the same residue from the representation side, and a construct whose close is not statically known needs the group to carry an **open-time-bound close** rather than a fixed list. Filed here rather than left in that carve's Item 6 handoff because the defect is live in this repository today (Bill, 2026-08-21)
- [ ] O19: **An unmatched word closer in ordinary text closes a group that was never opened.** `LangShell` makes `done` a `Close` for the `while`/`for` group, and the tokenizer honours it with no matching opener on the stack — so `echo done`, a line anyone writes, ends the enclosing chunk. Measured 2026-08-21, isolated against a control differing by **one word**: `alpha() { echo done … }` returns `1-3, 4-5, 7-9` and tears the function in two, while the same file with `echo finished` returns `1-5, 7-9` and is correct. **This is the sharper of the pair** because it needs no exotic construct — no heredoc, no unbalanced punctuation, just a shell word that happens to be a keyword elsewhere. It is also the argument for **word brackets earning a named test** rather than riding along with the punctuation ones: `}` in running text is rare, an English word is not. Candidate repair: a `Close` matches only when the corresponding `Open` is on the stack, which is a property of the stack rather than of the language table — and worth checking against `if`/`fi`, `case`/`esac` and Pascal's `begin`/`end` before it is called general. *Root shared with `O18`*
- T2: R648 retired by R655 (2026-06-03 content-transform-index-only: retrieval no longer transforms; returns original content)
- T3: R651 retired by R655 (2026-06-03 content-transform-index-only: fast path reads stored C-record Attrs, no transform repopulation)
- T4: R652 retired by R656 (2026-06-03 content-transform-index-only: dedup hash over original Content only, Attrs dropped from hash)
- T5: R653 retired by R657 (2026-06-03 content-transform-index-only: dedup identity is original Content)
- T6: R654 retired by R658 (2026-06-03 content-transform-index-only: tag edit changes original Content, re-indexes naturally)
- T7: R642 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T8: R643 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T9: R644 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T10: R645 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T11: R646 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T12: R647 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T13: R649 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T14: R650 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T15: R658 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T16: R659 retired (2026-06-03 retire-content-transform: ContentTransform hook rolled back; trigram index keeps tags (full-text), stripping moves ark-side)
- T17: R379 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T18: R380 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T19: R381 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T20: R382 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T21: R383 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T22: R384 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T23: R385 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T24: R386 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T25: R387 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T26: R388 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T27: R389 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T28: R390 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T29: R391 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T30: R392 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T31: R393 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T32: R394 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T33: R395 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T34: R396 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T35: R397 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T36: R398 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T37: R399 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T38: R400 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T39: R401 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T40: R402 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T41: R403 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T42: R404 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T43: R405 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T44: R406 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T45: R407 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T46: R408 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T47: R409 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T48: R410 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T49: R411 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T50: R412 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T51: R413 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T52: R414 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T53: R415 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T54: R416 retired (2026-06-12 bigram-removed (design.md A4); SearchFuzzy supersedes)
- T55: R91 retired by R661 (2026-06-15 lmdb-to-bbolt: R91 LMDB API/contract -> R661 bbolt)
- T56: R101 retired by R666 (2026-06-15 lmdb-to-bbolt: R101 LMDB API/contract -> R666 bbolt)
- T57: R264 retired by R663 (2026-06-15 lmdb-to-bbolt: R264 LMDB API/contract -> R663 bbolt)
- T58: R571 retired by R664 (2026-06-15 lmdb-to-bbolt: R571 LMDB API/contract -> R664 bbolt)
- T59: R2 retired by R660 (2026-06-15 lmdb-to-bbolt: R2 LMDB API/contract -> R660 bbolt)
- T60: R218 retired by R662 (2026-06-15 lmdb-to-bbolt: R218 LMDB API/contract -> R662 bbolt)
- T61: R546 retired by R665 (2026-06-15 lmdb-to-bbolt: R546 LMDB API/contract -> R665 bbolt)
- T62: R555 retired by R665 (2026-06-15 lmdb-to-bbolt: R555 LMDB API/contract -> R665 bbolt)
- T63: R201 retired by R703 (2026-10-24 getchunks-range-match: ChunkResult gains Attrs (already present) and Missing)
- T64: R347 retired by R705 (2026-10-24 fuzzy-only-after-cut: R347 stated chunk-level discard; code has always discarded per result)
- T65: R342 retired by R712 (2026-10-24 verify-exact-only: WithVerify no longer composes with WithLoose)
- T66: R344 retired by R713 (2026-10-24 verify-exact-only: named the loose flag by its pre-rename name -fuzzy, and -verify no longer composes)
- T67: R234 retired by R717 (2026-10-25 f-chunker-version: F header gained chunkerVersion)
- T68: R451 retired by R717 (2026-10-25 f-chunker-version: header decode gained ChunkerVersion)
- [x] O20: Delete cmd/microfts2-migrate-v3 (and crc-MigrateV3.md, test-MigrateV3.md) once ark's databases are at format 3; retire R722-R725 then — done 2026-10-25: ark migrated its only database (request microfts2-chunker-stamp-migrated)
- T69: R625 retired by R726 (an append with no chunks now updates the F record (R728))
- T70: R720 retired by R727 (the version check now covers appends with no chunks)
- T71: R722 retired (migration program deleted after ark migrated (O20))
- T72: R723 retired (migration program deleted after ark migrated (O20))
- T73: R724 retired (migration program deleted after ark migrated (O20))
- T74: R725 retired (migration program deleted after ark migrated (O20))
- T75: R417 retired (bigram search deleted (fuzzy trigram replaced it); converted from a hand-struck line the tool could not read)
- T76: R503 retired by R524 (ChunkTexter removed, superseded by RandomAccessChunker; converted from a hand-struck line)
- T77: R504 retired (removed with ChunkTexter; converted from a hand-struck line)
- T78: R517 retired by R529 (ChunkText dispatch removed with ChunkTexter; RandomAccessChunker dispatch replaces it; converted from a hand-struck line)
- T79: R521 retired (removed with ChunkTexter; converted from a hand-struck line)
- T80: R522 retired (removed with ChunkTexter; converted from a hand-struck line)
- T81: R523 retired (removed with ChunkTexter; converted from a hand-struck line)
- T82: R265 retired by R734 (2026-10-26 TxnHolder is bucket-based: Bucket() accessor, bucketWrap)
- T83: R267 retired by R734 (2026-10-26 TxnHolder is bucket-based: bucketWrap replaced txnWrap)
- T84: R663 retired by R734 (2026-10-26 TxnHolder accessor is Bucket(), not Tx(); bucketWrap, not txnWrap)
- T85: R252 retired by R735 (2026-10-26 encode/decode is MarshalValue + UnmarshalXValue on C/F/T/W; migration 001's planned R252->R668 pairing was never applied and was wrong)
- T86: R248 retired (2026-10-26 HRecord deleted: dead exported type, never marshalled)
