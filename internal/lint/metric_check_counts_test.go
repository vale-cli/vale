package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// countAlerts returns how many of files' alerts match check.
func countAlerts(files []*core.File, check string) int {
	n := 0
	for _, f := range files {
		for _, a := range f.Alerts {
			if a.Check == check {
				n++
			}
		}
	}
	return n
}

// writeCollisionSourceStyles writes two styles whose check names would have
// collided under the old identifier-flattening design -- style "Foo-Bar"
// rule "Baz" (check "Foo-Bar.Baz") and style "Foo" rule "Bar-Baz" (check
// "Foo.Bar-Baz"), both of which used to sanitize to the same identifier,
// check_Foo_Bar_Baz -- into stylesDir. Shared with
// TestCheckObjectResolvesFormerlyCollidingCheckNamesIndependently in
// check_object_test.go (same package), which exercises the real check[...]
// indexing path against this exact pair to confirm the redesign resolves
// their counts independently now that there's no flattening step to collide
// on.
func writeCollisionSourceStyles(t *testing.T, stylesDir string) {
	t.Helper()

	fooBarDir := filepath.Join(stylesDir, "Foo-Bar")
	fooDir := filepath.Join(stylesDir, "Foo")
	if err := os.MkdirAll(fooBarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fooDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(fooBarDir, "Baz.yml"), []byte(
		"extends: existence\n"+
			"message: \"baz: '%s'\"\n"+
			"level: warning\n"+
			"scope: paragraph\n"+
			"tokens:\n"+
			"  - collidesA\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(fooDir, "Bar-Baz.yml"), []byte(
		"extends: existence\n"+
			"message: \"barbaz: '%s'\"\n"+
			"level: warning\n"+
			"scope: paragraph\n"+
			"tokens:\n"+
			"  - collidesB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestMetricFormulaSkipsWordlessDocumentWithoutError is the exact scenario
// that broke Vale's own shipped Readability style after the round-4 fix (in
// the now-deleted collision-detection machinery): a heading-and-code-fence-
// only document -- no prose "words" at all -- linted with a real,
// division-based readability formula (the shape of the bundled
// AutomatedReadability/LIX styles, referencing "characters", "words", and
// "sentences").
//
// The built-in readability values themselves mean nothing without real
// prose and stay absent from ComputeMetrics's params for such a document;
// evaluating the formula anyway would fail with a Tengo "unresolved
// reference" compile error instead of the graceful skip this rule has
// always had for such a document.
//
// This must lint clean, with the readability rule skipped (no alert, no
// error) -- exactly matching testdata/fixtures/styles/Readability/test2.md,
// the actual shipped fixture this regression was caught against in
// internal/e2e's TestScenarios/styles/readability.
func TestMetricFormulaSkipsWordlessDocumentWithoutError(t *testing.T) {
	dir := t.TempDir()
	styleDir := filepath.Join(dir, "styles", "Readability")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// The same shape as testdata/styles/Readability/AutomatedReadability.yml:
	// a division-based formula that would hit "unresolved reference" for any
	// operand ComputeMetrics leaves out of params.
	automatedReadability := "extends: metric\n" +
		"message: \"Try to keep the Automated Readability Index (%s) below 8.\"\n" +
		"formula: |\n" +
		"  (4.71 * (characters / words)) + (0.5 * (words / sentences)) - 21.43\n" +
		"condition: \"> 8\"\n"
	if err := os.WriteFile(filepath.Join(styleDir, "AutomatedReadability.yml"), []byte(automatedReadability), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(filepath.Join(dir, "styles"))
	cfg.Styles = []string{"Readability"}
	cfg.GBaseStyles = []string{"Readability"}
	cfg.MinAlertLevel = 0
	cfg.Flags.InExt = ".md"

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Heading + code fence only, no prose -- the exact shape of
	// testdata/fixtures/styles/Readability/test2.md.
	files, lintErr := linter.LintString("# A section with only code\n\n``` shell\nls\n```\n")
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v -- a wordless "+
			"document should skip a readability formula cleanly, not fail "+
			"it with an unresolved-reference compile error", lintErr)
	}

	if got := countAlerts(files, "Readability.AutomatedReadability"); got != 0 {
		t.Errorf("Readability.AutomatedReadability fired %d times, want 0 -- "+
			"it should be skipped entirely for a wordless document", got)
	}
}
