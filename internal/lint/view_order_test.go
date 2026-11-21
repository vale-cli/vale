//go:build !vale_noviews

package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// When two sections match a file and both name a View, the later one wins,
// every time: it used to depend on the order a map happened to yield them in.
// See #1235.
func TestOverlappingSectionsPickLaterView(t *testing.T) {
	dir := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("styles/T/Bad.yml", "extends: existence\nmessage: \"'%s'\"\nlevel: error\ntokens:\n  - bad\n")
	write("styles/config/views/First.yml", "engine: dasel\nscopes:\n  - expr: first\n")
	write("styles/config/views/Second.yml", "engine: dasel\nscopes:\n  - expr: second\n")
	doc := "first: a bad start\nsecond: a bad end\n"

	cases := []struct {
		name     string
		sections string
		want     int // the line the alert is on
	}{
		{"the narrower section last", "[*.yml]\nBasedOnStyles = T\nView = First\n\n[*.special.yml]\nBasedOnStyles = T\nView = Second\n", 2},
		{"the wider section last", "[*.special.yml]\nBasedOnStyles = T\nView = Second\n\n[*.yml]\nBasedOnStyles = T\nView = First\n", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := core.LoadConfigFromString("StylesPath = " + filepath.ToSlash(filepath.Join(dir, "styles")) +
				"\nMinAlertLevel = suggestion\n\n" + c.sections)
			if err != nil {
				t.Fatal(err)
			}
			linter, err := NewLinter(cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Map order is random per iteration, so one run could pass by luck.
			for range 50 {
				files, lerr := linter.LintStringAs(doc, "test.special.yml")
				if lerr != nil {
					t.Fatal(lerr)
				}
				var lines []int
				for _, a := range files[0].Alerts {
					lines = append(lines, a.Line)
				}
				if len(lines) != 1 || lines[0] != c.want {
					t.Fatalf("alerts on lines %v, want only %d", lines, c.want)
				}
			}
		})
	}
}
