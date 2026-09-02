package check

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// TestSkippedByDefaultUnicodeWords checks that the default filters pass
// Unicode words to the spell checker while still skipping uppercase and
// non-word tokens.
func TestSkippedByDefaultUnicodeWords(t *testing.T) {
	cases := map[string]bool{
		"pozícióben":   false,
		"pozícióban":   false,
		"árvíztűrő":    false,
		"tükörfúrógép": false,
		"ÁRVÍZTŰRŐ":    true,
		"TÜKÖRFÚRÓGÉP": true,
		"célkitűzés":   false,
		"café":         false,
		"naïve":        false,
		"Straße":       false,
		"foo-bar":      true,
		"foo.bar":      true,
	}

	for word, want := range cases {
		if got := skippedByDefault(word); got != want {
			t.Errorf("skippedByDefault(%q) = %v, want %v", word, got, want)
		}
	}
}

// TestSpellFiltersMatchRegex checks the hand-written filters against the
// patterns they replace, over both fixed cases and random strings.
func TestSpellFiltersMatchRegex(t *testing.T) {
	cases := []string{
		"", "a", "A", "hello", "Hello", "HELLO", "helloW", "CamelCase",
		"camelCase", "XMLHttpRequest", "iOS", "IDs", "don't", "it's", "_foo",
		"foo_bar", "foo-bar", "foo.bar", "café", "naïve", "Straße", "42",
		"pozícióben", "pozícióban", "árvíztűrő", "tükörfúrógép",
		"ÁRVÍZTŰRŐ", "TÜKÖRFÚRÓGÉP", "célkitűzés",
		"a1b2", "ABCd", "aBC", "aBCd", "HTTPServer", "getHTTPResponse",
		"McDonald", "O'Brien", "e.g", "U.S.A", "ZZ", "aZ", "AaB", "AaBc",
	}
	// Coverage, not secrecy: these strings are fed to two implementations of
	// the same filter to check they agree, so a predictable generator is all
	// that is wanted -- and a fixed seed makes a failure reproducible.
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // not security-sensitive
	alphabet := []rune(" aAzZ_'0-.áéíóöőúüűÁÉÍÓÖŐÚÜŰß")
	for i := 0; i < 4000; i++ {
		n := 1 + rng.Intn(12)
		var b strings.Builder
		for j := 0; j < n; j++ {
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		cases = append(cases, b.String())
	}

	for _, w := range cases {
		for i, re := range defaultFilters {
			var got bool
			switch i {
			case 0:
				got = skipsCamel(w)
			case 1:
				got = skipsTrailingCaps(w)
			case 2:
				got = skipsNonWord(w)
			}
			if want := re.MatchString(w); got != want {
				t.Fatalf("filter %d (%s) on %q: got %v, want %v",
					i, re.String(), w, got, want)
			}
		}
	}
}

// TestSpellingRunChecksUnicodeWordsWithDefaultFilters verifies that a spelling
// rule without `custom: true` reports a misspelled Unicode word and accepts a
// correctly spelled one.
func TestSpellingRunChecksUnicodeWordsWithDefaultFilters(t *testing.T) {
	dir := t.TempDir()
	aff := filepath.Join(dir, "hu.aff")
	dic := filepath.Join(dir, "hu.dic")

	if err := os.WriteFile(aff, []byte("SET UTF-8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dic, []byte("1\npozícióban\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddStylesPath(dir)

	rule, err := NewSpelling(cfg, baseCheck{
		"name":    "Test.Spelling",
		"extends": "spelling",
		"message": "Did you really mean '%s'?",
		"level":   "error",
		"aff":     filepath.Base(aff),
		"dic":     filepath.Base(dic),
	}, filepath.Join(dir, "Spelling.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if rule.Custom || !rule.stdFilters {
		t.Fatal("test rule must exercise the default filters without custom: true")
	}

	alerts, err := rule.Run(nlp.NewBlock("", "pozícióben pozícióban", "text"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want 1: %#v", len(alerts), alerts)
	}
	if got := alerts[0].Match; got != "pozícióben" {
		t.Errorf("alert matched %q, want %q", got, "pozícióben")
	}
}
