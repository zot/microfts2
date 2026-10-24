package main

import (
	"strings"
	"testing"
)

// CRC: crc-CLI.md | R711
func TestSearchFlagConflict(t *testing.T) {
	for _, c := range []struct {
		verify, loose, fuzzy bool
		other                string // flag the error must name; "" = no error
	}{
		{false, false, false, ""},
		{true, false, false, ""},
		{false, true, false, ""},
		{false, false, true, ""},
		{true, true, false, "-loose"},
		{true, false, true, "-fuzzy"},
	} {
		err := searchFlagConflict(c.verify, c.loose, c.fuzzy)
		switch {
		case c.other == "" && err != nil:
			t.Errorf("verify=%v loose=%v fuzzy=%v: unexpected error %v", c.verify, c.loose, c.fuzzy, err)
		case c.other != "" && (err == nil || !strings.Contains(err.Error(), "-verify") || !strings.Contains(err.Error(), c.other)):
			t.Errorf("verify=%v loose=%v fuzzy=%v: want error naming -verify and %s, got %v", c.verify, c.loose, c.fuzzy, c.other, err)
		}
	}
}
