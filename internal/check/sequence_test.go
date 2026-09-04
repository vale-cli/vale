package check

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// A sequence position names the word it accepts, so it has to match the whole
// token.
//
// The regex that finds candidate positions is deliberately unanchored -- it is
// run against the whole sentence -- and reusing it to test a token made
// `pattern: self` accept the single token `self-worth`, so a rule for
// `your self` fired on `your self-worth`.
func TestSequenceMatchesWholeTokens(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.Yourself",
		"level":   "error",
		"message": "Did you mean 'yourself'?",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "your"},
			map[string]interface{}{"pattern": "self"},
		},
	}, "Test.Yourself")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		text string
		want int
	}{
		{"exact words", "Ask your self what matters.", 1},
		{"hyphenated compound", "Question your self-worth sometimes.", 0},
		{"longer word", "Consider your selfishness here.", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{}}

			alerts, rerr := rule.Run(nlp.NewBlock(c.text, c.text, "text"), f, testConfig())
			if rerr != nil {
				t.Fatalf("running rule: %v", rerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q produced %d alerts, want %d", c.text, len(alerts), c.want)
			}
		})
	}
}

func testConfig() *core.Config {
	return &core.Config{WordTemplate: wordTemplate}
}

// sentenceScope narrows a declared scope to the sentences within it. Asking
// for blocks that are never built makes the rule match nothing and say
// nothing, which is the failure #1126 reported.
func TestSentenceScope(t *testing.T) {
	cases := []struct {
		name     string
		declared []string
		want     []string
	}{
		{"unset means every sentence", nil, []string{"sentence"}},
		{"a block scope is narrowed", []string{"list"}, []string{"sentence & list"}},
		{"already a sentence scope", []string{"sentence"}, []string{"sentence"}},
		{"already narrowed", []string{"sentence.list"}, []string{"sentence.list"}},
		{"negation stays in front", []string{"~list"}, []string{"~list"}},
		{"a selector is a term, not a sub-scope",
			[]string{"doc(h2 + p)"}, []string{"sentence & doc(h2 + p)"}},
		{"several at once",
			[]string{"heading", "list"},
			[]string{"sentence & heading", "sentence & list"}},

		// `paragraph` names no block of its own: splitting wraps every block
		// as `paragraph.<scope>`, so it already describes what an undeclared
		// scope does. `sentence.paragraph` is built for nothing.
		{"paragraph is not narrowed further",
			[]string{"paragraph"}, []string{"sentence"}},
		{"a qualified paragraph keeps its qualifier",
			[]string{"paragraph.md"}, []string{"sentence.md"}},
		{"a scope merely starting with the word is left alone",
			[]string{"paragraphs"}, []string{"sentence & paragraphs"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sentenceScope(c.declared)
			if len(got) != len(c.want) {
				t.Fatalf("sentenceScope(%v) = %v, want %v", c.declared, got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("sentenceScope(%v)[%d] = %q, want %q",
						c.declared, i, got[i], c.want[i])
				}
			}
		})
	}
}

// The narrowed scope has to match a block that is actually built. A paragraph's
// sentences arrive as `sentence.text.<ext>`, so a rule scoped to `paragraph`
// must match that or it reports nothing at all.
func TestSequenceParagraphScopeMatchesParagraphSentences(t *testing.T) {
	paragraphSentence := nlp.NewLinedBlock(
		"", "A sentence in a paragraph.", "sentence.text.md", 1)

	for _, declared := range []string{"paragraph", "sentence"} {
		t.Run(declared, func(t *testing.T) {
			rule, err := NewSequence(testConfig(), baseCheck{
				"extends": "sequence",
				"name":    "Test.Sequence",
				"level":   "error",
				"message": "Sequence matched '%s'.",
				"scope":   []string{declared},
				"tokens": []interface{}{
					map[string]interface{}{"pattern": "in"},
					map[string]interface{}{"pattern": "a"},
				},
			}, "Test.Sequence")
			if err != nil {
				t.Fatalf("building rule: %v", err)
			}

			narrowed := rule.Fields().Scope
			if !NewScope(narrowed).Matches(paragraphSentence) {
				t.Errorf("scope %q narrowed to %v, which matches no paragraph sentence",
					declared, narrowed)
			}
		})
	}
}

// A token found inside its `skip` window satisfies that window alone: the
// tokens after it still have to hold. This rule once fired on any "the ...
// noun" tail, whether or not a past-tense verb followed.
func TestSequenceVerifiesTokensAfterWindow(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.AfterWindow",
		"level":   "error",
		"message": "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "the"},
			map[string]interface{}{"tag": "NN", "skip": 3},
			map[string]interface{}{"tag": "VBD"},
		},
	}, "Test.AfterWindow")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		text string
		want int
	}{
		{"verb follows the noun", "Then the big dog barked loudly today.", 1},
		{"no verb follows", "Near the big dog yesterday morning.", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{}}
			alerts, rerr := rule.Run(nlp.NewBlock(c.text, c.text, "text"), f, testConfig())
			if rerr != nil {
				t.Fatalf("running rule: %v", rerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q produced %d alerts, want %d", c.text, len(alerts), c.want)
			}
		})
	}
}

// `min` asks for repeated occurrences, each with its own `skip` window: two
// nouns, anywhere in the several words before a pronoun -- the ambiguous
// "it" -- rather than just one.
func TestSequenceMinCountsOccurrences(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.Ambiguous",
		"level":   "warning",
		"message": "Avoid ambiguous pronouns.",
		"tokens": []interface{}{
			map[string]interface{}{"tag": "NN|NNP|NNPS|NNS", "skip": 4, "min": 2},
			map[string]interface{}{"pattern": `\w+`, "tag": `PRP`},
		},
	}, "Test.Ambiguous")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		text string
		want int
	}{
		{"two nouns before it", "The dog chased the cat until it tired.", 1},
		{"one noun before it", "The dog barked because it hungered.", 0},
		{"no nouns before it", "Suddenly it stopped.", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{}}
			alerts, rerr := rule.Run(nlp.NewBlock(c.text, c.text, "text"), f, testConfig())
			if rerr != nil {
				t.Fatalf("running rule: %v", rerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q produced %d alerts, want %d", c.text, len(alerts), c.want)
			}
		})
	}
}

// A negated token asserts the absence of something, so it is satisfied at a
// sentence boundary: "not preceded by X" holds when nothing precedes the match
// at all. Before this, any leading token -- negated or not -- demanded a word
// to its left, so such a rule could never fire on a sentence-initial match.
func TestSequenceNegatedTokenAtBoundary(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.NotAfterAnd",
		"level":      "warning",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"tag": "CC", "negate": true},
			map[string]interface{}{"pattern": "dogs"},
			map[string]interface{}{"pattern": "bark"},
		},
	}, "Test.NotAfterAnd")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		text string
		want int
	}{
		{"sentence-initial match", "Dogs bark at night.", 1},
		{"preceded by an allowed word", "Some dogs bark at night.", 1},
		{"preceded by a conjunction", "Cats meow and dogs bark.", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{}}
			alerts, rerr := rule.Run(nlp.NewBlock(c.text, c.text, "text"), f, testConfig())
			if rerr != nil {
				t.Fatalf("running rule: %v", rerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q produced %d alerts, want %d", c.text, len(alerts), c.want)
			}
		})
	}
}

// The same vacuous truth applies on the right: a trailing negated token is
// satisfied by the end of the sentence, while a trailing required token still
// needs a word to match.
func TestSequenceNegatedTokenAtRightBoundary(t *testing.T) {
	negated, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.NotBeforeNoun",
		"level":      "warning",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "dogs"},
			map[string]interface{}{"pattern": "bark"},
			map[string]interface{}{"tag": "IN", "negate": true},
		},
	}, "Test.NotBeforeNoun")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	required, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.BeforePreposition",
		"level":      "warning",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "dogs"},
			map[string]interface{}{"pattern": "bark"},
			map[string]interface{}{"tag": "IN"},
		},
	}, "Test.BeforePreposition")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		rule Sequence
		text string
		want int
	}{
		{"negated, sentence ends", negated, "Dogs bark", 1},
		{"negated, followed by a preposition", negated, "Dogs bark at night", 0},
		{"required, sentence ends", required, "Dogs bark", 0},
		{"required, followed by a preposition", required, "Dogs bark at night", 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{}}
			alerts, rerr := c.rule.Run(nlp.NewBlock(c.text, c.text, "text"), f, testConfig())
			if rerr != nil {
				t.Fatalf("running rule: %v", rerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q produced %d alerts, want %d", c.text, len(alerts), c.want)
			}
		})
	}
}

// An exception marks a region of the sentence, and a sequence match that
// begins inside it is dropped. Tokens judge one word at a time; a guard like
// "the comma closing a fronted phrase is not a list comma" is about a region,
// so it is handed to a regex instead.
func TestSequenceExceptions(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.Excepted",
		"level":      "warning",
		"ignorecase": true,
		"message":    "matched",
		"exceptions": []interface{}{`^[^,]+,`},
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "dogs"},
			map[string]interface{}{"pattern": "bark"},
		},
	}, "Test.Excepted")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		text string
		want int
	}{
		{"match begins inside the region", "Wild dogs bark, cats meow.", 0},
		{"match begins after the region", "After dark, dogs bark.", 1},
		{"no region in the sentence", "Wild dogs bark loudly.", 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{}}
			alerts, rerr := rule.Run(nlp.NewBlock(c.text, c.text, "text"), f, testConfig())
			if rerr != nil {
				t.Fatalf("running rule: %v", rerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q produced %d alerts, want %d", c.text, len(alerts), c.want)
			}
		})
	}
}

// Without `skip`, `min` means consecutive occurrences.
func TestSequenceMinConsecutive(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.Stacked",
		"level":   "warning",
		"message": "Stacked modifiers.",
		"tokens": []interface{}{
			map[string]interface{}{"tag": "JJ", "min": 2},
			map[string]interface{}{"pattern": `\w+`, "tag": "NN"},
		},
	}, "Test.Stacked")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		text string
		want int
	}{
		{"two adjectives", "It was a big red dog.", 1},
		{"one adjective", "It was a big dog.", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{}}
			alerts, rerr := rule.Run(nlp.NewBlock(c.text, c.text, "text"), f, testConfig())
			if rerr != nil {
				t.Fatalf("running rule: %v", rerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q produced %d alerts, want %d", c.text, len(alerts), c.want)
			}
		})
	}
}
