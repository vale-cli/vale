package lint

import (
	"reflect"
	"strings"
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
)

// TestFootnoteSuppression pins that a comment control reaches a footnote
// definition. goldmark moves every definition into a trailing
// `div.footnotes`, so a walk that took the toggles in the order they arrive
// saw a `= NO` ... `= YES` pair already closed by the time the definition was
// linted, and the suppression silently did nothing. See #1078.
func TestFootnoteSuppression(t *testing.T) {
	// want holds the source lines expected to carry an alert on the misspelling,
	// so that a case with more than one occurrence says which of them survives.
	cases := []struct {
		name string
		in   string
		want []int
	}{
		{
			"a definition inside the block is suppressed",
			"Text with a footnote[^x].\n\n<!-- vale Vale.Spelling = NO -->\n\n" +
				"[^x]: I want to talk about thingamajigg in a footnote.\n\n" +
				"<!-- vale Vale.Spelling = YES -->\n\nMore text here.\n",
			nil,
		},
		{
			"a definition outside any block is not",
			"Text with a footnote[^x].\n\n" +
				"[^x]: I want to talk about thingamajigg in a footnote.\n\nMore text here.\n",
			[]int{3},
		},
		{
			"a later block does not reach back to an earlier definition",
			"Text with a footnote[^x].\n\n" +
				"[^x]: I want to talk about thingamajigg in a footnote.\n\n" +
				"<!-- vale Vale.Spelling = NO -->\n\nMore text here.\n\n" +
				"<!-- vale Vale.Spelling = YES -->\n",
			[]int{3},
		},
		{
			"an earlier mention of the same word does not place the definition",
			"An intro mentioning thingamajigg early on, plus a footnote[^x].\n\n" +
				"<!-- vale Vale.Spelling = NO -->\n\n" +
				"[^x]: I want to talk about thingamajigg in a footnote.\n\n" +
				"<!-- vale Vale.Spelling = YES -->\n\nTrailing text.\n",
			[]int{1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
			if err != nil {
				t.Fatal(err)
			}
			cfg.MinAlertLevel = 0
			cfg.GBaseStyles = []string{"Vale"}
			cfg.Flags.InExt = ".md"

			linter, err := NewLinter(cfg)
			if err != nil {
				t.Fatal(err)
			}

			files, err := linter.LintString(c.in)
			if err != nil {
				t.Fatal(err)
			}

			var got []int
			for _, f := range files {
				for _, a := range f.Alerts {
					if strings.Contains(a.Match, "thingamajigg") {
						got = append(got, a.Line)
					}
				}
			}

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("alerts on lines %v; want %v", got, c.want)
			}
		})
	}
}
