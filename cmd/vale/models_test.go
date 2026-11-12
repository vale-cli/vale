package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/glob"
	"github.com/vale-cli/vale/v3/internal/lint/code"
)

func TestIsRelease(t *testing.T) {
	for version, want := range map[string]bool{
		"v3.2.3":                               true,
		"v3.3.0-rc.1":                          true,
		"v3.2.4-0.20261106120000-5e77b40ab1c2": false,
		"v3.3.0-rc.1.0.20261106120000-5e77b40ab1c2": false,
		"v2.0.0+incompatible":                       false,
		"":                                          false,
	} {
		if got := isRelease(version); got != want {
			t.Errorf("isRelease(%q) = %t, want %t", version, got, want)
		}
	}
}

// VALE_MODELS_URL names a mirror or a local directory; a directory may hold
// archives or unpacked packages.
func TestModelSource(t *testing.T) {
	t.Setenv(modelsURLVar, "https://example.com/models/")
	if got, err := modelSource("de"); err != nil || got != "https://example.com/models/de.zip" {
		t.Errorf("modelSource = %q, %v", got, err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fr"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(modelsURLVar, dir)
	if got, _ := modelSource("de"); got != filepath.Join(dir, "de.zip") {
		t.Errorf("an archive in a directory: got %q", got)
	}
	if got, _ := modelSource("fr"); got != filepath.Join(dir, "fr") {
		t.Errorf("an unpacked package in a directory: got %q", got)
	}
}

// A grammar package comes from VALE_GRAMMARS_URL when it is set, else from
// this Vale's own release, which a development build has none of.
func TestGrammarSource(t *testing.T) {
	t.Setenv(grammarsURLVar, "https://example.com/grammars")
	if got, err := grammarSource("grammar-go"); err != nil || got != "https://example.com/grammars/grammar-go.zip" {
		t.Errorf("grammarSource = %q, %v", got, err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "grammar-go"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(grammarsURLVar, dir)
	if got, _ := grammarSource("grammar-go"); got != filepath.Join(dir, "grammar-go") {
		t.Errorf("an unpacked package: got %q", got)
	}
	if got, _ := grammarSource("grammar-rust"); got != filepath.Join(dir, "grammar-rust.zip") {
		t.Errorf("an archive: got %q", got)
	}

	t.Setenv(grammarsURLVar, "")
	if _, err := grammarSource("grammar-go"); err == nil {
		t.Error("a development build has no release to fetch from")
	}
}

func TestGrammarsToSync(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if names, _ := grammarsToSync(cfg); len(names) != 0 {
		t.Errorf("no key: got %v, want no grammars", names)
	}
	cfg.Grammars = []string{"ALL"}
	if names, _ := grammarsToSync(cfg); len(names) != len(code.Names()) {
		t.Errorf("ALL: got %d grammars, want every one", len(names))
	}
	cfg.Grammars = []string{"Go", " python "}
	if names, _ := grammarsToSync(cfg); strings.Join(names, ",") != "go,python" {
		t.Errorf("named: got %v", names)
	}
	cfg.Grammars = []string{}
	if names, _ := grammarsToSync(cfg); len(names) != 0 {
		t.Errorf("NONE: got %v", names)
	}
	cfg.Grammars = []string{"cobol"}
	if _, bad := grammarsToSync(cfg); bad == nil || !strings.Contains(bad.Error(), `"cobol"`) {
		t.Errorf("unknown: %v", bad)
	}
}

func TestImpliedGrammars(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pat := range []string{"*", "*.md", "*.go", "*.{py,rs}", "src/**/*.ts", "*.golden", "!*.rb"} {
		g, gerr := glob.Compile(pat)
		if gerr != nil {
			t.Fatal(gerr)
		}
		cfg.SecToPat[pat] = g
	}

	// Without the key, the sections decide; `[*]` and the negated section
	// add nothing.
	if names, _ := grammarsToSync(cfg); strings.Join(names, ",") != "go,python,rust,typescript" {
		t.Errorf("implied: got %v", names)
	}

	// Each implied grammar can say which sections asked for it.
	if got := grammarDetail(cfg, "python"); got != "for [*.{py,rs}]" {
		t.Errorf("detail: got %q", got)
	}
	if got := grammarDetail(cfg, "bash"); got != "" {
		t.Errorf("detail for an unimplied grammar: got %q", got)
	}

	// With the key, the sections are not consulted.
	cfg.Grammars = []string{}
	if names, _ := grammarsToSync(cfg); len(names) != 0 {
		t.Errorf("NONE with sections: got %v", names)
	}
}
