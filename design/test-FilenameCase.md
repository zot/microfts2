# Test Design: Case-insensitive filenames
**Source:** crc-DB.md

The tests run on a case-sensitive filesystem, so each writes the spellings it
uses to disk with identical content. That stands in for a case-insensitive
filesystem, where both spellings name one file.

## Test: the option persists and defines the fold
**Purpose:** `CaseInsensitiveFilenames` is written at create time and read back on open; `FilenameKey` folds whole paths, leaves `tmp://` alone, and is the identity when the option is off
**Input:** create a DB with the option, close, reopen; create a second DB without it
**Expected:** reopened: `FilenameKey("/d/Casey.md")` is `/D/CASEY.MD`, `FilenameKey("tmp://Casey")` is unchanged; without the option the path comes back unchanged
**Refs:** crc-DB.md, R736, R737, R738, R743
**Code:** filename_case_test.go
**Alarm:** 1
**Fire alarm:** stop writing the `caseInsensitiveFilenames` I record; red is the reopened DB returning `/d/Casey.md` unfolded
**Inject:** db.go:writeSettings
**Pulled:** 2026-10-26 — rang: reopened `FilenameKey = "/d/Casey.md", want /D/CASEY.MD`

## Test: another spelling reaches the same file
**Purpose:** N keys come from the filename key, so another spelling finds the indexed file, and a read does not respell it
**Input:** option on; AddFile `Casey.md`; GetChunks via `CASEY.md`; FileIDPaths
**Expected:** GetChunks returns the chunk; FileIDPaths reports `Casey.md`
**Refs:** crc-DB.md, R739, R763
**Code:** filename_case_test.go
**Alarm:** 2
**Fire alarm:** make `FilenameKey` return the path unchanged; red is GetChunks via `CASEY.md` reporting file not found
**Inject:** db.go:DB.FilenameKey
**Pulled:** 2026-10-26 — rang: `GetChunks via CASEY.md: file not found` (also fails the persistence and warm-cache tests)

## Test: the warm path cache folds too
**Purpose:** `pathToID`, once loaded, is keyed by the filename key and agrees with the N records
**Input:** option on; AddFile `Casey.md`; FileIDPaths (loads the cache); GetChunks via `CASEY.md`
**Expected:** GetChunks returns the chunk
**Refs:** crc-DB.md, R742
**Code:** filename_case_test.go
**Alarm:** 3
**Fire alarm:** build the reverse cache from the raw `Name`; red is GetChunks via `CASEY.md` reporting file not found through the cache
**Inject:** db.go:DB.loadPathCache
@alarm-hash: 99c86b575f7e104a
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved loadPathCache: reverse map keyed by the raw path, TestFilenameCaseWarmCacheFolds red (`file not found` via the other spelling), three sibling warm-cache tests red too; restored clean *Earlier —* 2026-11-04 — rang (re-pulled for the one-name record, 2026-11-04): reverse cache built from the raw Name; GetChunks via CASEY.md reported file not found *Earlier —* 2026-10-26 — rang: `GetChunks via other spelling with a warm cache: file not found`
## Test: removal by another spelling clears the N records
**Purpose:** removal deletes the N records under the folded key, so the file can be added again
**Input:** option on; AddFile `Casey.md`; RemoveFile `CASEY.md`; AddFile `Casey.md`
**Expected:** the remove succeeds and the re-add returns a fileid with no error
**Refs:** crc-DB.md, R739
**Code:** filename_case_test.go
**Alarm:** 4
**Fire alarm:** delete N records by the raw path in `deleteFileMeta`; red is the re-add returning `ErrAlreadyIndexed`
**Inject:** db.go:DB.deleteFileMeta
**Pulled:** 2026-10-26 — rang: `re-add after removal: file already indexed`

## Test: a warm-cache add caches the filename key
**Purpose:** a file added after `pathToID` is loaded is cached under its filename key
**Input:** option on; FileIDPaths on the empty DB (loads the cache); AddFile `Casey.md`; GetChunks via `CASEY.md`
**Expected:** GetChunks returns the chunk
**Refs:** crc-DB.md, R742
**Code:** filename_case_test.go
**Alarm:** 5
**Fire alarm:** cache the raw path in `storePrepared`; red is GetChunks via `CASEY.md` reporting file not found. Found by injecting past the list: no earlier test warmed the cache before adding
**Inject:** db.go:DB.storePrepared
@alarm-hash: 53b3df9faa40890d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved storePrepared: pathToID keyed by the raw path, TestFilenameCaseWarmCacheAdd red, `file not found` via the other spelling; restored clean *Earlier —* 2026-11-05 — rang — raw path cached in storePrepared: TestFilenameCaseWarmCacheAdd red, GetChunks via `CASEY.md` "file not found"; restored clean *Earlier —* 2026-10-26 — rang: `GetChunks via other spelling after a warm-cache add: file not found`
## Test: a warm-cache removal by another spelling drops the entry
**Purpose:** removal clears the cache entry under the filename key
**Input:** option on; AddFile `Casey.md`; FileIDPaths; RemoveFile `CASEY.md`; FileIDPaths
**Expected:** FileIDPaths is empty
**Refs:** crc-DB.md, R742
**Code:** filename_case_test.go
**Alarm:** 6
**Fire alarm:** delete the cache entry by the raw path on removal; red is FileIDPaths still listing `Casey.md`
**Inject:** db.go:DB.removeFileWithCallbackImpl
@alarm-hash: 032c188aadb83a3e
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved removeFileWithCallbackImpl: cache key the raw path, TestFilenameCaseWarmCacheRemove red, FileIDPaths still lists the removed file; restored clean *Earlier —* 2026-10-26 — rang: `FileIDPaths still lists map[1:…/Casey.md]`
## Test: a warm-cache reindex by another spelling replaces the fileid
**Purpose:** reindex retires the old fileid's cache entry found under the filename key
**Input:** option on; AddFile `Casey.md`; FileIDPaths; Reindex `CASEY.md`; FileIDPaths; GetChunks via `Casey.md`
**Expected:** one file listed; GetChunks returns the chunk
**Refs:** crc-DB.md, R742
**Code:** filename_case_test.go
**Alarm:** 7
**Fire alarm:** key the cache by the raw path in `reindexPrepared`; red is two files listed and GetChunks reading the deleted fileid
**Inject:** db.go:DB.reindexPrepared
@alarm-hash: af9fa204f41eead9
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved reindexPrepared: cache key the raw path, TestFilenameCaseWarmCacheReindex red, FileIDPaths lists 2 files, want 1; restored clean *Earlier —* 2026-11-05 — rang — cache keyed by the raw path in reindexPrepared: TestFilenameCaseWarmCacheReindex red, FileIDPaths lists 2 files and GetChunks reads the deleted fileid; restored clean *Earlier —* 2026-10-26 — rang: `FileIDPaths lists 2 files, want 1`; `read F record 1: not found`
## Test: with the option off, spellings are distinct files
**Purpose:** the default is unchanged: two spellings index as two files
**Input:** option off; AddFile `Casey.md` and `CASEY.md`
**Expected:** both succeed with distinct fileids
**Refs:** crc-DB.md, R737
**Code:** filename_case_test.go

## Test: AddFile by another spelling respells and refuses
**Purpose:** the last spelling wins: Name, the final N value and the fileid are checked after AddFile by a new spelling
**Input:** option on; AddFile `Casey.md`; AddFile `CASEY.md`; read FileIDPaths on a cold cache and the final N record directly
**Expected:** the second AddFile returns `ErrAlreadyIndexed`; one file, same fileid, stored as `CASEY.md` in both Name and the N value; GetChunks via `Casey.md` still works
**Refs:** crc-DB.md, R764, R746
**Code:** filename_case_test.go
**Alarm:** 8
**Fire alarm:** (1) skip the respell; red is the stored spelling still `Casey.md`. (2) return the error from inside the write transaction so bbolt rolls back; same red. (3) rewrite Name but not the N value; red is the N record still naming `Casey.md`
**Inject:** db.go:DB.respell, db.go:DB.storePrepared
@alarm-hash: 81b1d9debc131821
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved storePrepared, three injections each from a clean tree: respell skipped, ErrAlreadyIndexed returned inside the txn, N value left at the old name; each red in TestFilenameCaseAddFileRespells (F record or final N value naming the old spelling); restored clean *Earlier —* 2026-11-05 — rang — (1) respell skipped in storePrepared: TestFilenameCaseAddFileRespells red, the stored spelling and the final N value still `Casey.md`; restored clean *Earlier —* 2026-11-04 — rang (re-pulled for the one-name record, 2026-11-04): with the Name not respelled, the F record still stored Casey.md; with the N value not rewritten, the final N value still named Casey.md *Earlier —* 2026-10-26 — all three rang: (1) and (2) `F record after respell: …Casey.md` plus the N value and warm-cache failures; (3) `final N value after respell names …Casey.md`
## Test: respelling refreshes a warm path cache
**Purpose:** the in-memory path cache reports the new spelling at once
**Input:** option on; AddFile `Casey.md`; FileIDPaths (warm); AddFile `CASEY.md`; FileIDPaths
**Expected:** FileIDPaths reports `CASEY.md`
**Refs:** crc-DB.md, R764
**Code:** filename_case_test.go
**Alarm:** 9
**Fire alarm:** skip the pathCache update after a respell; red is FileIDPaths still reporting `Casey.md`
**Inject:** db.go:DB.storePrepared
@alarm-hash: 53b3df9faa40890d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved storePrepared: pathCache refresh removed from the respell block, TestFilenameCaseRespellWarmCache red, warm cache reports the old spelling; restored clean *Earlier —* 2026-11-05 — rang — pathCache update skipped after a respell: TestFilenameCaseRespellWarmCache red, warm cache still reports `Casey.md`; restored clean *Earlier —* 2026-10-26 — rang: `warm cache after respell reports …Casey.md`
## Test: Reindex by another spelling respells
**Purpose:** reindex stores the spelling it was given
**Input:** option on; AddFile `Casey.md`; Reindex `CASEY.md`; FileIDPaths on a cold cache
**Expected:** one file, stored as `CASEY.md`
**Refs:** crc-DB.md, R745
**Code:** filename_case_test.go

## Test: a respell inside an active search cache drops the cached F record
**Purpose:** the opt-in F-record cache does not keep serving the old spelling
**Input:** option on; NewSearchCache; AddFile `Casey.md`; FileInfoByID (caches the F record); AddFile `CASEY.md`; FileInfoByID
**Expected:** Name is `CASEY.md`
**Refs:** crc-DB.md, R764
**Code:** filename_case_test.go
**Alarm:** 10
**Fire alarm:** keep the frecordCache entry after a respell; red is the cached record still naming `Casey.md`. Found by injecting past the list: no earlier test ran a respell with the cache active
**Inject:** db.go:DB.storePrepared
@alarm-hash: 53b3df9faa40890d
**Pulled:** 2026-11-05 — 2026-11-05 — rang — re-pulled after #221 moved storePrepared: frecordCache delete removed from the respell block, TestFilenameCaseRespellSearchCache red, cached F record names the old spelling; restored clean *Earlier —* 2026-11-05 — rang — frecordCache entry kept after a respell: TestFilenameCaseRespellSearchCache red, the cached F record still names `Casey.md`; restored clean *Earlier —* 2026-10-26 — rang: `cached F record after respell names […Casey.md]`
