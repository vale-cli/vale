package lint

import (
	"bytes"
	"testing"
)

// TestMathInline pins Pandoc's delimiter rules for `$…$`: the opening `$` must
// be followed by a non-space character, and the closing `$` must be preceded
// by one and not followed by a digit.
//
// The rules exist to keep money out of math. Every case that renders as `code`
// is text vale will not check; every case that stays plain is text it will.
func TestMathInline(t *testing.T) {
	cases := []struct {
		description string
		content     string
		expected    string
	}{
		{
			description: "math",
			content:     "A test $g_i$ here.",
			expected:    "<p>A test <code>$g_i$</code> here.</p>\n",
		},
		{
			description: "math with spaces inside",
			content:     "A test $g_i = g(p)_i = p_i$ here.",
			expected:    "<p>A test <code>$g_i = g(p)_i = p_i$</code> here.</p>\n",
		},
		{
			description: "currency",
			content:     "It costs $5 and $10 today.",
			expected:    "<p>It costs $5 and $10 today.</p>\n",
		},
		{
			description: "currency glued to a word",
			content:     "Between US$5 and CA$10 there.",
			expected:    "<p>Between US$5 and CA$10 there.</p>\n",
		},
		{
			description: "unclosed",
			content:     "A lone $5 in a sentence.",
			expected:    "<p>A lone $5 in a sentence.</p>\n",
		},
		{
			description: "space after the opening delimiter",
			content:     "A test $ g_i$ here.",
			expected:    "<p>A test $ g_i$ here.</p>\n",
		},
		{
			description: "wrapped across lines",
			content:     "A test $g_i =\ng(p)_i$ here.",
			expected:    "<p>A test <code>$g_i =\ng(p)_i$</code> here.</p>\n",
		},
		{
			description: "escaped delimiter",
			content:     "It costs \\$5 and \\$10 today.",
			expected:    "<p>It costs $5 and $10 today.</p>\n",
		},
		{
			description: "angle bracket inside math",
			content:     "The bound $a < b$ holds.",
			expected:    "<p>The bound <code>$a &lt; b$</code> holds.</p>\n",
		},
		{
			description: "display math is a block",
			content:     "Intro.\n\n$$\ng_i = g(p)_i\n$$\n",
			expected:    "<p>Intro.</p>\n<pre>g_i = g(p)_i\n</pre>\n",
		},
		{
			description: "code span is untouched",
			content:     "A test `$g_i$` here.",
			expected:    "<p>A test <code>$g_i$</code> here.</p>\n",
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			var buf bytes.Buffer
			if err := goldQmd.Convert([]byte(c.content), &buf); err != nil {
				t.Fatalf("Convert returned an error: %s", err)
			}
			if got := buf.String(); got != c.expected {
				t.Fatalf("Expected %q, but got %q", c.expected, got)
			}
		})
	}
}

// TestMathBlock pins where `$$…$$` display math closes. goldmark-mathjax closed
// only on a line of nothing but `$$`, so any other Pandoc shape left the block
// open and, as an open block does, hid the rest of the document from linting
// (#1148). Each case renders the equation as `pre` -- text vale skips -- and
// keeps the paragraph after it as prose vale still checks.
func TestMathBlock(t *testing.T) {
	cases := []struct {
		description string
		content     string
		expected    string
	}{
		{
			description: "opener also closes on one line",
			content:     "Intro.\n\n$$x=1$$\n\nAfter.\n",
			expected:    "<p>Intro.</p>\n<pre>$$x=1$$\n</pre>\n<p>After.</p>\n",
		},
		{
			description: "closing delimiter ends a content line",
			content:     "$$\n\\begin{aligned}\nx=1\n\\end{aligned}$$\n\nAfter.\n",
			expected:    "<pre>\\begin{aligned}\nx=1\n\\end{aligned}$$\n</pre>\n<p>After.</p>\n",
		},
		{
			description: "single line with a trailing label",
			content:     "$$ x=1 $$ {#eq-foo}\n\nAfter.\n",
			expected:    "<pre>$$ x=1 $$ {#eq-foo}\n</pre>\n<p>After.</p>\n",
		},
		{
			description: "label after the closing delimiter",
			content:     "$$\nx=1\n$$ {#eq-foo}\n\nAfter.\n",
			expected:    "<pre>x=1\n$$ {#eq-foo}\n</pre>\n<p>After.</p>\n",
		},
		{
			description: "bare closing line still closes",
			content:     "Intro.\n\n$$\ng_i = g(p)_i\n$$\n\nAfter.\n",
			expected:    "<p>Intro.</p>\n<pre>g_i = g(p)_i\n</pre>\n<p>After.</p>\n",
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			var buf bytes.Buffer
			if err := goldQmd.Convert([]byte(c.content), &buf); err != nil {
				t.Fatalf("Convert returned an error: %s", err)
			}
			if got := buf.String(); got != c.expected {
				t.Fatalf("Expected %q, but got %q", c.expected, got)
			}
		})
	}
}
