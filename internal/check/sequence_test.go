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
		{"a block scope is narrowed", []string{"list"}, []string{"sentence.list"}},
		{"already a sentence scope", []string{"sentence"}, []string{"sentence"}},
		{"already narrowed", []string{"sentence.list"}, []string{"sentence.list"}},
		// A bare negated term never mentions `sentence`, so asksForSentence
		// (scope.go) skipped every `sentence.*` fragment block for it and the
		// rule matched the whole unsegmented block instead: `~list` alone left
		// `s` unchanged. `sentence&~list` still excludes list items, but only
		// within sentence-fragment blocks.
		{"negation is AND-ed with sentence",
			[]string{"~list"}, []string{"sentence&~list"}},
		{"a chained negation is AND-ed the same way",
			[]string{"~list&text"}, []string{"sentence&~list&text"}},
		{"several at once",
			[]string{"heading", "list"},
			[]string{"sentence.heading", "sentence.list"}},

		// `paragraph` names no block of its own: splitting wraps every block
		// as `paragraph.<scope>`, so it already describes what an undeclared
		// scope does. `sentence.paragraph` is built for nothing.
		{"paragraph is not narrowed further",
			[]string{"paragraph"}, []string{"sentence"}},
		{"a qualified paragraph keeps its qualifier",
			[]string{"paragraph.md"}, []string{"sentence.md"}},
		{"a scope merely starting with the word is left alone",
			[]string{"paragraphs"}, []string{"sentence.paragraphs"}},
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

// The real, dispatched consequence of the negation bug above: a plain rule
// with a bare negated scope matched the same real-world sentence through two
// different blocks at once. `~list` narrowed to nothing, so Scope.Matches
// treated the rule as if it had no scope at all and matched both
// `paragraph.text.md` and its own whole-block copy `text.md` -- the same
// underlying text, dispatched to Run twice, once for each block. One real
// match produced two identical alerts.
//
// Dispatched the way the real linter dispatches a `sequence` check: through
// the same block splitting (nlp.Info.Compute) and scope matching
// (Scope.Matches) it uses, instead of handing text to Run directly. A block
// built by hand and passed straight to Run bypasses that dispatch entirely,
// so it cannot see this bug at all.
func TestSequenceNegatedScopeDoesNotDoubleReport(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrivedNegatedScope",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"scope":      []string{"~list"},
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrivedNegatedScope")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "I bought a widget that arrived promptly, said the courier."

	f := &core.File{NLP: nlp.Info{Segmentation: true, Splitting: true}}
	paragraph := nlp.NewLinedBlock("", text, "text.md", 1)

	blocks, cerr := f.NLP.Compute(&paragraph, true)
	if cerr != nil {
		t.Fatalf("computing blocks: %v", cerr)
	}

	scope := NewScope(rule.Fields().Scope)

	var alerts []core.Alert
	for _, blk := range blocks {
		if !scope.Matches(blk) {
			continue
		}
		got, rerr := rule.Run(blk, f, testConfig())
		if rerr != nil {
			t.Fatalf("running rule: %v", rerr)
		}
		alerts = append(alerts, got...)
	}

	if len(alerts) != 1 {
		t.Errorf("produced %d alerts for one real match, want exactly 1", len(alerts))
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
