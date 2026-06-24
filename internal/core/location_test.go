package core

import "testing"

// A punctuation-only match in a block whose text was altered by inline markup
// (e.g. a stripped code span) must still be located within that block, not at
// the first occurrence anywhere in the document -- see #994.
func TestInitialPositionPunctAnchor(t *testing.T) {
	// ctx keeps the raw code span; txt is the extracted sentence without it.
	ctx := "Test, line.\nLine, with, four, commas, `yes`.\n"
	txt := "Line, with, four, commas, yes."

	pos, sub := initialPosition(ctx, txt, Alert{Match: ","})
	// The comma belongs to the second sentence (after "Line"), not the first
	// comma in "Test,". Position is 1-based rune count.
	if pos != 17 {
		t.Errorf("pos = %d, want 17 (second sentence)", pos)
	}
	if sub != "," {
		t.Errorf("sub = %q, want %q", sub, ",")
	}
}

func TestIsPunctOnly(t *testing.T) {
	cases := map[string]bool{
		",":      true,
		"...":    true,
		"":       false,
		"hav":    false,
		"that's": false,
		"OAuth2": false,
	}
	for in, want := range cases {
		if got := isPunctOnly(in); got != want {
			t.Errorf("isPunctOnly(%q) = %v, want %v", in, got, want)
		}
	}
}

// A match whose smart apostrophe/quote was normalized to ASCII (as
// spell-checking does) must still be located in the original source. Before
// the fix, the straight-apostrophe match couldn't be found in smart-apostrophe
// text, so the alert was dropped -- see #1003.
func TestInitialPositionSmartApostrophe(t *testing.T) {
	straight := "The toolkit's plugin." // a.Match is always normalized
	smart := "The toolkit’s plugin."    // source keeps the smart form

	tests := []struct {
		name string
		ctx  string
	}{
		{"straight source", straight},
		{"smart source", smart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, sub := initialPosition(tt.ctx, tt.ctx, Alert{Match: "toolkit's"})
			if pos != 5 {
				t.Errorf("pos = %d, want 5", pos)
			}
			if sub != "toolkit's" {
				t.Errorf("sub = %q, want %q", sub, "toolkit's")
			}
		})
	}
}
