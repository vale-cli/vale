//go:build !vale_noviews

package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestFileToValuePositions(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		src  string
		want []scalarPos
	}{
		{
			name: "plain scalar",
			ext:  ".yaml",
			src:  "info:\n  title: sample API\n",
			want: []scalarPos{{Value: "sample API", Line: 2, Column: 10}},
		},
		{
			name: "double-quoted with line continuation",
			ext:  ".yaml",
			src:  "info:\n  description: \"Line 1\\\n    \\ Line 2\"\n",
			// yaml.v3 reports the opening quote at col 16; the
			// resolver advances past it to the first content char.
			want: []scalarPos{{Value: "Line 1 Line 2", Line: 2, Column: 17, Quote: '"'}},
		},
		{
			name: "literal block scalar",
			ext:  ".yaml",
			src:  "info:\n  description: |\n    First line\n    Second line\n",
			want: []scalarPos{{Value: "First line\nSecond line\n", Line: 3, Column: 5}},
		},
		{
			name: "folded block with trailing whitespace is rewritten to literal",
			ext:  ".yaml",
			// Trailing space after "First line." in the source — the
			// nateKlaux scenario from issue #1018. After the folded→
			// literal rewrite the value carries newlines, so the
			// flat-scalar walk reports the first content line/col.
			src:  "info:\n  description: >\n    First line. \n    Second line.\n",
			want: []scalarPos{{Value: "First line. \nSecond line.\n", Line: 3, Column: 5}},
		},
		{
			name: "single-quoted",
			ext:  ".yaml",
			src:  "title: 'sample'\n",
			want: []scalarPos{{Value: "sample", Line: 1, Column: 9, Quote: '\''}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &File{Content: tt.src, RealExt: tt.ext}
			_, scalars, err := fileToValue(f)
			if err != nil {
				t.Fatalf("fileToValue: %v", err)
			}
			if !reflect.DeepEqual(scalars, tt.want) {
				t.Errorf("scalars mismatch\n got=%#v\nwant=%#v", scalars, tt.want)
			}
		})
	}
}

func TestFileToValueArrayRoot(t *testing.T) {
	// A top-level array must be accepted as a document root, not just an
	// object -- see issue #1017. dasel can navigate either.
	tests := []struct {
		name string
		ext  string
		src  string
		expr string
	}{
		{
			name: "json array of objects",
			ext:  ".json",
			src:  "[\n  {\"title\": \"first\"},\n  {\"title\": \"second\"}\n]\n",
			expr: "all().title",
		},
		{
			name: "yaml sequence of mappings",
			ext:  ".yaml",
			src:  "- title: first\n- title: second\n",
			expr: "all().title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &File{Content: tt.src, RealExt: tt.ext}
			value, _, err := fileToValue(f)
			if err != nil {
				t.Fatalf("fileToValue: %v", err)
			}
			got, serr := selectStrings(value, tt.expr)
			if serr != nil {
				t.Fatalf("selectStrings: %v", serr)
			}
			want := []string{"first", "second"}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("selectStrings = %v, want %v", got, want)
			}
		})
	}
}

func TestScalarResolverConsumesInOrder(t *testing.T) {
	scalars := []scalarPos{
		{Value: "shared", Line: 1, Column: 5},
		{Value: "unique", Line: 2, Column: 5},
		{Value: "shared", Line: 3, Column: 5},
	}
	r := newScalarResolver(scalars)

	if sp, ok := r.locate("shared"); !ok || sp.Line != 1 || sp.Column != 5 {
		t.Errorf("first 'shared' = %+v, want (1,5)", sp)
	}
	if sp, ok := r.locate("shared"); !ok || sp.Line != 3 || sp.Column != 5 {
		t.Errorf("second 'shared' = %+v, want (3,5)", sp)
	}
	if sp, ok := r.locate("unique"); !ok || sp.Line != 2 || sp.Column != 5 {
		t.Errorf("'unique' = %+v, want (2,5)", sp)
	}
	if sp, ok := r.locate("missing"); ok {
		t.Errorf("missing = %+v, want not found", sp)
	}
}

func TestFoldedToLiteralRewritesIndicator(t *testing.T) {
	// `>` indicators on folded scalars should be rewritten to `|` so the
	// parsed value preserves newlines. `|` blocks and `>` characters
	// inside string values must be left alone.
	src := []byte("a: >\n  one\n  two\nb: |\n  literal\nc: \"x > y\"\n")
	out, err := foldedToLiteral(src)
	if err != nil {
		t.Fatalf("foldedToLiteral: %v", err)
	}
	got := string(out)
	want := "a: |\n  one\n  two\nb: |\n  literal\nc: \"x > y\"\n"
	if got != want {
		t.Errorf("rewritten output mismatch\n got=%q\nwant=%q", got, want)
	}
}

func TestFileToValueLineContinuationFromIssue1018(t *testing.T) {
	// Reproduces the original issue #1018 report: a double-quoted scalar
	// using `\<newline>` line continuation. Before this fix, vale could
	// not locate "Line 1 Line 2" in the source and erred with E100.
	f := &File{
		RealExt: ".yaml",
		Content: "openapi: 3.0.1\ninfo:\n  description: \"Line 1\\\n    \\ Line 2\"\n",
	}
	value, scalars, err := fileToValue(f)
	if err != nil {
		t.Fatalf("fileToValue: %v", err)
	}

	got, err := selectStrings(value, "info.description")
	if err != nil {
		t.Fatalf("selectStrings: %v", err)
	}
	if len(got) != 1 || got[0] != "Line 1 Line 2" {
		t.Fatalf("selectStrings = %v, want [\"Line 1 Line 2\"]", got)
	}

	r := newScalarResolver(scalars)
	// Walk past the openapi version scalar first.
	r.locate("3.0.1")
	sp, ok := r.locate("Line 1 Line 2")
	if !ok || sp.Line != 3 || sp.Column != 17 {
		t.Errorf("description position = %+v, want (3,17)", sp)
	}
}

// A selector that fails in both dialects reports both causes, without the
// wrapping dasel adds at every level.
func TestSelectStringsReportsBothDialects(t *testing.T) {
	value := map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": "asked"},
		map[string]any{"role": "assistant", "content": "answered"},
	}}

	_, err := selectStrings(value, `messages.filter(role == "assistant").content`)
	if err == nil {
		t.Fatal("expected an error")
	}

	msg := err.Error()
	for _, want := range []string{
		"unexpected type: expected map, got array",
		"as a v2 selector: cannot use property selector on non map/struct types",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
	if strings.Contains(msg, "ast.") {
		t.Errorf("error %q still carries dasel's wrapping", msg)
	}
}

// A selector in either dialect is accepted.
func TestSelectStringsEitherDialect(t *testing.T) {
	value := map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": "asked"},
		map[string]any{"role": "assistant", "content": "answered"},
	}}

	for _, expr := range []string{
		`messages.all().filter(equal(role,assistant)).content`,
		`messages.filter($this.role == "assistant").map(content)`,
	} {
		got, err := selectStrings(value, expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if len(got) != 1 || got[0] != "answered" {
			t.Errorf("%s = %v, want [answered]", expr, got)
		}
	}
}

// A value the template never fills still yields its scope, as one empty
// value at the top of the file, so a rule that requires text can report it.
func TestApplyTemplateUnfilledValue(t *testing.T) {
	view := &View{
		Engine: "textfsm",
		Template: `Value Subject (.+)
Value List Trailer ([A-Z][\w-]+: .+)

Start
  ^${Trailer}
  ^${Subject}
`,
		Scopes: []Scope{{Name: "subject", Expr: "Subject"}, {Name: "trailer", Expr: "Trailer"}},
	}
	if err := view.compileTemplate(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, content string
		want          []ScopedValue
	}{
		{"missing", "fix: a thing\n", []ScopedValue{{Text: "", Line: 1, Column: 1}}},
		{"present", "fix: a thing\n\nSigned-off-by: J <j@x.y>\n",
			[]ScopedValue{{Text: "Signed-off-by: J <j@x.y>", Line: 3, Column: 1}}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, err := view.applyTemplate(&File{Content: c.content})
			if err != nil {
				t.Fatal(err)
			}
			var trailer []ScopedValue
			for _, sv := range found {
				if sv.Scope == "trailer" {
					trailer = sv.Values
				}
			}
			if !reflect.DeepEqual(trailer, c.want) {
				t.Errorf("trailer = %#v, want %#v", trailer, c.want)
			}
		})
	}
}

// A file a section matched is read whatever it is called; JSON is YAML.
func TestFileToValueUnknownExtension(t *testing.T) {
	f := &File{RealExt: ".log", Content: `{"messages":[{"role":"assistant","content":"answered"}]}`}

	value, _, err := fileToValue(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := selectStrings(value, "messages.all().content")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "answered" {
		t.Errorf("got %v, want [answered]", got)
	}
}

// With `join`, a list of strings is one value at the first element's line.
func TestApplyJoinsList(t *testing.T) {
	src := `{
 "cells": [
  {"cell_type": "markdown", "source": [
    "# Title\n",
    "\n",
    "A paragraph split\n",
    "across lines."
  ]},
  {"cell_type": "code", "source": ["print(1)"]},
  {"cell_type": "markdown", "source": "One string cell."}
 ]
}
`
	sep := ""
	view := &View{Engine: "dasel", Scopes: []Scope{{
		Name: "cell", Expr: "cells.all().filter(equal(cell_type,markdown)).source", Type: "md", Join: &sep,
	}}}

	found, err := view.Apply(&File{RealExt: ".ipynb", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	want := []ScopedValue{
		{Text: "# Title\n\nA paragraph split\nacross lines.", Line: 4, Column: 6, Joined: true},
		{Text: "One string cell.", Line: 10, Column: 40},
	}
	got := make([]ScopedValue, len(found[0].Values))
	for i, v := range found[0].Values {
		v.Parts = nil
		got[i] = v
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("values = %#v, want %#v", got, want)
	}

	// Each element is a part of its own, on its own line.
	lines := []int{}
	for _, p := range found[0].Values[0].Parts {
		lines = append(lines, p.Line)
	}
	if !reflect.DeepEqual(lines, []int{4, 5, 6, 7}) {
		t.Errorf("part lines = %v, want [4 5 6 7]", lines)
	}

	// Without `join`, the list is dropped and the string cell stays.
	view.Scopes[0].Join = nil
	found, err = view.Apply(&File{RealExt: ".ipynb", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(found[0].Values) != 1 || found[0].Values[0].Text != "One string cell." {
		t.Errorf("values = %#v, want the string cell alone", found[0].Values)
	}
}

// Locate maps an offset in a value to the source: through the escapes of a
// quoted scalar, across the elements of a joined list however they are laid
// out, and down the lines of a block scalar.
func TestLocate(t *testing.T) {
	sep := ""
	md := "md"
	view := &View{Engine: "dasel", Scopes: []Scope{
		{Name: "cell", Expr: "cells.all().source", Type: md, Join: &sep},
		{Name: "note", Expr: "note"},
	}}

	src := `{
 "cells": [
  {"source": ["a \"quoted\" word\n", "\ttab \u00e9 end"]},
  {"source": ["one", " line\n", "two"]}
 ],
 "note": "plain \\ back"
}
`
	found, err := view.Apply(&File{RealExt: ".ipynb", Content: src})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		scope, what string
		value       ScopedValue
		off         int
		line, col   int
	}{
		// Line 3, col 16 is the `a`; `word` is at col 29 in the source,
		// after two escaped quotes, and at rune 11 in the value.
		{"cell", "before an escape", found[0].Values[0], 0, 3, 16},
		{"cell", "after two escapes", found[0].Values[0], 11, 3, 29},
		// The second element starts on the same line, at col 39 after the
		// closing quote, comma, space, and opening quote.
		{"cell", "second element", found[0].Values[0], 16, 3, 39},
		// `end` is rune 7 of the element: tab, "tab ", é, space; in the
		// source, \t and \u00e9 are wider.
		{"cell", "after unicode escapes", found[0].Values[0], 23, 3, 52},
		// Three elements make two value lines; "two" is the third element.
		{"cell", "element mid-line", found[0].Values[1], 3, 4, 23},
		{"cell", "element on the next value line", found[0].Values[1], 9, 4, 34},
		{"note", "escaped backslash", found[1].Values[0], 8, 6, 20},
	}
	for _, tt := range tests {
		line, col := tt.value.Locate(tt.off)
		if line != tt.line || col != tt.col {
			t.Errorf("%s/%s: offset %d = (%d,%d), want (%d,%d)",
				tt.scope, tt.what, tt.off, line, col, tt.line, tt.col)
		}
	}
}

// A block scalar's lines are source lines, each at the block's indentation.
func TestLocateBlockScalar(t *testing.T) {
	view := &View{Engine: "dasel", Scopes: []Scope{{Name: "d", Expr: "info.description"}}}
	src := "info:\n  description: |\n    First line\n    Second line\n"

	found, err := view.Apply(&File{RealExt: ".yaml", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	v := found[0].Values[0]
	if line, col := v.Locate(0); line != 3 || col != 5 {
		t.Errorf("first line = (%d,%d), want (3,5)", line, col)
	}
	if line, col := v.Locate(18); line != 4 || col != 12 {
		t.Errorf("second line = (%d,%d), want (4,12)", line, col)
	}
}
