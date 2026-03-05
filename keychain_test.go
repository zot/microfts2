package microfts2

import (
	"strings"
	"testing"
)

func TestEncodeFilenameShort(t *testing.T) {
	pairs := EncodeFilename("/tmp/test.txt")
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want 1", len(pairs))
	}
	if pairs[0].Key[0] != fPrefix {
		t.Errorf("key[0] = %c, want %c", pairs[0].Key[0], fPrefix)
	}
	if pairs[0].Key[1] != fFinalPart {
		t.Errorf("key[1] = %d, want %d (final)", pairs[0].Key[1], fFinalPart)
	}
	if string(pairs[0].Key[2:]) != "/tmp/test.txt" {
		t.Errorf("key payload = %q, want %q", string(pairs[0].Key[2:]), "/tmp/test.txt")
	}
}

func TestEncodeFilenameLong(t *testing.T) {
	// Create a filename longer than 509 bytes
	name := "/" + strings.Repeat("a", 600)
	pairs := EncodeFilename(name)
	if len(pairs) < 2 {
		t.Fatalf("got %d pairs, want >= 2 for a %d-byte name", len(pairs), len(name))
	}
	// Non-final parts have sequential part numbers
	for i := 0; i < len(pairs)-1; i++ {
		if pairs[i].Key[1] != byte(i) {
			t.Errorf("pair %d: part = %d, want %d", i, pairs[i].Key[1], i)
		}
	}
	// Final part has fFinalPart
	last := pairs[len(pairs)-1]
	if last.Key[1] != fFinalPart {
		t.Errorf("last pair: part = %d, want %d (final)", last.Key[1], fFinalPart)
	}
}

func TestDecodeFilenameRoundtrip(t *testing.T) {
	for _, name := range []string{
		"/short.txt",
		"/" + strings.Repeat("x", 600),
		"/" + strings.Repeat("y", 1200),
	} {
		pairs := EncodeFilename(name)
		keys := make([][]byte, len(pairs))
		for i, p := range pairs {
			keys[i] = p.Key
		}
		got := DecodeFilename(keys)
		if got != name {
			t.Errorf("roundtrip failed for %d-byte name: got %d bytes", len(name), len(got))
		}
	}
}

func TestFinalKeyShort(t *testing.T) {
	name := "/tmp/test.txt"
	fk := FinalKey(name)
	pairs := EncodeFilename(name)
	// For short names, FinalKey should equal the only key
	if string(fk) != string(pairs[0].Key) {
		t.Error("FinalKey != EncodeFilename key for short name")
	}
}

func TestFinalKeyLong(t *testing.T) {
	name := "/" + strings.Repeat("z", 600)
	fk := FinalKey(name)
	pairs := EncodeFilename(name)
	last := pairs[len(pairs)-1]
	if string(fk) != string(last.Key) {
		t.Error("FinalKey != last EncodeFilename key for long name")
	}
}

func TestEncodeFilenameMaxPartLen(t *testing.T) {
	// Exactly maxPartLen should produce a single pair
	name := strings.Repeat("b", maxPartLen)
	pairs := EncodeFilename(name)
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs for exactly maxPartLen, want 1", len(pairs))
	}
	// One byte over should produce two pairs
	name = strings.Repeat("b", maxPartLen+1)
	pairs = EncodeFilename(name)
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs for maxPartLen+1, want 2", len(pairs))
	}
}
