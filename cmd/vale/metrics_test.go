package main

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// TestPrintMetricsResultSucceedsWithoutCollision pins the ordinary,
// non-colliding path: printMetricsResult must still succeed and print
// normally when ComputeMetrics finds nothing ambiguous.
func TestPrintMetricsResultSucceedsWithoutCollision(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	f, err := core.NewFile("A document with real prose for a word count.", cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.Summary.WriteString("A document with real prose for a word count.")

	if err = printMetricsResult(f); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestPrintMetricsResultNoLongerErrorsOnSanitizedKeyCollision pins the
// metric-check-counts redesign's effect on this CLI surface: with check
// names no longer flattened into Tengo identifiers at all, there is no more
// sanitized-key collision for ComputeMetrics to detect or for
// printMetricsResult to propagate specially -- see item 4 of the redesign
// (cmd/vale/command.go's printMetricsResult should "revert to something
// much simpler, there's no more collision to propagate specially").
//
// This reproduces the f.Metrics = {"words": 999} setup the deleted
// TestPrintMetricsResultSurfacesCollision (an old-mechanism test pinning
// the now-removed collision machinery) used to assert an error for, but
// asserts the opposite outcome: printMetricsResult must now succeed.
func TestPrintMetricsResultNoLongerErrorsOnSanitizedKeyCollision(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	f, err := core.NewFile("A document with real prose for a word count.", cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.Summary.WriteString("A document with real prose for a word count.")
	f.Metrics["words"] = 999

	if err = printMetricsResult(f); err != nil {
		t.Fatalf("expected no error under the new design (there is no more "+
			"collision-detection machinery left to trip), got: %v", err)
	}
}
