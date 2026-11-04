# Migration: an F record has one name (DB format "3" → "4")

## Problem

The F record holds `Names []string`, a count and a list of paths. The storage
design that introduced it (2026-03-14) said why: "names (handles dup files)",
identical files sharing one record under several names. That was never built.
Deduplication landed at the chunk instead: a copy gets an F record of its own,
and the shared chunk lists both fileids in its C record. The only writer of
`Names` is the respelling path (R744), which replaces `Names[0]`. So every
record has exactly one name, and every reader takes `Names[0]`, which reads as
though a list were being ignored and invites the question of whether the
others were ever checked. Ark asked for it on 2026-11-04 (its storage carve,
Item 14): ark has no other users, and a field kept only for compatibility
confuses the next reader.

## Target state

The steady-state specs describe it:

- `api.md` — `FRecord.Name string`; `UnmarshalFHeader` decodes `Name`.
- `storage.md` — the F record carries `[name: str]` directly after
  `[chunkerVersion: varint]`, with no count before it; the format version is
  `"4"`.
- `filename-case.md` — the stored spelling is `FRecord.Name`, and respelling
  replaces it.
- `api.md` — `Open` refuses any format version other than `"4"`.

The layout changes underneath existing databases, which cannot be told apart
from new ones by their bytes, hence the format version, `Open`'s refusal, and a
one-shot migration.

## The migration program

Not in this repository. Ark is the only consumer and has one database, so ark
converts it with a throwaway program of its own, which also deletes records of
ark's in the same pass. It is a byte splice over bbolt, needing no microfts2
code: in one write transaction, refuse unless the bucket's `version` is `"3"`;
for every F record, drop the name-count varint after `chunkerVersion`, refusing
any record whose count is not 1; then set `version` to `"4"`. Any failure
aborts the transaction and leaves the database untouched.

It ran on 2026-11-04 against ark's only database, after a dry run: 9,887 F
records rewritten (one byte each, the count), every one decoded by the new
`UnmarshalFHeader` before the commit, and ark's 29 retired `M` records deleted
in the same transaction. The program was then deleted; what it did is
recorded here, in ark's storage carve (Item 14).

## Ark

Ark must convert its database before opening it with this microfts2. Ark is the
only consumer, so there is no compatibility window.
