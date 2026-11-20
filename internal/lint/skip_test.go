package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/glob"
)

func TestRunsNothing(t *testing.T) {
	cases := []struct {
		desc    string
		base    []string
		checks  map[string]bool
		unset   map[string]bool
		global  map[string]bool
		nothing bool
	}{
		{desc: "a style", base: []string{"Vale"}},
		{desc: "a rule on in a section", checks: map[string]bool{"T.W": true}},
		{desc: "a rule on globally", global: map[string]bool{"T.W": true}},
		{desc: "nothing at all", nothing: true},
		{
			desc:    "a global rule the section switched off",
			checks:  map[string]bool{"T.W": false},
			global:  map[string]bool{"T.W": true},
			nothing: true,
		},
		{
			desc:    "a global rule the section unset",
			unset:   map[string]bool{"T.W": true},
			global:  map[string]bool{"T.W": true},
			nothing: true,
		},
	}
	for _, c := range cases {
		cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
		if err != nil {
			t.Fatal(err)
		}
		if c.global != nil {
			cfg.GChecks = c.global
		}
		l, lErr := NewLinter(cfg)
		if lErr != nil {
			t.Fatal(lErr)
		}

		f := &core.File{BaseStyles: c.base, Checks: c.checks, Unset: c.unset}
		if f.Checks == nil {
			f.Checks = map[string]bool{}
		}
		if got := l.runsNothing(f); got != c.nothing {
			t.Errorf("%s: runsNothing = %v, want %v", c.desc, got, c.nothing)
		}
	}
}

// TestSkippedFileIsNotParsed checks that a file no rule runs on is never
// parsed: parsing is what counts its metrics.
func TestSkippedFileIsNotParsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.md")
	if err := os.WriteFile(path, []byte("# Title\n\nA paragraph.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, skipped := range []bool{false, true} {
		cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
		if err != nil {
			t.Fatal(err)
		}
		cfg.GBaseStyles = []string{"Vale"}
		cfg.Flags.InExt = ".txt" // as ls-metrics sets it
		if skipped {
			pat, gErr := glob.Compile("*.md")
			if gErr != nil {
				t.Fatal(gErr)
			}
			cfg.SecToPat["*.md"] = pat
			cfg.RuleKeys = []string{"*.md"}
			cfg.SBaseStyles["*.md"] = []string{}
		}

		linter, err := NewLinter(cfg)
		if err != nil {
			t.Fatal(err)
		}
		files, err := linter.Lint([]string{path}, "*")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("linted %d files, want 1", len(files))
		}
		if parsed := len(files[0].Metrics) > 0; parsed == skipped {
			t.Errorf("skipped = %v, but parsed = %v", skipped, parsed)
		}
	}
}

// An ignored class covers the text and the alt text of everything inside its
// container, nested or not, and nothing after the container closes. Linting
// a file leaves the shared defaults as they were: they once grew by the
// config's classes on every file.
func TestIgnoredClassesPerFile(t *testing.T) {
	dir := t.TempDir()
	rule := "extends: existence\nmessage: \"'%s'\"\nlevel: error\ntokens:\n  - bad\n"
	if err := os.MkdirAll(filepath.Join(dir, "Test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Test", "Bad.yml"), []byte(rule), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := core.LoadConfigFromString("StylesPath = " + filepath.ToSlash(dir) +
		"\nIgnoredClasses = hidden, other\n\n[*]\nBasedOnStyles = Test\n")
	if err != nil {
		t.Fatal(err)
	}
	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	page := `<div class="hidden"><div><p>bad text</p><img alt="bad alt"></div></div>
<p>A bad line.</p>
<div class="x hidden"><p>more bad</p></div>
<p>One more bad line.</p>
`
	want := len(defaultSkipClasses)
	for range 3 {
		files, lerr := linter.LintStringAs(page, "page.html")
		if lerr != nil {
			t.Fatal(lerr)
		}
		var lines []int
		for _, f := range files {
			for _, a := range f.SortedAlerts() {
				lines = append(lines, a.Line)
			}
		}
		if fmt.Sprint(lines) != "[2 4]" {
			t.Fatalf("alerts on lines %v, want [2 4]", lines)
		}
	}
	if len(defaultSkipClasses) != want {
		t.Errorf("the default classes grew from %d to %d", want, len(defaultSkipClasses))
	}
}
