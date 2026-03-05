# CharSet
**Requirements:** R3, R4, R5, R6, R9, R31, R45, R46, R47, R110

Maps characters to 8-bit values. Extracts trigrams from text. Unmapped characters become space (0); consecutive spaces collapse to one.

## Knows
- chars: the character set string (up to 255 chars, no spaces)
- lookup: map[rune]uint8 — character to 8-bit value (1-255); absent = 0 = space
- aliases: map[rune]rune — input character substitutions applied before lookup
- caseInsensitive: fold case before lookup

## Does
- New(chars, caseInsensitive, aliases): validate charset, build lookup table
- Encode(ch): apply alias, then map rune to 8-bit value (0 if unmapped)
- Trigrams(text): slide 3-char window over encoded text, collapse space runs, return []uint32
- TrigramCounts(text): like Trigrams but returns map[uint32]int — count of each trigram occurrence
- TrigramValue(a, b, c): compute (a<<16 | b<<8 | c), 24-bit result
- TrigramChars(trigram): decode trigram to 3 characters (for debugging)

## Collaborators
- none (leaf type)

## Sequences
- seq-add.md
- seq-search.md
