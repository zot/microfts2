# CharSet
**Requirements:** R3, R4, R5, R6, R9, R31, R45, R46, R47

Maps characters to 6-bit values. Extracts trigrams from text. Unmapped characters become space (0); consecutive spaces collapse to one.

## Knows
- chars: the character set string (up to 63 chars, no spaces)
- lookup: map[rune]uint8 — character to 6-bit value (1-63); absent = 0 = space
- aliases: map[rune]rune — input character substitutions applied before lookup
- caseInsensitive: fold case before lookup

## Does
- New(chars, caseInsensitive, aliases): validate charset, build lookup table
- Encode(ch): apply alias, then map rune to 6-bit value (0 if unmapped)
- Trigrams(text): slide 3-char window over encoded text, collapse space runs, return []uint32
- TrigramValue(a, b, c): compute (a<<12 | b<<6 | c), 18-bit result
- TrigramChars(trigram): decode trigram to 3 characters (for debugging)

## Collaborators
- none (leaf type)

## Sequences
- seq-add.md
- seq-search.md
