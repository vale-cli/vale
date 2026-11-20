package lint

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// An element HTML reads as raw text -- `<title>`, `<textarea>` -- left open in
// prose does not swallow the rest of the document: what follows is still
// linted where it is.
func TestRawTextElementInProse(t *testing.T) {
	styles, err := filepath.Abs("../../testdata/styles")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := core.LoadConfigFromString("StylesPath = " + filepath.ToSlash(styles) +
		"\nMinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = demo\n")
	if err != nil {
		t.Fatal(err)
	}
	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, tag := range []string{"title", "textarea", "xmp"} {
		doc := "Have the function create the <" + tag + "> tag itself.\n\nWe meet up at noon.\n\nThe meet up is on Friday.\n"
		var lost []string
		linter.BlockHook = func(blk nlp.Block) {
			if blk.Offset < 0 && !strings.HasPrefix(blk.Scope, "summary") && strings.Contains(blk.Text, "meet") {
				lost = append(lost, blk.Text)
			}
		}
		files, lerr := linter.LintStringAs(doc, "input.md")
		if lerr != nil {
			t.Fatal(lerr)
		}
		if len(lost) > 0 {
			t.Errorf("<%s>: blocks after it have no position: %q", tag, lost)
		}
		found := false
		for _, a := range files[0].Alerts {
			if a.Check == "demo.Meetup" {
				found = true
				if a.Line != 5 || a.Span[0] != 5 {
					t.Errorf("<%s>: demo.Meetup at %d:%d, want 5:5", tag, a.Line, a.Span[0])
				}
			}
		}
		if !found {
			t.Errorf("<%s>: demo.Meetup not reported", tag)
		}
	}
}
