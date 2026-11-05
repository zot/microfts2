# Migration: postings carry the counts; chunks carry no lists (DB format "5" → "6")

## Problem

Every C record carries a forward list of its chunk's trigrams and tokens, each
with a count, and the T and W records hold the same facts inverted. In ark's
index on 2026-11-04 the C trigram lists held 57.6 million entries and the token
lists 11.1 million, exactly the posting counts; C values were 351 MB of a 1.3 GB
file. The lists have three readers: removal, to learn which postings to edit;
search, which decodes each candidate's whole C record and copies its entire
trigram list into a map to look up the query's few trigrams; and `ScoreFile`,
the same per chunk of one file. No scorer reads more than the chunk's counts of
the query's trigrams and one per-chunk length.

The W records (token postings) are written on every add and removal and never
read. `storage.md` listed them for "token IDF for BM25", but BM25 takes its IDF
from the T records.

Measured by ark (its storage carve, Item 16) on a copy of its index: with the
counts in the T postings and no lists in C, T values went from 170.6 MB to
84.5 MB and C values from 351.3 MB to 15.1 MB, and over 300 queries search
returned exactly today's results while the median fell from about 55 ms to
25 ms and p99 from 0.9-2.4 s to 110-130 ms. Compacting today's layout, or
reusing postings across a query's terms, changed nothing measurable: the gain is
the layout.

## Target state

### Records

- `T` [trigram: 3] → a run of postings in ascending chunk-id order. Each posting
  is `[(delta << 1) | hasCount : uvarint]`, then `[count: uvarint]` only when
  `hasCount` is 1. `delta` is the chunk id minus the previous posting's (the
  first posting's previous is 0); a posting without a count has count 1. The
  count is how many times the trigram occurs in that chunk.
- `C` [chunkid: varint] → `[hash: 32] [contentLen: varint] [distinctTokens:
  varint] [tokenTotal: varint] [n-attrs: varint] [[key: bytes] [value:
  bytes]]... [n-fileids: varint] [[fileid: varint] [count: varint]]...`: no
  trigram list and no token list. `distinctTokens` is the number of distinct
  tokens in the chunk (what scoring has always used as the chunk's length,
  `len(Tokens)`), and `tokenTotal` their summed counts (what the `totalTokens`
  corpus counter adds and subtracts).
- `D` [chunkid: varint] → empty: the dead set. A chunk id is dead from the
  moment its C record is deleted until `Purge` has taken it out of every
  posting list.
- No `W` records.

Chunk ids come from the `nextChunkID` counter and are never reused, so a dead
id in a posting can only name a chunk that no longer exists.

### Reading

- **The dead set is skipped everywhere a posting is read.** A posting list's
  document frequency is the number of its postings whose chunk id is not dead;
  the trigram filter and BM25's IDF use that count. The candidate walk skips
  dead ids. So search results are exactly what eager removal would give.
- **Search** decodes each active query trigram's posting list once per query.
  Per term it intersects the lists' live chunk ids, and across terms it
  intersects (or, loose, unions) as before. For each candidate it reads the C
  record (attributes for the chunk filters, file ids, `distinctTokens`) and
  builds the counts for the active trigrams only, from the postings. Scoring
  functions are unchanged and receive the same arguments.
- **`ScoreFile`** takes the counts for each of the file's chunks from the
  active trigrams' postings, restricted to the file's chunk ids.

### Writing

- **Adding a chunk** appends a posting, with its count, to the T record of each
  of its trigrams, coalesced per write as today.
- **Orphaning a chunk** (the last occurrence removed, by `RemoveFile`, a
  reindex, or an append's drop-and-replace) deletes its C and H records,
  subtracts its `tokenTotal` from `totalTokens`, and writes its `D` record. It
  edits no posting.
- **A posting list a write rewrites anyway drops its dead postings** in the
  same pass, and is deleted if nothing live is left. The `D` records stay: the
  same ids may sit in lists no write touched.
- **`Purge()`** takes the dead set as it stands when it starts, scans the T
  records and rewrites each one holding a dead posting without it (deleting
  lists left empty), then deletes those `D` records. With an empty dead set it
  does nothing. It commits in batches; ids that die while it runs stay in the set
  for the next purge. It reports the lists rewritten, the postings dropped and
  the ids cleared.

### API

- `CRecord` loses `Trigrams` and `Tokens` and gains `DistinctTokens int` and
  `TokenTotal int`.
- New `(*DB).Purge() (PurgeStats, error)`.
- `Create` writes format `"6"`; `Open` refuses any other version.

The overlay (`tmp://` documents, in memory) keeps its own structures and is
not changed.

## The migration program

Not in this repository, as for formats "4" and "5". Ark converts its one
database with a throwaway program that keeps every chunk id (ark's own records
are keyed by them): in one write transaction, refuse unless `version` is `"5"`;
pair each C record's trigram counts with the T postings and rewrite every T
record in the new layout; rewrite every C record without its lists; delete
every W record; set `version` to `"6"`. Any failure aborts the transaction. Ark's
prototype of this conversion (2026-11-04) built the new layout from a copy of
its index in 21 s, with every count matched to its posting.

It ran on 2026-11-05 against ark's only database, after a dry run on a copy
(2026-11-04) and one on the database itself: 222,554 C records cut from
351,862,602 bytes to 15,144,833; 204,684 T records, 58,174,136 postings,
rewritten with their counts from 170,916,129 bytes to 84,694,507, every
posting's chunk id checked against the C lists and the count totals equal;
927,856 W records deleted; about 19 s. Ark's next start packed the file from
1.4 GB to 705.7 MB. The program was then deleted; what it did is recorded here
and in ark's storage carve (Item 16).

## Ark

Ark must convert its database before opening it with this microfts2, and its
startup compaction calls `Purge` before it packs (ark's `serve-compact.md`).
Ark is the only consumer, so there is no compatibility window.
