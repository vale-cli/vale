package regex

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// The scanner has to find exactly the literals a search for each finds.
func TestScanner(t *testing.T) {
	cases := []struct {
		lits []string
		text string
	}{
		{[]string{"he", "she", "his", "hers"}, "ushers"},
		{[]string{"abc", "bc", "c"}, "xabcx"},
		{[]string{"aa", "aaa"}, "aaaa"},
		{[]string{"a chip", "chip off", "off the old"}, "a chip off the old block"},
		{[]string{"café", "fé", "naïve"}, "a naïve café"},
		{[]string{"", "x"}, "y"},
		{[]string{"same", "same"}, "the same"},
		{[]string{"abcd", "bcx"}, "abcx"},
		{[]string{"needle"}, ""},
	}

	for _, c := range cases {
		s := newScanner(c.lits)
		seen := make([]uint64, 1)
		s.scan(c.text, seen)

		for id, lit := range c.lits {
			got := seen[0]&(1<<id) != 0
			if want := strings.Contains(c.text, lit); got != want {
				t.Errorf("%q in %q: found = %v, want %v", lit, c.text, got, want)
			}
		}

		want := slices.ContainsFunc(c.lits, func(l string) bool { return strings.Contains(c.text, l) })
		if got := s.any(c.text); got != want {
			t.Errorf("any(%q) over %q = %v, want %v", c.text, c.lits, got, want)
		}
	}
}

// Random literals over a small alphabet overlap constantly, which is where
// a fallback link would go wrong.
func TestScannerRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // a fixed seed, so a failure repeats
	word := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = "abc "[rng.Intn(4)]
		}
		return string(b)
	}

	for range 200 {
		lits := make([]string, 1+rng.Intn(90))
		for i := range lits {
			lits[i] = word(1 + rng.Intn(6))
		}
		text := word(rng.Intn(60))

		s := newScanner(lits)
		seen := make([]uint64, (len(lits)+63)/64)
		s.scan(text, seen)

		for id, lit := range lits {
			got := seen[id>>6]&(1<<(id&63)) != 0
			if want := strings.Contains(text, lit); got != want {
				t.Fatalf("%q in %q: found = %v, want %v", lit, text, got, want)
			}
		}
	}
}

// A set answers as its patterns' tests do one at a time, on either side of
// the point where it starts to scan.
func TestFilterSet(t *testing.T) {
	few := [][][]string{
		{{"alpha"}},
		{{"beta", "gamma"}, {"delta"}},
		nil,
	}

	many := [][][]string{nil}
	for i := range 3 * scanFloor {
		many = append(many, [][]string{{fmt.Sprintf("word%d", i), "shared"}, {"also"}})
	}
	many = append(many, [][]string{{"alpha"}})

	texts := []string{
		"",
		"alpha",
		"beta and delta",
		"gamma only",
		"word3 also",
		"shared also alpha",
		"word1 word2",
	}

	for _, tests := range [][][][]string{few, many} {
		set := NewFilterSet(tests)
		for _, text := range texts {
			var want []int
			for i, test := range tests {
				if Holds(test, text) {
					want = append(want, i)
				}
			}

			if got := set.Active(text); !slices.Equal(got, want) {
				t.Errorf("Active(%q) = %v, want %v", text, got, want)
			}
			if got := set.Any(text); got != (len(want) > 0) {
				t.Errorf("Any(%q) = %v, want %v", text, got, len(want) > 0)
			}
		}
	}
}

// One pattern with one clause takes the early exit.
func TestFilterSetAny(t *testing.T) {
	var clause []string
	for i := range 2 * scanFloor {
		clause = append(clause, fmt.Sprintf("term%d", i))
	}
	set := NewFilterSet([][][]string{{clause}})

	if !set.Any("a term7 here") {
		t.Error("Any missed a literal the text holds")
	}
	if set.Any("nothing here") {
		t.Error("Any found a literal the text does not hold")
	}
}
