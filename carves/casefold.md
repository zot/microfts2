# Case-insensitive filenames

On a case-insensitive filesystem (Windows), renaming `Casey.md` to `CASEY.md`
leaves the file indexed under both names. Both come back from search, and
nothing ever drops the old one, because a stat of the old spelling still
succeeds. ark measured this on a Windows VM on 2026-10-26 and asked for a
database-level option that treats paths differing only in case as one file.

Origin: ark request `microfts2-casefold` (2026-10-26), in ark's `requests/`.

## Status

- [x] ~~**Item 1 — the fold.**~~ **LANDED (2026-10-26 — `#6`.)**
- [x] ~~**Item 2 — respelling: the last spelling wins.**~~ **LANDED (2026-10-26 — `#7`.)**
- [x] ~~**Item 3 — answer ark.**~~ **SENT (Daneel, 2026-10-26.)**

## Decisions

- **DECIDED (Bill, 2026-10-26):** the option is create-time, persisted in an I
  record, and off when absent, so every existing database is unchanged. No
  migration of N records: ark sets it only on databases it creates on Windows.
- **DECIDED (Bill, 2026-10-26):** content trigrams are out of scope. The
  existing `CaseInsensitive` option is unrelated and stays as it is.

## Constraints found in the code

These are facts, checked 2026-10-26, that the request's wording could suggest
otherwise:

- **The final N value is one `[name, fileid]` pair, and lookup never compares
  the name.** `lookupFileByPath` takes the fileid from `parseNFinalValue`
  directly. So folding the key is enough; the stored name can keep its
  original spelling with no change to lookup.
- **`FRecord.Names` only ever holds one entry.** The format allows a list
  ("multiple names for dup/copied files"), but the one writer is
  `Names: []string{fpath}` and every reader takes `Names[0]`. Respelling
  `Names[0]` is therefore correct as the code stands.
- **`AppendChunks` takes a fileid, not a path**, so it never sees a spelling
  and has nothing to respell.
- **Nobody outside the package calls `EncodeFilename`, `FinalKey` or
  `DecodeFilename`** — not ark, not the CLI.

## Item 1

A new `Options` field, `CaseInsensitiveFilenames bool`, written at create time
as an I record beside `caseInsensitive` and read on open into a `DB` field.
With it set, every N key is computed from `strings.ToUpper(path)` over the
whole path, extension included. Uppercase is the fold Windows itself compares
by.

`DB.FilenameKey(path) string` is the one exported definition of the fold, and
returns the path unchanged when the option is off. ark keeps its own path-keyed
state and must fold exactly as microfts2 does.

**The keychain functions stay pure encoders.** ark proposed making
`EncodeFilename` and `FinalKey` `DB` methods so they could see the option. The
aim — no code path in the DB can skip the fold — is met instead by routing
every DB call site through unexported methods that fold first. The exported
functions keep encoding exactly the string they are given, so the API does not
change. The call sites are the N lookup, the dedup guard and N write in
`addFileInTxn`, and `deleteFileMeta`.

**`pathToID` keys by the folded path**, or the cache and the N records
disagree. It is built from `Names[0]` and written in `AddFile`, `RemoveFile`
and the reindex path.

**`tmp://` overlay paths do not fold.** They name no file on any filesystem.

## Item 2

After `Casey.md` becomes `CASEY.md`, the folded key finds the existing record,
but its stored spelling is still `Casey.md`. **Every path-taking write that
reaches an existing record through a different spelling updates the stored
spelling**: `Names[0]`, the final N value, and the `pathToID` entry.

- **`Reindex` respells as a side effect.** It deletes the file's metadata and
  re-adds under the path it was given, so the new spelling lands with no extra
  code. A test pins that.
- **`AddFile` respells, then returns `ErrAlreadyIndexed`.**
  **DECIDED (Bill, 2026-10-26)**, choosing between this and respelling with
  success. The error stays true — the file is already indexed — so every
  caller's handling is unchanged. The respelling is the general rule applied to
  this write, not a special case of `AddFile`.

This is the path ark's flow takes. After a rename, `RefreshStale` stats the
stored spelling, which still succeeds on a case-insensitive filesystem, and
sees nothing stale. ark's scan then finds the new spelling and calls `AddFile`.

## Item 3

Tell ark what landed and where it differs from the request: the keychain
functions stay functions, `AddFile` respells and returns `ErrAlreadyIndexed`,
and overlay paths do not fold.
