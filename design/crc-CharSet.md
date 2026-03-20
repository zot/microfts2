# Trigrams
**Requirements:** R3, R4, R5, R6, R9, R31, R45, R46, R47, R110, R112, R113, R114, R115, R382, R383, R384, R385, R405, R406

Raw byte trigram and bigram extraction. Every byte is its own value — no character set mapping. Whitespace bytes are boundaries; runs collapse. Case insensitivity via bytes.ToLower(). Byte aliases applied before extraction. Character-internal byte trigrams/bigrams (windows entirely within a multibyte UTF-8 character) are skipped.

## Knows
- aliases: map[byte]byte — input byte substitutions applied before extraction
- caseInsensitive: fold case via bytes.ToLower before extraction

## Does
- New(caseInsensitive, aliases): create trigram extractor
- ValidateAliases(aliases): returns error if any source or target byte ≥ 0x80
- ExtractTrigrams(data []byte): slide 3-byte window, skip whitespace and character-internal windows, return []uint32
- TrigramCounts(data []byte): like ExtractTrigrams but returns map[uint32]int — count per trigram
- TrigramValue(a, b, c byte): compute (a<<16 | b<<8 | c), 24-bit result
- EncodeTrigram(s string): convert 3-byte string to 24-bit trigram (for regex integration)
- BigramCounts(data []byte): normalize input (case fold, aliases), split on whitespace into tokens, pad each with `_` prefix/suffix, slide 2-byte window, skip character-internal windows, return map[uint16]int
- BigramValue(a, b byte): compute (a<<8 | b), 16-bit result
- ExtractBigrams(data []byte): like BigramCounts but returns deduplicated []uint16

## Collaborators
- none (leaf type)

## Sequences
- seq-add.md
- seq-search.md
- seq-bigram-add.md
- seq-bigram-search.md
