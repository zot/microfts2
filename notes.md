- see PERFORMANCE.md

- reorganization
  - Note: strings are length-prefixed (varint), except the final field in a key can use remaining bytes
  - Single subdatabase (prefix-distinguished records, no content/index split)
  - Chunk deduplication: same content = same chunkid across files
  - 'I' [name: str] = [value: str] -> empty -- config record, data-in-key pattern
  - 'H' [hash: 32] -> [chunkid: varint] -- content hash to chunkid lookup
  - 'C' [chunkid: varint] -> [hash:32] [n-trigrams: varint] [[trigram: 3] [count: varint]]... [n-tokens: varint] [[count: varint] [token: str]]... [n-attrs: varint] [[key: str] [value: str]]... [n-fileids: varint] [fileid: varint]...
    -- per-chunk: trigrams, tokens, attrs (from HasAttrs chunkers), and which files contain this chunk
  - 'F' [fileid: varint] -> [modTime: 8] [contentHash: 32] [fileLength: varint] [strategy: str] [filecount: varint] [name: str]... [chunkcount: varint] [[chunkid: varint] [location: str]]... [tokencount: varint] [[token: str] [count: varint]]
    -- per-file: metadata (staleness), names (handles dup files), ordered chunks, aggregated token bag for file-level scoring
  - 'N' [N: 1 = 0-254] [name: str] -> empty -- filename prefix chain
  - 'N' [N: 1 = 255] [name: str] -> [[full-name: str] [fileid: varint]]... -- final chain key; value has full filename + fileid
  - 'T' [trigram: 3] -> [chunkid: varint]... -- trigram inverted index; value length / chunkid size = document frequency
  - 'W' [token-hash: 4] -> [chunkid: varint]... -- token inverted index for IDF; value length / chunkid size = document frequency

- [ ] fuzzy search support for Ark — multi-strategy FTS scoring
  - Token overlap (OR semantics, count matches — the basic fuzzy)
  - BM25-style term frequency / inverse document frequency
  - Trigram density (percentage of chunk's trigrams that match)
  - Proximity scoring (matching tokens near each other rank higher)
  - Take top N from each strategy, merge and deduplicate
  - Chunks scoring well across multiple strategies are high confidence
  - All pure FTS — no vectors, no model loading, parallelizable
  - **All data already in LMDB trigram index:**
    - Token overlap: decompose query to trigrams, count matches per chunk (AND→OR)
    - BM25: TF is in chunk trigram list, IDF from global trigram counts (already maintained for cutoff percentile)
    - Trigram density: matched / total trigrams per chunk — both in index
    - Proximity: only one needing chunk text (positions not in index) — cheap rerank on top candidates, not full scan
  - Three of four strategies are pure index lookups; fourth is post-filter
  - @ark-request-sent: requests/microfts2-fuzzy-search.md

# microfts2 notes

## Weighted FTS Scoring

Current index records are `[trigram:3][fileid:8][chunknum:8]` — presence
only, no count. Scoring is binary: what fraction of active query trigrams
appear in the chunk's T-record bitset.

### Index change

Change index records to `[trigram:3][count:varint][fileid:8][chunknum:8]`.
Count is how many times this trigram appears in this chunk. With count in
the key between trigram and fileid, LMDB cursor scan for a trigram returns
chunks ordered by occurrence count — high-count chunks first, enabling
early termination for top-k.

### Scoring strategies

The search function takes a scoring strategy func. microfts2 provides
built-in strategies and exposes them in the CLI via options.

#### Coverage (current behavior, improved)

"Does this chunk contain what I searched for?"

For intentional, short queries. User typed specific terms and wants
chunks that match them.

Score = matching active trigrams / total active query trigrams

This is what we have now. With counts in the index we could add a
saturation bonus (a chunk where the trigram appears 5 times is slightly
better than 1, but not 5x better), but the binary fraction is a
reasonable default.

#### Density

"Is this chunk about any of my terms?"

For long queries (conversation turns, full documents) where most query
tokens won't match any given chunk. You want chunks that are *dense*
with whatever portion of the query they do overlap.

1. Tokenize query on spaces.
2. For each token, extract trigrams, filter to active set. Tokens with
   no surviving trigrams are discarded.
3. For each candidate chunk, for each surviving query token:
   - Look up that token's trigram counts in the chunk.
   - Token match strength = min count across the token's trigrams.
     This approximates word frequency — "turnip" produces trigrams
     `tur`, `urn`, `rni`, `nip`; if counts are [3, 3, 1, 3] then
     the word appears ~1 time (bottleneck trigram governs).
   - If any trigram has count 0, the token doesn't match.
4. Score = sum of token match strengths / chunk token count.

Normalizing by chunk token count prevents long chunks from winning on
surface area alone. A 50-word chunk with 10 matching words scores higher
than a 500-word chunk with the same 10 words.

No saturation function (BM25's k1) initially. Add if raw counts
over-weight repetitive chunks.

#### API shape

```go
type ScoreFunc func(queryTrigrams []uint32, chunkCounts map[uint32]int, chunkTokenCount int) float64

func (db *DB) Search(query string, opts ...SearchOption) (*SearchResults, error)

// Built-in strategies
func ScoreCoverage() SearchOption   // default — current behavior
func ScoreDensity() SearchOption    // token-density for inspiration/reminding
```

CLI:
```
microfts search --score coverage "query"    # default
microfts search --score density "query"     # long-query mode
```

### Why this matters

For ark's inspiration engine: matching entire conversation turns (many
trigrams) against chunks where only a portion overlaps. Coverage scoring
produces a flat noise floor — every chunk shares some common trigrams
with a long query. Density scoring separates "chunk is about this topic"
from "chunk shares a few common trigrams."

The active set cutoff helps (filters common trigrams) but doesn't replace
term frequency — it controls *which* trigrams participate, not *how
strongly* they match.
