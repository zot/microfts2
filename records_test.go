package microfts2

// CRC: crc-DB.md | Test: test-DB.md

import (
	"bytes"
	"slices"
	"testing"
)

func bytesEqual(a, b []byte) bool { return bytes.Equal(a, b) }

func TestCRecordRoundtrip(t *testing.T) {
	orig := CRecord{
		ChunkID:        42,
		ContentLen:     1234,
		DistinctTokens: 2,
		TokenTotal:     7,
		Attrs: []Pair{
			{Key: []byte("timestamp"), Value: []byte("1709900000")},
			{Key: []byte("role"), Value: []byte("user")},
		},
		FileIDs: []FileIDCount{
			{FileID: 1, Count: 1},
			{FileID: 7, Count: 3},
			{FileID: 42, Count: 1},
		},
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
	if got.ContentLen != orig.ContentLen {
		t.Errorf("ContentLen: got %d, want %d", got.ContentLen, orig.ContentLen)
	}
	if got.DistinctTokens != orig.DistinctTokens || got.TokenTotal != orig.TokenTotal {
		t.Errorf("token counts: got %d/%d, want %d/%d", got.DistinctTokens, got.TokenTotal, orig.DistinctTokens, orig.TokenTotal)
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
			t.Errorf("FileID[%d]: got %+v, want %+v", i, got.FileIDs[i], orig.FileIDs[i])
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
	if got.DistinctTokens != 0 || got.TokenTotal != 0 || len(got.Attrs) != 0 || len(got.FileIDs) != 0 {
		t.Errorf("expected zero counts and empty slices, got tok=%d/%d attr=%d fid=%d",
			got.DistinctTokens, got.TokenTotal, len(got.Attrs), len(got.FileIDs))
	}
}

func TestCRecordFileIDIncDec(t *testing.T) {
	c := CRecord{}
	c.IncFileID(7)
	c.IncFileID(7)
	c.IncFileID(7)
	c.IncFileID(42)
	if got := c.FileIDCountFor(7); got != 3 {
		t.Errorf("count for 7: got %d, want 3", got)
	}
	if got := c.FileIDCountFor(42); got != 1 {
		t.Errorf("count for 42: got %d, want 1", got)
	}

	if removed := c.DecFileID(7); removed {
		t.Errorf("DecFileID(7) at count 3: should not remove entry")
	}
	if got := c.FileIDCountFor(7); got != 2 {
		t.Errorf("count for 7 after dec: got %d, want 2", got)
	}

	c.DecFileID(7)
	if removed := c.DecFileID(7); !removed {
		t.Errorf("DecFileID(7) at count 1: should remove entry")
	}
	if got := c.FileIDCountFor(7); got != 0 {
		t.Errorf("count for 7 after final dec: got %d, want 0", got)
	}
	if len(c.FileIDs) != 1 {
		t.Errorf("FileIDs length after removing 7: got %d, want 1", len(c.FileIDs))
	}
}

func TestCRecordRefcountRoundtrip(t *testing.T) {
	orig := CRecord{
		ChunkID: 1,
		FileIDs: []FileIDCount{
			{FileID: 5, Count: 7},
			{FileID: 99, Count: 2},
		},
	}
	data := orig.MarshalValue()
	got, err := UnmarshalCValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.FileIDs) != 2 {
		t.Fatalf("FileIDs len: got %d, want 2", len(got.FileIDs))
	}
	for i, want := range orig.FileIDs {
		if got.FileIDs[i] != want {
			t.Errorf("FileIDs[%d]: got %+v, want %+v", i, got.FileIDs[i], want)
		}
	}
}

func TestFRecordLocatorRoundtrip(t *testing.T) {
	orig := FRecord{
		FileID:   1,
		Strategy: "line",
		Name:     "/x/y.txt",
		Chunks: []FileChunkEntry{
			{ChunkID: 10, Location: "1-1", Locator: []byte{0, 100}},
			{ChunkID: 11, Location: "2-2", Locator: nil},
			{ChunkID: 12, Location: "3-3", Locator: []byte("docx#para/47")},
		},
	}
	data := orig.MarshalValue()
	got, err := UnmarshalFValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chunks) != 3 {
		t.Fatalf("Chunks len: got %d, want 3", len(got.Chunks))
	}
	for i, want := range orig.Chunks {
		if got.Chunks[i].ChunkID != want.ChunkID {
			t.Errorf("Chunks[%d].ChunkID: got %d, want %d", i, got.Chunks[i].ChunkID, want.ChunkID)
		}
		if got.Chunks[i].Location != want.Location {
			t.Errorf("Chunks[%d].Location: got %q, want %q", i, got.Chunks[i].Location, want.Location)
		}
		if !bytesEqual(got.Chunks[i].Locator, want.Locator) {
			t.Errorf("Chunks[%d].Locator: got %v, want %v", i, got.Chunks[i].Locator, want.Locator)
		}
	}
}

func TestEncodeByteRangeLocator(t *testing.T) {
	loc := EncodeByteRangeLocator(100, 250)
	start, end, ok := DecodeByteRangeLocator(loc)
	if !ok {
		t.Fatal("decode failed")
	}
	if start != 100 || end != 250 {
		t.Errorf("got %d-%d, want 100-250", start, end)
	}
	if _, _, ok := DecodeByteRangeLocator(nil); ok {
		t.Error("expected ok=false for nil locator")
	}
}

func TestFRecordRoundtrip(t *testing.T) {
	orig := FRecord{
		FileID:     99,
		ModTime:    1709900000000000000,
		FileLength: 12345,
		Strategy:   "chunk-lines",
		Name:       "/home/user/file.txt",
		Chunks: []FileChunkEntry{
			{ChunkID: 1, Location: "1-10", Locator: []byte{0, 100}},
			{ChunkID: 2, Location: "11-20"},
			{ChunkID: 3, Location: "21-30", Locator: []byte("docx#para/3")},
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
	if got.Name != orig.Name {
		t.Errorf("Name: got %q, want %q", got.Name, orig.Name)
	}
	if len(got.Chunks) != len(orig.Chunks) {
		t.Fatalf("Chunks len: got %d, want %d", len(got.Chunks), len(orig.Chunks))
	}
	for i := range orig.Chunks {
		if got.Chunks[i].ChunkID != orig.Chunks[i].ChunkID ||
			got.Chunks[i].Location != orig.Chunks[i].Location ||
			!bytesEqual(got.Chunks[i].Locator, orig.Chunks[i].Locator) {
			t.Errorf("Chunk[%d]: got %+v, want %+v", i, got.Chunks[i], orig.Chunks[i])
		}
	}
}

// TestFRecordEndsAfterItsChunks: the F record value ends after the chunk list,
// and a value with bytes past it (a format "4" token bag read as format "5")
// is refused rather than decoded.
// Test: test-DB.md | CRC: crc-DB.md | R765
func TestFRecordEndsAfterItsChunks(t *testing.T) {
	orig := FRecord{Strategy: "lines", Name: "/f", Chunks: []FileChunkEntry{{ChunkID: 7, Location: "1-2"}}}
	data := orig.MarshalValue()
	if _, err := UnmarshalFValue(data); err != nil {
		t.Fatalf("the record itself: %v", err)
	}
	// A format "4" record's empty token bag is one more byte, a zero count.
	if _, err := UnmarshalFValue(append(data, 0)); err == nil {
		t.Fatal("a record with a token bag after its chunks decoded without error")
	}
}

// TestTRecordRoundtrip: postings with counts of 1 (no count written) and above
// 1, ids spanning several varint widths, decode to what was encoded; a count
// of 1 costs no byte. R770, R780
// Test: test-DB.md | CRC: crc-DB.md | R770, R780
func TestTRecordRoundtrip(t *testing.T) {
	orig := TRecord{
		Trigram:  0x616263,
		Postings: []Posting{{1, 1}, {42, 3}, {1000, 1}, {999999, 200}},
	}
	data := orig.MarshalValue()
	got, err := UnmarshalTValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, orig.Postings) {
		t.Errorf("postings: got %v, want %v", got, orig.Postings)
	}
	ones := TRecord{Postings: []Posting{{1, 1}, {2, 1}, {3, 1}}}
	if n := len(ones.MarshalValue()); n != 3 {
		t.Errorf("three postings with count 1 took %d bytes, want 3", n)
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

	// D key (R774)
	dk := makeDKey(300)
	if dk[0] != prefixD {
		t.Errorf("D key prefix: got %c, want %c", dk[0], prefixD)
	}
	if id, n := readUvarint(dk[1:]); id != 300 || n != len(dk)-1 {
		t.Errorf("D key chunkid: got %d (%d bytes), want 300", id, n)
	}

}
