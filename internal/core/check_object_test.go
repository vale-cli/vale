package core

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

// TestComputeMetricsExcludesCheckCountsFromGenericSanitization pins the
// target design from the metric-check-counts redesign: a per-check alert
// count (f.checkCounts, written by AddAlert -- see its own doc comment for
// why this is a dedicated field rather than a "check."-prefixed f.Metrics
// entry) must never be sanitized into a "check_Style_Rule" Tengo identifier
// and handed out through the generic structural-metrics params map at all.
// It's exposed instead through the second, raw-counts return value, keyed by
// the check's real, unflattened name -- see internal/check/metric.go's
// checkCounts, which wraps it in a separate indexable Tengo object under
// "check".
func TestComputeMetricsExcludesCheckCountsFromGenericSanitization(t *testing.T) {
	f := &File{
		ChkToCtx:    map[string]string{},
		history:     map[string]int{},
		limits:      map[string]int{},
		Metrics:     map[string]int{},
		checkCounts: map[string]int{"Demo.RuleA": 3},
	}
	f.Summary.WriteString("Some real prose so the readability builtins are computed too.")

	params, checkCounts, _, err := f.ComputeMetrics()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if _, ok := params["check_Demo_RuleA"]; ok {
		t.Errorf(`expected a check count to never surface in the generic `+
			"sanitize-into-Tengo-identifier path at all (it's exposed "+
			"through the new check[...] object instead), got params = %v",
			params)
	}

	if got, want := checkCounts["Demo.RuleA"], 3; got != want {
		t.Errorf("expected checkCounts[%q] = %d (the raw, unflattened name "+
			"and count), got %d", "Demo.RuleA", want, got)
	}
}

// TestComputeMetricsIgnoresCraftedMetricsKeyShapedLikeACheckCount is the
// regression test for a real integrity bug found in review: f.Metrics is
// also written by ast.go from raw document content (HTML/XML tag names,
// ...), so before per-check counts moved to their own field, a document
// containing a crafted tag literally named e.g. "check.Demo.Forged" --
// paired with a skip class, default or configured via IgnoredClasses --
// landed in f.Metrics indistinguishable, by prefix alone, from a genuine
// counter AddAlert would have written. Confirmed directly against a real
// lint run (not just reasoning about the code): such a tag incremented
// f.Metrics["check.Demo.Forged"] to 1 even though Demo.Forged never
// actually fired, and a `metric` formula referencing check["Demo.Forged"]
// read the forged count as real.
//
// This simulates the injected key directly, the shape ast.go's
// f.Metrics[txt]++ would produce for it, and confirms it's now completely
// inert: AddAlert is the only writer of f.checkCounts, so a "check."-shaped
// f.Metrics key, however it got there, is never read as a check count at
// all -- there's no shared keyspace left for it to collide with.
func TestComputeMetricsIgnoresCraftedMetricsKeyShapedLikeACheckCount(t *testing.T) {
	f := &File{
		ChkToCtx: map[string]string{},
		history:  map[string]int{},
		limits:   map[string]int{},
		Metrics:  map[string]int{"check.Demo.Forged": 1},
	}
	f.Summary.WriteString("Some real prose so the readability builtins are computed too.")

	_, checkCounts, _, err := f.ComputeMetrics()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if got, ok := checkCounts["Demo.Forged"]; ok {
		t.Errorf("expected a crafted \"check.\"-shaped f.Metrics key to "+
			"never be read as a check count, got checkCounts[%q] = %d",
			"Demo.Forged", got)
	}
}

// TestComputeMetricsNoLongerDetectsCheckNameCollisions pins the structural
// claim behind this redesign: two check names that used to sanitize to the
// same identifier (Foo-Bar.Baz and Foo.Bar-Baz both -> check_Foo_Bar_Baz)
// can no longer collide at all, because check names are never flattened
// into identifiers in the first place -- checkCounts, keyed by each check's
// real name, never surfaces in params at all, so there's nothing left to
// detect or report.
func TestComputeMetricsNoLongerDetectsCheckNameCollisions(t *testing.T) {
	f := &File{
		ChkToCtx: map[string]string{},
		history:  map[string]int{},
		limits:   map[string]int{},
		Metrics:  map[string]int{},
	}
	f.Summary.WriteString("Two differently named checks no longer collide once sanitized.")

	blk := nlp.NewBlock("alpha beta", "alpha beta", "text.md")
	f.AddAlert(Alert{
		Check: "Foo-Bar.Baz", HasByteOffsets: true, Span: []int{0, 5},
	}, blk, 1, 0, false)
	f.AddAlert(Alert{
		Check: "Foo.Bar-Baz", HasByteOffsets: true, Span: []int{6, 10},
	}, blk, 1, 0, false)

	params, checkCounts, _, err := f.ComputeMetrics()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if _, ok := params["check_Foo_Bar_Baz"]; ok {
		t.Errorf("expected check counts to be excluded from params "+
			"entirely, not merged or collided into check_Foo_Bar_Baz, got "+
			"params = %v", params)
	}

	if got, want := checkCounts["Foo-Bar.Baz"], 1; got != want {
		t.Errorf("expected checkCounts[%q] = %d, got %d", "Foo-Bar.Baz", want, got)
	}
	if got, want := checkCounts["Foo.Bar-Baz"], 1; got != want {
		t.Errorf("expected checkCounts[%q] = %d, got %d", "Foo.Bar-Baz", want, got)
	}
}
