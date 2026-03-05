package microfts2

import "math/bits"

// CRC: crc-Bitset.md

const BitsetSize = 2097152 // 2^21 bytes = 2^24 bits

// Bitset is a fixed-size bitset for 16,777,216 trigrams (2^24).
type Bitset [BitsetSize]byte

// Set sets the bit for the given trigram.
func (b *Bitset) Set(trigram uint32) {
	b[trigram>>3] |= 1 << (trigram & 7)
}

// Test returns whether the bit for the given trigram is set.
func (b *Bitset) Test(trigram uint32) bool {
	return b[trigram>>3]&(1<<(trigram&7)) != 0
}

// ForEach calls fn for each set bit in ascending order.
func (b *Bitset) ForEach(fn func(uint32)) {
	for i, v := range b {
		if v == 0 {
			continue
		}
		base := uint32(i) << 3
		for v != 0 {
			tz := bits.TrailingZeros8(v)
			fn(base + uint32(tz))
			v &^= 1 << tz
		}
	}
}

// Bytes returns the bitset as a byte slice for storage.
func (b *Bitset) Bytes() []byte {
	return b[:]
}

// FromBytes loads the bitset from stored bytes.
func (b *Bitset) FromBytes(data []byte) {
	copy(b[:], data)
}

// Count returns the number of set bits.
func (b *Bitset) Count() int {
	n := 0
	for _, v := range b {
		n += bits.OnesCount8(v)
	}
	return n
}
