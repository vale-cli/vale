package spell

import (
	"strings"
	"testing"
)

func TestParseFlagsASCII(t *testing.T) {
	dc := dictConfig{Flag: "ASCII"}
	flags := dc.parseFlags("ABC")
	if len(flags) != 3 || flags[0] != "A" || flags[1] != "B" || flags[2] != "C" {
		t.Errorf("ASCII parseFlags(%q) = %v, want [A B C]", "ABC", flags)
	}
}

func TestParseFlagsNum(t *testing.T) {
	dc := dictConfig{Flag: "num"}
	flags := dc.parseFlags("14308,10482,4720")
	if len(flags) != 3 || flags[0] != "14308" || flags[1] != "10482" || flags[2] != "4720" {
		t.Errorf("num parseFlags(%q) = %v, want [14308 10482 4720]", "14308,10482,4720", flags)
	}
}

func TestParseFlagsLong(t *testing.T) {
	dc := dictConfig{Flag: "long"}
	flags := dc.parseFlags("AABB")
	if len(flags) != 2 || flags[0] != "AA" || flags[1] != "BB" {
		t.Errorf("long parseFlags(%q) = %v, want [AA BB]", "AABB", flags)
	}
}

func TestParseFlagsUTF8(t *testing.T) {
	dc := dictConfig{Flag: "UTF-8"}
	flags := dc.parseFlags("AğB")
	if len(flags) != 3 || flags[0] != "A" || flags[1] != "ğ" || flags[2] != "B" {
		t.Errorf("UTF-8 parseFlags(%q) = %v, want [A ğ B]", "AğB", flags)
	}
}

func TestFlagNumAffixParsing(t *testing.T) {
	// Minimal FLAG num AFF file
	affContent := `SET UTF-8
FLAG num

SFX 100 N 1
SFX 100 0 ler .

SFX 200 N 1
SFX 200 0 in .
`
	aff, err := newDictConfig(strings.NewReader(affContent))
	if err != nil {
		t.Fatalf("newDictConfig error: %v", err)
	}

	if aff.Flag != "num" {
		t.Errorf("Flag = %q, want %q", aff.Flag, "num")
	}

	// Check that affix 100 exists with "ler" suffix
	a100, ok := aff.AffixMap["100"]
	if !ok {
		t.Fatal("AffixMap missing flag 100")
	}
	if len(a100.Rules) != 1 || a100.Rules[0].AffixText != "ler" {
		t.Errorf("flag 100 rules = %v, want [{ler}]", a100.Rules)
	}

	// Check that affix 200 exists with "in" suffix
	a200, ok := aff.AffixMap["200"]
	if !ok {
		t.Fatal("AffixMap missing flag 200")
	}
	if len(a200.Rules) != 1 || a200.Rules[0].AffixText != "in" {
		t.Errorf("flag 200 rules = %v, want [{in}]", a200.Rules)
	}
}

func TestNoSuggestLongFlag(t *testing.T) {
	// French dictionaries declare `FLAG long` and use `--` as the NOSUGGEST
	// flag. This previously failed to parse ("NOSUGGEST stanza had more than
	// one flag"). See #862.
	affContent := `SET UTF-8
FLAG long
NOSUGGEST --

SFX Aa Y 1
SFX Aa 0 s .
`
	aff, err := newDictConfig(strings.NewReader(affContent))
	if err != nil {
		t.Fatalf("newDictConfig error: %v", err)
	}
	if aff.NoSuggestFlag != "--" {
		t.Errorf("NoSuggestFlag = %q, want %q", aff.NoSuggestFlag, "--")
	}
	if _, ok := aff.AffixMap["Aa"]; !ok {
		t.Error("AffixMap missing long flag 'Aa'")
	}
}

func TestFlagNumExpand(t *testing.T) {
	affContent := `SET UTF-8
FLAG num

SFX 100 N 1
SFX 100 0 ler .

SFX 200 N 1
SFX 200 0 in .
`
	aff, err := newDictConfig(strings.NewReader(affContent))
	if err != nil {
		t.Fatalf("newDictConfig error: %v", err)
	}

	// "belge/100,200" should expand to: belge, belgeler, belgein
	words, err := aff.expand("belge/100,200", nil)
	if err != nil {
		t.Fatalf("expand error: %v", err)
	}

	expected := map[string]bool{"belge": true, "belgeler": true, "belgein": true}
	for _, w := range words {
		if !expected[w] {
			t.Errorf("unexpected word %q in expansion", w)
		}
		delete(expected, w)
	}
	for w := range expected {
		t.Errorf("missing expected word %q", w)
	}
}

func TestFlagNumGoSpellReader(t *testing.T) {
	affContent := `SET UTF-8
FLAG num

SFX 100 N 1
SFX 100 0 ler .

SFX 200 N 1
SFX 200 0 nin .
`
	dicContent := `2
belge/100,200
sistem/100,200
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	tests := []struct {
		word string
		want bool
	}{
		{"belge", true},
		{"belgeler", true},
		{"belgenin", true},
		{"sistem", true},
		{"sistemler", true},
		{"sistemnin", true},
		{"bilinmeyen", false},
	}

	for _, tt := range tests {
		got := gs.spell(tt.word)
		if got != tt.want {
			t.Errorf("spell(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

// TestDanishDictionary covers the three parsing bugs exposed by the
// stavekontrolden.dk Danish dictionary (see #1065):
//
//  1. Affixes that carry their own continuation flags (e.g. `t/34,22`) must
//     have those flags stripped from the generated word.
//  2. Space-separated morphological fields (e.g. `coitus/10,39,31 al:coituum`)
//     must be stripped so they don't corrupt FLAG num parsing.
//  3. A single malformed entry (e.g. `/34 st:coitus`, flags but no word) must
//     not abort the whole dictionary, which would flag every word.
func TestDanishDictionary(t *testing.T) {
	affContent := `SET UTF-8
FLAG num

SFX 1 Y 1
SFX 1 0 t/34,22 e

SFX 34 Y 1
SFX 34 0 s .
`
	// Mirrors the real file: space-separated morphology, plus a malformed
	// orphan-slash entry split off from its word.
	dicContent := `3
stave/1,34 al:stave
coituum
/34 st:coitus
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	tests := []struct {
		word string
		want bool
	}{
		{"stave", true},   // base word, morphology stripped
		{"stavet", true},  // SFX 1 with continuation flags stripped
		{"staves", true},  // SFX 34
		{"coituum", true}, // word before the malformed line still loaded
		{"thtis", false},  // a genuine misspelling is still caught
	}

	for _, tt := range tests {
		if got := gs.spell(tt.word); got != tt.want {
			t.Errorf("spell(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

func TestASCIFlagBackwardCompatibility(t *testing.T) {
	// Original ASCII flag format must still work
	affContent := `SET UTF-8

SFX A N 1
SFX A 0 s .

SFX B N 1
SFX B 0 ed .
`
	dicContent := `1
test/AB
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	tests := []struct {
		word string
		want bool
	}{
		{"test", true},
		{"tests", true},
		{"tested", true},
		{"testing", false},
	}

	for _, tt := range tests {
		got := gs.spell(tt.word)
		if got != tt.want {
			t.Errorf("spell(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

func TestCompoundSegmentation(t *testing.T) {
	// A dictionary that enables affix-flag compounding should accept words
	// that split into dictionary segments (e.g. German "Funktionswert"). See
	// #848.
	dic := "2\nfoo\nbar\n"

	withFlags := "SET UTF-8\nCOMPOUNDFLAG A\nCOMPOUNDMIN 2\n"
	gs, err := newGoSpellReader(strings.NewReader(withFlags), strings.NewReader(dic))
	if err != nil {
		t.Fatal(err)
	}
	if !gs.spell("foobar") {
		t.Error("expected 'foobar' (foo+bar) to be accepted as a compound")
	}
	if gs.spell("fooqux") {
		t.Error("expected 'fooqux' (qux not a word) to be rejected")
	}

	// Without compound flags, no segmentation happens (English behavior).
	noFlags := "SET UTF-8\n"
	gs2, err := newGoSpellReader(strings.NewReader(noFlags), strings.NewReader(dic))
	if err != nil {
		t.Fatal(err)
	}
	if gs2.spell("foobar") {
		t.Error("expected 'foobar' to be rejected when compounding is disabled")
	}
}
