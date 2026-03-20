package microfts2

import "testing"

// CRC: crc-CharSet.md

func TestTrigramsBasic(t *testing.T) {
	tg := NewTrigrams(false, nil)
	trigrams := tg.ExtractTrigrams([]byte("abc"))
	if len(trigrams) != 1 {
		t.Fatalf("ExtractTrigrams(\"abc\"): got %d trigrams, want 1", len(trigrams))
	}
	want := TrigramValue('a', 'b', 'c')
	if trigrams[0] != want {
		t.Errorf("ExtractTrigrams(\"abc\")[0] = %d, want %d", trigrams[0], want)
	}
}

func TestTrigramsSpaceCollapsing(t *testing.T) {
	tg := NewTrigrams(false, nil)
	t1 := tg.ExtractTrigrams([]byte("ab   cd"))
	t2 := tg.ExtractTrigrams([]byte("ab cd"))
	if len(t1) != len(t2) {
		t.Fatalf("space collapsing: got %d trigrams, want %d", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Errorf("trigram[%d]: %d != %d", i, t1[i], t2[i])
		}
	}
}

func TestTrigramsCaseInsensitive(t *testing.T) {
	tg := NewTrigrams(true, nil)
	lower := tg.ExtractTrigrams([]byte("abc"))
	upper := tg.ExtractTrigrams([]byte("ABC"))
	if len(lower) != len(upper) {
		t.Fatalf("case insensitive: different trigram counts %d vs %d", len(lower), len(upper))
	}
	if lower[0] != upper[0] {
		t.Errorf("case insensitive: trigrams differ: %d != %d", lower[0], upper[0])
	}
}

func TestTrigramsAliases(t *testing.T) {
	aliases := map[byte]byte{'\n': '^'}
	tg := NewTrigrams(false, aliases)
	trigrams := tg.ExtractTrigrams([]byte("abc\ndef"))
	// \n becomes ^, so "abc^def" → trigrams: abc, bc^, c^d, ^de, def
	if len(trigrams) != 5 {
		t.Fatalf("aliases: got %d trigrams, want 5", len(trigrams))
	}
	wantTri := TrigramValue('c', '^', 'd')
	if trigrams[2] != wantTri {
		t.Errorf("alias trigram c^d: got %d, want %d", trigrams[2], wantTri)
	}
}

func TestTrigramsShortInput(t *testing.T) {
	tg := NewTrigrams(false, nil)
	if tri := tg.ExtractTrigrams([]byte("ab")); tri != nil {
		t.Errorf("2-byte input should produce nil, got %v", tri)
	}
	if tri := tg.ExtractTrigrams([]byte("a")); tri != nil {
		t.Errorf("1-byte input should produce nil, got %v", tri)
	}
	if tri := tg.ExtractTrigrams([]byte("")); tri != nil {
		t.Errorf("empty input should produce nil, got %v", tri)
	}
}

func TestTrigramsCounts(t *testing.T) {
	tg := NewTrigrams(false, nil)
	counts := tg.TrigramCounts([]byte("abcabc"))
	if counts == nil {
		t.Fatal("TrigramCounts returned nil")
	}
	tri := TrigramValue('a', 'b', 'c')
	if counts[tri] != 2 {
		t.Errorf("count for 'abc' = %d, want 2", counts[tri])
	}
}

func TestTrigramsEncodeTrigram(t *testing.T) {
	tg := NewTrigrams(false, nil)

	// Valid 3-byte trigram
	tri, ok := tg.EncodeTrigram("abc")
	if !ok {
		t.Fatal("EncodeTrigram(\"abc\") returned false")
	}
	want := TrigramValue('a', 'b', 'c')
	if tri != want {
		t.Errorf("EncodeTrigram(\"abc\") = %d, want %d", tri, want)
	}

	// All-whitespace trigram
	_, ok = tg.EncodeTrigram("   ")
	if ok {
		t.Error("EncodeTrigram(\"   \") should return false for all-space")
	}

	// Wrong length
	_, ok = tg.EncodeTrigram("ab")
	if ok {
		t.Error("EncodeTrigram(\"ab\") should return false for 2-byte input")
	}
	_, ok = tg.EncodeTrigram("abcd")
	if ok {
		t.Error("EncodeTrigram(\"abcd\") should return false for 4-byte input")
	}
}

func TestTrigramsWhitespaceBoundaries(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// Tab, newline, carriage return should all act as boundaries
	t1 := tg.ExtractTrigrams([]byte("ab\tcd"))
	t2 := tg.ExtractTrigrams([]byte("ab cd"))
	if len(t1) != len(t2) {
		t.Fatalf("whitespace boundaries: tab gave %d trigrams, space gave %d", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Errorf("trigram[%d]: tab=%d space=%d", i, t1[i], t2[i])
		}
	}
}

func TestTrigramValue(t *testing.T) {
	v := TrigramValue('a', 'b', 'c')
	want := uint32('a')<<16 | uint32('b')<<8 | uint32('c')
	if v != want {
		t.Errorf("TrigramValue('a','b','c') = %d, want %d", v, want)
	}
}

func TestTrigramsSkipInternalCJK(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// '中' is U+4E2D, encoded as 3 bytes: 0xE4 0xB8 0xAD
	// The internal trigram [0xE4, 0xB8, 0xAD] should be skipped.
	// Two adjacent CJK chars produce cross-boundary trigrams.
	text := []byte("中文") // 6 bytes: [E4 B8 AD] [E6 96 87]
	trigrams := tg.ExtractTrigrams(text)
	// 6 bytes → 4 windows, minus 2 internal (one per char) = 2 cross-boundary trigrams
	if len(trigrams) != 2 {
		t.Fatalf("CJK trigrams: got %d, want 2 (cross-boundary only)", len(trigrams))
	}
	// Cross-boundary trigrams: [B8,AD,E6] and [AD,E6,96]
	want0 := TrigramValue(0xB8, 0xAD, 0xE6)
	want1 := TrigramValue(0xAD, 0xE6, 0x96)
	if trigrams[0] != want0 {
		t.Errorf("CJK cross-boundary[0] = %06x, want %06x", trigrams[0], want0)
	}
	if trigrams[1] != want1 {
		t.Errorf("CJK cross-boundary[1] = %06x, want %06x", trigrams[1], want1)
	}
}

func TestTrigramsSkipInternalEmoji(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// '😀' is U+1F600, encoded as 4 bytes: 0xF0 0x9F 0x98 0x80
	// Internal trigrams: [F0,9F,98] and [9F,98,80] — both skipped.
	text := []byte("😀")
	trigrams := tg.ExtractTrigrams(text)
	// 4 bytes → 2 windows, both internal → 0 trigrams
	if len(trigrams) != 0 {
		t.Fatalf("single emoji: got %d trigrams, want 0", len(trigrams))
	}
}

func TestTrigramsSkipInternal2Byte(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// 'é' is U+00E9, encoded as 2 bytes: 0xC3 0xA9
	// No 3-byte window fits inside a 2-byte char, so no skipping occurs.
	text := []byte("éé") // 4 bytes: [C3 A9] [C3 A9]
	trigrams := tg.ExtractTrigrams(text)
	// 4 bytes → 2 windows, neither is internal → 2 trigrams
	if len(trigrams) != 2 {
		t.Fatalf("2-byte chars: got %d trigrams, want 2", len(trigrams))
	}
}

func TestTrigramsASCIIUnchanged(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// ASCII is unaffected by internal trigram skipping
	text := []byte("hello")
	trigrams := tg.ExtractTrigrams(text)
	if len(trigrams) != 3 {
		t.Fatalf("ASCII trigrams: got %d, want 3", len(trigrams))
	}
}

func TestTrigramCountsSkipInternal(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// Same CJK test but via TrigramCounts
	text := []byte("中文")
	counts := tg.TrigramCounts(text)
	if len(counts) != 2 {
		t.Fatalf("CJK TrigramCounts: got %d distinct, want 2", len(counts))
	}
}

func TestEncodeTrigramRejectsInternal(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// 0xE4 0xB8 0xAD is the internal trigram of '中' — should be rejected
	s := string([]byte{0xE4, 0xB8, 0xAD})
	_, ok := tg.EncodeTrigram(s)
	if ok {
		t.Error("EncodeTrigram should reject character-internal trigram")
	}
}

func TestIsInternalTrigram(t *testing.T) {
	// 3-byte char lead + 2 continuations
	if !isInternalTrigram(0xE4, 0xB8, 0xAD) {
		t.Error("3-byte char internal should be detected")
	}
	// 4-byte char: lead + 2 continuations
	if !isInternalTrigram(0xF0, 0x9F, 0x98) {
		t.Error("4-byte char [lead,c1,c2] should be detected")
	}
	// 4-byte char: 3 continuations
	if !isInternalTrigram(0x9F, 0x98, 0x80) {
		t.Error("4-byte char [c1,c2,c3] should be detected")
	}
	// ASCII — not internal
	if isInternalTrigram('a', 'b', 'c') {
		t.Error("ASCII should not be detected as internal")
	}
	// Cross-boundary: continuation, continuation, lead
	if isInternalTrigram(0xB8, 0xAD, 0xE6) {
		t.Error("cross-boundary [cont,cont,lead] should not be internal")
	}
	// 2-byte: lead + continuation + next lead
	if isInternalTrigram(0xC3, 0xA9, 0xC3) {
		t.Error("2-byte cross-boundary should not be internal")
	}
}

func TestValidateAliases(t *testing.T) {
	// Valid: ASCII only
	if err := ValidateAliases(map[byte]byte{'\n': '^', 'A': 'a'}); err != nil {
		t.Errorf("valid ASCII aliases should pass: %v", err)
	}

	// Invalid: non-ASCII source
	if err := ValidateAliases(map[byte]byte{0x80: 'a'}); err == nil {
		t.Error("non-ASCII source byte should fail")
	}

	// Invalid: non-ASCII target
	if err := ValidateAliases(map[byte]byte{'a': 0xC0}); err == nil {
		t.Error("non-ASCII target byte should fail")
	}

	// Nil aliases: valid
	if err := ValidateAliases(nil); err != nil {
		t.Errorf("nil aliases should pass: %v", err)
	}
}

// --- Bigram tests ---

func TestBigramCountsBasic(t *testing.T) {
	tg := NewTrigrams(false, nil)
	counts := tg.BigramCounts([]byte("cat"))
	// "cat" → padded "_cat_" → bigrams: _c, ca, at, t_
	want := map[uint16]int{
		BigramValue('_', 'c'): 1,
		BigramValue('c', 'a'): 1,
		BigramValue('a', 't'): 1,
		BigramValue('t', '_'): 1,
	}
	if len(counts) != len(want) {
		t.Fatalf("BigramCounts(\"cat\"): got %d bigrams, want %d", len(counts), len(want))
	}
	for bi, wantCnt := range want {
		if counts[bi] != wantCnt {
			t.Errorf("bigram 0x%04X: got %d, want %d", bi, counts[bi], wantCnt)
		}
	}
}

func TestBigramCountsMultiWord(t *testing.T) {
	tg := NewTrigrams(false, nil)
	counts := tg.BigramCounts([]byte("hi lo"))
	// "hi" → "_hi_" → _h, hi, i_
	// "lo" → "_lo_" → _l, lo, o_
	if len(counts) != 6 {
		t.Fatalf("BigramCounts(\"hi lo\"): got %d bigrams, want 6", len(counts))
	}
	if counts[BigramValue('_', 'h')] != 1 {
		t.Error("missing _h bigram")
	}
	if counts[BigramValue('o', '_')] != 1 {
		t.Error("missing o_ bigram")
	}
}

func TestBigramCountsCaseInsensitive(t *testing.T) {
	tg := NewTrigrams(true, nil)
	upper := tg.BigramCounts([]byte("Cat"))
	lower := tg.BigramCounts([]byte("cat"))
	if len(upper) != len(lower) {
		t.Fatalf("case insensitive: got %d vs %d bigrams", len(upper), len(lower))
	}
	for bi, cnt := range lower {
		if upper[bi] != cnt {
			t.Errorf("bigram 0x%04X: upper=%d lower=%d", bi, upper[bi], cnt)
		}
	}
}

func TestBigramCountsSkipInternal(t *testing.T) {
	tg := NewTrigrams(false, nil)
	// CJK character 中 = 3 bytes: e4 b8 ad
	// Padded: _ e4 b8 ad _
	// bigrams: (_,e4), (e4,b8) SKIP (both continuation? no e4 is lead), (b8,ad) SKIP (both continuation), (ad,_)
	counts := tg.BigramCounts([]byte("中"))
	// Should have: (_,e4), (e4,b8), (ad,_) — b8,ad are both continuation bytes so skipped
	if counts[BigramValue(0xb8, 0xad)] != 0 {
		t.Error("character-internal bigram (b8,ad) should be skipped")
	}
	if counts[BigramValue('_', 0xe4)] != 1 {
		t.Error("boundary bigram (_,e4) should be present")
	}
}

func TestBigramValue(t *testing.T) {
	v := BigramValue('a', 'b')
	if v != 0x6162 {
		t.Errorf("BigramValue('a','b') = 0x%04X, want 0x6162", v)
	}
}

func TestExtractBigrams(t *testing.T) {
	tg := NewTrigrams(false, nil)
	bigrams := tg.ExtractBigrams([]byte("ab"))
	// "ab" → padded "_ab_" → _a, ab, b_
	if len(bigrams) != 3 {
		t.Fatalf("ExtractBigrams(\"ab\"): got %d, want 3", len(bigrams))
	}
}

func TestBigramCountsEmpty(t *testing.T) {
	tg := NewTrigrams(false, nil)
	counts := tg.BigramCounts([]byte(""))
	if counts != nil {
		t.Errorf("BigramCounts(\"\"): expected nil, got %v", counts)
	}
	counts = tg.BigramCounts([]byte("   "))
	if counts != nil {
		t.Errorf("BigramCounts(whitespace): expected nil, got %v", counts)
	}
}
