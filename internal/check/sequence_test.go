package check

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdkato/prose/v3/tag"

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

// runScoped runs rule the way the real linter dispatches a `sequence` check:
// through the same block splitting (nlp.Info.Compute) and scope matching
// (Scope.Matches) the linter itself uses, instead of handing text to Run
// directly.
//
// A block built by hand and passed straight to Run bypasses that dispatch
// entirely -- Run sees whatever text the test wrote, not what the rule's own
// declared scope would actually receive in production. That distinction is
// the whole point here: a `max`/`min` rule's declared scope decides whether
// Run is called once per sentence or once per paragraph, and only routing
// through the real scope-matching logic can tell the two apart.
func runScoped(t *testing.T, rule Sequence, text string) []core.Alert {
	t.Helper()

	f := &core.File{NLP: nlp.Info{Segmentation: true, Splitting: true}}
	paragraph := nlp.NewLinedBlock("", text, "text.md", 1)

	blocks, err := f.NLP.Compute(&paragraph, true)
	if err != nil {
		t.Fatalf("computing blocks: %v", err)
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

	return alerts
}

// runScopedAs is runScoped generalized to the block kind under test.
//
// A threshold rule with an undeclared scope must still dispatch into a
// heading, a list item, or a table cell -- not just a body paragraph --
// since ast.go builds each of those with split=false (see #1132): a heading
// or a list item is prose, but it is never a "paragraph" block. selector is
// the block's own scope, as ast.go would build it (e.g. "text.heading.h1.md",
// "text.list.md", "text.table.cell.md"); split mirrors ast.go's own choice
// for that block kind (false for all three).
func runScopedAs(t *testing.T, rule Sequence, text, selector string, split bool) []core.Alert {
	t.Helper()

	f := &core.File{NLP: nlp.Info{Segmentation: true, Splitting: true}}
	blk := nlp.NewLinedBlock("", text, selector, 1)

	blocks, err := f.NLP.Compute(&blk, split)
	if err != nil {
		t.Fatalf("computing blocks: %v", err)
	}

	scope := NewScope(rule.Fields().Scope)

	var alerts []core.Alert
	for _, b := range blocks {
		if !scope.Matches(b) {
			continue
		}
		got, rerr := rule.Run(b, f, testConfig())
		if rerr != nil {
			t.Fatalf("running rule: %v", rerr)
		}
		alerts = append(alerts, got...)
	}

	return alerts
}

// runScopedRemote is runScoped, but dispatched under a remote NLP endpoint --
// the file carries Endpoint and Lang, the same as a real remotely-tagged
// file would, so every block reaches Run with positioned == false.
func runScopedRemote(t *testing.T, rule Sequence, text, endpoint, lang string) []core.Alert {
	t.Helper()

	f := &core.File{NLP: nlp.Info{
		Segmentation: true, Splitting: true, Endpoint: endpoint, Lang: lang,
	}}
	paragraph := nlp.NewLinedBlock("", text, "text.md", 1)

	blocks, err := f.NLP.Compute(&paragraph, true)
	if err != nil {
		t.Fatalf("computing blocks: %v", err)
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

	return alerts
}

// `max` turns repeated matches of the whole pattern into a single density
// alert, reading "more than N tricolons in this paragraph" instead of one
// alert per tricolon -- the same threshold `occurrence` already applies to a
// raw regex count, but here counting a tag-aware sequence match instead.
func TestSequenceMaxCountsWholePatternOccurrences(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.Tricolon",
		"level":   "error",
		"message": "Too many tricolons (found %d).",
		"scope":   []string{"paragraph"},
		"max":     1,
		"tokens": []interface{}{
			map[string]interface{}{"tag": "VB|VBD|VBG|VBN|VBP|VBZ"},
			map[string]interface{}{"tag": ",", "skip": 2},
			map[string]interface{}{"tag": "VB|VBD|VBG|VBN|VBP|VBZ", "skip": 1},
			map[string]interface{}{"tag": ",", "skip": 2},
			map[string]interface{}{"tag": "CC"},
			map[string]interface{}{"tag": "VB|VBD|VBG|VBN|VBP|VBZ", "skip": 1},
		},
	}, "Test.Tricolon")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name        string
		text        string
		wantAlerts  int
		wantMessage string
	}{
		{
			"exactly one tricolon stays under max",
			"It validates the payload, transforms the record, and writes the result.",
			0, "",
		},
		{
			"two tricolons trip max",
			"It validates the payload, transforms the record, and writes the result. " +
				"It parses the header, checks the signature, and rejects the request.",
			1, "Too many tricolons (found 2).",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alerts := runScoped(t, rule, c.text)
			if len(alerts) != c.wantAlerts {
				t.Fatalf("%q produced %d alerts, want %d", c.text, len(alerts), c.wantAlerts)
			}
			if c.wantAlerts > 0 && alerts[0].Message != c.wantMessage {
				t.Errorf("message = %q, want %q", alerts[0].Message, c.wantMessage)
			}
		})
	}
}

// Without `max`/`min` set, a `sequence` rule alerts once per match exactly
// as it always has -- the zero value must not change existing behavior.
func TestSequenceWithoutMaxMinAlertsPerMatch(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.TricolonUnbounded",
		"level":   "error",
		"message": "Tricolon found.",
		"scope":   []string{"paragraph"},
		"tokens": []interface{}{
			map[string]interface{}{"tag": "VB|VBD|VBG|VBN|VBP|VBZ"},
			map[string]interface{}{"tag": ",", "skip": 2},
			map[string]interface{}{"tag": "VB|VBD|VBG|VBN|VBP|VBZ", "skip": 1},
			map[string]interface{}{"tag": ",", "skip": 2},
			map[string]interface{}{"tag": "CC"},
			map[string]interface{}{"tag": "VB|VBD|VBG|VBN|VBP|VBZ", "skip": 1},
		},
	}, "Test.TricolonUnbounded")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "It validates the payload, transforms the record, and writes the result. " +
		"It parses the header, checks the signature, and rejects the request."

	f := &core.File{NLP: nlp.Info{}}
	alerts, rerr := rule.Run(nlp.NewBlock(text, text, "text"), f, testConfig())
	if rerr != nil {
		t.Fatalf("running rule: %v", rerr)
	}
	if len(alerts) != 2 {
		t.Fatalf("produced %d alerts, want 2 (one per match, max/min unset)", len(alerts))
	}
	for _, a := range alerts {
		if a.Message != "Tricolon found." {
			t.Errorf("message = %q, want the unformatted per-match message", a.Message)
		}
	}
}

// `min` can trip on zero matches, which has no span to point at -- the same
// document-scoped fallback `occurrence` uses for its own zero case.
func TestSequenceMinOnZeroMatches(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.RequireTwo",
		"level":   "error",
		"message": "Expected at least two, found %d.",
		"scope":   []string{"paragraph"},
		"min":     2,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.RequireTwo")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "This paragraph never mentions the term at all."

	f := &core.File{NLP: nlp.Info{}}
	alerts, rerr := rule.Run(nlp.NewBlock(text, text, "text"), f, testConfig())
	if rerr != nil {
		t.Fatalf("running rule: %v", rerr)
	}
	if len(alerts) != 1 {
		t.Fatalf("produced %d alerts, want 1", len(alerts))
	}
	if alerts[0].Message != "Expected at least two, found 0." {
		t.Errorf("message = %q, want the zero-count message", alerts[0].Message)
	}
	if len(alerts[0].Span) != 2 || alerts[0].Span[0] != 1 || alerts[0].Span[1] != 1 {
		t.Errorf("span = %v, want the document-scoped [1, 1] fallback", alerts[0].Span)
	}
}

// A `max`-set rule's declared scope names a real block -- a paragraph, not
// the sentence sequence.go currently forces every rule into. Two matches
// split across two sentences of the same paragraph must trip `max: 1` the
// same as two matches in one sentence would; today they don't, because each
// sentence gets its own Run call with no shared count between them.
func TestSequenceMaxAggregatesAcrossSentences(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.Widget",
		"level":   "error",
		"message": "Too many widgets (found %d).",
		"scope":   []string{"paragraph"},
		"max":     1,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.Widget")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "The first widget arrived today. The second widget arrived yesterday."

	alerts := runScoped(t, rule, text)
	if len(alerts) != 1 {
		t.Fatalf("produced %d alerts, want 1 (one density alert for the paragraph)", len(alerts))
	}
	if alerts[0].Message != "Too many widgets (found 2)." {
		t.Errorf("message = %q, want %q", alerts[0].Message, "Too many widgets (found 2).")
	}
}

// A threshold rule that names no `scope` at all still has to dispatch.
// `manager.compileCheck` deliberately leaves a `sequence` rule's scope unset
// rather than defaulting it to `text` -- the rule needs to tell "nothing
// declared" apart from an explicit `text` -- so NewSequence is the only place
// left to give an unset threshold rule a real scope. Before that default was
// added, the rule's declared scope stayed empty, and an empty selector set
// never matches any block: `max`/`min` was silently never enforced for any
// rule that did not name `scope: paragraph` (or similar) itself.
func TestSequenceMaxWithoutDeclaredScopeStillDispatches(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.WidgetNoScope",
		"level":   "error",
		"message": "Too many widgets (found %d).",
		"max":     1,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.WidgetNoScope")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "The first widget arrived today. The second widget arrived yesterday."

	alerts := runScoped(t, rule, text)
	if len(alerts) != 1 {
		t.Fatalf("produced %d alerts, want 1 (rule must dispatch with no declared scope)", len(alerts))
	}
	if alerts[0].Message != "Too many widgets (found 2)." {
		t.Errorf("message = %q, want %q", alerts[0].Message, "Too many widgets (found 2).")
	}
}

// `max` and `min` combine the same way `occurrence`'s do: either threshold
// alone can trip the rule. Both need the same real, declared-scope count
// `max` alone needs -- a rule can't apply either bound correctly while still
// counting one sentence at a time.
func TestSequenceMaxAndMinTogether(t *testing.T) {
	newRule := func(t *testing.T) Sequence {
		t.Helper()
		rule, err := NewSequence(testConfig(), baseCheck{
			"extends": "sequence",
			"name":    "Test.WidgetBounds",
			"level":   "error",
			"message": "Expected 2-3 widgets, found %d.",
			"scope":   []string{"paragraph"},
			"max":     3,
			"min":     2,
			"tokens": []interface{}{
				map[string]interface{}{"pattern": "widget"},
			},
		}, "Test.WidgetBounds")
		if err != nil {
			t.Fatalf("building rule: %v", err)
		}
		return rule
	}

	cases := []struct {
		name        string
		text        string
		wantAlerts  int
		wantMessage string
	}{
		{
			"below min, split across sentences",
			"The first widget arrived today. Nothing else happened.",
			1, "Expected 2-3 widgets, found 1.",
		},
		{
			"within bounds, split across sentences",
			"The first widget arrived today. The second widget arrived yesterday.",
			0, "",
		},
		{
			"above max, split across sentences",
			"The first widget and second widget arrived today. " +
				"The third widget and fourth widget arrived yesterday.",
			1, "Expected 2-3 widgets, found 4.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alerts := runScoped(t, newRule(t), c.text)
			if len(alerts) != c.wantAlerts {
				t.Fatalf("%q produced %d alerts, want %d", c.text, len(alerts), c.wantAlerts)
			}
			if c.wantAlerts > 0 && alerts[0].Message != c.wantMessage {
				t.Errorf("message = %q, want %q", alerts[0].Message, c.wantMessage)
			}
		})
	}
}

// `min`'s at-threshold case is silent, the same way `max`'s already is: a
// count that exactly meets the floor is not "too few." That has to hold for
// the rule's real declared scope -- here a paragraph -- not just for
// whichever single sentence happens to be under test.
func TestSequenceMinSilentAtExactThreshold(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.WidgetFloor",
		"level":   "error",
		"message": "Expected at least two widgets, found %d.",
		"scope":   []string{"paragraph"},
		"min":     2,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.WidgetFloor")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "The first widget arrived today. The second widget arrived yesterday."

	alerts := runScoped(t, rule, text)
	if len(alerts) != 0 {
		t.Fatalf("produced %d alerts, want 0 (2 widgets exactly meets min: 2)", len(alerts))
	}
}

// Skipping the sentence-out-early check when `min` is set has to survive past
// a single sentence too: a paragraph where no sentence contains the required
// literal must still report exactly one zero-count alert for the whole
// paragraph -- not silently drop every sentence for lacking the word, and not
// report one alert per sentence either.
func TestSequenceMinReportsAbsentLiteralOncePerParagraph(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.RequireWidgetMention",
		"level":   "error",
		"message": "Expected at least one widget, found %d.",
		"scope":   []string{"paragraph"},
		"min":     1,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.RequireWidgetMention")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "This paragraph never mentions the term at all. Neither does this second sentence."

	alerts := runScoped(t, rule, text)
	if len(alerts) != 1 {
		t.Fatalf("produced %d alerts, want 1 (one zero-count alert for the whole paragraph)", len(alerts))
	}
	if alerts[0].Message != "Expected at least one widget, found 0." {
		t.Errorf("message = %q, want %q", alerts[0].Message, "Expected at least one widget, found 0.")
	}
}

// The match-walk itself has no notion of a sentence boundary: `Run` tags a
// block a sentence at a time, but hands `sequenceMatches` one flat token
// slice with no marker between sentences. Called directly on a hand-built
// block, the way this test does deliberately, that already reaches a block
// spanning two sentences -- runScoped can't get here today, since a plain
// rule's scope narrows to `sentence` and dispatches one sentence at a time,
// but a paragraph-scoped Max/Min rule will hand Run exactly this kind of
// multi-sentence block once the narrowing is relaxed for it. A `skip` window
// only checks word distance, so the last word of one sentence and the first
// of the next can satisfy it as if they were never apart.
func TestSequenceRejectsMatchSpanningSentenceBoundary(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrived",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrived")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	cases := []struct {
		name string
		text string
		want int
	}{
		{
			"same sentence: match completes",
			"I bought a widget that arrived promptly.",
			1,
		},
		{
			"across a sentence boundary: match must not complete",
			"I bought a widget. Arrived promptly, said the courier.",
			0,
		},
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

// A remote NLP endpoint's tokens carry no real offsets into the source --
// Start is always 0 in what it returns (see nlp.TextToTokens) -- so the
// boundary guard cannot lean on word.Start there. sentenceIndices instead
// locates each word by its text, which has to work identically whether the
// tokens came from the local tagger or a remote one. This mocks a `/tag`
// endpoint that reproduces that shape (real text and tags, zeroed offsets)
// to make sure the guard still holds over it.
func TestSequenceRejectsMatchSpanningSentenceBoundaryOverRemoteEndpoint(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrivedRemote",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrivedRemote")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Tag the text with the same local tagger the non-remote tests use,
		// so the words and tags are real -- then drop the offsets, the one
		// thing an actual remote endpoint's response does not carry.
		local, terr := nlp.TextToTokens(r.URL.Query().Get("text"), nil)
		if terr != nil {
			t.Errorf("tagging mock request text: %v", terr)
			return
		}
		remote := make([]tag.Token, len(local))
		for i, tok := range local {
			remote[i] = tag.Token{Text: tok.Text, Tag: tok.Tag, Start: 0}
		}

		if encErr := json.NewEncoder(w).Encode(nlp.TagResult{Tokens: remote}); encErr != nil {
			t.Errorf("encoding mock /tag response: %v", encErr)
		}
	}))
	defer server.Close()

	cases := []struct {
		name string
		text string
		want int
	}{
		{
			"same sentence: match completes",
			"I bought a widget that arrived promptly.",
			1,
		},
		{
			"across a sentence boundary: match must not complete",
			"I bought a widget. Arrived promptly, said the courier.",
			0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{Endpoint: server.URL, Lang: "en"}}
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

// A remote tagger's normalization does not always leave a token's text
// unchanged, even when it leaves the token count and order alone. Locating
// tokens by searching txt for their (possibly rewritten) text broke exactly
// here: normalizing the pronoun "I" to "i" made a naive search match the
// letter "i" embedded inside "widget" instead of the real word, stalling the
// search cursor mid-word; the next lookup then matched inside a word in the
// *next* sentence, jumping the cursor across the real sentence boundary and
// mis-assigning every word after it -- silently dropping the guard for a
// genuine violation. boundarySentenceIndices aligns by ordinal position
// instead, which does not depend on the token text matching at all, only the
// count -- this reproduces the normalization and confirms the guard holds.
func TestSequenceRejectsMatchSpanningSentenceBoundaryOverRemoteEndpointNormalization(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrivedRemoteNormalized",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrivedRemoteNormalized")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, terr := nlp.TextToTokens(r.URL.Query().Get("text"), nil)
		if terr != nil {
			t.Errorf("tagging mock request text: %v", terr)
			return
		}
		remote := make([]tag.Token, len(local))
		for i, tok := range local {
			text := tok.Text
			if text == "I" {
				// The normalization that broke text-based location: a
				// single-letter token whose lowercased form recurs inside an
				// unrelated word ("widget") later in the same text.
				text = "i"
			}
			remote[i] = tag.Token{Text: text, Tag: tok.Tag, Start: 0}
		}
		if encErr := json.NewEncoder(w).Encode(nlp.TagResult{Tokens: remote}); encErr != nil {
			t.Errorf("encoding mock /tag response: %v", encErr)
		}
	}))
	defer server.Close()

	text := "I bought a widget. Arrived promptly, said the courier."

	f := &core.File{NLP: nlp.Info{Endpoint: server.URL, Lang: "en"}}
	alerts, rerr := rule.Run(nlp.NewBlock(text, text, "text"), f, testConfig())
	if rerr != nil {
		t.Fatalf("running rule: %v", rerr)
	}
	if len(alerts) != 0 {
		t.Errorf("%q produced %d alerts, want 0 (normalization must not defeat the boundary guard)",
			text, len(alerts))
	}
}

// Under the old design, a remote tokenizer that disagreed with the local one
// on token count (not just text) left boundarySentenceIndices unable to
// align the two lists by position, so it gave up and returned nil --
// which crossesSentence read as "reject nothing," letting a genuine
// cross-sentence match complete. That was a documented trade-off of
// inferring sentence membership from a whole block's flat token list after
// the fact.
//
// Per-sentence tagging removes the scenario entirely rather than handling it
// better: each sentence is tagged by its own call, using only that
// sentence's own text, so there is no shared, block-wide token list for a
// remote tokenizer to disagree with the local one *about* in the first
// place. A dropped token still changes what that one sentence's own tagging
// looks like, but it cannot let a match reach across into a different
// sentence's words -- those simply are never in the same slice. This test
// now asserts the guard holds even under a token-dropping mock endpoint,
// where the old design would have silently let it through.
func TestSequenceRemoteTokenCountMismatchStillGuardsBoundary(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrivedRemoteMismatch",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrivedRemoteMismatch")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, terr := nlp.TextToTokens(r.URL.Query().Get("text"), nil)
		if terr != nil {
			t.Errorf("tagging mock request text: %v", terr)
			return
		}
		// Drop the last token of whichever text this call received. Under
		// the new per-sentence design that is always just one sentence's
		// text, so this reproduces a per-sentence tokenizer disagreement,
		// not a whole-block one.
		remote := make([]tag.Token, 0, len(local)-1)
		for _, tok := range local[:len(local)-1] {
			remote = append(remote, tag.Token{Text: tok.Text, Tag: tok.Tag, Start: 0})
		}
		if encErr := json.NewEncoder(w).Encode(nlp.TagResult{Tokens: remote}); encErr != nil {
			t.Errorf("encoding mock /tag response: %v", encErr)
		}
	}))
	defer server.Close()

	text := "I bought a widget. Arrived promptly, said the courier."

	f := &core.File{NLP: nlp.Info{Endpoint: server.URL, Lang: "en"}}
	alerts, rerr := rule.Run(nlp.NewBlock(text, text, "text"), f, testConfig())
	if rerr != nil {
		t.Fatalf("running rule: %v", rerr)
	}
	// Each sentence is tagged on its own text, so a dropped token never
	// creates a merged, misaligned view spanning both sentences: the
	// cross-sentence match must still be rejected.
	if len(alerts) != 0 {
		t.Errorf("%q produced %d alerts, want 0 (per-sentence tagging must not let a "+
			"per-sentence tokenizer disagreement open a cross-sentence match)",
			text, len(alerts))
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

// ---------------------------------------------------------------------------
// RED-phase regression tests for the new per-sentence tagging design (see the
// architectural review that replaces sentenceIndices/boundarySentenceIndices/
// crossesSentence/sentIdx). Each test below reproduces a specific bug found
// in the old, count-threshold cross-sentence guard across three review
// rounds, and must keep passing under the new design without reproducing it.
// ---------------------------------------------------------------------------

// A threshold rule's undeclared scope defaults to `["paragraph"]` (see
// NewSequence). "paragraph" names no block of its own, the same way it
// doesn't for a plain rule's own scope-narrowing (see sentenceScope's own
// comment) -- but a heading or list item is prose, not a paragraph: ast.go
// builds both with split=false, precisely so that a `scope: paragraph` rule
// does not reach them (#1132). Real dispatch must still deliver a threshold
// rule with no declared scope into a heading, exactly as a plain
// (sentence-scoped) `sequence` rule already does. Today it does not: the
// declared scope literally reads "paragraph", and neither the heading's own
// block (`text.heading.h1.md`) nor its sentence fragments satisfy that
// selector, so the rule is never called at all and the violation goes
// unreported, silently.
func TestSequenceMaxWithoutDeclaredScopeDispatchesIntoHeading(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.WidgetHeadingNoScope",
		"level":   "error",
		"message": "Too many widgets (found %d).",
		"max":     1,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.WidgetHeadingNoScope")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "The first widget arrived today. The second widget arrived yesterday."

	// split=false: exactly how ast.go builds a heading (lintProse(f, b,
	// lines, false), see internal/lint/ast.go).
	alerts := runScopedAs(t, rule, text, "text.heading.h1.md", false)
	if len(alerts) != 1 {
		t.Fatalf("heading: produced %d alerts, want 1 (an undeclared scope "+
			"must still reach a heading, see #1132)", len(alerts))
	}
	if alerts[0].Message != "Too many widgets (found 2)." {
		t.Errorf("message = %q, want %q", alerts[0].Message, "Too many widgets (found 2).")
	}
}

// Same gap, for a list item -- also built with split=false (ast.go maps
// `li` to `text.list`), and also not a paragraph.
func TestSequenceMaxWithoutDeclaredScopeDispatchesIntoListItem(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.WidgetListNoScope",
		"level":   "error",
		"message": "Too many widgets (found %d).",
		"max":     1,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.WidgetListNoScope")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "The first widget arrived today. The second widget arrived yesterday."

	alerts := runScopedAs(t, rule, text, "text.list.md", false)
	if len(alerts) != 1 {
		t.Fatalf("list item: produced %d alerts, want 1 (an undeclared scope "+
			"must still reach a list item, see #1132)", len(alerts))
	}
	if alerts[0].Message != "Too many widgets (found 2)." {
		t.Errorf("message = %q, want %q", alerts[0].Message, "Too many widgets (found 2).")
	}
}

// Same gap again, for a table cell -- ast.go maps `td`/`th` to
// `text.table.cell`/`text.table.header`, also split=false, also not a
// paragraph.
func TestSequenceMaxWithoutDeclaredScopeDispatchesIntoTableCell(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends": "sequence",
		"name":    "Test.WidgetTableNoScope",
		"level":   "error",
		"message": "Too many widgets (found %d).",
		"max":     1,
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
		},
	}, "Test.WidgetTableNoScope")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "The first widget arrived today. The second widget arrived yesterday."

	alerts := runScopedAs(t, rule, text, "text.table.cell.md", false)
	if len(alerts) != 1 {
		t.Fatalf("table cell: produced %d alerts, want 1 (an undeclared scope "+
			"must still reach a table cell, see #1132)", len(alerts))
	}
	if alerts[0].Message != "Too many widgets (found 2)." {
		t.Errorf("message = %q, want %q", alerts[0].Message, "Too many widgets (found 2).")
	}
}

// A remote tagger's normalization is not always length-preserving:
// contracting "do" + "n't" into a single "don't" token, exactly like a real
// tagger would, changes the *total token count* of the sentence it occurs
// in -- not just the text of one token, the way case-folding "I" to "i"
// does. boundarySentenceIndices' realignment (see its doc comment) trusts an
// equal token count between a fresh local retag and the remote list as
// proof the two line up position-for-position; a merge like this breaks
// that equality even though nothing at all is wrong with the remote
// tagger's segmentation. When the counts disagree, boundarySentenceIndices
// gives up and returns nil, which crossesSentence reads as "reject
// nothing" -- so the boundary guard is silently disabled for the entire
// block, and a real cross-sentence match completes.
//
// The new per-sentence design must not reproduce this: each sentence is its
// own call, so which call produced a token is a fact, not something
// recovered by comparing counts -- a token-count-changing normalization
// elsewhere in the paragraph has no way to affect it.
func TestSequenceRemoteNormalizationMergingTokensDoesNotDefeatGuard(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrivedMergeNorm",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrivedMergeNorm")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	// Reproduces two ordinary normalizations at once: "I" is lowercased to
	// "i" (same token count, as the existing normalization test already
	// covers), and "do"+"n't" is merged into a single "don't" token (changes
	// the token count, which the existing tests do not cover).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, terr := nlp.TextToTokens(r.URL.Query().Get("text"), nil)
		if terr != nil {
			t.Errorf("tagging mock request text: %v", terr)
			return
		}
		var remote []tag.Token
		for i := 0; i < len(local); i++ {
			tok := local[i]
			txt := tok.Text
			if txt == "I" {
				txt = "i"
			}
			if txt == "do" && i+1 < len(local) && local[i+1].Text == "n't" {
				remote = append(remote, tag.Token{Text: "don't", Tag: tok.Tag, Start: 0})
				i++
				continue
			}
			remote = append(remote, tag.Token{Text: txt, Tag: tok.Tag, Start: 0})
		}
		if encErr := json.NewEncoder(w).Encode(nlp.TagResult{Tokens: remote}); encErr != nil {
			t.Errorf("encoding mock /tag response: %v", encErr)
		}
	}))
	defer server.Close()

	cases := []struct {
		name string
		text string
		want int
	}{
		{
			"cross-sentence: match must not complete, even though a same-length " +
				"normalization elsewhere ('I'->'i') and a count-changing one " +
				"('do'+\"n't\"->\"don't\") both occur in the same text",
			"I don't want the widget. Arrived promptly, said the courier.",
			0,
		},
		{
			"same sentence: a genuine match must still be counted despite the " +
				"same normalizations occurring earlier in the text",
			"I don't wait; the widget arrived promptly.",
			1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &core.File{NLP: nlp.Info{Endpoint: server.URL, Lang: "en"}}
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

// File.Sentences (and TokenCache.Sentences beneath it) must route through
// the same segmentation dispatch Info.Compute already uses: local Punkt for
// English, the endpoint's own `/segment` response for non-English text under
// a configured remote endpoint (see Info.Compute). Today it does not --
// TokenCache.Sentences always calls punktSegmenter().Segment(text) directly,
// ignoring f.NLP entirely, so a `sequence` rule's cross-sentence guard reads
// sentence boundaries from an English-trained local segmenter even when the
// file is configured with a non-English remote endpoint that would have
// reported different boundaries.
//
// This mocks a `/segment` endpoint that deliberately disagrees with local
// Punkt's segmentation of the same text (merging what Punkt splits into two
// sentences into one), so the two are distinguishable: if f.Sentences
// reflects the mock, the dispatch changed; if it still reflects local
// Punkt's own split, it did not.
func TestFileSentencesRoutesThroughEndpointDispatchForNonEnglish(t *testing.T) {
	text := "Ini kalimat pertama. Ini kalimat kedua."

	// Sanity check on the test's own premise: local Punkt splits this into
	// two sentences (confirmed separately), so returning exactly one from
	// the mock is a real, detectable disagreement -- not a coincidence.
	local, lerr := (&core.File{}).Sentences(text)
	if lerr != nil {
		t.Fatalf("segmenting locally: %v", lerr)
	}
	if len(local) != 2 {
		t.Fatalf("test setup: want local Punkt to split %q into 2 sentences, got %d",
			text, len(local))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		result := nlp.SegmentResult{Sents: []string{text}}
		if err := json.NewEncoder(w).Encode(result); err != nil {
			t.Errorf("encoding mock /segment response: %v", err)
		}
	}))
	defer server.Close()

	f := &core.File{NLP: nlp.Info{Endpoint: server.URL, Lang: "id"}}
	got, gerr := f.Sentences(text)
	if gerr != nil {
		t.Fatalf("segmenting via endpoint: %v", gerr)
	}

	if len(got) != 1 {
		t.Errorf("f.Sentences returned %d sentences, want 1 (the mocked "+
			"non-English endpoint's /segment response) -- File.Sentences must "+
			"route through the same endpoint dispatch Info.Compute uses for "+
			"non-English text under a configured remote endpoint, not always "+
			"local Punkt", len(got))
	}
}

// End-to-end companion to TestFileSentencesRoutesThroughEndpointDispatchForNonEnglish:
// that test confirms File.Sentences itself routes through the endpoint
// dispatch; this confirms a real sequence rule's own cross-sentence guard,
// reached through Run, actually reflects it too, now that Run calls
// f.Sentences directly as part of its main loop (see Run) rather than
// through the doomed sentenceIndices detour.
//
// Both /tag and /segment are mocked. The /segment mock deliberately
// disagrees with local Punkt, merging what Punkt splits into two sentences
// into one -- so the same text produces a different outcome depending on
// which segmenter answers: under local Punkt's own two-sentence split the
// match crosses a boundary and is rejected (see
// TestSequenceRejectsMatchSpanningSentenceBoundary); under the mocked
// endpoint's one-sentence view there is no boundary to cross, and the match
// completes.
func TestSequenceRunUsesEndpointSegmentationForNonEnglish(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrivedSegmentRouting",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrivedSegmentRouting")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	text := "I bought a widget. Arrived promptly, said the courier."

	// Sanity check on the test's own premise: local Punkt splits this into
	// two sentences, so the mocked /segment response below (one sentence)
	// is a real, detectable disagreement, not a coincidence.
	local, lerr := (&core.File{}).Sentences(text)
	if lerr != nil {
		t.Fatalf("segmenting locally: %v", lerr)
	}
	if len(local) != 2 {
		t.Fatalf("test setup: want local Punkt to split %q into 2 sentences, got %d",
			text, len(local))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/segment":
			result := nlp.SegmentResult{Sents: []string{r.URL.Query().Get("text")}}
			if encErr := json.NewEncoder(w).Encode(result); encErr != nil {
				t.Errorf("encoding mock /segment response: %v", encErr)
			}
		case "/tag":
			local, terr := nlp.TextToTokens(r.URL.Query().Get("text"), nil)
			if terr != nil {
				t.Errorf("tagging mock request text: %v", terr)
				return
			}
			remote := make([]tag.Token, len(local))
			for i, tok := range local {
				remote[i] = tag.Token{Text: tok.Text, Tag: tok.Tag, Start: 0}
			}
			if encErr := json.NewEncoder(w).Encode(nlp.TagResult{Tokens: remote}); encErr != nil {
				t.Errorf("encoding mock /tag response: %v", encErr)
			}
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	f := &core.File{NLP: nlp.Info{Endpoint: server.URL, Lang: "id"}}
	alerts, rerr := rule.Run(nlp.NewBlock(text, text, "text"), f, testConfig())
	if rerr != nil {
		t.Fatalf("running rule: %v", rerr)
	}
	if len(alerts) != 1 {
		t.Errorf("%q produced %d alerts, want 1 -- Run must read sentence "+
			"boundaries from the configured non-English remote endpoint's own "+
			"/segment response (which merges this into one sentence here), not "+
			"always local Punkt's (which splits it into two and would reject "+
			"the match)", text, len(alerts))
	}
}

// SegmentWith used to panic when a configured remote endpoint's /segment
// request failed -- a network error, timeout, or malformed response -- which
// crashed the whole vale process rather than reporting a normal lint error:
// there is no recover() anywhere in Vale, unlike the tagging path, which
// already threads a real error end-to-end (textToTokensWith ->
// TokenCache.TokensWith -> File.TokensWith -> Run's existing terr handling).
// This mocks a /segment endpoint that fails -- a non-2xx status with a
// malformed (non-JSON) body, which doSegment's json.Unmarshal cannot parse
// -- and confirms Run now returns a normal error the same way it already
// does for a tagging failure, instead of panicking.
func TestSequenceRunReturnsErrorOnSegmentEndpointFailure(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetArrivedSegmentFailure",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetArrivedSegmentFailure")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A malformed, non-JSON /segment response: doSegment's
		// json.Unmarshal fails on this the same way it would on a real
		// endpoint's timeout page or other non-JSON error body.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	// A non-English language, so Run reaches SegmentWith's remote branch
	// (see usesRemoteSegmentation) rather than local Punkt.
	text := "I bought a widget. Arrived promptly, said the courier."
	f := &core.File{NLP: nlp.Info{Endpoint: server.URL, Lang: "id"}}

	var alerts []core.Alert
	var rerr error
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("Run panicked instead of returning an error: %v", p)
			}
		}()
		alerts, rerr = rule.Run(nlp.NewBlock(text, text, "text"), f, testConfig())
	}()

	if rerr == nil {
		t.Fatalf("Run returned a nil error for a failed /segment request, want a non-nil error")
	}
	if alerts != nil {
		t.Errorf("alerts = %v, want nil alongside the error", alerts)
	}
}

// Span-offset correctness is the highest-risk part of the new design: a
// sentence-relative span found while tagging one sentence at a time has to
// be rebased -- first onto the paragraph's own text (add the sentence's
// offset within it), then onto the document (blk.Offset, unchanged) -- to
// end up as the correct absolute span in the final alert. Get any of that
// wrong and a real violation either mislocates or, as reproduced here,
// disappears.
//
// This is deliberately end-to-end: a two-paragraph document dispatched
// through real scope matching, under a remote (unpositioned) endpoint, where
// the only genuine violation sits in the second sentence of the second
// paragraph. Today, a `max`/`min` rule tags and locates the *whole paragraph*
// in one pass; a remote endpoint's tokens carry no offsets (Start is always
// 0), so `locate` falls back to searching the whole paragraph's text for the
// matched words -- and that search always resolves to the *first* occurrence
// of that text in the paragraph, not necessarily the one the match actually
// came from. An exception region placed over an identical but excluded
// occurrence earlier in the paragraph turns that into an observable bug: the
// genuine, later match's own span is computed as if it were the earlier,
// excluded one, so it is wrongly dropped as beginning inside the exception
// too -- the real violation vanishes instead of being reported with the
// correct span.
//
// Under the new design, each sentence is tagged and located on its own text,
// so a same-text-but-excluded earlier sentence cannot contaminate a later
// sentence's own, correctly-rebased span.
func TestSequenceSpanRebasedCorrectlyAcrossParagraphsAndSentences(t *testing.T) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Test.WidgetSpanRebase",
		"level":      "error",
		"message":    "Expected at least two, found %d.",
		"min":        2,
		"exceptions": []interface{}{`^[^,]+,`},
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Test.WidgetSpanRebase")
	if err != nil {
		t.Fatalf("building rule: %v", err)
	}

	para1 := "This warm-up paragraph never mentions the target term at all, " +
		"so it should be entirely inert."
	// Sentence 1 of para2 contains a "widget arrived" that begins inside the
	// exception region (up to its first comma) and must stay excluded.
	// Sentence 2 contains the one genuine, un-excepted match; it is the only
	// alert that should survive into the final result.
	para2Sent1 := "Nothing about a widget arrived here, so ignore it."
	para2Sent2 := "The real widget arrived at noon."
	para2 := para2Sent1 + " " + para2Sent2
	doc := para1 + "\n\n" + para2

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, terr := nlp.TextToTokens(r.URL.Query().Get("text"), nil)
		if terr != nil {
			t.Errorf("tagging mock request text: %v", terr)
			return
		}
		remote := make([]tag.Token, len(local))
		for i, tok := range local {
			remote[i] = tag.Token{Text: tok.Text, Tag: tok.Tag, Start: 0}
		}
		if encErr := json.NewEncoder(w).Encode(nlp.TagResult{Tokens: remote}); encErr != nil {
			t.Errorf("encoding mock /tag response: %v", encErr)
		}
	}))
	defer server.Close()

	alerts := runScopedRemote(t, rule, doc, server.URL, "en")

	// The correct absolute span: the second, real "widget arrived" sits in
	// para2's second sentence, at its position within the whole document.
	wantStart := strings.LastIndex(doc, "widget arrived")
	wantEnd := wantStart + len("widget arrived")

	var found *core.Alert
	for i := range alerts {
		if alerts[i].Message == "Expected at least two, found 1." {
			found = &alerts[i]
		}
	}
	if found == nil {
		t.Fatalf("no alert with message %q among %d alerts (got messages: %v) -- "+
			"the genuine match in paragraph 2's second sentence must not be dropped",
			"Expected at least two, found 1.", len(alerts), alertMessages(alerts))
	}
	if len(found.Span) != 2 || found.Span[0] != wantStart || found.Span[1] != wantEnd {
		t.Errorf("span = %v, want [%d, %d] (the real match in para2's second "+
			"sentence, not a span carried over from the excluded, textually "+
			"identical occurrence in para2's first sentence)",
			found.Span, wantStart, wantEnd)
	}
}

func alertMessages(alerts []core.Alert) []string {
	out := make([]string, len(alerts))
	for i, a := range alerts {
		out[i] = a.Message
	}
	return out
}
