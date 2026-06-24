package core

import "testing"

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
