package microfts2

// CRC: crc-DB.md | Test: test-DB.md

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.etcd.io/bbolt"
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
	if id, n := KeyID(dk[1:]); id != 300 || n != len(dk)-1 { // R794
		t.Errorf("D key chunkid: got %d (%d bytes), want 300", id, n)
	}

}

// CRC: crc-DB.md | Test: test-DB.md | R791, R793
func TestKeyIDOrder(t *testing.T) {
	vals := make([]uint64, 0, 70100)
	for v := uint64(0); v <= 70000; v++ {
		vals = append(vals, v)
	}
	for _, b := range []uint64{1 << 24, 1 << 32, 1 << 40, 1 << 48, 1 << 56} {
		vals = append(vals, b-2, b-1, b, b+1)
	}
	vals = append(vals, ^uint64(0)-1, ^uint64(0))
	wantLen := map[uint64]int{240: 1, 241: 2, 2287: 2, 2288: 3, 67823: 3, 67824: 4,
		1<<24 - 1: 4, 1 << 24: 5, 1<<56 - 1: 8, 1 << 56: 9, ^uint64(0): 9}
	var prev []byte
	for i, v := range vals {
		enc := AppendKeyID(nil, v)
		if n, ok := wantLen[v]; ok && len(enc) != n {
			t.Fatalf("AppendKeyID(%d) is %d bytes, want %d", v, len(enc), n)
		}
		got, n := KeyID(enc)
		if got != v || n != len(enc) {
			t.Fatalf("KeyID(AppendKeyID(%d)) = %d, %d; want %d, %d", v, got, n, v, len(enc))
		}
		if i > 0 && bytes.Compare(prev, enc) >= 0 {
			t.Fatalf("AppendKeyID(%d) = %x does not sort after AppendKeyID(%d) = %x", v, enc, vals[i-1], prev)
		}
		prev = enc
	}
}

// CRC: crc-DB.md | Test: test-DB.md | R792
func TestKeyIDRefusesBadInput(t *testing.T) {
	short := [][]byte{nil, {}}
	for _, v := range []uint64{241, 2288, 67824, 1 << 24, 1 << 56} {
		enc := AppendKeyID(nil, v)
		short = append(short, enc[:len(enc)-1])
	}
	for _, b := range short {
		if _, n := KeyID(b); n != 0 {
			t.Errorf("KeyID(%x) n = %d, want 0 (short)", b, n)
		}
	}
	for _, b := range [][]byte{{250, 0, 0, 1}, {251, 0, 0, 0, 0}, {251, 0, 1, 0, 0}} {
		if _, n := KeyID(b); n >= 0 {
			t.Errorf("KeyID(%x) n = %d, want < 0 (non-minimal)", b, n)
		}
	}
}

// CRC: crc-DB.md | Test: test-DB.md | R794
func TestCFDKeysSortByID(t *testing.T) {
	dir := t.TempDir()
	db := newLineDB(t, dir, "db")
	var paths []string
	for i := 0; i < 300; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("unique line number %d here\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AddFile(p, "line"); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if err := db.RemoveFile(paths[7]); err != nil {
		t.Fatal(err)
	}
	counts := map[byte]int{}
	err := db.DB().View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte("fts")).Cursor()
		for _, pfx := range []byte{prefixC, prefixF, prefixD} {
			var last uint64
			for k, _ := c.Seek([]byte{pfx}); k != nil && k[0] == pfx; k, _ = c.Next() {
				id, n := KeyID(k[1:])
				if n != len(k)-1 {
					t.Fatalf("%c key %x does not hold one ordered id", pfx, k)
				}
				if counts[pfx] > 0 && id <= last {
					t.Fatalf("%c keys out of order: %d after %d", pfx, id, last)
				}
				last = id
				counts[pfx]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts[prefixC] != 299 || counts[prefixF] != 299 || counts[prefixD] != 1 {
		t.Fatalf("C/F/D counts = %d/%d/%d, want 299/299/1", counts[prefixC], counts[prefixF], counts[prefixD])
	}
	for _, i := range []int{0, 127, 128, 200, 299} {
		res, err := db.Search(fmt.Sprintf(`"unique line number %d here"`, i))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Results) != 1 || res.Results[0].Path != paths[i] {
			t.Fatalf("search for file %d found %+v", i, res.Results)
		}
	}
}

// CRC: crc-DB.md | Test: test-DB.md | R796
func TestWritesFillAppendedPages(t *testing.T) {
	db := newLineDB(t, t.TempDir(), "db")
	val := bytes.Repeat([]byte{7}, 100)
	for i := uint64(1); i <= 5000; i++ {
		err := db.DB().Update(func(tx *bbolt.Tx) error {
			return db.bucket(tx).Put(makeCKey(i), val)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	err := db.DB().View(func(tx *bbolt.Tx) error {
		st := tx.Bucket([]byte("fts")).Stats()
		fill := float64(st.LeafInuse) / float64(st.LeafPageN*db.DB().Info().PageSize)
		if fill < 0.8 {
			t.Fatalf("leaf fill %.2f over %d pages, want >= 0.8", fill, st.LeafPageN)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
