package check

import (
	"fmt"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// TestAddExceptions tests that the accepted vocabulary becomes one whole-word
// pattern, built once from every term, and that an empty vocabulary leaves
// it unset. See #1214.
func TestAddExceptions(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	s := &Spelling{}
	if err = addExceptions(s, baseCheck{}, cfg); err != nil {
		t.Fatal(err)
	} else if s.exceptRe != nil {
		t.Fatalf("empty vocabulary compiled %q", s.exceptRe.String())
	}

	for i := range 500 {
		cfg.AcceptedTokens = append(cfg.AcceptedTokens, fmt.Sprintf("term%d", i))
	}
	cfg.AcceptedTokens = append(cfg.AcceptedTokens, "Vale")

	s = &Spelling{}
	if err = addExceptions(s, baseCheck{}, cfg); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		word string
		want bool
	}{
		{"term0", true},
		{"term499", true},
		{"vale", true},
		{"term500", false},
		{"valet", false},
	}
	for _, c := range cases {
		if got := isMatch(s.exceptRe, c.word); got != c.want {
			t.Errorf("isMatch(%q) = %v; want %v", c.word, got, c.want)
		}
	}
}

// A vocabulary too long for one pattern under the default stack limit still
// reaches the spelling rule.
func TestAddExceptionsLargeVocabulary(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20000 {
		cfg.AcceptedTokens = append(cfg.AcceptedTokens, fmt.Sprintf("term%d", i))
	}
	cfg.AcceptedTokens = append(cfg.AcceptedTokens, "Ceph")

	s := &Spelling{}
	if err = addExceptions(s, baseCheck{}, cfg); err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"Ceph", "term19999"} {
		if ok, merr := s.exceptRe.MatchString(word); merr != nil || !ok {
			t.Errorf("exceptRe.MatchString(%q) = %v, %v; want true, nil", word, ok, merr)
		}
	}
}
