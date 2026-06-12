# Migration: lmdb-go (CGO) → bbolt (pure Go) store

**Source survey:** `~/work/ark/.scratch/BBOLT.md` (both repos surveyed
2026-06-12). microfts2 is the **env-owning half** and **ports first**; ark's
store port (its own `specs/migrations/lmdb-to-bbolt.md`) consumes the handle
this migration changes.

## Status (2026-06-12) — NOT STARTED

Spec phase. microfts2 clean w.r.t. git.

## Problem

microfts2 links `github.com/bmatsuo/lmdb-go` → LMDB as a **CGO** dependency.
CGO makes the binary impossible to cross-compile without a C toolchain, so
there is no `GOOS/GOARCH` release sweep. microfts2 is one of the two remaining
CGO deps in the ark ecosystem (the other is ark's own store, which **shares
microfts2's LMDB env** — see Ark Integration below). Both must move to pure Go
before `CGO_ENABLED=0` is achievable. The yzma migration already removed the
embedding-engine CGO dep and is **blocked on this work** (ark
`specs/migrations/yzma-embedding.md`, R2971/R2972).

## State B (target)

microfts2 binds **`go.etcd.io/bbolt`** — a pure-Go, single-writer/multi-reader,
mmap'd B+tree. The binary compiles `CGO_ENABLED=0`. The concurrency model is
unchanged (one writer, many concurrent readers — same as LMDB and ark's write
actor). The single `fts` LMDB subdatabase becomes a single bbolt **bucket** of
the same name. One `*bbolt.DB` file; ark opens its own `ark` bucket inside it
(the shared-env linchpin, preserved — a `bbolt.Tx` spans all buckets).

bbolt is read-heavy-single-user-friendly and has no compaction step or map-size
ceiling. Its one weakness vs LMDB — no DUPSORT — does not apply: microfts2 uses
**no DUPSORT** (confirmed by survey; multi-value records pack varint lists in
the value).

## Changes

### Public API (the contract ark depends on)

The transaction and environment types flip from LMDB to bbolt. Every signature
below is consumed by ark across the shared env, so each is a forcing change:

- `func (db *DB) Env() *lmdb.Env` → `func (db *DB) DB() *bbolt.DB`. The host
  process opens microfts2 first and passes this handle to ark's store.
- `type TxnHolder interface { Txn() *lmdb.Txn }` → `{ Tx() *bbolt.Tx }`. The
  accessor returns a bbolt transaction. (`txnWrap`, `CRecord` adjust to match.)
- `func (c *CRecord) Txn() *lmdb.Txn` → `func (c *CRecord) Tx() *bbolt.Tx`;
  the unexported `CRecord.txn` field retypes to `*bbolt.Tx`.
- `func (db *DB) ReadCRecord(txn *lmdb.Txn, chunkID uint64) (CRecord, error)`
  → `ReadCRecord(tx *bbolt.Tx, chunkID uint64)`. ark calls this at ~12 sites.
- `type RemoveCallback func(txn *lmdb.Txn, orphanedChunkIDs []uint64) error`
  → `func(tx *bbolt.Tx, …)`. ark registers it (indexer.go).
- `type ReindexCallback func(txn *lmdb.Txn, orphaned, new []uint64) error`
  → `func(tx *bbolt.Tx, …)`. ark registers it (indexer.go).

### Options

bbolt has neither named-DB limits nor a pre-declared map size:

- `Options.MaxDBs` — **removed.** LMDB-only (max named subdatabases). bbolt
  buckets are unbounded. ark stops passing it.
- `Options.MapSize` — **removed.** bbolt grows the file automatically. ark
  stops passing it.
- `Options.DBName` (subdatabase/bucket name, default `fts`) — **kept**; now
  names the bbolt bucket.
- `CaseInsensitive`, `Aliases` — unchanged (chunking/charset, not storage).

No back-compat shim: microfts2 is a **subsidiary library of ark** — created
purely to service it, ark is its only real consumer, and they share the
go.work. The API is rewritten directly and ark is updated in lockstep (#1a);
there is no external caller to preserve.

### Internal: the `(TxnHolder, lmdb.DBI)` pair collapses to a bucket

LMDB passes a transaction and a DBI handle as two separate values; ~30 internal
helpers take `(th TxnHolder, dbi lmdb.DBI, …)` and call `th.Txn().Get(dbi, …)`.
In bbolt a **bucket carries its transaction**, so the pair collapses: helpers
obtain the `fts` bucket from the tx (`tx.Bucket([]byte(name))`) and operate on
it directly. The `dbi lmdb.DBI` parameter is dropped throughout. This is a real
internal refactor of the txn/dbi abstraction, not a line-for-line swap — design
phase owns the concrete shape (carry the bucket on the holder, or derive it per
helper).

### Storage operations (mechanical)

- `lmdb.NewEnv`/`SetMaxDBs`/`SetMapSize`/`env.Open` → `bbolt.Open(path, 0644, …)`.
- `txn.OpenDBI(name, lmdb.Create)` → `tx.CreateBucketIfNotExists([]byte(name))`;
  `OpenDBI(name, 0)` → `tx.Bucket(...)`.
- `env.Update`/`env.View(func(txn))` → `db.Update`/`db.View(func(tx *bbolt.Tx))`.
- `txn.Get` + `lmdb.IsNotFound(err)` (15 sites) → `b.Get(k)` returning nil for
  absent keys. `txn.Del` + IsNotFound guard → `b.Delete(k)` (no error on
  missing; guard dropped). `txn.Put(…, 0)` → `b.Put(k, v)` (flags always 0).
- Cursors (5: `loadSettings`, `RecordCounts`, `loadPathCache`, `allChunkIDs`,
  `StaleFiles`) use only `First`/`SetRange`/`Next` → bbolt
  `Cursor.First`/`Seek`/`Next`. All read-only; no delete-during-iteration.
- Value/txn lifetime contract is identical (valid only within the txn); the
  existing copy-out discipline (`make+copy`, `string(v)`, decode-to-struct)
  transfers unchanged.

### Drop dead tooling

Delete `cmd/bigram-estimate/` — it reaches through `Env()` with its own DBI
opens and cursor scans, and is dead tooling (confirmed 2026-06-12). Removing it
deletes the only external `Env()` consumer besides ark. `cmd/microfts` (the
main CLI) goes through the DB API and ports for free.

## Validation

`db_test.go` is the port's gate (90 test/bench fns). The standalone-env test
helper (`lmdb.NewEnv` on a temp dir) becomes `bbolt.Open` on a temp file.
`TestDBEnv` (asserts `Env()` non-nil) becomes the `DB()` non-nil check. Key
coverage: create/open, add+search, remove/reindex (+ raw-tx callbacks),
record-counts and stale scans (the cursor paths), fileid/path cache.

## Supersede at source (Gaps phase)

Old-behavior prose to rewrite so no agent reverts the migration:

- `specs/main.md` — Library API `Env() *lmdb.Env` signature; the **Single
  Subdatabase** / **Why one tree** LMDB-page framing; **Key Chains** "LMDB only
  supports 511 bytes per key" (bbolt's limit is 32768 — see Out of scope);
  `Options.MaxDBs`; the **TxnHolder** doc ("tied to the transaction" stays true,
  `*lmdb.Txn` → `*bbolt.Tx`); record-type tables that say "LMDB record"; the
  **Ark Integration** → **MaxDBs** and **Env accessor** sections and the "LMDB
  does not allow two env handles on the same path" sentence (→ "bbolt allows one
  open `*bbolt.DB` per file; the first library opened shares it").
- `README.md` — "A dynamic **LMDB** trigram index" → bbolt.
- `design/requirements.md` — at `migration-complete`, retire the LMDB-specific
  requirements with replacements: R91→R661 (`Env`→`DB`), R101→R666 (`MaxDBs`),
  R264→R663 (`TxnHolder.Txn`), R571→R664 (`ReadCRecord` sig), R2/R218→R660/R662
  (LMDB subdatabase → bbolt bucket), R252→R668 (encode/decode), and reword the
  R25/R26/R267/R164 prose (511-byte limit, `txnWrap`, single-LMDB-txn).
- `design/` prose layer (cosmetic "LMDB"-as-shorthand — sweep at completion):
  `crc-Overlay.md` ("alongside the LMDB index", "LMDB ids", "no LMDB txn"),
  `crc-Bitset.md`, `crc-KeyChain.md` (511-byte note), `seq-init.md`,
  `seq-tmp-search.md`, `seq-fuzzy-trigram.md`, `seq-search-multi.md`,
  `test-DB.md`, `test-Overlay.md`, and the `design.md` O13/ChunkCache gap notes.
  (Core state-B design — `crc-DB.md` + `design.md` **Transactions** cross-cutting
  — was rewritten in the design phase, 2026-06-12.)
- `CHANGES.md` / `notes.md` / `PERFORMANCE.md` — annotate LMDB references as
  historical where they describe the storage engine.

## Out of scope (follow-ups)

- **N-record key chains.** bbolt's 32 KB key limit makes the long-filename key
  chains unnecessary, but they remain correct under bbolt. Simplifying them is a
  separate concern — this migration keeps them as-is.
- **ark's own store port** — ark `specs/migrations/lmdb-to-bbolt.md` (PENDING
  #1a), depends on the `DB() *bbolt.DB` handle this migration introduces.
- **`CGO_ENABLED=0` build + release sweep** — ark-side, after both stores port
  (yzma R2971/R2972, PENDING #1b).
- **compaction** — microfts2 has no compaction API; ark's `compact.go` (its
  own concern) moves to `bbolt.Tx.WriteTo`.
