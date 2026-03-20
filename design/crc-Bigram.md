# Bigram
**Requirements:** R382, R383, R384, R385, R405, R406

Bigram extraction from normalized byte sequences. Reuses CharSet's normalization (case folding, alias application) then extracts 2-byte windows with word-boundary padding. Skips character-internal bigrams (both bytes inside one multibyte UTF-8 character).

## Knows
- (no state — functions on CharSet)

## Does
- BigramCounts(data []byte) map[uint16]int: normalize input via CharSet (case fold, aliases), split on whitespace into tokens, pad each token with `_` prefix and `_` suffix, slide 2-byte window, skip character-internal windows, return counts per bigram
- BigramValue(a, b byte) uint16: compute (a<<8 | b)
- ExtractBigrams(data []byte) []uint16: like BigramCounts but returns deduplicated list without counts

## Collaborators
- CharSet: normalization (case folding, alias application)

## Sequences
- seq-bigram-add.md
- seq-bigram-search.md
