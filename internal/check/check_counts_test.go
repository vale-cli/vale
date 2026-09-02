package check

import (
	"strings"
	"testing"

	"github.com/d5/tengo/v2"
)

// checkCounts is the target design's replacement for the deleted
// formulaIdentifiers/identScope/checkCounterRE machinery (see metric.go):
// rather than flattening every check name into a sanitized Tengo
// identifier (check_Style_Rule) -- which can collide, since two distinct
// names can sanitize to the same identifier -- a `metric` formula indexes a
// single tengo.Object by the check's real, unflattened name:
//
//	check["AITells.FigurativeOwns"] + check["AITells.HedgingPhrases"] > 3
//
// This makes the collision this branch spent 7 rounds chasing structurally
// impossible (there's no more name-mangling step to collide), and lets
// IndexGet distinguish "this check exists and never fired" (0) from "this
// check name doesn't exist at all" (a real error), fixing the silent-typo
// problem checkCounterRE's shape-only matching could never catch.
//
// newCheckCounts does not exist yet -- this file is the RED-phase
// specification for it, not a passing test. counts holds the raw,
// unsanitized per-check alert count (e.g. "AITells.FigurativeOwns" -> 3,
// the same raw key AddAlert's f.Metrics["check."+a.Check]++ produces once
// the "check." prefix is stripped); known holds the set of check names
// actually loaded for this run, which is what lets IndexGet tell a
// never-fired check apart from a nonexistent one.
func TestCheckCountsIndexGetOnNeverFiredCheckReturnsZero(t *testing.T) {
	cc := newCheckCounts(
		map[string]int{},
		map[string]bool{"Style.Rule": true},
	)

	val, err := cc.IndexGet(&tengo.String{Value: "Style.Rule"})
	if err != nil {
		t.Fatalf("expected a loaded-but-never-fired check to resolve without "+
			"error, got: %v", err)
	}

	got, ok := tengo.ToFloat64(val)
	if !ok {
		t.Fatalf("expected a numeric result, got %T (%v)", val, val)
	}
	if got != 0 {
		t.Errorf("expected a never-fired check to read as 0, got %v", got)
	}
}

// A check that actually fired must read back its real count, not just a
// truthy/nonzero placeholder -- the whole point of exposing alert counts to
// a formula at all.
func TestCheckCountsIndexGetOnFiredCheckReturnsRealCount(t *testing.T) {
	cc := newCheckCounts(
		map[string]int{"Style.Rule": 5},
		map[string]bool{"Style.Rule": true},
	)

	val, err := cc.IndexGet(&tengo.String{Value: "Style.Rule"})
	if err != nil {
		t.Fatalf("expected a fired, loaded check to resolve without error, got: %v", err)
	}

	got, ok := tengo.ToFloat64(val)
	if !ok {
		t.Fatalf("expected a numeric result, got %T (%v)", val, val)
	}
	if got != 5 {
		t.Errorf("expected the check's real count 5, got %v", got)
	}
}

// This is the concrete fix for the design review's most-likely-real-mistake
// finding: under the old checkCounterRE (`^check_\w+$`, a shape-only regex
// match against an already-flattened identifier), a misspelled check/rule
// name in a formula silently evaluated to 0 -- indistinguishable from a
// real check that simply never fired. Indexing by the check's actual,
// unflattened name against the set of checks genuinely loaded for this run
// lets IndexGet tell the two apart and surface a real error instead.
func TestCheckCountsIndexGetOnUnknownCheckReturnsError(t *testing.T) {
	cc := newCheckCounts(
		map[string]int{},
		map[string]bool{"Style.Rule": true},
	)

	val, err := cc.IndexGet(&tengo.String{Value: "Style.Typo"})
	if err == nil {
		t.Fatalf("expected an error for a check name that isn't loaded, got "+
			"value %v with no error -- this is the exact silent-typo failure "+
			"mode the redesign exists to fix", val)
	}
	if !strings.Contains(err.Error(), "Style.Typo") {
		t.Errorf("expected the error to name the unknown check %q, got: %v",
			"Style.Typo", err)
	}
}

// TypeName/String only need to be sane for Tengo's own error messages and
// debugging output -- not asserted exhaustively, but they must exist and
// not panic, which ObjectImpl's defaults do (ObjectImpl.TypeName panics
// with ErrNotImplemented), so checkCounts must actually override both.
func TestCheckCountsHasATypeNameAndString(t *testing.T) {
	cc := newCheckCounts(map[string]int{}, map[string]bool{})

	if cc.TypeName() == "" {
		t.Error("expected a non-empty TypeName")
	}
	if cc.String() == "" {
		t.Error("expected a non-empty String representation")
	}
}
