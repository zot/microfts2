package microfts2

import "testing"

// CRC: crc-Bitset.md

func TestBitsetSetAndTest(t *testing.T) {
	var b Bitset
	b.Set(0)
	b.Set(100)
	b.Set(16777215) // max trigram (2^24 - 1)

	if !b.Test(0) {
		t.Error("expected bit 0 set")
	}
	if !b.Test(100) {
		t.Error("expected bit 100 set")
	}
	if !b.Test(16777215) {
		t.Error("expected bit 16777215 set")
	}
	if b.Test(1) {
		t.Error("expected bit 1 unset")
	}
	if b.Test(50000) {
		t.Error("expected bit 50000 unset")
	}
}

func TestBitsetBytesRoundtrip(t *testing.T) {
	var b1 Bitset
	b1.Set(42)
	b1.Set(999)
	b1.Set(200000)

	var b2 Bitset
	b2.FromBytes(b1.Bytes())

	if !b2.Test(42) || !b2.Test(999) || !b2.Test(200000) {
		t.Error("roundtrip lost bits")
	}
	if b2.Test(43) {
		t.Error("roundtrip gained bits")
	}
}

func TestBitsetForEach(t *testing.T) {
	var b Bitset
	b.Set(5)
	b.Set(1000)
	b.Set(200000)

	var got []uint32
	b.ForEach(func(v uint32) {
		got = append(got, v)
	})

	want := []uint32{5, 1000, 200000}
	if len(got) != len(want) {
		t.Fatalf("ForEach: got %d items, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ForEach[%d]: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestBitsetCount(t *testing.T) {
	var b Bitset
	for i := uint32(0); i < 50; i++ {
		b.Set(i * 1000)
	}
	if b.Count() != 50 {
		t.Errorf("Count: got %d, want 50", b.Count())
	}
}

func TestBitsetEmpty(t *testing.T) {
	var b Bitset
	if b.Count() != 0 {
		t.Error("empty bitset should have count 0")
	}
	var called bool
	b.ForEach(func(uint32) { called = true })
	if called {
		t.Error("ForEach should not call fn on empty bitset")
	}
	for _, v := range b.Bytes() {
		if v != 0 {
			t.Error("empty bitset Bytes should be all zeros")
			break
		}
	}
}
