package lint

import (
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"

	"github.com/vale-cli/vale/v3/internal/check"
)

// TestWrapSections pins the tree the selectors run against: a heading and
// what follows it become a section, sections nest by level, and a heading
// inside a container sections only its siblings there.
func TestWrapSections(t *testing.T) {
	in := "<h1>T</h1><p>a</p><h2>A</h2><p>b</p><h3>A1</h3><p>c</p><h2>B</h2><p>d</p>" +
		"<blockquote><h4>Q</h4><p>e</p></blockquote>"
	want := "<section><h1>T</h1><p>a</p>" +
		"<section><h2>A</h2><p>b</p><section><h3>A1</h3><p>c</p></section></section>" +
		"<section><h2>B</h2><p>d</p><blockquote><section><h4>Q</h4><p>e</p></section></blockquote></section>" +
		"</section>"

	got, err := markSelections([]byte(in), nil)
	if err != nil {
		t.Fatal(err)
	}
	if body := between(string(got), "<body>", "</body>"); body != want {
		t.Errorf("wrapSections:\n got %s\nwant %s", body, want)
	}
}

// TestMarkSelections pins that a matched element is marked with the id of
// every selector that matched it, and an author's class is left alone.
func TestMarkSelections(t *testing.T) {
	in := `<h2 class="x">A</h2><p>b</p><h2>B</h2><p>c</p>`
	sels := []check.Selection{
		{ID: "s1", Sel: mustParse(`section:haschild(h2:contains("A"))`)},
		{ID: "s2", Sel: mustParse("h2")},
		{ID: "s3", Sel: mustParse(`h2:contains("A")`)},
	}

	got, err := markSelections([]byte(in), sels)
	if err != nil {
		t.Fatal(err)
	}
	body := between(string(got), "<body>", "</body>")
	want := `<section data-vale-doc="s1"><h2 class="x" data-vale-doc="s2 s3">A</h2><p>b</p></section>` +
		`<section><h2 data-vale-doc="s2">B</h2><p>c</p></section>`
	if body != want {
		t.Errorf("markSelections:\n got %s\nwant %s", body, want)
	}
}

func between(s, from, to string) string {
	if i := strings.Index(s, from); i >= 0 {
		s = s[i+len(from):]
	}
	if i := strings.Index(s, to); i >= 0 {
		s = s[:i]
	}
	return s
}

func mustParse(s string) cascadia.Sel {
	sel, err := cascadia.Parse(s)
	if err != nil {
		panic(err)
	}
	return sel
}
