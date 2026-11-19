package lint

import (
	"reflect"
	"strings"
	"testing"
	"time"
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
			if concurrentKinds[memoized.Manager.Rules()[name].Fields().Extends] {
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

		// RuleHook sees every rule, run or remembered, so count the misses.
		misses := len(memoized.memo.now) - carried(memoized.memo)
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

// carried counts the current generation's entries that came from the last.
func carried(m *blockMemo) int {
	n := 0
	for k := range m.now {
		if _, ok := m.last[k]; ok {
			n++
		}
	}
	return n
}

func slicesWith(list []string, i int, s string) []string {
	out := append([]string{}, list...)
	out[i] = s
	return out
}
