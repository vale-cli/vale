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
