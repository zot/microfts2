package microfts2

import (
	"bytes"
	"fmt"
)

// CRC: crc-CharSet.md

// Trigrams extracts raw byte trigrams from text.
// Every byte is its own value — no character set mapping.
// Whitespace bytes are boundaries; runs collapse.
// Case insensitivity via bytes.ToLower(). Byte aliases applied before extraction.
type Trigrams struct {
	aliases         map[byte]byte
	caseInsensitive bool
}

// NewTrigrams creates a trigram extractor.
func NewTrigrams(caseInsensitive bool, aliases map[byte]byte) *Trigrams {
	return &Trigrams{
		aliases:         aliases,
		caseInsensitive: caseInsensitive,
	}
}

// ValidateAliases returns an error if any alias source or target byte is non-ASCII (≥ 0x80).
// Aliasing UTF-8 continuation or leading bytes would corrupt multibyte characters
// and break character-internal trigram skipping.
func ValidateAliases(aliases map[byte]byte) error {
	for from, to := range aliases {
		if from >= 0x80 {
			return fmt.Errorf("alias source byte 0x%02X is non-ASCII; aliases must be ASCII (< 0x80)", from)
		}
		if to >= 0x80 {
			return fmt.Errorf("alias target byte 0x%02X is non-ASCII; aliases must be ASCII (< 0x80)", to)
		}
	}
	return nil
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isContinuation(b byte) bool {
	return b&0xC0 == 0x80
}

// isInternalTrigram returns true if the 3-byte window falls entirely within
// a single multibyte UTF-8 character. Assumes valid UTF-8 input (enforced
// by AddFile's utf8.Valid check). For valid UTF-8:
//   - 3-byte char [lead,cont,cont]: the char itself is the internal trigram
//   - 4-byte char [lead,cont,cont,cont]: two internal trigrams
//     (lead,c1,c2) and (c1,c2,c3)
//   - 2-byte chars: no 3-byte window fits inside, never internal
func isInternalTrigram(a, b, c byte) bool {
	if !isContinuation(b) || !isContinuation(c) {
		return false
	}
	// b and c are both continuation bytes; check if a starts or continues the same char
	return a >= 0xE0 || isContinuation(a) // 3-byte lead (0xE0-0xEF), 4-byte lead (0xF0-0xF7), or continuation
}

// prepare applies case folding and aliases to input data.
func (t *Trigrams) prepare(data []byte) []byte {
	out := data
	if t.caseInsensitive {
		out = bytes.ToLower(out)
	}
	if len(t.aliases) > 0 && len(out) > 0 {
		if &out[0] == &data[0] {
			out = make([]byte, len(data))
			copy(out, data)
		}
		for i, b := range out {
			if r, ok := t.aliases[b]; ok {
				out[i] = r
			}
		}
	}
	return out
}

// encode maps bytes to a sequence of values with whitespace collapsing.
func (t *Trigrams) encode(data []byte) []byte {
	src := t.prepare(data)
	encoded := make([]byte, 0, len(src))
	lastSpace := true
	for _, b := range src {
		if isWhitespace(b) {
			if !lastSpace {
				encoded = append(encoded, 0)
				lastSpace = true
			}
			continue
		}
		encoded = append(encoded, b)
		lastSpace = false
	}
	return encoded
}

// ExtractTrigrams extracts all trigrams from data.
// Character-internal trigrams (windows entirely within a multibyte UTF-8 char) are skipped.
func (t *Trigrams) ExtractTrigrams(data []byte) []uint32 {
	encoded := t.encode(data)
	if len(encoded) < 3 {
		return nil
	}
	result := make([]uint32, 0, len(encoded)-2)
	for i := 0; i <= len(encoded)-3; i++ {
		if isInternalTrigram(encoded[i], encoded[i+1], encoded[i+2]) {
			continue
		}
		result = append(result, TrigramValue(encoded[i], encoded[i+1], encoded[i+2]))
	}
	return result
}

// TrigramCounts extracts trigrams with occurrence counts.
// Character-internal trigrams (windows entirely within a multibyte UTF-8 char) are skipped.
func (t *Trigrams) TrigramCounts(data []byte) map[uint32]int {
	encoded := t.encode(data)
	if len(encoded) < 3 {
		return nil
	}
	counts := make(map[uint32]int)
	for i := 0; i <= len(encoded)-3; i++ {
		if isInternalTrigram(encoded[i], encoded[i+1], encoded[i+2]) {
			continue
		}
		counts[TrigramValue(encoded[i], encoded[i+1], encoded[i+2])]++
	}
	return counts
}

// TrigramValue computes the 24-bit trigram from three byte values.
func TrigramValue(a, b, c byte) uint32 {
	return uint32(a)<<16 | uint32(b)<<8 | uint32(c)
}

// DecodeTrigram converts a 24-bit trigram value back to a 3-byte string.
// Bytes that are 0 (whitespace-encoded) are shown as spaces.
func DecodeTrigram(v uint32) string {
	b := [3]byte{byte(v >> 16), byte(v >> 8), byte(v)}
	for i := range b {
		if b[i] == 0 {
			b[i] = ' '
		}
	}
	return string(b[:])
}

// EncodeTrigram converts a 3-byte string to a 24-bit trigram using the same
// encoding as ExtractTrigrams: case folding, aliases, whitespace→0.
// Returns 0, false if the trigram cannot appear in the index (e.g. all
// whitespace, or consecutive whitespace which encode() collapses away).
func (t *Trigrams) EncodeTrigram(s string) (uint32, bool) {
	if len(s) != 3 {
		return 0, false
	}
	a, b, c := s[0], s[1], s[2]
	if t.caseInsensitive {
		a = toLowerByte(a)
		b = toLowerByte(b)
		c = toLowerByte(c)
	}
	if len(t.aliases) > 0 {
		if r, ok := t.aliases[a]; ok {
			a = r
		}
		if r, ok := t.aliases[b]; ok {
			b = r
		}
		if r, ok := t.aliases[c]; ok {
			c = r
		}
	}
	// Map whitespace to 0
	if isWhitespace(a) {
		a = 0
	}
	if isWhitespace(b) {
		b = 0
	}
	if isWhitespace(c) {
		c = 0
	}
	// encode() collapses whitespace runs, so consecutive zeros never appear
	// in indexed trigrams. Reject them here to avoid phantom matches.
	if (a == 0 && b == 0) || (b == 0 && c == 0) {
		return 0, false
	}
	// Character-internal trigrams are never emitted by ExtractTrigrams.
	if isInternalTrigram(a, b, c) {
		return 0, false
	}
	if a == 0 && c == 0 {
		// Pattern like " x " — valid: encode produces [0, x, 0] trigrams
	}
	return TrigramValue(a, b, c), true
}

// toLowerByte folds ASCII uppercase to lowercase; non-ASCII bytes pass through.
func toLowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 0x20
	}
	return b
}

