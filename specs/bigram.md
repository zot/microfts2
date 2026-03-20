# Bigram Index

Bigram-based scoring for fuzzy character-level matching. Trigrams are
blind to single-character substitutions in short words — "cat" and
"cot" share zero trigrams but 2 of 4 bigrams. Bigrams capture the
overlap that trigrams miss.

## Opt-in Control

Bigram indexing is on by default. `--no-bigrams` at init time disables
it. The setting is stored in an I record and checked at index time —
when bigrams are disabled, no B records are written and C records omit
the bigram section.

## Bigram Extraction

Bigrams use the same byte-level extraction as trigrams: raw bytes,
whitespace boundaries collapsed, case-insensitive when the DB is
case-insensitive. Character-internal bigrams (both bytes inside a
single multibyte character) are skipped, same principle as trigram
skipping. Word-boundary padding: each token gets a leading `_` and
trailing `_` (e.g. "cat" -> `_c`, `ca`, `at`, `t_`).

Byte aliases apply before bigram extraction, same as trigrams.

## Storage

### B Records

`B` prefix + 2 bytes (bigram) -> packed varint chunkid list. Same
format and semantics as T records but with a 2-byte key. One B record
per distinct bigram. Document frequency derived from value length (same
as T records).

### C Record Extension

When bigrams are enabled, C records include a bigram counts section
after the existing trigram counts: `[n-bigrams:varint]
[[bigram:2] [count:varint]]...`. When bigrams are disabled, this
section is absent — the I record flag tells the reader whether to
expect it.

Version bump from v2 to v3 for the format change.

### Batch Writes

Bigram B record updates are coalesced alongside T/W updates during
AddFile — one read-modify-write per unique bigram across all chunks in
the file.

## Search Path

Bigrams are a scoring signal, not a candidate filter:

1. Candidates come from trigram intersection (existing path, unchanged)
2. Bigram scoring evaluates candidates using bigram overlap from C
   record bigram counts
3. Available as a ScoreFunc via `WithBigramOverlap()` search option

B records exist for DF lookups needed by potential BM25-style bigram
scoring in the future.

## Scoring

`ScoreBigramOverlap` score function: count of query bigrams matching
the chunk's bigram counts, divided by total query bigrams. Fits
`ScoreFunc` signature. `WithBigramOverlap()` search option is sugar
for `WithScoring(ScoreBigramOverlap)`.

The score function extracts bigrams from the query at call time (same
extraction as indexing).

## SearchMulti Composition

`SearchStrategy` type wraps a `ScoreFunc` with metadata about what
data it needs. `SearchMulti` accepts `map[string]SearchStrategy`
instead of `map[string]ScoreFunc`. Each strategy declares whether it
uses bigram counts (`UseBigrams bool`). When any strategy in the map
uses bigrams, `SearchMulti` populates bigram counts for all candidates.
During per-strategy scoring, `scoreAndResolve` passes bigram counts
to strategies that declared `UseBigrams`.

Helper constructors for common strategies:

- `StrategyFunc(fn ScoreFunc) SearchStrategy` — wraps a plain ScoreFunc
- `StrategyBigramOverlap(queryBigrams) SearchStrategy` — bigram scoring

This allows ark to add `"bigram"` as a fifth strategy in
`buildStrategies` alongside coverage, density, overlap, and bm25.

## CLI

`microfts init -db <path> --no-bigrams` — create DB without bigram
index.

`microfts search -db <path> -score bigram <query>` — search using
bigram overlap scoring.

## Overlay

Overlay (tmp:// documents) includes bigram data when the DB has bigrams
enabled. Overlay chunks store bigram counts, overlay maintains B-record
equivalent maps, and `searchOverlay` includes bigram data in candidates.

## Removal and Reindex

RemoveFile and Reindex update B records alongside T/W records — same
orphan-cleanup logic (remove chunkid from B records for orphaned
chunks).

AppendChunks includes bigram extraction and B record updates when
bigrams are enabled.
