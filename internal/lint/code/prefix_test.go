//go:build !vale_nocode

package code

import (
	"strings"
	"testing"
)

// cStylePrefix decides what is decoration and what is content. Getting this
// wrong deletes markup rather than exposing it, so the boundary cases are
// spelled out.
func TestCStylePrefix(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string // the decoration, or "" for none
	}{
		{"jsdoc line", " * Some text.", " *"},
		{"no leading space", "* Some text.", "*"},
		{"tab indented", "\t\t* Some text.", "\t\t*"},
		{"empty jsdoc line", " *", " *"},
		{"star then tab", " *\tSome text.", " *"},

		// The star is content in all of these.
		{"emphasis at line start", " *emphasis* here.", ""},
		{"bold at line start", " **bold** here.", ""},
		{"no star at all", " Some text.", ""},
		{"star after text", "Some * text.", ""},
		{"blank line", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := cStylePrefix.FindStringSubmatch(c.line)
			got := ""
			if m != nil {
				got = m[1]
			}
			if got != c.want {
				t.Errorf("prefix of %q = %q, want %q", c.line, got, c.want)
			}
		})
	}
}

func blanked(lang *Language, s string) string {
	return (&QueryEngine{lang: lang, cutset: " "}).blankPrefixes(s)
}

// Blanking must not move anything: the dedent that follows is the only step
// allowed to change a column, and it is the only step that reports one.
func TestBlankPrefixesPreservesWidth(t *testing.T) {
	lang := &Language{Prefix: cStylePrefix}

	for _, in := range []string{
		" * Some text.",
		" *\n * More.\n *",
		" * * a list item",
		" *emphasis* stays",
		"no decoration here",
	} {
		if got := blanked(lang, in); len(got) != len(in) {
			t.Errorf("blanking %q gave %q: %d bytes became %d", in, got, len(in), len(got))
		}
	}
}

func TestBlankPrefixesKeepsMarkup(t *testing.T) {
	lang := &Language{Prefix: cStylePrefix}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain line", " * Some text.", "   Some text."},
		{"empty line", " *", "  "},
		// The second star is a list marker and has to survive.
		{"list item", " * * a list item", "   * a list item"},
		{"emphasis", " *emphasis* here", " *emphasis* here"},
		{"indented code", " *     code()", "       code()"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := blanked(lang, c.in); got != c.want {
				t.Errorf("blank(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestBlankPrefixesIsANoOpWithoutAPrefix(t *testing.T) {
	in := " * Some text.\n * More."

	if got := blanked(&Language{}, in); got != in {
		t.Errorf("a language with no Prefix changed %q to %q", in, got)
	}
}

// A JSDoc block has to reach the Markdown parser as paragraphs, not as a
// bullet list -- the whole point of the exercise.
func TestJSDocBecomesCleanMarkdown(t *testing.T) {
	src := []byte(`/**
 * Formats the value for display.
 *
 * @example
 * const x = format(y);
 */
`)

	comments, err := GetComments(src, JavaScript())
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}

	text := comments[0].Text
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "*") {
			t.Errorf("line %q still carries decoration; Markdown reads it as a list item:\n%s",
				line, text)
		}
	}
	if !strings.Contains(text, "Formats the value for display.") {
		t.Errorf("lost the description:\n%s", text)
	}
}

// Strip is what lets an alert be put back exactly. It has to describe every
// line of Text, and describe it correctly.
func TestStripRecordsWhatWasRemoved(t *testing.T) {
	src := []byte(`/**
 * Formats the value.
 *
 *     code()
 */
`)

	comments, err := GetComments(src, JavaScript())
	if err != nil {
		t.Fatal(err)
	}

	c := comments[0]
	text := strings.Split(c.Text, "\n")
	source := strings.Split(c.Source, "\n")

	if len(c.Strip) < len(text)-1 {
		t.Fatalf("Strip has %d entries for %d lines", len(c.Strip), len(text))
	}

	// The invariant the alert adjustment depends on: blanking preserves width
	// and the dedent only takes a prefix, so a line of Text plus what Strip
	// says came off is the line of Source it came from.
	//
	// The first and last lines are excluded because Delims removed `/**` and
	// `*/` from them, which is a separate edit that Strip does not describe.
	for i := 1; i < len(text)-1 && i < len(source)-1; i++ {
		n, ok := c.StripAt(i + 1)
		if !ok {
			t.Errorf("line %d has no Strip entry", i+1)
			continue
		}
		if got, want := len(text[i])+n, len(source[i]); got != want {
			t.Errorf("line %d: %q (%d) + %d removed = %d, but source %q is %d",
				i+1, text[i], len(text[i]), n, got, source[i], want)
		}
	}

	// Relative indentation has to survive, or an indented code block stops
	// being one. See #1028.
	if !strings.Contains(c.Text, "\n    code()") {
		t.Errorf("the indented code block lost its indent:\n%q", c.Text)
	}
}

func TestStripAtBounds(t *testing.T) {
	c := Comment{Strip: []int{3, 4, 5}}

	for _, line := range []int{0, -1, 4, 99} {
		if _, ok := c.StripAt(line); ok {
			t.Errorf("StripAt(%d) reported a value it does not have", line)
		}
	}
	if n, ok := c.StripAt(2); !ok || n != 4 {
		t.Errorf("StripAt(2) = %d, %v; want 4, true", n, ok)
	}
	if _, ok := (Comment{}).StripAt(1); ok {
		t.Error("a comment with no Strip reported one")
	}
}
