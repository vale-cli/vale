package check

import (
	"reflect"
	"testing"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

const sel = `section:has(> h2:contains("A & B"))`

// TestSplitOutside pins that a selector's own `&` and `.` are left to it.
func TestSplitOutside(t *testing.T) {
	cases := []struct {
		in   string
		sep  rune
		want []string
	}{
		{"sentence & ~heading", '&', []string{"sentence ", " ~heading"}},
		{"text.list", '.', []string{"text", "list"}},
		{"sentence & doc(" + sel + ")", '&', []string{"sentence ", " doc(" + sel + ")"}},
		{"doc(h2.note)", '.', []string{"doc(h2.note)"}},
		{`doc(p:contains("a)b")) & text`, '&', []string{`doc(p:contains("a)b")) `, " text"}},
	}
	for _, c := range cases {
		if got := splitOutside(c.in, c.sep); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitOutside(%q, %q) = %q, want %q", c.in, c.sep, got, c.want)
		}
	}
}

// TestDocSelectors pins the terms a scope declares and the class each maps
// to, which the walker must reproduce from the selector text alone.
func TestDocSelectors(t *testing.T) {
	scope := "sentence & ~doc(" + sel + ") & doc(h2 + p)"
	got := DocSelectors(scope)
	want := map[string]string{docID(sel): sel, docID("h2 + p"): "h2 + p"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DocSelectors(%q) = %v, want %v", scope, got, want)
	}
	if none := DocSelectors("text.list"); len(none) != 0 {
		t.Errorf("DocSelectors(text.list) = %v, want none", none)
	}
}

// TestDocMatches pins the three meanings of a `doc(...)` term.
//
// The blocks are the ones the linter builds: a paragraph inside a matched
// element carries `in.<id>`, and the element itself is linted as `doc.<id>`.
func TestDocMatches(t *testing.T) {
	id := docID(sel)
	inside := "text.in." + id + ".md"
	fragment := "sentence.text.in." + id + ".md"
	outside := "text.md"
	selection := "doc." + id + ".md"

	cases := []struct {
		rule  string
		block string
		want  bool
	}{
		// Alone: the element is the block, and only that block.
		{"doc(" + sel + ")", selection, true},
		{"doc(" + sel + ")", inside, false},
		{"doc(" + sel + ")", outside, false},

		// Beside a scope: that scope, narrowed to the element.
		{"text & doc(" + sel + ")", inside, true},
		{"text & doc(" + sel + ")", outside, false},
		{"text & doc(" + sel + ")", selection, false},
		{"sentence & doc(" + sel + ")", fragment, true},
		{"sentence & doc(" + sel + ")", inside, false},

		// Negated: everywhere but the element, and never the selection.
		{"~doc(" + sel + ")", outside, true},
		{"~doc(" + sel + ")", inside, false},
		{"~doc(" + sel + ")", selection, false},
		{"~doc(" + sel + ")", "doc." + docID("h2 + p") + ".md", false},
	}
	for _, c := range cases {
		blk := nlp.NewBlock("", "text", c.block)
		if got := NewScope([]string{c.rule}).Matches(blk); got != c.want {
			t.Errorf("scope %q on %q = %v, want %v", c.rule, c.block, got, c.want)
		}
	}
}

// TestCompileSelector pins the standard spelling of a child selector.
func TestCompileSelector(t *testing.T) {
	for _, s := range []string{sel, "section:has( >h2)", "h2 + p", "ul:not(p + ul)"} {
		if _, err := compileSelector(s); err != nil {
			t.Errorf("compileSelector(%q): %v", s, err)
		}
	}
	if _, err := compileSelector("h2:has("); err == nil {
		t.Error("compileSelector accepted an unclosed selector")
	}
}
