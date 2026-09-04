package lint

import "testing"

func TestLeadingSpace(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"", ""},
		{"text", ""},
		{", text", ""},
		{" text", " "},
		{"\ttext", " "},
		{"\ntext", "\n"},
		{"\r\ntext", "\n"},
		{"\n  text", "\n"},
	}
	for _, c := range cases {
		if got := leadingSpace(c.raw); got != c.want {
			t.Errorf("leadingSpace(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestClean covers the whitespace at an inline-markup boundary: the source's
// own separator is kept after a closed element (#1111, #1119, #1174), while
// text inside an element is padded unless it opens with punctuation (#1052,
// #1056).
func TestClean(t *testing.T) {
	cases := []struct {
		desc         string
		txt          string
		inline       bool
		sep          string
		closedInline bool
		want         string
	}{
		{
			desc:         "after markup, no space in source",
			txt:          "s here", // `<code>X</code>s` (#1111)
			closedInline: true,
			want:         "s here",
		},
		{
			desc:         "after markup, space in source",
			txt:          ":", // `<strong>x</strong> :` (#1119)
			sep:          " ",
			closedInline: true,
			want:         " :",
		},
		{
			desc:         "after markup, line break in source",
			txt:          ", which", // `[x](u)\n, which` (#1174)
			sep:          "\n",
			closedInline: true,
			want:         "\n, which",
		},
		{
			desc:         "after markup, line break before a word",
			txt:          "which",
			sep:          "\n",
			closedInline: true,
			want:         "\nwhich",
		},
		{
			desc:   "inside markup",
			txt:    "code", // `in <code>code</code> for` (#1052)
			inline: true,
			want:   " code",
		},
		{
			desc:   "inside markup, opens with punctuation",
			txt:    ") here", // `(<a>x</a>) here` (#1056)
			inline: true,
			want:   ") here",
		},
		{
			desc:   "inside markup, opens with a dash",
			txt:    "—Article", // #1029
			inline: true,
			want:   "—Article",
		},
		{
			desc: "block text",
			txt:  "plain",
			want: "plain",
		},
		{
			desc: "block text, ignores the separator",
			txt:  "plain",
			sep:  " ",
			want: "plain",
		},
	}
	for _, c := range cases {
		got, _ := clean(c.txt, "", false, c.inline, c.sep, c.closedInline)
		if got != c.want {
			t.Errorf("%s: clean(%q) = %q, want %q", c.desc, c.txt, got, c.want)
		}
	}
}
