package microfts

// CRC: crc-KeyChain.md

const (
	fPrefix     = 'F'
	fFinalPart  = byte(255)
	maxPartLen  = 509 // 511 (LMDB key limit) - 1 (F prefix) - 1 (part byte)
)

// KeyPair is an F record key/value pair.
type KeyPair struct {
	Key   []byte
	Value []byte // nil for non-final parts; caller sets fileid on final part
}

// EncodeFilename returns F record key/value pairs for a filename.
// Short filenames (≤509 bytes) produce a single final key.
// Longer filenames are split across chained keys.
func EncodeFilename(filename string) []KeyPair {
	name := []byte(filename)
	if len(name) <= maxPartLen {
		return []KeyPair{{Key: fKey(fFinalPart, name)}}
	}
	var pairs []KeyPair
	part := byte(0)
	for len(name) > maxPartLen {
		pairs = append(pairs, KeyPair{Key: fKey(part, name[:maxPartLen])})
		name = name[maxPartLen:]
		part++
	}
	pairs = append(pairs, KeyPair{Key: fKey(fFinalPart, name)})
	return pairs
}

// FinalKey returns the final F record key for direct fileid lookup.
func FinalKey(filename string) []byte {
	name := []byte(filename)
	if len(name) <= maxPartLen {
		return fKey(fFinalPart, name)
	}
	offset := (len(name) / maxPartLen) * maxPartLen
	if offset == len(name) {
		offset -= maxPartLen
	}
	return fKey(fFinalPart, name[offset:])
}

// DecodeFilename reconstructs a filename from chained F record keys.
// Keys must be in order (part 0, 1, ..., 255).
func DecodeFilename(keys [][]byte) string {
	var result []byte
	for _, key := range keys {
		if len(key) < 2 {
			continue
		}
		result = append(result, key[2:]...) // skip F prefix and part byte
	}
	return string(result)
}

func fKey(part byte, segment []byte) []byte {
	key := make([]byte, 2+len(segment))
	key[0] = fPrefix
	key[1] = part
	copy(key[2:], segment)
	return key
}
