package lint

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vale-cli/vale/v3/internal/core"
)

// A memoized linter reports what a fresh one does, for a document linted
// again as it is edited, and runs the block-only rules on the changed blocks
// alone.
func TestMemoizeMatchesFreshLint(t *testing.T) {
	doc := sample()
	paras := strings.Split(strings.TrimSpace(doc), "\n\n")

	//nolint:dupword // the repeat is what Vale.Repetition fires on
	edited := slicesWith(paras, 3, "We will utilize a smart phone here, and and nothing else.")
	removed := append(append([]string{}, paras[:5]...), paras[6:]...)
	moved := append(append([]string{}, paras[10:]...), paras[:10]...)

	steps := []struct {
		name string
		text string
		runs string // how many block-only runs: "all", "none", "some", or "few"
	}{
		{"first", doc, "all"},
		{"unchanged", doc, "none"},
		{"one paragraph edited", strings.Join(edited, "\n\n") + "\n", "some"},
		{"the edit undone", doc, "some"},
		{"a paragraph removed", strings.Join(removed, "\n\n") + "\n", "none"},
		{"paragraphs reordered", strings.Join(moved, "\n\n") + "\n", "few"},
	}

	memoized := demoLinter(t)
	memoized.Memoize()

	var all int
	for _, step := range steps {
		fresh, err := demoLinter(t).LintStringAs(step.text, "input.md")
		if err != nil {
			t.Fatal(err)
		}
		want := alertLines(fresh)
		if len(want) == 0 {
			t.Fatalf("%s: no alerts, so this proves nothing", step.name)
		}

		runs := 0
		memoized.RuleHook = func(name string, _ time.Duration) {
			if memoKinds[memoized.Manager.Rules()[name].Fields().Extends] {
				runs++
			}
		}
		got, err := memoized.LintStringAs(step.text, "input.md")
		if err != nil {
			t.Fatal(err)
		}
		if lines := alertLines(got); !reflect.DeepEqual(lines, want) {
			t.Errorf("%s: memoized alerts differ:\n got  %v\n want %v", step.name, lines, want)
		}

		// RuleHook sees every rule, run or remembered; the memo counts the runs.
		misses := memoized.memo.runs
		switch step.runs {
		case "all":
			all = misses
			if misses == 0 {
				t.Errorf("%s: nothing was run", step.name)
			}
		case "none":
			if misses != 0 {
				t.Errorf("%s: %d runs, want none", step.name, misses)
			}
		case "some", "few":
			if (misses == 0 && step.runs == "some") || misses >= all/4 {
				t.Errorf("%s: %d runs, want a few of %d", step.name, misses, all)
			}
		}
		if runs == 0 {
			t.Errorf("%s: RuleHook saw no block-only rules", step.name)
		}
	}
}

func slicesWith(list []string, i int, s string) []string {
	out := append([]string{}, list...)
	out[i] = s
	return out
}

// A sequence rule is remembered like the block-only rules: what it found in an
// unchanged block is reported again, in place, without tagging the block again.
func TestMemoizeKeepsSequenceRules(t *testing.T) {
	doc := "The meet up is on Friday.\n\nWe should meet up later. The meet-ups were fun.\n\nNothing here.\n"
	moved := "Nothing here.\n\nThe meet up is on Friday.\n\nWe should meet up later. The meet-ups were fun.\n"

	styles, err := filepath.Abs("../../testdata/styles")
	if err != nil {
		t.Fatal(err)
	}
	linter := func() *Linter {
		cfg, cerr := core.LoadConfigFromString("StylesPath = " + filepath.ToSlash(styles) +
			"\nMinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = demo\n")
		if cerr != nil {
			t.Fatal(cerr)
		}
		l, lerr := NewLinter(cfg)
		if lerr != nil {
			t.Fatal(lerr)
		}
		return l
	}

	memoized := linter()
	memoized.Memoize()
	first := 0
	for i, text := range []string{doc, doc, moved} {
		fresh, ferr := linter().LintStringAs(text, "input.md")
		if ferr != nil {
			t.Fatal(ferr)
		}
		got, gerr := memoized.LintStringAs(text, "input.md")
		if gerr != nil {
			t.Fatal(gerr)
		}
		want := alertLines(fresh)
		if !strings.Contains(strings.Join(want, " "), "demo.Meetup") {
			t.Fatalf("lint %d: no sequence alert, so this proves nothing: %v", i, want)
		}
		if lines := alertLines(got); !reflect.DeepEqual(lines, want) {
			t.Errorf("lint %d: memoized alerts differ:\n got  %v\n want %v", i, lines, want)
		}
		switch i {
		case 0:
			first = memoized.memo.runs
		case 1:
			if memoized.memo.runs != 0 {
				t.Errorf("unchanged: %d rules run, want none", memoized.memo.runs)
			}
		case 2:
			// Rules that read the whole document see a new one.
			if memoized.memo.runs >= first {
				t.Errorf("reordered: %d rules run, want fewer than the first lint's %d", memoized.memo.runs, first)
			}
		}
	}
}
