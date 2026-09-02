package check

import (
	"fmt"

	"github.com/d5/tengo/v2"
)

// checkCounts is a Tengo object exposing every loaded check's per-document
// alert count by its real, unflattened name (e.g. "Style.Rule"), added to a
// `metric` formula's parameters under the "check" key. A formula indexes it
// directly:
//
//	check["AITells.FigurativeOwns"] + check["AITells.HedgingPhrases"] > 3
//
// This replaces the old design, where a check name was flattened into a
// sanitized Tengo identifier (check_Style_Rule) for direct reference in a
// formula -- a step where two distinct names could sanitize to the same
// identifier, with no way to tell them apart afterward. Indexing by the
// real name makes that collision structurally impossible: there's no
// flattening step left to collide on.
//
// It also fixes a silent-typo problem the old design had no way to catch:
// IndexGet distinguishes a check that's genuinely loaded but simply never
// fired on this document (reads as 0) from a name that was never loaded at
// all -- almost always a typo in the formula -- which returns a real error
// instead of silently reading 0 either way.
type checkCounts struct {
	tengo.ObjectImpl
	// counts holds the raw, unsanitized per-check alert count for this
	// document, keyed by the check's real name (e.g. "AITells.FigurativeOwns"
	// -> 3). A check absent from counts simply never fired.
	counts map[string]int
	// known holds the set of check names actually loaded for this run --
	// see core.File.LoadedChecks -- which is what lets IndexGet tell a
	// never-fired check apart from one that doesn't exist.
	known map[string]bool
}

// newCheckCounts builds a checkCounts object from counts (raw per-check
// alert counts) and known (the set of check names loaded for this run).
func newCheckCounts(counts map[string]int, known map[string]bool) *checkCounts {
	return &checkCounts{counts: counts, known: known}
}

// TypeName returns the name of the type, for Tengo's own error messages and
// debugging output.
func (c *checkCounts) TypeName() string {
	return "check-counts"
}

// String returns a string representation of the object, for Tengo's own
// error messages and debugging output.
func (c *checkCounts) String() string {
	return "<check counts>"
}

// IndexGet returns index's alert count as a *tengo.Float -- 0 if it names a
// check that's loaded but never fired on this document, its real count
// otherwise -- matching this codebase's existing convention that every other
// metric value a `metric` formula sees is a float64. Indexing a name that
// isn't a check genuinely loaded for this run returns a real error naming
// it, rather than silently reading 0 -- the exact silent-typo failure mode
// the old, shape-only checkCounterRE match could never catch.
func (c *checkCounts) IndexGet(index tengo.Object) (tengo.Object, error) {
	name, ok := tengo.ToString(index)
	if !ok {
		return nil, tengo.ErrInvalidIndexType
	}

	if !c.known[name] {
		return nil, fmt.Errorf(
			"%q is not a known check: it isn't defined by any loaded style, "+
				"so this is likely a typo in the metric formula", name)
	}

	return &tengo.Float{Value: float64(c.counts[name])}, nil
}
