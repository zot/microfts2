# Scoring Strategies

The search function accepts a scoring strategy that determines how candidate chunks are ranked. microfts2 provides built-in strategies and allows custom ones via `ScoreFunc`.

## Coverage (default)

"Does this chunk contain what I searched for?"

For intentional, short queries. User typed specific terms and wants chunks that match them.

Score = matching selected trigrams / total selected query trigrams

Binary match — counts are available but not consulted. A trigram either matches or it doesn't.

## Density

"Is this chunk about any of my terms?"

For long queries (conversation turns, full documents) where most query tokens won't match any given chunk. Separates "chunk is about this topic" from "chunk shares a few common trigrams."

1. Tokenize query on spaces
2. For each token, extract trigrams, apply trigram filter. Tokens with no surviving trigrams are discarded.
3. For each candidate chunk, for each surviving query token:
   - Look up that token's trigram counts in the chunk
   - Token match strength = min count across the token's trigrams. This approximates word frequency — "turnip" produces trigrams `tur`, `urn`, `rni`, `nip`; if counts are [3, 3, 1, 3] then the word appears ~1 time (bottleneck trigram governs).
   - If any trigram has count 0, the token doesn't match.
4. Score = sum of token match strengths / chunk token count

Normalizing by chunk token count prevents long chunks from winning on surface area alone. A 50-word chunk with 10 matching words scores higher than a 500-word chunk with the same 10 words.

## Overlap (OR semantics)

"How many of my query trigrams appear in this chunk?"

Count of matching query trigrams, no normalization. The simplest fuzzy score — more matches = better. Useful when any partial match is interesting and the caller wants to rank by breadth of overlap rather than precision.

```go
func ScoreOverlap(queryTrigrams []uint32, chunkCounts map[uint32]int, _ int) float64
```

Fits `ScoreFunc` signature directly. Pure function, no extra state.

## BM25

Standard term frequency / inverse document frequency scoring. Uses per-trigram TF from the chunk's C record and corpus-wide IDF from T record value lengths.

BM25 needs IDF data that isn't available through the `ScoreFunc` signature. Solution: a closure factory that captures IDF and average document length, returning a `ScoreFunc`.

```go
func ScoreBM25(idf map[uint32]float64, avgTokenCount float64) ScoreFunc
```

The caller pre-computes IDF from T record value lengths and average token count from I record counters, then passes the returned closure as a `ScoreFunc`. No signature change needed.

### BM25 formula

For each query trigram t in the chunk:
- `tf(t)` = trigram count in the chunk (from C record)
- `idf(t) = ln((N - df(t) + 0.5) / (df(t) + 0.5) + 1)` where N = total chunks, df(t) = T record value length
- `score += idf(t) * (tf(t) * (k1 + 1)) / (tf(t) + k1 * (1 - b + b * dl/avgdl))`
- `k1 = 1.2`, `b = 0.75` (standard defaults)
- `dl` = chunk token count, `avgdl` = average chunk token count across corpus

### BM25 helper

```go
func (db *DB) BM25Func(queryTrigrams []uint32) (ScoreFunc, error)
```

Reads T records for per-trigram document frequencies, reads I record counters for total chunks and total tokens, computes IDF map and avgdl, returns a `ScoreBM25` closure. Convenience for callers who don't need custom IDF computation.

### I record counters for BM25

Two I record counters maintained atomically during AddFile, RemoveFile, and AppendChunks:
- `totalTokens`: sum of all chunk token counts across the corpus
- `totalChunks`: total number of unique chunks

Average document length: `avgdl = totalTokens / totalChunks`.

Updated in the same write transaction as other record changes — one extra `Get` + `Put` per counter, no additional I/O round-trips.

## Proximity reranking

Position-aware reranking for multi-term queries. Takes top-N results from the primary scorer, re-chunks each file to recover text, finds query term positions in the chunk content, and computes a proximity bonus based on how close the terms appear to each other.

```go
func WithProximityRerank(topN int) SearchOption
```

Proximity is a post-filter, not a primary scorer — it needs chunk text that isn't in the index. Applied after scoring and before final sort. Works with `Search`, `SearchMulti`, and `ScoreFile`.

The proximity bonus is computed as: for each pair of query terms found in the chunk, measure the minimum token distance. Score adjustment = `1 / (1 + minSpan)` where minSpan is the smallest window (in tokens) containing all query terms. Chunks where terms appear closer together get a higher adjustment.

# Multi-Strategy Search

`SearchMulti` runs one query through multiple scoring strategies in a single bbolt read transaction, sharing candidate collection. The candidate set (trigram intersection, T record reads, C record reads, chunk filter application) is computed once; only scoring diverges.

```go
type MultiSearchResult struct {
    Strategy string
    Results  []SearchResult
}

func (db *DB) SearchMulti(query string, strategies map[string]ScoreFunc,
    k int, opts ...SearchOption) ([]MultiSearchResult, error)
```

- `strategies`: map of name → ScoreFunc. Each strategy scores the same candidate set independently.
- `k`: number of top results to keep per strategy. Same k for all strategies.
- `opts`: shared SearchOptions (TrigramFilter, ChunkFilter, verify, regex filters) applied once during candidate collection.
- Returns one `MultiSearchResult` per strategy, each containing that strategy's top-k results sorted by score descending.

The same chunk can appear in results from multiple strategies. No deduplication — the caller handles merge and can use cross-strategy agreement as a confidence signal.

# Dynamic Trigram Filtering

## Problem

A static global cutoff can't adapt to what you're searching for. Different content types have different frequency distributions — trigrams that are noise in one corpus are signal in another.

## Design: Caller-Supplied Filter Function

Move the trigram selection policy out of microfts2 and into the caller. microfts2 provides the mechanism (trigram counts, search pipeline); the caller provides the strategy.

### TrigramFilter type

```go
// TrigramCount pairs a trigram code with its document frequency.
type TrigramCount struct {
    Trigram uint32
    Count   int
}

// TrigramFilter decides which trigrams to use for a given query.
// Receives the query's trigrams with their corpus-wide document
// frequencies, and the total number of indexed chunks.
// Returns the subset to search with.
type TrigramFilter func(trigrams []TrigramCount, totalChunks int) []TrigramCount
```

### Search integration

`WithTrigramFilter(fn TrigramFilter)` search option supplies a filter function.

- The search path looks up each query trigram's C record count, calls the filter, and uses the returned subset.
- `totalChunks` comes from the I counter (maintained by add/remove), not from scanning F records. Include overlay chunk count when overlay is present.
- When no filter is supplied, `FilterAll` is used (all query trigrams searched).
- `WithTrigramFilter` applies to both `Search` and `ScoreFile`.

### Stock filters

microfts2 ships stock filter functions:

- `FilterAll`: uses every query trigram, no filtering.
- `FilterByRatio(maxRatio float64)`: skips trigrams appearing in more than `maxRatio` of total chunks. E.g., `FilterByRatio(0.50)` skips trigrams in >50% of chunks. Below two chunks ratio filtering cannot discriminate — every trigram present is in 100% of chunks — so the filter returns all trigrams unmodified rather than skipping them all, which would make every query unanswerable on a one-chunk index.
- `FilterBestN(n int)`: keeps the N trigrams with the lowest document frequency. Good for long queries where only the most discriminating trigrams matter.

### Trigram count lookup

Per-query T record reads: look up each query trigram's document frequency from T record value length. Typically 3-10 index reads per query.

The total chunk count is derived from the database (sum of file chunk counts from F records, or maintained as a counter).

# Multi-Regex Post-Filtering

Search results can be post-filtered at the chunk level using multiple regex patterns. Two kinds of filter:

- **Regex filters (AND):** every pattern must match the chunk content. A chunk is kept only if all regex filters match.
- **Except-regex filters (subtract):** any pattern matching rejects the chunk. A chunk is discarded if any except-regex matches.

Both filter types operate on chunk content recovered by re-chunking the file (same mechanism as `WithVerify`). They apply after trigram candidate selection and scoring, before final sort — to both `Search` and `SearchRegex`.

When combined with `SearchRegex`, the primary regex still drives trigram extraction and candidate selection. Regex filters and except-regex filters are independent post-filters applied to those candidates.

## Library API

```go
// WithRegexFilter adds AND post-filters: every pattern must match chunk content.
// Multiple calls accumulate patterns.
func WithRegexFilter(patterns ...string) SearchOption

// WithExceptRegex adds subtract post-filters: any match rejects the chunk.
// Multiple calls accumulate patterns.
func WithExceptRegex(patterns ...string) SearchOption
```

Patterns are stored as strings in the search config. They are compiled to `*regexp.Regexp` at the start of `Search`/`SearchRegex`, which already return errors — compilation failure is a normal error return. Filtering uses the existing `filterResults` helper with a combined match function that checks all compiled regexes.

## CLI

```
microfts search -db <path> [-regex] [-contains <text>] [-filter-regex <pattern>]... [-except-regex <pattern>]... <query>
```

- `-filter-regex` is repeatable: each invocation adds an AND regex filter
- `-except-regex` is repeatable: each invocation adds a subtract regex filter
- Both work with literal and regex search modes
- Implemented via a custom `flag.Value` type for string slice accumulation

### Composing `--contains` with `--regex`

`--contains` provides an explicit FTS text query. When combined with `--regex`, the two compose naturally:

- `--regex` alone with positional args: positional args are the regex pattern → `SearchRegex` (unchanged)
- `--contains` alone (no positional args needed): FTS text query → `Search`
- `--contains` with `--regex` and positional args: FTS on the `--contains` text via `Search`, with the positional regex pattern added as a `WithRegexFilter` post-filter
- Neither flag, positional args: FTS text query → `Search` (unchanged)

This removes the mutual exclusion between FTS and regex — `--contains` narrows candidates via trigram index, `--regex` verifies via post-filter.

## Use cases

```
microfts search -db idx --contains "chunk" --regex '@to-project:.*\bmicrofts2\b'
ark search --regex '@to-project:.*\bark\b' --except-regex '@status:.*\bcompleted\b'
```

Ark translates its own `--regex`/`--except-regex` flags to `WithRegexFilter`/`WithExceptRegex` options on the microfts2 library call. Finds open requests filed against ark — positive match on the project tag, subtract anything marked done.

# Loose Search

OR-semantics search mode for exploratory queries. When the user isn't sure of exact phrasing, loose search returns chunks matching *any* query term, ranked by how many terms match.

## Semantics

- **AND mode (default):** candidate set is the intersection of all terms' trigram candidate sets — a chunk must match all terms.
- **Loose mode:** candidate set is the union of all terms' trigram candidate sets — a chunk matches if it contains any query term's trigrams.

Within each term, trigram intersection is still AND (all trigrams of that term must match). The OR is at the term level, not the trigram level.

## Scoring

Score = (terms matched) / (total query terms). Range [0.0, 1.0]. A term matches if its trigram set intersects the chunk's trigrams. Results sorted by score descending.

This is the default loose scoring. Custom `ScoreFunc` can be used instead via `WithScoring`.

## Library API

```go
func WithLoose() SearchOption
```

Composable with all existing search options — `WithVerify`, `WithRegexFilter`, `WithExceptRegex`, `WithChunkFilter`, `WithTrigramFilter`, `WithProximityRerank`. Also works with `SearchMulti` (loose candidate collection, per-strategy scoring).

## CLI

```
microfts search -db <path> -loose <query>
```

The `-loose` flag enables OR semantics. Composable with `-verify`, `-filter-regex`, `-except-regex`, `-score`.

## Use case: search escalation

```go
results, _ := db.Search(query)
if len(results.Results) == 0 {
    results, _ = db.Search(query, microfts2.WithLoose())
}
```

Exact search first for precision. If no results, fall back to loose for recall.
