# Migration: the F record drops its token bag (DB format "4" → "5")

## Problem

The F record ends in a file-level token bag: every token in the file with its
summed count, `[tokencount: varint] [[token: str] [count: varint]]...`. It is
built when a file is added, merged on every append, subtracted when an append
drops a chunk, and mirrored in the overlay, and nothing reads it. `storage.md`
listed it for "file-level ranking, pre-filtering", which was never built.
Measured in ark's index on 2026-11-04 (its storage carve, Item 15): 113.8 MB of
the F records' 120.2 MB, 94.7% of every record, and each append to a chat log
rewrites all of it.

## Target state

The steady-state specs describe it:

- `storage.md`: the F record ends after its chunk list; the format version is
  `"5"`.
- `api.md`: `FRecord` has no `Tokens` field; `Create` writes `"5"` and `Open`
  refuses any other version.
- `append.md`, `indexing.md`, `overlay.md`: adding and appending update the
  chunk list, with no token bag to merge.

The per-chunk token counts in the C record, and the W records, are untouched:
search reads those.

## The migration program

Not in this repository, as for format "4". Ark converts its one database with
a throwaway byte splice over bbolt: in one write transaction, refuse unless the
`version` I record is `"4"`; for every F record, decode its header and chunk
list with the old layout and cut the bytes after the chunk list; then set
`version` to `"5"`. Any failure aborts the transaction and leaves the database
untouched.

It ran on 2026-11-04 against ark's only database, after a dry run on a copy
and another on the database itself: 9,897 F records cut from 120,422,688 bytes
to 6,362,968, every one decoded by the new `UnmarshalFValue` before the commit;
the converted copy passed bbolt's `tx.Check` and opened under format `"5"`. The
program was then deleted; what it did is recorded here and in ark's storage
carve (Item 15).

## Ark

Ark must convert its database before opening it with this microfts2. Ark is the
only consumer, so there is no compatibility window.
