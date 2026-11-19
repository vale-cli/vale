package check

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	rx "github.com/vale-cli/vale/v3/internal/regex"
)

// Tokens run apart find what their alternation finds, in its order.
func TestTokenSplitMatchesAlternation(t *testing.T) {
	cases := []struct {
		name     string
		template string
		tokens   []string
		text     string
	}{
		{
			"plain words",
			ignoreCase + wordTemplate,
			[]string{"very", "really", "just", "quite", "basically"},
			"It is very, very good. Really just quite good, basically. Justice is not just.",
		},
		{
			"the earlier token wins a shared start",
			ignoreCase + wordTemplate,
			[]string{"in order", "in order to", "in", "order to go", "to"},
			"In order to go in, order to go. In order.",
		},
		{
			"a match ends where a later one would have begun",
			nonwordTemplate,
			[]string{"abc", "bcd", "cde", "de"},
			"abcde abcd bcde cde",
		},
		{
			"wildcards that run long",
			wordTemplate,
			[]string{
				`[Ii]t'?s not just [^;,]+[;,] it'?s [^.!?]+`,
				`\w+ing [^,.!?]+, \w+ing [^,.!?]+, and \w+ing [^.!?]+`,
				`not only [^,]+, but`,
				`(?:is|are) (?:clean|simple)`,
			},
			"It's not just a tool; it's a shift. Reading books, writing notes, and sharing them. Not only fast, but simple. The API is clean.",
		},
		{
			"lookarounds",
			ignoreCase + wordTemplate,
			[]string{`(?<!un)clear`, `simple(?! enough)`, `easy(?= to)`, `obvious`},
			"Unclear, clear, simple enough, simple, easy to do, easy, obvious.",
		},
		{
			"an empty match",
			nonwordTemplate,
			[]string{`x*`, `ab`, `b+`, `zz`},
			"abxb",
		},
		{
			"beyond ASCII",
			ignoreCase + wordTemplate,
			[]string{"naïve", "café", "über", "résumé"},
			"A naïve café — über naïve, with a résumé.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			split := newTokenSplit(c.template, c.tokens)
			if split == nil {
				t.Fatal("the tokens were not split")
			}
			joined := rx.MustCompile(fmt.Sprintf(c.template, strings.Join(c.tokens, "|")))

			var want [][]int
			for _, m := range joined.FindAllStringSubmatchIndex(c.text, -1) {
				want = append(want, m[:2])
			}
			if len(want) == 0 {
				t.Fatal("the alternation matches nothing, so this proves nothing")
			}

			// Every token awake, and only those the text could match.
			all := make([]int, len(c.tokens))
			for i := range all {
				all[i] = i
			}
			for _, active := range [][]int{all, split.active(strings.ToLower(c.text))} {
				got, by, ok := split.find(c.text, active)
				if !ok {
					t.Fatal("a token has no pattern of its own")
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("tokens %v:\n got  %v\n want %v", active, got, want)
				}
				if len(by) != len(got) {
					t.Errorf("%d matches but %d tokens named", len(got), len(by))
				}
			}
		})
	}
}

// A token tied to the pattern around it keeps the rule in one piece.
func TestTokenSplitRefuses(t *testing.T) {
	rest := []string{"alpha", "beta", "gamma"}
	cases := map[string]string{
		"a capture group":     `(very) good`,
		"a named group":       `(?<w>very) good`,
		"a back-reference":    `(?:very) \1`,
		"a bare inline flag":  `(?i)very`,
		"a group after class": `[(]x[)] (y)`,
	}
	for name, token := range cases {
		if newTokenSplit(wordTemplate, append([]string{token}, rest...)) != nil {
			t.Errorf("%s: %q was split", name, token)
		}
	}

	fine := []string{`\(very\)`, `[()]+`, `(?:very|quite) good`, `(?i:very)`, `good(?= enough)`}
	if newTokenSplit(wordTemplate, fine) == nil {
		t.Errorf("%q was not split", fine)
	}
	if newTokenSplit(wordTemplate, rest) != nil {
		t.Error("three tokens were split, under the floor")
	}
}
