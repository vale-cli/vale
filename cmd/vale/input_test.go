package main

import "testing"

func TestLooksLikePath(t *testing.T) {
	cases := map[string]bool{
		"READNE.md":       true,
		"docs/READNE.md":  true,
		"nosuchdir/":      true,
		`docs\page`:       true,
		"main.c":          true,
		"XXX":             false,
		"very":            false,
		"e.g.":            false,
		"v1.2":            false,
		"Done.":           false,
		"some text.md":    false,
		"A sentence here": false,
	}
	for in, want := range cases {
		if got := looksLikePath(in); got != want {
			t.Errorf("looksLikePath(%q) = %v, want %v", in, got, want)
		}
	}
}
