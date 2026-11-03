package microfts2

// CRC: crc-DB.md | Test: test-DB.md | R752, R753

import (
	"path/filepath"
	"testing"
)

// searchPaths returns the base names of the files a search hit.
func searchPaths(t *testing.T, db *DB, sr *SearchResults) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	for _, r := range sr.Results {
		got[filepath.Base(r.Path)] = true
	}
	return got
}

// TestSearchTermsTakesTermsAsGiven: a term is used exactly as given (a quote is
// a character, a space is part of the term), where Search would read the same
// text through the query syntax; and Search is SearchTerms over its split.
func TestSearchTermsTakesTermsAsGiven(t *testing.T) {
	db, dir := testDB(t)
	files := map[string]string{
		"quoted.txt":  "she said \"hello world\" twice\n",
		"bare.txt":    "she said hello world twice\n",
		"apart.txt":   "the world said hello to me\n",
		"filler1.txt": "Alpha beta gamma delta\nEpsilon zeta eta theta\n",
		"filler2.txt": "Quick brown fox jumps\nLazy dog sleeps all day\n",
	}
	for name, body := range files {
		if _, err := db.AddFile(writeTestFile(t, dir, name, body), "line"); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name  string
		terms []string
		want  []string
	}{
		// a quote in a term is a character: only the file holding the quotes
		{"quote is a character", []string{`"hello world"`}, []string{"quoted.txt"}},
		// a space in a term keeps the words together: not apart.txt
		{"space is part of the term", []string{"hello world"}, []string{"quoted.txt", "bare.txt"}},
		// two terms: both anywhere in the chunk
		{"two terms", []string{"hello", "world"}, []string{"quoted.txt", "bare.txt", "apart.txt"}},
		// empty terms are dropped
		{"empty term dropped", []string{"", "hello world", ""}, []string{"quoted.txt", "bare.txt"}},
	}
	for _, c := range cases {
		sr, err := db.SearchTerms(c.terms, WithVerify())
		if err != nil {
			t.Fatal(err)
		}
		got := searchPaths(t, db, sr)
		if len(got) != len(c.want) {
			t.Errorf("%s: SearchTerms(%q) hit %v, want %v", c.name, c.terms, got, c.want)
			continue
		}
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("%s: SearchTerms(%q) hit %v, want %v", c.name, c.terms, got, c.want)
			}
		}
	}
	// SearchMultiTerms takes its terms as-is too
	multi, err := db.SearchMultiTerms([]string{`"hello world"`}, map[string]ScoreFunc{"coverage": scoreCoverage}, 10, WithVerify())
	if err != nil {
		t.Fatal(err)
	}
	if len(multi) != 1 || len(multi[0].Results) != 1 || filepath.Base(multi[0].Results[0].Path) != "quoted.txt" {
		t.Errorf("SearchMultiTerms(%q) = %+v, want only quoted.txt", `"hello world"`, multi)
	}
	// R753: Search is SearchTerms over the query syntax's split, so the same
	// text as a query reads its quotes as a phrase marker.
	sr, err := db.Search(`"hello world"`, WithVerify())
	if err != nil {
		t.Fatal(err)
	}
	if got := searchPaths(t, db, sr); !got["bare.txt"] || !got["quoted.txt"] || got["apart.txt"] {
		t.Errorf(`Search("\"hello world\"") hit %v, want the phrase in quoted.txt and bare.txt`, got)
	}
}
