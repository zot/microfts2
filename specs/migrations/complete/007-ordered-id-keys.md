# Migration: ordered id keys (DB format "6" → "7")

## Problem

A `C`, `F` or `D` key is the prefix byte and an id as a LEB128 uvarint. LEB128
puts the low 7 bits first, so consecutive ids sort far apart: 300, 301 and 428
land in three places in the tree. Every new chunk is then a random insert into
the middle of the B+tree, splitting a full page into two half-full ones, and the
bucket's leaves drift toward half full however often a copy repacks them.

Measured by ark (its storage carve, Item 19) on a copy of its index, replaying
2026-11-05's real workload as the exact record writes microfts2 made: with ids
in keys that sort numerically and the bucket written at `FillPercent` 0.9, the
file grew 94.7 MB over the workload against 108.6 MB with today's keys at the
same fill, and appended pages kept their fill (ark's own `EC` pages 78.4%
against 72.8%). With today's scattered keys the fill setting made no difference
at all. A nested bucket per record class gained nothing over one bucket.

## Target state

### The key id encoding

Every id in a **key** is written in SQLite4's order-preserving varint, so that
byte order is numeric order and a new, larger id appends at the tree's right
edge. The first byte gives the length:

| first byte `A0` | bytes | value |
|---|---|---|
| 0-240 | 1 | `A0` |
| 241-248 | 2 | 240 + 256 × (`A0` − 241) + `A1` |
| 249 | 3 | 2288 + 256 × `A1` + `A2` |
| 250-255 | `A0` − 246 | the next `A0` − 247 bytes, big-endian (3 to 8 bytes) |

Only the shortest encoding of a value is valid. Minimal encodings sort as their
values.

The encoder appends to a slice. The decoder reads one varint from the front of
a slice and returns the value and the bytes it took; it returns `n == 0` when
the input is empty or too short and `n < 0` when the encoding is not minimal,
never panicking, the way `encoding/binary.Uvarint` reports bad input.

**Values keep LEB128**: posting deltas and counts, the H record's chunkid, the
chunk and file lists inside C and F records. Values never sort, and LEB128 is as
small as these numbers get.

### Records

- `C` [chunkid: ordered varint] → unchanged value.
- `F` [fileid: ordered varint] → unchanged value.
- `D` [chunkid: ordered varint] → empty.
- `H`, `T`, `N`, `I` unchanged.

### Fill

Every write transaction sets the bucket's `FillPercent` to 0.9 before writing.
bbolt keeps `FillPercent` per bucket handle, per transaction, and fills a page
it splits to that fraction; a page left behind by right-edge appends keeps that
fill. bbolt's default, 0.5, would leave every appended page half full.

### API

- `AppendKeyID(b []byte, v uint64) []byte` and `KeyID(b []byte) (v uint64, n
  int)` are exported, so a consumer keying its own records by chunk or file id
  uses the same encoding.
- `Create` writes format `"7"`; `Open` refuses any other version.

## The migration program

Not in this repository, as for formats "4" to "6". Ark converts its one database
with a throwaway program that keeps every id (ark's own records are keyed by
them): in one write transaction, refuse unless `version` is `"6"`; rewrite every
`C`, `F` and `D` key in the ordered encoding, values untouched; set `version` to
`"7"`. Any failure aborts the transaction. Ark's records change in the same
program (ark's `specs/migrations/ordered-id-keys.md`).

It ran on 2026-11-05 against ark's only database, with the server stopped,
after a dry run on the database itself that converted and verified everything
and rolled back (the file byte-identical afterwards): 223,216 C, 9,911 F and
2,545 D keys rewritten, every key read back in the new form before commit,
about 53 s. What it did is recorded here and in ark's storage carve (Item 19).

## Ark

Ark must convert its database before opening it with this microfts2. Ark is the
only consumer, so there is no compatibility window.
