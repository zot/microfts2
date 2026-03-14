package microfts2

// CRC: crc-KeyChain.md

const (
	nPrefix    = 'N'
	nFinalPart = byte(255)
	maxPartLen = 509 // 511 (LMDB key limit) - 1 (N prefix) - 1 (part byte)
)

// KeyPair is an N record key/value pair for filename key chains.
type KeyPair struct {
	Key   []byte
	Value []byte // nil for non-final parts; caller sets fileid on final part
}

// EncodeFilename returns N record key/value pairs for a filename.
// Short filenames (≤509 bytes) produce a single final key.
// Longer filenames are split across chained keys.
func EncodeFilename(filename string) []KeyPair {
	name := []byte(filename)
	if len(name) <= maxPartLen {
		return []KeyPair{{Key: nKey(nFinalPart, name)}}
	}
	var pairs []KeyPair
	part := byte(0)
	for len(name) > maxPartLen {
		pairs = append(pairs, KeyPair{Key: nKey(part, name[:maxPartLen])})
		name = name[maxPartLen:]
		part++
	}
	pairs = append(pairs, KeyPair{Key: nKey(nFinalPart, name)})
	return pairs
}

// FinalKey returns the final N record key for direct fileid lookup.
func FinalKey(filename string) []byte {
	name := []byte(filename)
	if len(name) <= maxPartLen {
		return nKey(nFinalPart, name)
	}
	offset := (len(name) / maxPartLen) * maxPartLen
	if offset == len(name) {
		offset -= maxPartLen
	}
	return nKey(nFinalPart, name[offset:])
}

// DecodeFilename reconstructs a filename from chained N record keys.
// Keys must be in order (part 0, 1, ..., 255).
func DecodeFilename(keys [][]byte) string {
	var result []byte
	for _, key := range keys {
		if len(key) < 2 {
			continue
		}
		result = append(result, key[2:]...) // skip N prefix and part byte
	}
	return string(result)
}

func nKey(part byte, segment []byte) []byte {
	key := make([]byte, 2+len(segment))
	key[0] = nPrefix
	key[1] = part
	copy(key[2:], segment)
	return key
}
