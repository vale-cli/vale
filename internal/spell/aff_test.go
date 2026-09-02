package spell

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseFlagsASCII(t *testing.T) {
	dc := dictConfig{Flag: "ASCII"}
	flags := dc.parseFlags("ABC")
	if len(flags) != 3 || flags[0] != "A" || flags[1] != "B" || flags[2] != "C" {
		t.Errorf("ASCII parseFlags(%q) = %v, want [A B C]", "ABC", flags)
	}
}

// TestParseFlagsDefaultEightBit verifies that the default flag mode treats a
// UTF-8 sequence as separate eight-bit flags.
func TestParseFlagsDefaultEightBit(t *testing.T) {
	dc := dictConfig{Flag: "ASCII"}
	flagStr := "\xc3\xa9"
	flags := dc.parseFlags(flagStr)
	if len(flags) != 2 || flags[0] != "\xc3" || flags[1] != "\xa9" {
		t.Errorf("ASCII parseFlags(%q) = %q, want [\\xc3 \\xa9]", flagStr, flags)
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

// TestFlagAliasesParsing verifies that AF directives retain their declared
// order and support Unicode flag vectors.
func TestFlagAliasesParsing(t *testing.T) {
	affContent := `SET UTF-8
FLAG UTF-8
AF 2
AF A
AF AŐ # second alias
`

	aff, err := newDictConfig(strings.NewReader(affContent))
	if err != nil {
		t.Fatalf("newDictConfig error: %v", err)
	}

	if len(aff.FlagAliases) != 2 {
		t.Fatalf("FlagAliases = %v, want [A AŐ]", aff.FlagAliases)
	}
	if aff.FlagAliases[0] != "A" || aff.FlagAliases[1] != "AŐ" {
		t.Errorf("FlagAliases = %v, want [A AŐ]", aff.FlagAliases)
	}
}

// TestFlagAliasIndexing verifies that dictionary alias numbers select the
// corresponding one-based AF entry.
func TestFlagAliasIndexing(t *testing.T) {
	affContent := `SET UTF-8
AF 2
AF A
AF B

SFX A N 1
SFX A 0 s .

SFX B N 1
SFX B 0 ed .
`
	dicContent := `2
first/1
second/2
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
		{"first", true},
		{"firsts", true},
		{"firsted", false},
		{"second", true},
		{"seconds", false},
		{"seconded", true},
	}

	for _, tt := range tests {
		if got := gs.spell(tt.word); got != tt.want {
			t.Errorf("spell(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

// TestFlagAliasWithMultipleFlags verifies that one AF alias can enable more
// than one affix class.
func TestFlagAliasWithMultipleFlags(t *testing.T) {
	affContent := `SET UTF-8
AF 1
AF AB

SFX A N 1
SFX A 0 s .

SFX B N 1
SFX B 0 ed .
`
	dicContent := `1
root/1
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	for _, word := range []string{"root", "roots", "rooted"} {
		if !gs.spell(word) {
			t.Errorf("spell(%q) = false, want true", word)
		}
	}
}

// TestDefaultEightBitFlagExpansion verifies that a non-ASCII byte can identify
// an affix class in Hunspell's default flag mode.
func TestDefaultEightBitFlagExpansion(t *testing.T) {
	const flag = "\xc1"
	affContent := "SET UTF-8\n" +
		"SFX " + flag + " N 1\n" +
		"SFX " + flag + " 0 s .\n"
	dicContent := "1\nword/" + flag + "\n"

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	if !gs.spell("words") {
		t.Error("spell(\"words\") = false, want true")
	}
}

// TestUTF8FlagExpansion verifies that FLAG UTF-8 permits a Unicode code point
// to identify an affix class.
func TestUTF8FlagExpansion(t *testing.T) {
	affContent := `SET UTF-8
FLAG UTF-8

SFX Ő N 1
SFX Ő 0 s .
`
	dicContent := `1
word/Ő
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	if !gs.spell("words") {
		t.Error("spell(\"words\") = false, want true")
	}
}

// TestFlagAliasWithContinuationClass verifies that a numeric continuation is
// resolved through the AF table instead of being treated as a literal flag.
func TestFlagAliasWithContinuationClass(t *testing.T) {
	affContent := `SET UTF-8
AF 2
AF A
AF B

SFX A N 1
SFX A 0 ed/2 .

SFX B N 1
SFX B 0 ly .

SFX 2 N 1
SFX 2 0 wrong .
`
	dicContent := `1
root/1
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	for _, word := range []string{"root", "rooted", "rootedly"} {
		if !gs.spell(word) {
			t.Errorf("spell(%q) = false, want true", word)
		}
	}
	if gs.spell("rootedwrong") {
		t.Error("continuation alias was treated as a literal flag")
	}
}

// TestZeroAffixWithContinuationFlags verifies that a zero affix is normalized
// to empty text before its continuation flags are applied.
func TestZeroAffixWithContinuationFlags(t *testing.T) {
	affContent := `SET UTF-8
PFX L Y 1
PFX L 0 l' .

SFX F Y 1
SFX F 0 0/L .
`
	dicContent := `1
ordinateur/F
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
		{"l'ordinateur", true},
		{"ordinateur0", false},
		{"l'ordinateur0", false},
	}
	for _, tt := range tests {
		if got := gs.spell(tt.word); got != tt.want {
			t.Errorf("spell(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

func TestPrefixStrip(t *testing.T) {
	affContent := `SET UTF-8
PFX A N 1
PFX A a l'A a
`
	dicContent := `1
ami/A
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
		{"l'Ami", true},
		{"l'Aami", false},
	}
	for _, tt := range tests {
		if got := gs.spell(tt.word); got != tt.want {
			t.Errorf("spell(%q) = %v, want %v", tt.word, got, tt.want)
		}
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

// TestDefaultFlagAffixUsesSingleByteIdentifier verifies that directives naming
// one flag use a single byte in the default flag mode.
func TestDefaultFlagAffixUsesSingleByteIdentifier(t *testing.T) {
	// The Italian dictionary is UTF-8 text but uses Hunspell's default 8-bit
	// flag mode. Its `d/£$` entry and `SFX £` class must therefore resolve
	// through the same first-byte flag identifier, as they do in Hunspell.
	affContent := `SET UTF-8

SFX £ Y 1
SFX £ 0 i [ivxlcdm]
`
	dicContent := `1
d/£$
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}
	if !gs.spell("di") {
		t.Error("spell(\"di\") = false, want true")
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
		{"stavets", true}, // SFX 1, then its continuation into SFX 34
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

// TestCompoundRuleIncludesContinuationForms verifies that forms produced by a
// continuation class participate in COMPOUNDRULE matching.
func TestCompoundRuleIncludesContinuationForms(t *testing.T) {
	affContent := `SET UTF-8

COMPOUNDRULE 1
COMPOUNDRULE CD

SFX A N 1
SFX A 0 s/C .
`
	dicContent := `2
root/A
word/D
`
	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	if !gs.spell("rootsword") {
		t.Error("spell(\"rootsword\") = false, want true")
	}
	if gs.spell("rootword") {
		t.Error("spell(\"rootword\") = true, want false")
	}
}

func TestConditionlessAffixRule(t *testing.T) {
	// OpenTaal's Dutch dictionary writes affix rules without the (optional)
	// condition field, e.g. `SFX CA 0 /CaCp`. A 4-field rule must not be
	// mistaken for a header and parsed as a cross-product flag ("CrossProduct
	// is not Y or N: got 0"). See #776.
	aff := "SET UTF-8\nFLAG long\nSFX Xx Y 1\nSFX Xx 0 s\n"
	dic := "1\nkat/Xx\n"

	gs, err := newGoSpellReader(strings.NewReader(aff), strings.NewReader(dic))
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}
	if !gs.spell("kat") {
		t.Error("expected base word 'kat' to be recognized")
	}
	if !gs.spell("kats") {
		t.Error("expected suffixed 'kats' (conditionless SFX rule) to be recognized")
	}
}

// TestHungarianAffixConditionsWithLiteralHyphens verifies that representative
// Hungarian character classes containing hyphens match Hunspell semantics.
func TestHungarianAffixConditionsWithLiteralHyphens(t *testing.T) {
	affContent := `SET UTF-8
FLAG UTF-8

SFX A N 1
SFX A 0 x [áéiíoóőuúůüű-ø]

SFX B N 1
SFX B 0 y [áéiíóőuúůüű-àùø]
`
	dicContent := `7
tű/AB
tø/AB
tő/AB
tà/AB
tù/AB
ta/AB
tz/AB
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
		{"tűx", true},
		{"tøx", true},
		{"tőx", true},
		{"tàx", false},
		{"tùx", false},
		{"tax", false},
		{"tzx", false},
		{"tűy", true},
		{"tøy", true},
		{"tőy", true},
		{"tày", true},
		{"tùy", true},
		{"tay", false},
		{"tzy", false},
	}

	for _, tt := range tests {
		if got := gs.spell(tt.word); got != tt.want {
			t.Errorf("spell(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

// TestAffixConditionDoesNotTreatHyphenAsRange verifies that a hyphen inside a
// Hunspell character class is matched literally rather than as a range.
func TestAffixConditionDoesNotTreatHyphenAsRange(t *testing.T) {
	affContent := `SET UTF-8

SFX A N 1
SFX A 0 s [a-z]
`
	dicContent := `4
a/A
-/A
z/A
m/A
`

	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	for _, word := range []string{"as", "-s", "zs"} {
		if !gs.spell(word) {
			t.Errorf("spell(%q) = false, want true", word)
		}
	}
	if gs.spell("ms") {
		t.Error("spell(\"ms\") = true, want false")
	}
}

// TestDictionaryEntriesAreExpandedLazily verifies that derived forms are
// recognized without being materialized in the stored dictionary.
func TestDictionaryEntriesAreExpandedLazily(t *testing.T) {
	const ruleCount = 40
	var affContent strings.Builder
	affContent.WriteString("SET UTF-8\n\nSFX A Y 40\n")
	for i := range ruleCount {
		fmt.Fprintf(&affContent, "SFX A 0 a%d/B .\n", i)
	}
	affContent.WriteString("\nSFX B Y 40\n")
	for i := range ruleCount {
		fmt.Fprintf(&affContent, "SFX B 0 b%d .\n", i)
	}

	gs, err := newGoSpellReader(
		strings.NewReader(affContent.String()),
		strings.NewReader("1\nroot/A\n"),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	if got := len(gs.dict); got != 1 {
		t.Errorf("materialized dictionary size = %d, want only 1 root", got)
	}
	for _, word := range []string{"root", "roota0", "roota39b39"} {
		if !gs.spell(word) {
			t.Errorf("spell(%q) = false, want true", word)
		}
	}
	if gs.spell("roota40b40") {
		t.Error("spell(\"roota40b40\") = true, want false")
	}
}

// TestLazyCrossProduct verifies that lazy expansion preserves valid prefix and
// suffix cross-products.
func TestLazyCrossProduct(t *testing.T) {
	affContent := `SET UTF-8

PFX P Y 1
PFX P 0 re .

SFX S Y 1
SFX S 0 s .
`
	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader("1\nroot/PS\n"),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	for _, word := range []string{"root", "reroot", "roots", "reroots"} {
		if !gs.spell(word) {
			t.Errorf("spell(%q) = false, want true", word)
		}
	}
}

// TestLazyLookupAtMaximumAffixDepth verifies that continuation expansion
// reaches the configured depth limit without exceeding it.
func TestLazyLookupAtMaximumAffixDepth(t *testing.T) {
	affContent := `SET UTF-8
FLAG num

PFX 1 Y 1
PFX 1 0 p0 .
SFX 2 Y 1
SFX 2 0 s0/3,4 .

PFX 3 Y 1
PFX 3 0 p1 .
SFX 4 Y 1
SFX 4 0 s1/5,6 .

PFX 5 Y 1
PFX 5 0 p2 .
SFX 6 Y 1
SFX 6 0 s2 .
`
	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader("1\nroot/1,2\n"),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	if !gs.spell("p2p1p0roots0s1s2") {
		t.Error("spell at maximum affix depth = false, want true")
	}
}

// TestLazyLookupPreservesSuffixStripBehavior verifies that reverse lookup
// mirrors the existing forward behavior for suffix strip rules.
func TestLazyLookupPreservesSuffixStripBehavior(t *testing.T) {
	affContent := `SET UTF-8

SFX A N 2
SFX A e 0 e
SFX A ing s .
`
	dicContent := `2
make/A
root/A
`
	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	for _, word := range []string{"mak", "roots"} {
		if !gs.spell(word) {
			t.Errorf("spell(%q) = false, want true", word)
		}
	}
}

// TestLazySuggestionIncludesDerivedWord verifies that edit-distance candidates
// generated through affixes can be returned as suggestions.
func TestLazySuggestionIncludesDerivedWord(t *testing.T) {
	affContent := `SET UTF-8

SFX A N 1
SFX A 0 s .
`
	dicContent := `6
root/A
alpha
bravo
charlie
delta
echo
`
	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	found := false
	for _, match := range gs.suggest("rootss") {
		if match.word == "roots" {
			found = true
			break
		}
	}
	if !found {
		t.Error("suggest(\"rootss\") does not include derived word \"roots\"")
	}
}

// TestLazyHomographsDoNotCombineFlags verifies that separate entries for the
// same root do not combine their flags into an invalid expansion.
func TestLazyHomographsDoNotCombineFlags(t *testing.T) {
	affContent := `SET UTF-8

PFX P Y 1
PFX P 0 re .

SFX S Y 1
SFX S 0 s .
`
	dicContent := `2
root/P
root/S
`
	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader(dicContent),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	for _, word := range []string{"root", "reroot", "roots"} {
		if !gs.spell(word) {
			t.Errorf("spell(%q) = false, want true", word)
		}
	}
	if gs.spell("reroots") {
		t.Error("spell(\"reroots\") = true, want false")
	}
}

// TestLazySpellConcurrent verifies that lazy lookups and their caches are safe
// when spell checks run concurrently.
func TestLazySpellConcurrent(t *testing.T) {
	affContent := `SET UTF-8

SFX A Y 1
SFX A 0 ed/B .

SFX B Y 1
SFX B 0 ly .
`
	gs, err := newGoSpellReader(
		strings.NewReader(affContent),
		strings.NewReader("1\nroot/A\n"),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}

	words := map[string]bool{
		"root":       true,
		"rooted":     true,
		"rootedly":   true,
		"rootedness": false,
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				for word, want := range words {
					if got := gs.spell(word); got != want {
						t.Errorf("spell(%q) = %v, want %v", word, got, want)
					}
				}
			}
		}()
	}
	wait.Wait()
}

// TestLazySpellCacheIsBounded verifies that lazy spelling results cannot grow
// the cache beyond its configured capacity.
func TestLazySpellCacheIsBounded(t *testing.T) {
	cache := newSpellCache()
	for index := 0; index <= lazySpellCacheSize; index++ {
		cache.set(fmt.Sprintf("word-%d", index), index%2 == 0)
	}

	if got := len(cache.values); got != lazySpellCacheSize {
		t.Errorf("cache size = %d, want %d", got, lazySpellCacheSize)
	}
	if _, found := cache.get("word-0"); found {
		t.Error("oldest cache entry was not evicted")
	}
	if value, found := cache.get(fmt.Sprintf("word-%d", lazySpellCacheSize)); !found || !value {
		t.Error("newest cache entry is missing")
	}
}

// TestSuggestionWithSmallDictionary verifies suggestion generation when the
// dictionary contains fewer entries than the requested result limit.
func TestSuggestionWithSmallDictionary(t *testing.T) {
	gs, err := newGoSpellReader(
		strings.NewReader("SET UTF-8\n"),
		strings.NewReader("1\nword\n"),
	)
	if err != nil {
		t.Fatalf("newGoSpellReader error: %v", err)
	}
	matches := gs.suggest("wrod")
	if len(matches) != 1 || matches[0].word != "word" {
		t.Errorf("suggest(\"wrod\") = %v, want word", matches)
	}
}

// TestContinuationCycleTerminates covers an .aff file whose affix class
// continues to itself. Nothing in the format forbids it, and following it
// faithfully would not terminate, so expansion is bounded -- the point of the
// test is that loading finishes at all and still recognizes the forms the
// bound does allow.
func TestContinuationCycleTerminates(t *testing.T) {
	affContent := `SET UTF-8
FLAG num

SFX 1 Y 1
SFX 1 0 s/1 .
`
	dicContent := `1
loop/1
`

	done := make(chan *goSpell, 1)
	go func() {
		gs, err := newGoSpellReader(
			strings.NewReader(affContent),
			strings.NewReader(dicContent),
		)
		if err != nil {
			t.Error(err)
			done <- nil
			return
		}
		done <- gs
	}()

	select {
	case gs := <-done:
		if gs == nil {
			return
		}
		for _, w := range []string{"loop", "loops"} {
			if !gs.spell(w) {
				t.Errorf("spell(%q) = false, want true", w)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("expansion did not terminate on a self-continuing affix class")
	}
}

// A `.aff` is data Vale is handed, so a COMPOUNDMIN it cannot use must not
// reach the code that treats it as a length.
func TestCompoundMinIsBounded(t *testing.T) {
	tests := map[string]int{
		"COMPOUNDMIN 4":                   4,
		"COMPOUNDMIN 1":                   1,
		"COMPOUNDMIN 0":                   defaultCompoundMin,
		"COMPOUNDMIN -1":                  defaultCompoundMin,
		"COMPOUNDMIN 9223372036854775807": defaultCompoundMin,
	}

	for line, want := range tests {
		aff, err := newDictConfig(strings.NewReader(line))
		if err != nil {
			t.Errorf("%q: %v", line, err)
			continue
		}
		if aff.CompoundMin != want {
			t.Errorf("%q: CompoundMin = %d, want %d", line, aff.CompoundMin, want)
		}
	}
}

// A number too large to represent is refused outright, as any other
// unparseable COMPOUNDMIN already was.
func TestCompoundMinRejectsUnparseable(t *testing.T) {
	for _, line := range []string{
		"COMPOUNDMIN 99999999999999999999",
		"COMPOUNDMIN four",
	} {
		if _, err := newDictConfig(strings.NewReader(line)); err == nil {
			t.Errorf("%q: expected an error", line)
		}
	}
}

// A COMPOUNDRULE count only preallocates, so a wild one must not be able to
// ask for an enormous allocation.
func TestCompoundRuleCapacityIsBounded(t *testing.T) {
	aff, err := newDictConfig(strings.NewReader("COMPOUNDRULE 9223372036854775807"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if cap(aff.CompoundRule) > maxCompoundRules {
		t.Errorf("capacity = %d, want <= %d", cap(aff.CompoundRule), maxCompoundRules)
	}
}
