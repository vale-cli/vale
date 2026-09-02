package core

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

// AddAlert must not panic on an alert with a negative Span -- spelling can
// produce one when the matched token isn't found verbatim in the block. See
// #808 (panic: slice bounds out of range [-1:]).
func TestAddAlertNegativeSpan(t *testing.T) {
	f := &File{
		ChkToCtx: map[string]string{},
		history:  map[string]int{},
		limits:   map[string]int{},
	}
	// count("word") > 1 and ctx < 1000 -> the disambiguation branch that
	// previously sliced ctx[0:Span[0]] with a negative index.
	blk := nlp.NewBlock("word and word", "word and word", "text.md")
	a := Alert{Check: "X", Match: "word", Span: []int{-1, 3}}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AddAlert panicked on a negative span: %v", r)
		}
	}()
	f.AddAlert(a, blk, 1, 0, false)
}

// TestAddAlertNilMetrics verifies the nil-map guard added alongside the new
// per-check counter: AddAlert must not panic when Metrics is left nil, e.g.
// a File built without going through NewFile (which always initializes it).
// Unlike TestAddAlertNegativeSpan, this alert is actually appended to
// f.Alerts, so it exercises the f.Metrics write path directly.
func TestAddAlertNilMetrics(t *testing.T) {
	f := &File{
		ChkToCtx: map[string]string{},
		history:  map[string]int{},
		limits:   map[string]int{},
	}

	blk := nlp.NewBlock("alpha", "alpha", "text.md")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AddAlert panicked with a nil Metrics map: %v", r)
		}
	}()
	f.AddAlert(Alert{
		Check: "Demo.Rule", HasByteOffsets: true, Span: []int{0, 5},
	}, blk, 1, 0, false)
}

// TestAddAlertCheckCounterIncrementsOnlyForReportedAlerts verifies the new,
// unconditional per-check alert counter proposed in issue #1163: it
// increments once for every alert actually appended to f.Alerts -- the same
// place f.limits increments today, just without the a.Limit > 0 gate -- and
// does NOT increment for an alert a rule marks Hide, or for one f.history
// dedupes as a repeat of an already-reported (Line, Span[0], Check).
//
// The counter is surfaced in f.checkCounts, a dedicated field keyed by the
// check's real name, kept entirely separate from f.Metrics (which ast.go
// also writes to from document content) so a crafted document can't inject
// a false count under it -- see f.checkCounts's own doc comment.
//
// Alerts here use HasByteOffsets so AddAlert locates them deterministically
// via locFromByteOffset rather than a text search, making the resulting
// (Line, Span[0]) -- and therefore the dedup outcome -- fully controlled by
// the test rather than incidental to how the search happens to land.
func TestAddAlertCheckCounterIncrementsOnlyForReportedAlerts(t *testing.T) {
	f := &File{
		ChkToCtx: map[string]string{},
		history:  map[string]int{},
		limits:   map[string]int{},
		Metrics:  map[string]int{},
	}

	// "alpha beta" -- byte offsets: "alpha" = [0,5), "beta" = [6,10).
	blk := nlp.NewBlock("alpha beta", "alpha beta", "text.md")

	// Two genuine, distinct alerts from the same check: both should count.
	f.AddAlert(Alert{
		Check: "Demo.Rule", HasByteOffsets: true, Span: []int{0, 5},
	}, blk, 1, 0, false)
	f.AddAlert(Alert{
		Check: "Demo.Rule", HasByteOffsets: true, Span: []int{6, 10},
	}, blk, 1, 0, false)

	// A Hide alert from the same check: never reaches f.Alerts, so it must
	// not count either.
	f.AddAlert(Alert{
		Check: "Demo.Rule", HasByteOffsets: true, Span: []int{0, 5}, Hide: true,
	}, blk, 1, 0, false)

	// A repeat of the first alert's exact (Line, Span[0]): f.history dedupes
	// this, so it must not count a third time.
	f.AddAlert(Alert{
		Check: "Demo.Rule", HasByteOffsets: true, Span: []int{0, 5},
	}, blk, 1, 0, false)

	if len(f.Alerts) != 2 {
		t.Fatalf("expected 2 alerts actually reported (Hide and the dedup "+
			"attempt should not add more), got %d", len(f.Alerts))
	}

	if got := f.checkCounts["Demo.Rule"]; got != 2 {
		t.Fatalf("expected checkCounts[Demo.Rule] to be 2 (one per alert "+
			"actually reported, not per AddAlert call), got %d", got)
	}
}

// TestAddAlertLimitCapUnaffected pins the existing opt-in `limit:`/f.limits
// reporting cap: it must keep behaving exactly as it does today, capping
// f.Alerts at Limit regardless of the new unconditional counter added for
// issue #1163.
//
// The new counter only counts what was actually appended -- the same gate
// f.limits has always used -- so with Limit: 2 and three attempts, both
// f.limits and f.checkCounts stop at 2, not 3.
func TestAddAlertLimitCapUnaffected(t *testing.T) {
	f := &File{
		ChkToCtx: map[string]string{},
		history:  map[string]int{},
		limits:   map[string]int{},
		Metrics:  map[string]int{},
	}

	// "alpha beta gamma" -- three non-overlapping byte-offset spans.
	blk := nlp.NewBlock("alpha beta gamma", "alpha beta gamma", "text.md")
	spans := [][]int{{0, 5}, {6, 10}, {11, 16}}

	for _, span := range spans {
		f.AddAlert(Alert{
			Check: "Demo.Capped", HasByteOffsets: true, Span: span, Limit: 2,
		}, blk, 1, 0, false)
	}

	if len(f.Alerts) != 2 {
		t.Fatalf("expected the existing limit: 2 cap to allow only 2 alerts, got %d",
			len(f.Alerts))
	}

	if got := f.limits["Demo.Capped"]; got != 2 {
		t.Fatalf("expected the existing f.limits cap counter to stay at 2, got %d", got)
	}

	if got := f.checkCounts["Demo.Capped"]; got != 2 {
		t.Fatalf("expected the new counter to count only the 2 alerts actually "+
			"appended (matching where f.limits increments today), got %d", got)
	}
}
