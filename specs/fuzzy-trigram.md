# Fuzzy Trigram Search

Fast typo-tolerant search using trigram OR-union with posting-list
scoring. No C record reads for scoring, no verify step. Designed for
sub-second response on large corpora where SearchMulti is too expensive.

## How it works

Two-phase search: cheap recall, then precise scoring on the winners.

**Phase 1 — posting-list tally (fast):**
1. Extract trigrams from the query (same extraction as normal search)
2. For each query trigram, read its T record posting list
3. Count how many of the query's posting lists each chunkID appears in
4. Select top-k by count

**Phase 2 — C record re-score (precise):**
5. Read C records for the top-k candidates only
6. Re-score using ScoreCoverage (actual trigram counts, not binary)
7. Re-sort by the accurate score

Phase 1 is ~10-15 T record reads plus counting. Phase 2 is k C record
reads (default 20). Total cost is negligible compared to reading
thousands of C records for the entire OR-union.

## Why it catches typos

A single-character typo in a word destroys at most 3 of the word's
trigrams (the three windows touching the changed character). The
remaining trigrams survive. AND-intersection fails because one
missing trigram zeros out the result. OR-union succeeds because the
surviving trigrams still pull in the right chunks, and the overlap
count ranks them above noise.

## Library API

```go
func (db *DB) SearchFuzzy(query string, k int, opts ...SearchOption) (*SearchResults, error)
```

- `query`: search terms (same parsing as Search)
- `k`: maximum results to return (required — no unbounded fuzzy)
- `opts`: SearchOptions — WithChunkFilter, WithRegexFilter,
  WithExceptRegex, WithProximityRerank apply as post-filters on the
  top-k. WithVerify, WithScoring, WithTrigramFilter, WithLoose are
  ignored (they don't apply to posting-list scoring).
- Returns *SearchResults (same type as Search) for API compatibility

Overlay (tmp://) documents participate: overlay trigram maps are
OR-unioned into the tally alongside LMDB T records.

## CLI

```
microfts search -db <path> -fuzzy <query>
```

The `-fuzzy` flag calls `db.SearchFuzzy(query, k)`. Default k = 20
for CLI. (Term-level OR is available separately via `-loose`.)

## Search escalation

```go
results, _ := db.Search(query)              // exact, fast
if len(results.Results) == 0 {
    results, _ = db.SearchFuzzy(query, 20)  // fuzzy, still fast
}
```
