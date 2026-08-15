package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/errata-ai/vale/v3/internal/glob"
)

// TestNewFileSectionOrderWins verifies that when multiple config sections
// match the same file, NewFile resolves Lang and Transform deterministically
// by taking the LAST matching section in the order it was written -- the
// same "later one wins" semantics the rule/level resolution loop documents
// for #965 -- rather than depending on Go's randomized map iteration order.
func TestNewFileSectionOrderWins(t *testing.T) {
	sections := []struct {
		name string
		lang string
		xslt string
	}{
		{"*.md", "en", "one.xsl"},
		{"*.md", "fr", "two.xsl"},
		{"*.md", "de", "three.xsl"},
		{"*.md", "ja", "four.xsl"},
		{"*.md", "es", "five.xsl"},
		{"*.md", "it", "six.xsl"},
	}

	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	for i, s := range sections {
		// Reuse the same glob pattern across sections, but track each
		// occurrence under its own key so every one can carry its own
		// Lang/Transform value -- mirroring how repeated real config
		// sections (e.g. multiple `[*.md]` blocks) are keyed internally.
		key := s.name
		if i > 0 {
			key = s.name + string(rune('a'+i))
		}

		pat, err := glob.Compile(s.name)
		if err != nil {
			t.Fatal(err)
		}

		cfg.SecToPat[key] = pat
		cfg.RuleKeys = append(cfg.RuleKeys, key)
		cfg.FormatToLang[key] = s.lang
		cfg.Stylesheets[key] = s.xslt
		cfg.SChecks[key] = map[string]bool{}
		cfg.SLevels[key] = map[string]string{}
	}

	wantLang := sections[len(sections)-1].lang
	wantTransform := sections[len(sections)-1].xslt

	docPath := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(docPath, []byte("# Title\n"), 0600); err != nil {
		t.Fatal(err)
	}

	const iterations = 50
	for i := 0; i < iterations; i++ {
		f, err := NewFile(docPath, cfg)
		if err != nil {
			t.Fatal(err)
		}

		if f.NLP.Lang != wantLang {
			t.Fatalf("iteration %d: expected Lang %q (the last written matching section), got %q",
				i, wantLang, f.NLP.Lang)
		}

		if f.Transform != wantTransform {
			t.Fatalf("iteration %d: expected Transform %q (the last written matching section), got %q",
				i, wantTransform, f.Transform)
		}
	}
}

// TestNewFileGlobalLangFallback verifies that the `[*] Lang = ...` global
// default still applies when no format-specific section matches the file.
func TestNewFileGlobalLangFallback(t *testing.T) {
	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	cfg.FormatToLang["*"] = "ja"

	docPath := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(docPath, []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}

	f, err := NewFile(docPath, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if f.NLP.Lang != "ja" {
		t.Fatalf("expected global Lang fallback %q, got %q", "ja", f.NLP.Lang)
	}
}
