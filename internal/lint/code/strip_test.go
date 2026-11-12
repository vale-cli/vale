//go:build !vale_nocode

package code

import (
	"reflect"
	"testing"
)

// Strip records, per line of Text, everything extraction took off the front
// of the Source line: the opener, the indentation, and a line comment's own
// space, so a column in Text is put back with one addition.
func TestStripRecordsWhatCameOff(t *testing.T) {
	src := "/** one ZEBRA here */\n    /** two ZEBRA here */\n/** three ZEBRA\n * four ZEBRA\n */\nx();\n    // five ZEBRA\n    //   six ZEBRA\ny();\n"
	comments, err := GetComments([]byte(src), JavaScript())
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		text  string
		strip []int
	}{
		// A one-liner keeps the space before its closer; `/** ` came off.
		{"one ZEBRA here ", []int{4}},
		{"two ZEBRA here ", []int{4}},
		// The ` *` decoration and the closer line's ` ` came off with the dedent.
		{"three ZEBRA\nfour ZEBRA\n\n", []int{4, 3, 1}},
		// A run of line comments: each line loses its `//` and all its spaces.
		{"five ZEBRA\nsix ZEBRA\n", []int{3, 5}},
	}
	if len(comments) != len(want) {
		t.Fatalf("got %d comments, want %d: %+v", len(comments), len(want), comments)
	}
	for i, w := range want {
		if comments[i].Text != w.text {
			t.Errorf("comment %d: Text = %q, want %q", i, comments[i].Text, w.text)
		}
		if !reflect.DeepEqual(comments[i].Strip, w.strip) {
			t.Errorf("comment %d: Strip = %v, want %v", i, comments[i].Strip, w.strip)
		}
	}
}
