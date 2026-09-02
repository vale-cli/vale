package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/glob"
)

// buildCheckObjectLinter is the check["Style.Rule"]-syntax analog of
// compositeMetricLinter (metric_check_counts_test.go): a self-contained,
// temp-dir style with two independent `existence` rules (Composite.RuleA on
// "wordA", Composite.RuleB on "wordB") plus a `metric` rule ("Combined")
// whose formula and condition are supplied by the caller, so each test below
// can target a different check["..."] scenario without re-deriving the
// fixture setup. See buildCheckObjectLinter's caller comments for why this
// has to be Markdown, not plain text (a `metric` rule forces `scope:
// summary`, only ever reached from the Markdown/HTML AST walk).
func buildCheckObjectLinter(t *testing.T, formula, condition string) *Linter {
	t.Helper()

	dir := t.TempDir()
	styleDir := filepath.Join(dir, "styles", "Composite")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rules := map[string]string{
		"RuleA.yml": "extends: existence\n" +
			"message: \"ruleA: '%s'\"\n" +
			"level: warning\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordA\n",
		"RuleB.yml": "extends: existence\n" +
			"message: \"ruleB: '%s'\"\n" +
			"level: warning\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordB\n",
		"Combined.yml": "extends: metric\n" +
			"message: \"combined score: %s\"\n" +
			"level: error\n" +
			"formula: " + formula + "\n" +
			"condition: \"" + condition + "\"\n",
	}

	for name, content := range rules {
		if err := os.WriteFile(filepath.Join(styleDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(filepath.Join(dir, "styles"))
	cfg.Styles = []string{"Composite"}
	cfg.GBaseStyles = []string{"Composite"}
	cfg.MinAlertLevel = 0
	cfg.Flags.InExt = ".md"

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	return linter
}

// alertMessage returns the message of files' first alert matching check, or
// "" if none matched.
func alertMessage(files []*core.File, check string) string {
	for _, f := range files {
		for _, a := range f.Alerts {
			if a.Check == check {
				return a.Message
			}
		}
	}
	return ""
}

// TestCheckObjectCombinesTwoChecks is the check["Style.Rule"]-syntax version
// of TestMetricFormulaCombinesCheckCounts (issue #1163's original motivating
// case): a `metric` rule combining two other checks' alert counts in a
// single formula. Under the old design this same case required flattening
// both check names into check_Composite_RuleA / check_Composite_RuleB
// identifiers; here they're indexed by their real names directly.
//
// Today, "check" is not a defined Tengo identifier at all -- ComputeMetrics
// never adds one -- so check["Composite.RuleA"] fails to even compile,
// which LintString surfaces as a non-nil error; every case below currently
// gets that error rather than the pass/fail result asserted here, which is
// the correct RED state.
func TestCheckObjectCombinesTwoChecks(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantA     int
		wantB     int
		wantFired bool
	}{
		{
			name:      "under threshold",
			text:      "wordA wordA wordB stays quiet in this paragraph of prose.", //nolint:dupword // intentional repeat
			wantA:     2,
			wantB:     1,
			wantFired: false,
		},
		{
			name:      "over threshold",
			text:      "wordA wordA wordA wordB crosses the line in this paragraph.", //nolint:dupword // intentional repeat
			wantA:     3,
			wantB:     1,
			wantFired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linter := buildCheckObjectLinter(t,
				`check["Composite.RuleA"] + check["Composite.RuleB"]`, "> 3")

			files, lintErr := linter.LintString(tt.text)

			if got := countAlerts(files, "Composite.RuleA"); got != tt.wantA {
				t.Errorf("Composite.RuleA fired %d times, want %d", got, tt.wantA)
			}
			if got := countAlerts(files, "Composite.RuleB"); got != tt.wantB {
				t.Errorf("Composite.RuleB fired %d times, want %d", got, tt.wantB)
			}

			if lintErr != nil {
				t.Errorf("LintString returned an unexpected error: %v", lintErr)
			}

			fired := countAlerts(files, "Composite.Combined") > 0
			if fired != tt.wantFired {
				t.Errorf("Composite.Combined fired = %v, want %v (lint error: %v)",
					fired, tt.wantFired, lintErr)
			}
		})
	}
}

// TestCheckObjectNeverFiredCheckReadsAsZero verifies that indexing a check
// that IS loaded, but never fired on this document, reads as 0 -- not a
// compile failure, and not silently indistinguishable from a typo (see
// TestCheckObjectUnknownCheckNameSurfacesError below for that distinction).
// wordB never appears, so Composite.RuleB never fires and has no f.Metrics
// entry for it at all.
func TestCheckObjectNeverFiredCheckReadsAsZero(t *testing.T) {
	linter := buildCheckObjectLinter(t, `check["Composite.RuleB"]`, "> -1")

	files, lintErr := linter.LintString("wordA appears here in this paragraph of prose.")
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v -- a loaded, "+
			"never-fired check must read as 0, not fail to resolve", lintErr)
	}

	if got := countAlerts(files, "Composite.RuleB"); got != 0 {
		t.Fatalf("Composite.RuleB fired %d times, want 0 -- this test needs "+
			"RuleB to genuinely never fire", got)
	}

	if got := countAlerts(files, "Composite.Combined"); got != 1 {
		t.Errorf("Composite.Combined fired %d times, want 1 (0 > -1, treating "+
			"the never-fired check as 0)", got)
	}
}

// TestCheckObjectFiredCheckReturnsRealCount verifies the count read back is
// the check's genuine alert count, not just a truthy placeholder -- asserted
// against the alert's own message, which embeds the formula's numeric
// result (see formatMessages / "%.2f" in Metric.Run), so this fails if the
// object ever returned, say, 1 for "fired" instead of the real count.
func TestCheckObjectFiredCheckReturnsRealCount(t *testing.T) {
	linter := buildCheckObjectLinter(t, `check["Composite.RuleA"]`, "> 0")

	files, lintErr := linter.LintString(
		"wordA wordA wordA appears three times in this paragraph of prose.") //nolint:dupword // intentional repeat
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v", lintErr)
	}

	if got := countAlerts(files, "Composite.RuleA"); got != 3 {
		t.Fatalf("Composite.RuleA fired %d times, want 3", got)
	}

	msg := alertMessage(files, "Composite.Combined")
	if !strings.Contains(msg, "3.00") {
		t.Errorf("expected Composite.Combined's message to embed the real "+
			"count 3.00, got %q", msg)
	}
}

// TestCheckObjectUnknownCheckNameSurfacesError is the concrete fix for the
// design review's most-likely-real-mistake finding: a misspelled check/rule
// name in a formula must surface a real, actionable error -- naming the bad
// check -- rather than silently evaluating to 0 the way the old
// checkCounterRE shape-match (`^check_\w+$`, matched regardless of whether
// the name corresponded to any real, loaded check) did.
//
// Composite.NoSuchRule is never defined anywhere in this fixture's style, so
// this is a genuine, unresolvable typo, not a never-fired-but-real check
// (see TestCheckObjectNeverFiredCheckReadsAsZero for that case).
func TestCheckObjectUnknownCheckNameSurfacesError(t *testing.T) {
	linter := buildCheckObjectLinter(t, `check["Composite.NoSuchRule"]`, "> -1")

	files, lintErr := linter.LintString("wordA appears here in this paragraph of prose.")

	if lintErr == nil {
		t.Fatal("expected LintString to return an error for the unknown " +
			"check Composite.NoSuchRule, got nil")
	}
	if !strings.Contains(lintErr.Error(), "Composite.NoSuchRule") {
		t.Errorf("expected the error to name the unknown check "+
			"Composite.NoSuchRule, got: %v", lintErr)
	}
	// Today, "check" isn't a defined Tengo identifier at all, so *every*
	// check["..."] formula -- known-name or not -- fails to even compile,
	// with a generic "unresolved reference 'check'" error. That message
	// happens to echo the raw formula source (including the literal text
	// "Composite.NoSuchRule") as annotated context, which would let the
	// assertion above pass for the wrong reason: not because the unknown
	// check was actually detected, but because the whole source line is
	// quoted verbatim regardless of which check name appears in it. This
	// requires the error to NOT be that generic compile-time failure, so the
	// test still fails today for the right reason, and will only pass once
	// IndexGet genuinely distinguishes an unknown check name at runtime.
	if strings.Contains(lintErr.Error(), "unresolved reference") {
		t.Errorf("expected a real runtime error identifying the unknown "+
			"check, not Tengo's generic compile-time \"unresolved "+
			"reference\" (which today just means \"check\" isn't a defined "+
			"identifier at all, not that this specific check name was "+
			"looked up and found missing), got: %v", lintErr)
	}

	if got := countAlerts(files, "Composite.Combined"); got != 0 {
		t.Errorf("Composite.Combined fired %d times, want 0 -- it should "+
			"error, not evaluate the unknown check as 0", got)
	}
}

// TestCheckObjectHandlesHyphenatedStyleName regression-tests a style
// directory name containing "-", like Vale's own bundled `write-good` style
// (see README.md, cmd/vale/pkg_test.go) -- a mainstream, first-class case,
// not a contrived one.
//
// Under the old identifier-flattening design this needed its own dedicated
// fix (see TestMetricFormulaHandlesHyphenatedStyleName in
// metric_check_counts_test.go): "write-good.TooWordy" sanitized to
// "check_write-good_TooWordy", which Tengo parsed as subtraction of two
// undefined identifiers and failed to compile. Indexing by the real,
// unflattened name sidesteps that class of bug entirely -- there's no
// sanitization step left to fail on the hyphen -- so this should be a clean
// pass with no special-casing needed.
func TestCheckObjectHandlesHyphenatedStyleName(t *testing.T) {
	dir := t.TempDir()
	styleDir := filepath.Join(dir, "styles", "write-good")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rules := map[string]string{
		"TooWordy.yml": "extends: existence\n" +
			"message: \"wordy: '%s'\"\n" +
			"level: warning\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordy\n",
		"Combined.yml": "extends: metric\n" +
			"message: \"combined score: %s\"\n" +
			"level: error\n" +
			`formula: check["write-good.TooWordy"]` + "\n" +
			"condition: \"> 0\"\n",
	}
	for name, content := range rules {
		if err := os.WriteFile(filepath.Join(styleDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(filepath.Join(dir, "styles"))
	cfg.Styles = []string{"write-good"}
	cfg.GBaseStyles = []string{"write-good"}
	cfg.MinAlertLevel = 0
	cfg.Flags.InExt = ".md"

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	files, lintErr := linter.LintString("wordy prose fills this paragraph.")
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v", lintErr)
	}

	if got := countAlerts(files, "write-good.TooWordy"); got != 1 {
		t.Errorf("write-good.TooWordy fired %d times, want 1 -- if this is 0, "+
			"the fixture's rule isn't loading at all", got)
	}
	if got := countAlerts(files, "write-good.Combined"); got != 1 {
		t.Errorf("write-good.Combined fired %d times, want 1", got)
	}
}

// TestCheckObjectResolvesFormerlyCollidingCheckNamesIndependently is the
// redesign's central selling point, exercised through the real check[...]
// indexing path rather than through the absence of the old collisions map:
// "Foo-Bar.Baz" and "Foo.Bar-Baz" are the exact pair that used to sanitize
// to the same identifier, check_Foo_Bar_Baz (see writeCollisionSourceStyles
// and TestMetricFormulaFailsWhenReferencingCollision in
// metric_check_counts_test.go, which document and pin the OLD failure mode
// for that pair). Under the new design there's no flattening step left to
// collide on -- each name indexes the check object directly -- so both
// counts must come back independent and correct side by side in the same
// formula.
//
// collidesA fires twice and collidesB fires once, deliberately distinct
// counts: if the two were ever merged or one silently overwrote the other
// (the exact old failure mode), the combined formula would read 2 or 1
// rather than the genuine 3, and both the ">2" condition's outcome and the
// message's embedded total would give it away. This reuses
// writeCollisionSourceStyles from metric_check_counts_test.go (same
// package) so the fixture is the identical colliding-name pair the old
// mechanism's tests exercised, not a fresh, easier-to-satisfy example.
func TestCheckObjectResolvesFormerlyCollidingCheckNamesIndependently(t *testing.T) {
	dir := t.TempDir()
	stylesDir := filepath.Join(dir, "styles")
	writeCollisionSourceStyles(t, stylesDir)

	plainDir := filepath.Join(stylesDir, "Plain")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}

	combined := "extends: metric\n" +
		"message: \"combined score: %s\"\n" +
		"level: error\n" +
		`formula: check["Foo-Bar.Baz"] + check["Foo.Bar-Baz"]` + "\n" +
		"condition: \"> 2\"\n"
	if err := os.WriteFile(filepath.Join(plainDir, "Combined.yml"), []byte(combined), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(stylesDir)
	cfg.Styles = []string{"Foo-Bar", "Foo", "Plain"}
	cfg.GBaseStyles = []string{"Foo-Bar", "Foo", "Plain"}
	cfg.MinAlertLevel = 0
	cfg.Flags.InExt = ".md"

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	files, lintErr := linter.LintString(
		"collidesA collidesA collidesB appear in this single paragraph together.") //nolint:dupword // intentional repeat
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v", lintErr)
	}

	if got := countAlerts(files, "Foo-Bar.Baz"); got != 2 {
		t.Fatalf("Foo-Bar.Baz fired %d times, want 2", got)
	}
	if got := countAlerts(files, "Foo.Bar-Baz"); got != 1 {
		t.Fatalf("Foo.Bar-Baz fired %d times, want 1", got)
	}

	if got := countAlerts(files, "Plain.Combined"); got != 1 {
		t.Errorf("Plain.Combined fired %d times, want 1 (2 + 1 = 3 > 2) -- if "+
			"this is 0, the two formerly-colliding check names aren't "+
			"resolving to their own independent counts", got)
	}

	msg := alertMessage(files, "Plain.Combined")
	if !strings.Contains(msg, "3.00") {
		t.Errorf("expected Plain.Combined's message to embed the genuine "+
			"combined count 3.00 (2 + 1, each check's own independent count), "+
			"got %q -- a mixed or overwritten value would read 2.00 or 1.00 "+
			"instead", msg)
	}
}

// TestCheckObjectDisabledForThisExtensionSurfacesError is the regression
// case for LoadedChecks needing to be scoped per file, not to the whole
// merged config: l.Manager.Rules() covers every rule loaded from every
// style, regardless of section or extension, but a real, already-supported
// Vale feature -- per-extension check toggling, e.g. `Composite.RuleB = NO`
// under a `[*.mdx]` section -- can turn a specific check off for a specific
// file. Composite.RuleB is loaded (it's compiled into the style once, not
// per file) but disabled for *.mdx here, so it can never fire against
// doc.mdx at all; a formula on doc.mdx referencing check["Composite.RuleB"]
// is asking about something that structurally cannot happen on this file,
// and must get the same real error an outright-nonexistent check name
// would, not a silent 0 -- indistinguishable from "loaded here, just never
// fired". The identical formula on doc.md, where RuleB is NOT disabled,
// must behave normally (0 if never fired, the real count if it did).
//
// Both fixtures are Markdown-family formats (.md and .mdx), not .md vs
// .txt: a `metric` rule forces `scope: summary`, only ever reached from the
// Markdown/HTML AST walk (see buildCheckObjectLinter above), so a .txt
// fixture would never even run Composite.UsesB, disabled check or not, and
// the test would pass for the wrong reason.
//
// This drives the scoping through cfg.SChecks/SecToPat directly -- the same
// fields a real `.vale.ini`'s `[*.mdx]` section populates via ini.go's
// processConfig -- rather than parsing an actual .vale.ini file, matching
// the lightweight, direct-field-assignment style the rest of this file's
// fixtures already use.
func TestCheckObjectDisabledForThisExtensionSurfacesError(t *testing.T) {
	dir := t.TempDir()
	styleDir := filepath.Join(dir, "styles", "Composite")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rules := map[string]string{
		"RuleA.yml": "extends: existence\n" +
			"message: \"ruleA: '%s'\"\n" +
			"level: warning\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordA\n",
		"RuleB.yml": "extends: existence\n" +
			"message: \"ruleB: '%s'\"\n" +
			"level: warning\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordB\n",
		"UsesB.yml": "extends: metric\n" +
			"message: \"uses B: %s\"\n" +
			"level: error\n" +
			`formula: check["Composite.RuleB"]` + "\n" +
			"condition: \"> -1\"\n",
	}
	for name, content := range rules {
		if err := os.WriteFile(filepath.Join(styleDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(filepath.Join(dir, "styles"))
	cfg.Styles = []string{"Composite"}
	cfg.GBaseStyles = []string{"Composite"}
	cfg.MinAlertLevel = 0
	// NewFile only infers a real file's format from its own extension when
	// Flags.InExt is exactly ".txt" (see NewFile) -- this test lints doc.md
	// and doc.mdx in the same run, so each needs its own real extension
	// detected rather than a single overriding one.
	cfg.Flags.InExt = ".txt"

	// Composite.RuleB = NO under [*.mdx]: the same effect a real .vale.ini
	// section has, applied directly to the config fields ini.go's
	// processConfig would otherwise populate from it.
	mdxPat, err := glob.Compile("*.mdx")
	if err != nil {
		t.Fatal(err)
	}
	cfg.SecToPat["*.mdx"] = mdxPat
	cfg.RuleKeys = append(cfg.RuleKeys, "*.mdx")
	cfg.SChecks["*.mdx"] = map[string]bool{"Composite.RuleB": false}

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	text := "wordB appears once in this paragraph of prose.\n"
	mdPath := filepath.Join(dir, "doc.md")
	if err = os.WriteFile(mdPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	mdxPath := filepath.Join(dir, "doc.mdx")
	if err = os.WriteFile(mdxPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	mdFiles, mdErr := linter.Lint([]string{mdPath}, "*")
	if mdErr != nil {
		t.Fatalf("Lint returned an unexpected error for doc.md, where "+
			"Composite.RuleB is enabled: %v", mdErr)
	}
	if got := countAlerts(mdFiles, "Composite.RuleB"); got != 1 {
		t.Fatalf("Composite.RuleB fired %d times on doc.md, want 1 -- this "+
			"test needs RuleB to genuinely fire there", got)
	}
	if msg := alertMessage(mdFiles, "Composite.UsesB"); !strings.Contains(msg, "1.00") {
		t.Errorf("expected Composite.UsesB's message on doc.md, where "+
			"Composite.RuleB is enabled and fired once, to embed the real "+
			"count 1.00, got %q", msg)
	}

	mdxFiles, mdxErr := linter.Lint([]string{mdxPath}, "*")
	if mdxErr == nil {
		t.Fatal("expected linting doc.mdx to fail: Composite.RuleB is " +
			"disabled for *.mdx in this config, so it cannot fire on this " +
			"file at all, and Composite.UsesB references it -- reading that " +
			"as a silent 0 is exactly the failure mode this redesign exists " +
			"to prevent, just scoped to a single file rather than the whole " +
			"check name")
	}
	if !strings.Contains(mdxErr.Error(), "Composite.RuleB") {
		t.Errorf("expected the error to name Composite.RuleB, got: %v", mdxErr)
	}
	if got := countAlerts(mdxFiles, "Composite.UsesB"); got != 0 {
		t.Errorf("Composite.UsesB fired %d times on doc.mdx, want 0 -- it "+
			"should error, not evaluate the disabled check as 0", got)
	}
}

// TestCheckObjectBelowMinAlertLevelReadsAsZero is the regression case for
// checkApplies needing to exclude MinAlertLevel, not just reuse shouldRun
// wholesale: shouldRun answers two different questions with one bool --
// "can this check structurally apply to this file" (extension/section/
// base-style, what LoadedChecks needs) AND "is this check's severity at or
// above --minAlertLevel" (a display filter on which alerts get shown, see
// MinAlertLevel's doc comment in config.go, not a fact about whether the
// check can run at all). Composite.RuleB here is fully loaded and enabled
// for this file -- nothing disables it structurally -- it's just a
// `suggestion`-level check in a run whose MinAlertLevel is `warning`. Under
// the bug, that made it structurally indistinguishable from an outright
// nonexistent check: check["Composite.RuleB"] would hard-error instead of
// correctly reading 0 (RuleB is filtered out of the run entirely, so it can
// never produce a real count either way -- shouldRun gates it out before
// chk.Run ever executes -- but "never fired because filtered by level"
// still needs to read as a known check's honest 0, not an unknown-check
// error).
//
// wordB does appear in the text, but must not make RuleB actually report:
// if it did, this test would still pass even with the bug (a real,
// nonzero count also isn't the "not a known check" error), silently
// testing nothing about the MinAlertLevel exclusion specifically.
func TestCheckObjectBelowMinAlertLevelReadsAsZero(t *testing.T) {
	dir := t.TempDir()
	styleDir := filepath.Join(dir, "styles", "Composite")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rules := map[string]string{
		// suggestion is below the run's warning MinAlertLevel set below, so
		// this check is filtered out of every lint run entirely -- but it
		// is NOT disabled by any style/extension override, and IS in
		// f.BaseStyles: structurally, it's a real, applicable check.
		"RuleB.yml": "extends: existence\n" +
			"message: \"ruleB: '%s'\"\n" +
			"level: suggestion\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordB\n",
		"UsesB.yml": "extends: metric\n" +
			"message: \"uses B: %s\"\n" +
			"level: error\n" +
			`formula: check["Composite.RuleB"]` + "\n" +
			"condition: \"> -1\"\n",
	}
	for name, content := range rules {
		if err := os.WriteFile(filepath.Join(styleDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(filepath.Join(dir, "styles"))
	cfg.Styles = []string{"Composite"}
	cfg.GBaseStyles = []string{"Composite"}
	cfg.Flags.InExt = ".md"

	// The `.vale.ini` path: MinAlertLevel = warning. ini.go's own
	// "MinAlertLevel" handler does nothing more than this same assignment
	// (see coreOpts["MinAlertLevel"] in ini.go), so setting the field
	// directly is equivalent without needing a real ini file round-trip.
	cfg.MinAlertLevel = core.LevelToInt["warning"]

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	files, lintErr := linter.LintString("wordB appears here in this paragraph of prose.")
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v -- "+
			"Composite.RuleB is structurally applicable here, just below "+
			"MinAlertLevel, so check[\"Composite.RuleB\"] must read 0, not "+
			"error", lintErr)
	}

	if got := countAlerts(files, "Composite.RuleB"); got != 0 {
		t.Fatalf("Composite.RuleB fired %d times, want 0 -- this test needs "+
			"MinAlertLevel to genuinely keep it from ever running, or the "+
			"real-vs-error distinction this test targets isn't exercised",
			got)
	}
	if got := countAlerts(files, "Composite.UsesB"); got != 1 {
		t.Errorf("Composite.UsesB fired %d times, want 1 (0 > -1, treating "+
			"the below-MinAlertLevel check as 0)", got)
	}
}

// TestCheckObjectBelowMinAlertLevelViaCLIFlagReadsAsZero is
// TestCheckObjectBelowMinAlertLevelReadsAsZero, but driven through the
// `--minAlertLevel` CLI flag's translation into cfg.MinAlertLevel instead
// of `.vale.ini`'s MinAlertLevel key -- the other documented way to set the
// same field (see cmd/vale/flag.go's help text and internal/core/source.go,
// which applies it as `cfg.MinAlertLevel = LevelToInt[cfg.Flags.AlertLevel]`
// once cfg.Flags.AlertLevel is a recognized level). That one-line
// translation is applied directly here, matching source.go's own logic,
// rather than routing through a full ReadPipeline + real .vale.ini file:
// both paths converge on the identical cfg.MinAlertLevel field this test
// (like the one above) actually exercises against checkApplies, and the
// CLI-flag-to-field translation itself is pre-existing, untouched by this
// change, and orthogonal to what's being regression-tested here.
func TestCheckObjectBelowMinAlertLevelViaCLIFlagReadsAsZero(t *testing.T) {
	dir := t.TempDir()
	styleDir := filepath.Join(dir, "styles", "Composite")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rules := map[string]string{
		"RuleB.yml": "extends: existence\n" +
			"message: \"ruleB: '%s'\"\n" +
			"level: suggestion\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordB\n",
		"UsesB.yml": "extends: metric\n" +
			"message: \"uses B: %s\"\n" +
			"level: error\n" +
			`formula: check["Composite.RuleB"]` + "\n" +
			"condition: \"> -1\"\n",
	}
	for name, content := range rules {
		if err := os.WriteFile(filepath.Join(styleDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true, AlertLevel: "warning"})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(filepath.Join(dir, "styles"))
	cfg.Styles = []string{"Composite"}
	cfg.GBaseStyles = []string{"Composite"}
	cfg.Flags.InExt = ".md"

	// The `--minAlertLevel` CLI flag path: internal/core/source.go applies
	// exactly this once cfg.Flags.AlertLevel is a recognized level.
	if core.StringInSlice(cfg.Flags.AlertLevel, core.AlertLevels) {
		cfg.MinAlertLevel = core.LevelToInt[cfg.Flags.AlertLevel]
	}

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	files, lintErr := linter.LintString("wordB appears here in this paragraph of prose.")
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v -- "+
			"Composite.RuleB is structurally applicable here, just below "+
			"the --minAlertLevel-derived filter, so "+
			"check[\"Composite.RuleB\"] must read 0, not error", lintErr)
	}

	if got := countAlerts(files, "Composite.RuleB"); got != 0 {
		t.Fatalf("Composite.RuleB fired %d times, want 0 -- this test needs "+
			"MinAlertLevel to genuinely keep it from ever running", got)
	}
	if got := countAlerts(files, "Composite.UsesB"); got != 1 {
		t.Errorf("Composite.UsesB fired %d times, want 1 (0 > -1, treating "+
			"the below-MinAlertLevel check as 0)", got)
	}
}

// TestCheckObjectCraftedTagCannotForgeACount is the end-to-end regression
// test for a real integrity bug found in review: before per-check counts
// moved to their own field (core.File.checkCounts, decoupled from
// f.Metrics), a document could forge a check's count. ast.go also writes
// f.Metrics from raw document content -- an HTML/XML tag's own name, via
// f.Metrics[txt]++, for a tag treated as a skippable block (its content
// never linted, so nothing in it can trip any real check) -- so a tag
// literally named after a check, e.g. "check.demo.rule", combined with a
// skip class, landed in f.Metrics indistinguishable, by prefix alone, from
// a genuine "check."-namespaced counter AddAlert would have written.
// Confirmed directly (see the investigation that produced this test):
// `.html`-format documents reach ast.go's tag tokenizer directly
// (golang.org/x/net/html permits "." in a tag name), and the crafted tag
// below incremented the old shared f.Metrics key to 1 with demo.rule's own
// token never appearing anywhere -- a real false-positive injection, not
// just a theoretical one.
//
// The style/rule names here are deliberately all-lowercase ("demo"/"rule",
// not "Composite"/"RuleB" like this file's other fixtures): the HTML
// tokenizer folds a tag name to lowercase (confirmed directly -- a
// "<check.Composite.RuleB>" tag tokenizes as "check.composite.ruleb"), so
// this specific vector only lines up with a check name that's already
// lowercase, or with an attacker who names their own style/rule in
// lowercase specifically to exploit it. That's a real, exploitable subset
// of check names (nothing stops a style or rule file from being named in
// lowercase), not a hypothetical case picked to make this test pass; it
// just means a test using capitalized names like "Composite.RuleB" would
// pass even under the vulnerable code, for the wrong reason (case mismatch,
// not the fix), which is why this test doesn't reuse the mixed-case
// fixtures the rest of this file does.
//
// (Markdown specifically was not exploitable this way even before this
// fix, incidentally: CommonMark's raw-HTML-block tag grammar excludes ".",
// so goldmark never recognizes such a tag as an HTML block to begin with --
// that protection is a property of the Markdown grammar, not of Vale's own
// design, so it doesn't extend to .html or any other format whose
// converter is more permissive.)
//
// wordB never appears in the text below, so demo.rule's own token never
// matches and it does not actually fire; the crafted tag is the only
// possible source of a nonzero count. With the fix, check["demo.rule"] must
// read 0 regardless.
func TestCheckObjectCraftedTagCannotForgeACount(t *testing.T) {
	dir := t.TempDir()
	styleDir := filepath.Join(dir, "styles", "demo")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rules := map[string]string{
		"rule.yml": "extends: existence\n" +
			"message: \"rule: '%s'\"\n" +
			"level: warning\n" +
			"scope: paragraph\n" +
			"tokens:\n" +
			"  - wordB\n",
		"UsesIt.yml": "extends: metric\n" +
			"message: \"uses it: %s\"\n" +
			"level: error\n" +
			`formula: check["demo.rule"]` + "\n" +
			"condition: \"> 0\"\n",
	}
	for name, content := range rules {
		if err := os.WriteFile(filepath.Join(styleDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath(filepath.Join(dir, "styles"))
	cfg.Styles = []string{"demo"}
	cfg.GBaseStyles = []string{"demo"}
	cfg.MinAlertLevel = 0
	cfg.Flags.InExt = ".html"
	// The exact config path the investigation reproduced this under: a
	// user-configured IgnoredClasses skip class (distinct from the
	// built-in default skipClasses, which -- confirmed separately -- the
	// same crafted tag also reaches).
	cfg.IgnoredClasses = []string{"ignore"}

	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	doc := "<html><body><p>Some real prose here.</p>\n" +
		`<check.demo.rule class="ignore">forged</check.demo.rule>` + "\n" +
		"</body></html>\n"

	files, lintErr := linter.LintString(doc)
	if lintErr != nil {
		t.Fatalf("LintString returned an unexpected error: %v", lintErr)
	}

	if got := countAlerts(files, "demo.rule"); got != 0 {
		t.Fatalf("demo.rule fired %d times, want 0 -- this test needs "+
			"demo.rule to genuinely never fire, or the crafted-tag-vs-real-"+
			"alert distinction this test targets isn't exercised", got)
	}
	if got := countAlerts(files, "demo.UsesIt"); got != 0 {
		t.Errorf("demo.UsesIt fired %d times, want 0 -- the crafted tag "+
			"must not forge a nonzero count for demo.rule", got)
	}
}
