package microfts2

import (
	"testing"
)

func TestCRecordRoundtrip(t *testing.T) {
	orig := CRecord{
		ChunkID: 42,
		Trigrams: []TrigramEntry{
			{Trigram: 0x616263, Count: 3}, // "abc"
			{Trigram: 0x626364, Count: 1}, // "bcd"
		},
		Tokens: []TokenEntry{
			{Token: "hello", Count: 5},
			{Token: "world", Count: 2},
		},
		Attrs: []Pair{
			{Key: []byte("timestamp"), Value: []byte("1709900000")},
			{Key: []byte("role"), Value: []byte("user")},
		},
		FileIDs: []uint64{1, 7, 42},
	}
	copy(orig.Hash[:], "abcdefghijklmnopqrstuvwxyz012345")

	data := orig.MarshalValue()
	got, err := UnmarshalCValue(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.Hash != orig.Hash {
		t.Errorf("Hash mismatch")
	}
	if len(got.Trigrams) != len(orig.Trigrams) {
		t.Fatalf("Trigrams len: got %d, want %d", len(got.Trigrams), len(orig.Trigrams))
	}
	for i := range orig.Trigrams {
		if got.Trigrams[i] != orig.Trigrams[i] {
			t.Errorf("Trigram[%d]: got %+v, want %+v", i, got.Trigrams[i], orig.Trigrams[i])
		}
	}
	if len(got.Tokens) != len(orig.Tokens) {
		t.Fatalf("Tokens len: got %d, want %d", len(got.Tokens), len(orig.Tokens))
	}
	for i := range orig.Tokens {
		if got.Tokens[i] != orig.Tokens[i] {
			t.Errorf("Token[%d]: got %+v, want %+v", i, got.Tokens[i], orig.Tokens[i])
		}
	}
	if len(got.Attrs) != len(orig.Attrs) {
		t.Fatalf("Attrs len: got %d, want %d", len(got.Attrs), len(orig.Attrs))
	}
	for i, p := range orig.Attrs {
		if string(got.Attrs[i].Key) != string(p.Key) || string(got.Attrs[i].Value) != string(p.Value) {
			t.Errorf("Attr[%d]: got %q=%q, want %q=%q", i, got.Attrs[i].Key, got.Attrs[i].Value, p.Key, p.Value)
		}
	}
	if len(got.FileIDs) != len(orig.FileIDs) {
		t.Fatalf("FileIDs len: got %d, want %d", len(got.FileIDs), len(orig.FileIDs))
	}
	for i := range orig.FileIDs {
		if got.FileIDs[i] != orig.FileIDs[i] {
			t.Errorf("FileID[%d]: got %d, want %d", i, got.FileIDs[i], orig.FileIDs[i])
		}
	}
}

func TestCRecordEmptyRoundtrip(t *testing.T) {
	orig := CRecord{}
	data := orig.MarshalValue()
	got, err := UnmarshalCValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trigrams) != 0 || len(got.Tokens) != 0 || len(got.Attrs) != 0 || len(got.FileIDs) != 0 {
		t.Errorf("expected empty slices/maps, got tri=%d tok=%d attr=%d fid=%d",
			len(got.Trigrams), len(got.Tokens), len(got.Attrs), len(got.FileIDs))
	}
}

func TestFRecordRoundtrip(t *testing.T) {
	orig := FRecord{
		FileID:     99,
		ModTime:    1709900000000000000,
		FileLength: 12345,
		Strategy:   "chunk-lines",
		Names:      []string{"/home/user/file.txt", "/home/user/link.txt"},
		Chunks: []FileChunkEntry{
			{ChunkID: 1, Location: "1-10"},
			{ChunkID: 2, Location: "11-20"},
			{ChunkID: 3, Location: "21-30"},
		},
		Tokens: []TokenEntry{
			{Token: "hello", Count: 7},
			{Token: "world", Count: 3},
		},
	}
	copy(orig.ContentHash[:], "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345")

	data := orig.MarshalValue()
	got, err := UnmarshalFValue(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.ModTime != orig.ModTime {
		t.Errorf("ModTime: got %d, want %d", got.ModTime, orig.ModTime)
	}
	if got.ContentHash != orig.ContentHash {
		t.Errorf("ContentHash mismatch")
	}
	if got.FileLength != orig.FileLength {
		t.Errorf("FileLength: got %d, want %d", got.FileLength, orig.FileLength)
	}
	if got.Strategy != orig.Strategy {
		t.Errorf("Strategy: got %q, want %q", got.Strategy, orig.Strategy)
	}
	if len(got.Names) != len(orig.Names) {
		t.Fatalf("Names len: got %d, want %d", len(got.Names), len(orig.Names))
	}
	for i := range orig.Names {
		if got.Names[i] != orig.Names[i] {
			t.Errorf("Name[%d]: got %q, want %q", i, got.Names[i], orig.Names[i])
		}
	}
	if len(got.Chunks) != len(orig.Chunks) {
		t.Fatalf("Chunks len: got %d, want %d", len(got.Chunks), len(orig.Chunks))
	}
	for i := range orig.Chunks {
		if got.Chunks[i] != orig.Chunks[i] {
			t.Errorf("Chunk[%d]: got %+v, want %+v", i, got.Chunks[i], orig.Chunks[i])
		}
	}
	if len(got.Tokens) != len(orig.Tokens) {
		t.Fatalf("Tokens len: got %d, want %d", len(got.Tokens), len(orig.Tokens))
	}
	for i := range orig.Tokens {
		if got.Tokens[i] != orig.Tokens[i] {
			t.Errorf("Token[%d]: got %+v, want %+v", i, got.Tokens[i], orig.Tokens[i])
		}
	}
}

func TestTRecordRoundtrip(t *testing.T) {
	orig := TRecord{
		Trigram:  0x616263,
		ChunkIDs: []uint64{1, 42, 1000, 999999},
	}
	data := orig.MarshalValue()
	got, err := UnmarshalTValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(orig.ChunkIDs) {
		t.Fatalf("ChunkIDs len: got %d, want %d", len(got), len(orig.ChunkIDs))
	}
	for i := range orig.ChunkIDs {
		if got[i] != orig.ChunkIDs[i] {
			t.Errorf("ChunkID[%d]: got %d, want %d", i, got[i], orig.ChunkIDs[i])
		}
	}
}

func TestKeyConstruction(t *testing.T) {
	// C key
	ck := makeCKey(42)
	if ck[0] != prefixC {
		t.Errorf("C key prefix: got %c, want %c", ck[0], prefixC)
	}

	// F key
	fk := makeFKey(99)
	if fk[0] != prefixF {
		t.Errorf("F key prefix: got %c, want %c", fk[0], prefixF)
	}

	// H key
	var hash [32]byte
	copy(hash[:], "abcdefghijklmnopqrstuvwxyz012345")
	hk := makeHKey(hash)
	if hk[0] != prefixH {
		t.Errorf("H key prefix: got %c, want %c", hk[0], prefixH)
	}
	if len(hk) != 33 {
		t.Errorf("H key len: got %d, want 33", len(hk))
	}

	// I key
	ik := makeIKey("caseInsensitive")
	if ik[0] != prefixI {
		t.Errorf("I key prefix: got %c, want %c", ik[0], prefixI)
	}
	if string(ik[1:]) != "caseInsensitive" {
		t.Errorf("I key name: got %q", string(ik[1:]))
	}

	// T key
	tk := makeTKey(0x616263)
	if tk[0] != prefixT {
		t.Errorf("T key prefix: got %c, want %c", tk[0], prefixT)
	}
	if len(tk) != 4 {
		t.Errorf("T key len: got %d, want 4", len(tk))
	}

	// W key
	wk := makeWKey(0x12345678)
	if wk[0] != prefixW {
		t.Errorf("W key prefix: got %c, want %c", wk[0], prefixW)
	}
	if len(wk) != 5 {
		t.Errorf("W key len: got %d, want 5", len(wk))
	}

}
