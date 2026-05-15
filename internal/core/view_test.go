package core

import (
	"reflect"
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
			want: []scalarPos{{Value: "Line 1 Line 2", Line: 2, Column: 17}},
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
			want: []scalarPos{{Value: "sample", Line: 1, Column: 9}},
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

func TestScalarResolverConsumesInOrder(t *testing.T) {
	scalars := []scalarPos{
		{Value: "shared", Line: 1, Column: 5},
		{Value: "unique", Line: 2, Column: 5},
		{Value: "shared", Line: 3, Column: 5},
	}
	r := newScalarResolver(scalars)

	if line, col := r.locate("shared"); line != 1 || col != 5 {
		t.Errorf("first 'shared' = (%d,%d), want (1,5)", line, col)
	}
	if line, col := r.locate("shared"); line != 3 || col != 5 {
		t.Errorf("second 'shared' = (%d,%d), want (3,5)", line, col)
	}
	if line, col := r.locate("unique"); line != 2 || col != 5 {
		t.Errorf("'unique' = (%d,%d), want (2,5)", line, col)
	}
	if line, col := r.locate("missing"); line != 0 || col != 0 {
		t.Errorf("missing = (%d,%d), want (0,0)", line, col)
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
	line, col := r.locate("Line 1 Line 2")
	if line != 3 || col != 17 {
		t.Errorf("description position = (%d,%d), want (3,17)", line, col)
	}
}
