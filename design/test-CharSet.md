# Test Design: CharSet
**Source:** crc-CharSet.md

## Test: encode basic characters
**Purpose:** verify character-to-6bit mapping
**Input:** CharSet with "abcdefghijklmnopqrstuvwxyz0123456789"
**Expected:** 'a'→1, 'z'→26, '0'→27, '9'→36, ' '→0, '!'→0
**Refs:** crc-CharSet.md

## Test: trigram extraction
**Purpose:** verify sliding window and 18-bit encoding
**Input:** Trigrams("abc")
**Expected:** single trigram with value (1<<12 | 2<<6 | 3)
**Refs:** crc-CharSet.md

## Test: space collapsing
**Purpose:** unmapped chars become space, runs collapse
**Input:** Trigrams("ab!!!cd") with charset "abcd"
**Expected:** same trigrams as "ab cd" — space run collapsed
**Refs:** crc-CharSet.md

## Test: case insensitive
**Purpose:** case folding before lookup
**Input:** CharSet("abc", caseInsensitive=true), Encode('A')
**Expected:** same value as Encode('a')
**Refs:** crc-CharSet.md

## Test: short input
**Purpose:** inputs shorter than 3 encoded chars produce no trigrams
**Input:** Trigrams("ab") with charset "ab"
**Expected:** empty result (only 2 non-space chars, padded with space gives " ab " → trigrams from padding)
**Refs:** crc-CharSet.md

## Test: character aliases
**Purpose:** alias substitution before encoding
**Input:** CharSet("abc^", aliases={'\n': '^'}), Trigrams("abc\ndef")
**Expected:** newline encoded as ^, trigrams include "c^d" equivalent
**Refs:** crc-CharSet.md

## Test: trigram decode roundtrip
**Purpose:** TrigramChars inverts TrigramValue
**Input:** TrigramValue(1, 2, 3) → TrigramChars(result)
**Expected:** returns ('a', 'b', 'c') for charset starting with "abc"
**Refs:** crc-CharSet.md
