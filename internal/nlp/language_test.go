package nlp

import (
	"strings"
	"testing"

	"github.com/jdkato/prose/v3/segment"
	_ "github.com/jdkato/prose/v3/segment/lang/de" // models to register under another name
	"github.com/jdkato/prose/v3/tag"
	_ "github.com/jdkato/prose/v3/tag/lang/de"
	_ "github.com/jdkato/prose/v3/tag/lang/fr" // linked, as a Go program would, for the by-language tests
)

// A registered language tags and segments with the bytes it was given,
// built on first use, and a region tag falls back to its base language.
func TestRegisterLanguage(t *testing.T) {
	tagger, ok := tag.LanguageModel("de")
	if !ok {
		t.Fatal("the German tagger should be linked into this test")
	}
	gz, abbrevs, ok := segment.LanguageModel("de")
	if !ok {
		t.Fatal("the German segmenter should be linked into this test")
	}

	RegisterLanguage("XX", LanguageModel{Tagger: tagger, Segmenter: gz, Abbreviations: abbrevs})

	got, err := TaggerFor("", "xx-yy")
	if err != nil {
		t.Fatal(err)
	}
	if lang := got.(*tag.Tagger).Language(); lang != "de" {
		t.Errorf("tagger language = %q, want the German model", lang)
	}

	sents := Segment("xx", "Dr. Müller kommt z.B. morgen. Er bleibt.")
	if len(sents) != 2 {
		t.Errorf("got %d sentences, want 2: %q", len(sents), sents)
	}

	if !segmenterFor("xx").Trained() {
		t.Error("the registered segmenter should be the trained model")
	}

	// Registering again replaces the models and forgets the cached segmenter.
	RegisterLanguage("xx", LanguageModel{})
	if segmenterFor("xx").Trained() {
		t.Error("with no segmenter registered, the rule-based one should take over")
	}
	if _, missing := TaggerFor("", "xx"); missing == nil {
		t.Error("a language registered without a tagger should not tag")
	}
}

// The error for a missing model says what to do, which depends on whether
// prose publishes one.
func TestTaggerForMissingModel(t *testing.T) {
	for lang, want := range map[string]string{
		"pt": "run 'vale sync'",            // published, not installed
		"cs": "prose tags",                 // published, segmenter only
		"qq": "prose tags",                 // not a language prose knows
		"ja": `no model for language "ja"`, // what the e2e suite looks for
	} {
		_, err := TaggerFor("", lang)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("TaggerFor(%q) = %v, want it to mention %q", lang, err, want)
		}
	}
}
