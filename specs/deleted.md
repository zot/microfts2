# Deleted Features

Capabilities microfts2 deliberately **does not have**, each removed on
purpose. This is a real statement about the current system, not a history
page: it exists so a removed feature cannot be quietly re-proposed as though
it had never been tried.

It is also the `**Source:**` of last resort. A feature whose requirements are
*all* removed or retired has no live spec to point at — pointing it at the
spec that replaced it would be a lie, since that spec does not describe the
removed behavior. Those features name this file instead.

**Requirements are never renumbered.** Retired requirements keep their
original numbers and text with a `~~Rn:~~ (Retired Tn — see Rxxx)` marker, so
old design and code references still resolve. Nothing here is reclaimed or
reused.

---

## Bigram index — removed 2026-03-22 (`c8d1a19`)

**Requirements:** R379–R416 (retired T17–T54). **Gap:** A4.
**Full retired spec:** [`migrations/complete/002-bigram-index-removed.md`](migrations/complete/002-bigram-index-removed.md)

A second index of character bigrams, for typo tolerance that trigrams miss —
"cat" and "cot" share zero trigrams but two of four bigrams. Added and removed
within the same release cycle: too slow (2.5s on 74K chunks) and too fat (1.7×
index size).

Replaced by `SearchFuzzy` — a trigram OR-union with posting-list tally — which
gets typo tolerance from the index that already exists. See
[`fuzzy-trigram.md`](fuzzy-trigram.md).

`B` records, `BigramEntry`, and the `SearchStrategy` struct are gone. The DB
format version stayed at `"2"`.

**If you are considering reintroducing bigrams:** the cost was measured, not
guessed. Bring numbers for both index size and query latency at ≥74K chunks,
and say what `SearchFuzzy` cannot do.

## `Options.MaxDBs` — removed 2026-06-12 (`fcd4f28`)

**Requirements:** R101 (retired T56 — see R666).
**Record:** [`migrations/complete/001-lmdb-to-bbolt.md`](migrations/complete/001-lmdb-to-bbolt.md)

LMDB required declaring a maximum named-database count up front. bbolt has no
such ceiling — buckets are created on demand — so the option had nothing to
configure and was dropped rather than kept as a no-op. `Options.MapSize` went
the same way in the same migration.

## `BuildIndex` — removed

**Requirements:** R36.

A separate index-building step that selected which trigrams were worth
indexing, using a frequency cutoff fixed at build time. Replaced by the full
trigram index plus `TrigramFilter` functions supplied per query, which lets a
caller adapt selection to the query instead of living with one global cutoff.
See [`storage.md`](storage.md) and [`search.md`](search.md).

## Big-endian C record encoding — removed

**Requirements:** R123.

C record integers were fixed-width big-endian. Replaced by varint encoding
throughout, which is what [`storage.md`](storage.md) now specifies.
