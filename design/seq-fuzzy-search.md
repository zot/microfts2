# Sequence: Loose Search

**Requirements:** R336, R337, R338, R339, R340, R341, R342, R343, R345

Shows how Search with WithLoose() collects candidates via union instead of intersection.

## Participants
- Caller
- DB

## Flow

```
Caller                              DB
  |                                  |
  |--- Search(query, WithLoose()) -->|
  |                                  |
  |                    parseQueryTerms(query) → terms[]
  |                    for each term:
  |                      extractTrigrams(term) → termTrigrams[]
  |                      applyTrigramFilter(termTrigrams)
  |                                  |
  |                    collectCandidates (union, not intersection):
  |                      for each term's trigram set:
  |                        collectChunkIDs(trigrams) → term's chunkid set (AND within term)
  |                        union into candidateChunkIDs (OR across terms)
  |                                  |
  |                    read C records for all candidate chunkids
  |                    apply ChunkFilters
  |                                  |
  |                    score each candidate:
  |                      if custom ScoreFunc → use it (R345)
  |                      else default loose scoring:
  |                        for each term, check if all term trigrams have count > 0
  |                        score = matchingTerms / totalTerms
  |                                  |
  |                    apply post-filters (verify, regex, proximity rerank)
  |                    sort by score descending
  |                                  |
  |<-- SearchResults ----------------|
```
