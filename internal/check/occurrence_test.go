package check

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

// A zero-occurrence shortfall has no match to point at, but the scope that
// fell short has a position of its own: its first word. Unanchored, the alert
// collapsed onto line one, where one report also hid every other deficient
// scope in the file.
func TestOccurrenceMinAnchorsShortfall(t *testing.T) {
	rule, err := NewOccurrence(testConfig(), baseCheck{
		"extends": "occurrence",
		"name":    "Test.Presence",
		"level":   "warning",
		"message": "No slang here (found %d).",
		"scope":   "paragraph",
		"min":     1,
		"token":   `\b(?:no cap|cooked)\b`,
	}, "Test.Presence")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	ctx := "This rollout is cooked.\n\nPlain corporate prose here.\n"
	blk := nlp.NewBlock(ctx, "Plain corporate prose here.", "paragraph")
	blk.Offset = len("This rollout is cooked.\n\n")

	alerts, err := rule.Run(blk, nil, testConfig())
	if err != nil {
		t.Fatalf("running rule: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want 1", len(alerts))
	}

	a := alerts[0]
	if !a.HasByteOffsets || a.Span[0] != blk.Offset {
		t.Errorf("Span = %v (byte offsets: %v), want anchored at %d",
			a.Span, a.HasByteOffsets, blk.Offset)
	}
	if a.Match != "Plain" {
		t.Errorf("Match = %q, want the scope's first word", a.Match)
	}

	// A satisfied scope stays quiet.
	ok := nlp.NewBlock(ctx, "This rollout is cooked.", "paragraph")
	alerts, err = rule.Run(ok, nil, testConfig())
	if err != nil {
		t.Fatalf("running rule: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("satisfied scope produced %d alerts", len(alerts))
	}
}
