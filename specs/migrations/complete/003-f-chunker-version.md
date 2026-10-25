# Migration: F record gains chunkerVersion (DB format "2" → "3")

## Problem

Staleness (`classifyFile`) looks only at an F record's mtime and content hash,
so a change to a chunker's rules makes nothing stale. Ark hit this on
2026-10-23: its chat chunker's rules changed, and the index kept 18,765 chunks
the new rules no longer produce until a forced refresh of 3,220 files. Ark's
request: `microfts2-request-20261025`.

## Target state

The steady-state specs already describe it:

- `chunking.md` — the `VersionedChunker` interface.
- `storage.md` — the F record carries `[chunkerVersion: varint]` directly after
  `[strategy: str]`, inside the header that `UnmarshalFHeader` reads; the format
  version is `"3"`.
- `indexing.md` — a chunker-version difference makes a file stale.
- `append.md` — `ErrChunkerVersionStale` refuses an append onto chunks made
  under other rules.
- `api.md` — `Open` refuses any format version other than `"3"`.

The field sits in the header, not after the token bag, so the stale scan stays
a header-only decode. The cost is that the layout changes underneath existing
databases, which cannot be told apart from new ones by their bytes. Hence the
format version, `Open`'s refusal, and a one-shot migration.

## The migration program

A separate command, `cmd/microfts2-migrate-v3`, run once per database and never
by the library:

```
microfts2-migrate-v3 -db PATH [-bucket NAME]
```

- `-bucket` defaults to `"fts"`, the library's default `DBName`. A database
  shared with other buckets (ark's) is migrated by naming its microfts2 bucket.
- In one write transaction: refuses unless the bucket's `version` is `"2"`;
  rewrites every F record, inserting a zero `chunkerVersion` varint (one byte,
  `0x00`) directly after the strategy string; then sets `version` to `"3"`.
  Any failure aborts the transaction and leaves the database untouched.
- Already at `"3"`: reports that and exits successfully, changing nothing, so a
  second run is harmless.
- Reports the number of F records rewritten.

Version 0 everywhere means every chunker that declares nothing still matches,
so the migration re-chunks nothing.

The program depended on the old layout only in the byte splice it performed.
It was deleted on 2026-10-25, after ark migrated its only database; it remains
in microfts2 history at commit `9151394`.

## Ark

Ark must run the program on its database before opening it with this
microfts2. Ark is the only consumer, so there is no compatibility window.
