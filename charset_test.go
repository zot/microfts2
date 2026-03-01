package microfts

import "testing"

// CRC: crc-CharSet.md

func TestCharSetEncode(t *testing.T) {
	cs, err := NewCharSet("abcdefghijklmnopqrstuvwxyz0123456789", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v := cs.Encode('a'); v != 1 {
		t.Errorf("Encode('a') = %d, want 1", v)
	}
	if v := cs.Encode('z'); v != 26 {
		t.Errorf("Encode('z') = %d, want 26", v)
	}
	if v := cs.Encode('0'); v != 27 {
		t.Errorf("Encode('0') = %d, want 27", v)
	}
	if v := cs.Encode('9'); v != 36 {
		t.Errorf("Encode('9') = %d, want 36", v)
	}
	if v := cs.Encode(' '); v != 0 {
		t.Errorf("Encode(' ') = %d, want 0", v)
	}
	if v := cs.Encode('!'); v != 0 {
		t.Errorf("Encode('!') = %d, want 0", v)
	}
}

func TestCharSetTrigrams(t *testing.T) {
	cs, _ := NewCharSet("abcdefghijklmnopqrstuvwxyz", false, nil)
	trigrams := cs.Trigrams("abc")
	if len(trigrams) != 1 {
		t.Fatalf("Trigrams(\"abc\"): got %d trigrams, want 1", len(trigrams))
	}
	want := TrigramValue(1, 2, 3) // a=1, b=2, c=3
	if trigrams[0] != want {
		t.Errorf("Trigrams(\"abc\")[0] = %d, want %d", trigrams[0], want)
	}
}

func TestCharSetSpaceCollapsing(t *testing.T) {
	cs, _ := NewCharSet("abcd", false, nil)
	t1 := cs.Trigrams("ab!!!cd")
	t2 := cs.Trigrams("ab cd")
	if len(t1) != len(t2) {
		t.Fatalf("space collapsing: got %d trigrams, want %d", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Errorf("trigram[%d]: %d != %d", i, t1[i], t2[i])
		}
	}
}

func TestCharSetCaseInsensitive(t *testing.T) {
	cs, _ := NewCharSet("abc", true, nil)
	lower := cs.Encode('a')
	upper := cs.Encode('A')
	if lower != upper {
		t.Errorf("case insensitive: Encode('a')=%d != Encode('A')=%d", lower, upper)
	}
}

func TestCharSetAliases(t *testing.T) {
	aliases := map[rune]rune{'\n': '^'}
	cs, _ := NewCharSet("abcdef^", false, aliases)
	t1 := cs.Trigrams("abc\ndef")
	// \n becomes ^, so "abc^def" → trigrams: abc, bc^, c^d, ^de, def
	if len(t1) != 5 {
		t.Fatalf("aliases: got %d trigrams, want 5", len(t1))
	}
	// Verify the ^ trigram is present: c^d
	cVal := cs.Encode('c')
	caretVal := cs.Encode('^')
	dVal := cs.Encode('d')
	wantTri := TrigramValue(cVal, caretVal, dVal)
	if t1[2] != wantTri {
		t.Errorf("alias trigram c^d: got %d, want %d", t1[2], wantTri)
	}
}

func TestCharSetShortInput(t *testing.T) {
	cs, _ := NewCharSet("ab", false, nil)
	if tri := cs.Trigrams("ab"); tri != nil {
		t.Errorf("2-char input should produce nil, got %v", tri)
	}
	if tri := cs.Trigrams("a"); tri != nil {
		t.Errorf("1-char input should produce nil, got %v", tri)
	}
	if tri := cs.Trigrams(""); tri != nil {
		t.Errorf("empty input should produce nil, got %v", tri)
	}
}

func TestCharSetTrigramRoundtrip(t *testing.T) {
	cs, _ := NewCharSet("abc", false, nil)
	tri := TrigramValue(1, 2, 3)
	a, b, c := cs.TrigramChars(tri)
	if a != 'a' || b != 'b' || c != 'c' {
		t.Errorf("TrigramChars(%d) = (%c, %c, %c), want (a, b, c)", tri, a, b, c)
	}
}

func TestCharSetValidation(t *testing.T) {
	_, err := NewCharSet("a b", false, nil)
	if err == nil {
		t.Error("expected error for charset with space")
	}
	long := ""
	for i := 0; i < 64; i++ {
		long += string(rune('a' + i))
	}
	_, err = NewCharSet(long, false, nil)
	if err == nil {
		t.Error("expected error for charset > 63 characters")
	}
}
