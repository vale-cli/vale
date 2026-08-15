package glob

import (
	"fmt"
	"testing"
)

type globTest struct {
	query string
	match bool
}

var globTests = []struct {
	pattern string
	tests   []globTest
}{
	{`docs/**`, []globTest{
		{`docs/a.md`, true}, {`docs/info/b.py`, true}, {`info/c.cc`, false}},
	},
	{`!docs/**`, []globTest{
		{`docs/a.md`, false}, {`docs/info/b.py`, false}, {`info/c.cc`, true}},
	},
	{`!**/*.min.js`, []globTest{
		{`a/b/c/foo.py`, true}, {`a/b/c/foo.min.js`, false}},
	},
	{`docs/**/*.md`, []globTest{
		{`docs/a.md`, true}, {`docs/info/b.md`, true}, {`docs/c.cc`, false}},
	},
	{`{documentation,website}/content/{ja,zh-tw}/**/*.adoc`, []globTest{
		{`website/content/zh-tw/where.adoc`, true},
		{`documentation/content/ja/articles/test.adoc`, true}},
	},
}

// Patterns doublestar accepts and gobwas/glob rejects. NewGlob has to report
// them, since Match can't: it used to compile with MustCompile and panicked
// partway through the walk.
var invalidGlobs = []string{`[a-]`, `[a-b-c]`}

func TestInvalidGlob(t *testing.T) {
	for _, pat := range invalidGlobs {
		g, err := NewGlob(pat)
		if err == nil {
			t.Errorf("%s: expected an error, got %+v", pat, g)
		}
	}
}

func TestGlob(t *testing.T) {
	for _, tt := range globTests {
		g, _ := NewGlob(tt.pattern)
		for _, tc := range tt.tests {
			test := fmt.Sprintf("%s -> %s", tt.pattern, tc.query)
			if tc.match != g.Match(tc.query) {
				t.Error(test)
			}
		}
	}
}
