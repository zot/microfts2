package microfts2

import (
	"fmt"
	"strings"
	"unicode"
)

// CRC: crc-CharSet.md

// CharSet maps characters to 8-bit values and extracts trigrams from text.
type CharSet struct {
	chars           string
	lookup          [256]uint8   // ASCII fast path
	runeLookup      map[rune]uint8 // non-ASCII
	aliases         map[rune]rune
	caseInsensitive bool
}

// NewCharSet creates a CharSet from the given character string.
// Aliases map input characters to charset characters before encoding
// (e.g. {'\n': '^'} for line-start matching).
func NewCharSet(chars string, caseInsensitive bool, aliases map[rune]rune) (*CharSet, error) {
	runes := []rune(chars)
	if len(runes) > 255 {
		return nil, fmt.Errorf("character set too large: %d (max 255)", len(runes))
	}
	if strings.ContainsRune(chars, ' ') {
		return nil, fmt.Errorf("character set must not contain spaces")
	}
	cs := &CharSet{
		chars:           chars,
		runeLookup:      make(map[rune]uint8),
		aliases:         aliases,
		caseInsensitive: caseInsensitive,
	}
	for i, ch := range runes {
		v := uint8(i + 1) // 0 is reserved for space
		cs.setRune(ch, v)
		if caseInsensitive {
			cs.setRune(unicode.ToLower(ch), v)
			cs.setRune(unicode.ToUpper(ch), v)
		}
	}
	return cs, nil
}

func (cs *CharSet) setRune(ch rune, v uint8) {
	if ch < 256 {
		cs.lookup[ch] = v
	} else {
		cs.runeLookup[ch] = v
	}
}

// Encode maps a rune to its 8-bit value. Applies aliases first.
// Unmapped runes return 0 (space).
func (cs *CharSet) Encode(ch rune) uint8 {
	if cs.aliases != nil {
		if alias, ok := cs.aliases[ch]; ok {
			ch = alias
		}
	}
	if cs.caseInsensitive {
		ch = unicode.ToLower(ch)
	}
	if ch < 256 {
		return cs.lookup[ch]
	}
	return cs.runeLookup[ch]
}

// TrigramValue computes the 24-bit trigram from three 8-bit values.
func TrigramValue(a, b, c uint8) uint32 {
	return uint32(a)<<16 | uint32(b)<<8 | uint32(c)
}

// EncodeTrigram converts a 3-character string trigram to our 24-bit encoding.
// Returns the encoded trigram and true, or 0 and false if the trigram would be
// all-space (not useful for index filtering).
func (cs *CharSet) EncodeTrigram(s string) (uint32, bool) {
	runes := []rune(s)
	if len(runes) != 3 {
		return 0, false
	}
	a := cs.Encode(runes[0])
	b := cs.Encode(runes[1])
	c := cs.Encode(runes[2])
	if a == 0 && b == 0 && c == 0 {
		return 0, false
	}
	return TrigramValue(a, b, c), true
}

// TrigramChars decodes a trigram value to its three characters.
func (cs *CharSet) TrigramChars(trigram uint32) (rune, rune, rune) {
	a := uint8((trigram >> 16) & 0xFF)
	b := uint8((trigram >> 8) & 0xFF)
	c := uint8(trigram & 0xFF)
	return cs.valRune(a), cs.valRune(b), cs.valRune(c)
}

func (cs *CharSet) valRune(v uint8) rune {
	if v == 0 {
		return ' '
	}
	runes := []rune(cs.chars)
	if int(v-1) < len(runes) {
		return runes[v-1]
	}
	return ' '
}

// encode maps text to a sequence of encoded values with space collapsing.
func (cs *CharSet) encode(text string) []uint8 {
	encoded := make([]uint8, 0, len(text))
	lastSpace := true
	for _, ch := range text {
		v := cs.Encode(ch)
		if v == 0 {
			if !lastSpace {
				encoded = append(encoded, 0)
				lastSpace = true
			}
			continue
		}
		encoded = append(encoded, v)
		lastSpace = false
	}
	return encoded
}

// Trigrams extracts all trigrams from text.
// Unmapped characters become space; consecutive spaces collapse.
func (cs *CharSet) Trigrams(text string) []uint32 {
	encoded := cs.encode(text)
	if len(encoded) < 3 {
		return nil
	}
	result := make([]uint32, 0, len(encoded)-2)
	for i := 0; i <= len(encoded)-3; i++ {
		result = append(result, TrigramValue(encoded[i], encoded[i+1], encoded[i+2]))
	}
	return result
}

// TrigramCounts extracts trigrams with occurrence counts.
func (cs *CharSet) TrigramCounts(text string) map[uint32]int {
	encoded := cs.encode(text)
	if len(encoded) < 3 {
		return nil
	}
	counts := make(map[uint32]int)
	for i := 0; i <= len(encoded)-3; i++ {
		counts[TrigramValue(encoded[i], encoded[i+1], encoded[i+2])]++
	}
	return counts
}
