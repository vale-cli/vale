package check

import (
	"fmt"
	"strings"

	"github.com/jdkato/prose/v3/tag"
	"github.com/mitchellh/mapstructure"
	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// NLPToken represents a token of text with NLP-related attributes.
type NLPToken struct {
	Pattern string
	Tag     string
	Skip    int

	// `min` (`int`): How many times the token must occur -- "at least two
	// nouns", not just one. Each occurrence gets its own `skip` window, so
	// `skip: 8, min: 2` reads "a noun within eight words, then another noun
	// within eight words". The default is 1.
	Min int

	// UPOS matches a universal part-of-speech tag -- NOUN, VERB, ADJ and so
	// on -- rather than a Penn Treebank one.
	//
	// It compiles down to the equivalent Penn tags, plus a word constraint for
	// the few categories Penn cannot express (see upos.go). Rules written
	// against universal tags are portable; rules written against Penn tags are
	// more precise.
	UPOS string

	// Target narrows the alert to this token alone.
	//
	// Without it a match spans every token in the sequence. Marking one lets a
	// rule require surrounding context while pointing at only the part the
	// writer should change -- "flag the space, but only between these two
	// words".
	Target bool

	// re finds candidate positions by scanning the whole sentence, so it must
	// not be anchored.
	re *rx.Regexp

	// tagRe matches the token's part-of-speech tag. Compiled once with the
	// rule: this is tested against every word of every sentence, and compiling
	// it per call was the single largest cost in a tag-heavy style.
	tagRe *rx.Regexp

	// tokenRe tests one token, so it must be. A `pattern` names the word a
	// position accepts: without anchoring, `self` also accepts the single
	// token `self-worth`, and a rule for `your self` fires on `your
	// self-worth`.
	tokenRe *rx.Regexp

	// wordRe narrows a universal tag to the words it can apply to.
	//
	// Kept apart from `re` because that one doubles as the anchor and is run
	// against the whole sentence; this is only ever tested against a single
	// token's text.
	wordRe *rx.Regexp

	Negate   bool
	optional bool
	start    bool
	end      bool

	// group ties the expanded copies of one occurrence together: a `skip`
	// window's fillers and the required token they pad. When any copy
	// matches, the walk moves past the whole group -- and no further, so the
	// tokens after it are still verified.
	group int
}

// Sequence looks for a user-defined sequence of tokens.
type Sequence struct {
	Definition `mapstructure:",squash"`
	Tokens     []NLPToken

	// `model` (`string`): The tagger to read this rule's tags with.
	//
	// Rules ported from another checker are written against that checker's
	// idea of a noun, and read differently under prose's. Naming its model
	// here has them read as intended. A model is a dictionary under
	// `config/dictionaries`, so it ships and syncs like any other asset.
	//
	// Empty means prose's own tagger, which is what every existing rule gets.
	Model string

	// `exceptions` (`[]string`): Regexes matched against the sentence; a
	// sequence match that *begins inside* one of their regions is dropped.
	//
	// Tokens see the sentence one word at a time, so a guard about a region
	// -- "the comma closing a fronted phrase is not a list comma" -- is not
	// expressible as a token. An exception hands that judgment to a regex,
	// which is the right tool for it, while the tokens keep doing the
	// part-of-speech work.
	//
	// Unlike other checks' `exceptions`, these are not vocabulary terms and
	// the project's accepted tokens are never merged in.
	Exceptions []string

	Ignorecase   bool
	needsTagging bool

	// exceptRe holds the compiled `exceptions`, one per entry.
	exceptRe []*rx.Regexp

	// filter holds literals the sentence must contain one of. Every token in
	// the sequence has to match, so any one token's requirement is the whole
	// rule's. Tokens that only name a tag contribute nothing, and a rule made
	// entirely of those gets no filter and runs as before.
	//
	// This matters more here than elsewhere: Run tags the block before it does
	// anything else, so ruling the rule out first skips the tagger too.
	filter []string
}

// NewSequence creates a new rule from the provided `baseCheck`.
func NewSequence(cfg *core.Config, generic baseCheck, path string) (Sequence, error) {
	rule := Sequence{}

	err := makeTokens(&rule, generic)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	for i, token := range rule.Tokens {
		if token.UPOS != "" {
			if token.Tag != "" {
				return rule, core.NewE201FromPosition(
					"a token cannot set both `tag` and `upos`", path, 1)
			}

			pattern, uerr := uposTagPattern(token.UPOS)
			if uerr != nil {
				return rule, core.NewE201FromPosition(uerr.Error(), path, 1)
			}
			rule.Tokens[i].Tag = pattern
			token.Tag = pattern

			// A category Penn cannot express on its own also constrains the
			// word. Only applied when the rule did not ask for a pattern of
			// its own, which is the more specific request.
			if token.Pattern == "" {
				if words := uposWordPattern(token.UPOS); words != "" {
					wre, werr := rx.Compile(words)
					if werr != nil {
						return rule, core.NewE201FromPosition(werr.Error(), path, 1)
					}
					rule.Tokens[i].wordRe = wre
				}
			}
		}

		if !rule.needsTagging && token.Tag != "" {
			rule.needsTagging = true
		}

		if token.Tag != "" {
			tre, terr := rx.Compile(token.Tag)
			if terr != nil {
				return rule, core.NewE201FromPosition(terr.Error(), path, 1)
			}
			rule.Tokens[i].tagRe = tre
		}

		if token.Pattern != "" {
			regex := makeRegexp(
				cfg.WordTemplate,
				rule.Ignorecase,
				func() bool { return false },
				func() string { return "" },
				false)
			regex = fmt.Sprintf(regex, token.Pattern)

			re, errc := rx.Compile(regex)
			if errc != nil {
				return rule, core.NewE201FromPosition(errc.Error(), path, 1)
			}
			rule.Tokens[i].re = re

			anchored := fmt.Sprintf(tokenTemplate, token.Pattern)
			if rule.Ignorecase {
				anchored = ignoreCase + anchored
			}

			tre, terr := rx.Compile(anchored)
			if terr != nil {
				return rule, core.NewE201FromPosition(terr.Error(), path, 1)
			}
			rule.Tokens[i].tokenRe = tre
		}
	}

	// A model is a dictionary asset, resolved and loaded the same way a
	// spelling dictionary is. Doing it here means an unreadable one is
	// reported when the style loads rather than on the first sentence.
	if rule.Model != "" {
		asset := core.FindConfigAsset(cfg, rule.Model+".dict", core.DictDir)
		if asset == "" {
			return rule, core.NewE201FromTarget(
				fmt.Sprintf("model %q not found in %s", rule.Model, core.DictDir),
				rule.Model, path)
		}
		if merr := nlp.RegisterModel(rule.Model, asset); merr != nil {
			return rule, core.NewE201FromTarget(merr.Error(), rule.Model, path)
		}
	}

	for _, pattern := range rule.Exceptions {
		re, cerr := rx.Compile(pattern)
		if cerr != nil {
			return rule, core.NewE201FromPosition(cerr.Error(), path, 1)
		}
		rule.exceptRe = append(rule.exceptRe, re)
	}

	rule.Definition.Scope = sentenceScope(rule.Definition.Scope)
	rule.filter = rule.literals()

	return rule, nil
}

// exceptionSpans returns the byte spans of every exception region in txt.
func (s Sequence) exceptionSpans(txt string) [][]int {
	var spans [][]int
	for _, re := range s.exceptRe {
		for _, loc := range re.FindAllStringIndex(txt, -1) {
			if lo, hi, ok := runeSpanToBytes(txt, loc[0], loc[1]); ok {
				spans = append(spans, []int{lo, hi})
			}
		}
	}
	return spans
}

// beginsInside reports whether pos falls within any of the given spans.
func beginsInside(spans [][]int, pos int) bool {
	for _, span := range spans {
		if pos >= span[0] && pos < span[1] {
			return true
		}
	}
	return false
}

// literals derives the strongest requirement any single token provides.
func (s *Sequence) literals() []string {
	var best []string
	for i := range s.Tokens {
		pat := s.Tokens[i].Pattern
		if pat == "" || s.Tokens[i].Negate || s.Tokens[i].Skip > 0 {
			// A negated token requires the *absence* of something, and a
			// skipped one is optional. Neither guarantees any text.
			continue
		}
		got := rx.Required(pat)
		if len(got) > 0 && rx.Weight(got) > rx.Weight(best) {
			best = got
		}
	}
	return best
}

// Fields provides access to the rule definition.
func (s Sequence) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s Sequence) Pattern() string {
	return ""
}

func makeTokens(s *Sequence, generic baseCheck) error {
	group := 0

	for _, token := range generic["tokens"].([]interface{}) {
		tok := NLPToken{}
		if err := mapstructure.WeakDecode(token, &tok); err != nil {
			return err
		}

		reps := tok.Min
		if reps < 1 {
			reps = 1
		}

		for r := 0; r < reps; r++ {
			group++
			tok.group = group

			tok.optional = true
			tok.end = false
			for i := tok.Skip; i > 0; i-- {
				tok.start = false
				if i == tok.Skip {
					tok.start = true
				}
				s.Tokens = append(s.Tokens, tok)
			}

			if tok.Pattern != "" || tok.Tag != "" || tok.UPOS != "" {
				tok.optional = false
				tok.start = false
				tok.end = true
				s.Tokens = append(s.Tokens, tok)
			}
		}
	}

	delete(generic, "tokens")
	return nil
}

// negatedToBoundary reports whether every remaining token is negated. A
// negated token asserts the absence of something, and at the edge of the
// sentence there is no word at all, so the assertion holds vacuously. This is
// what lets a rule require "not preceded by X" without also demanding that
// something precede the match.
func negatedToBoundary(toks []NLPToken) bool {
	for _, tok := range toks {
		if !tok.Negate {
			return false
		}
	}
	return true
}

func tokensMatch(token NLPToken, word tag.Token) bool {
	failedTag := token.tagRe == nil || token.tagRe.MatchStringStd(word.Tag)
	failedTag = failedTag == token.Negate
	failedTok := token.tokenRe != nil &&
		token.tokenRe.MatchStringStd(word.Text) == token.Negate

	// A universal tag that Penn cannot express also restricts which words
	// qualify -- `upos: AUX` is "a verb, and one of these words".
	if token.wordRe != nil && token.wordRe.MatchStringStd(word.Text) == token.Negate {
		return false
	}

	if (token.Pattern == "" && failedTag) ||
		(token.Tag == "" && failedTok) ||
		(token.Tag != "" && token.Pattern != "") && (failedTag || failedTok) {
		return false
	}

	return true
}

// match describes one sequence hit: the matched words' text, the index of the
// anchor word, and the range of word indices the match covers.
//
// The index range is what lets Run report where the match actually is. The
// text alone is not enough: the same sequence can occur more than once, and
// re-joining the words does not reproduce the source when the spacing is
// irregular.
type match struct {
	text  []string
	index int
	lo    int
	hi    int

	// wordAt maps a position in the expanded token slice to the word it
	// matched, so a targeted token can be resolved back to its span.
	wordAt map[int]int
}

func (m match) ok() bool { return len(m.text) > 0 && m.lo >= 0 && m.hi >= m.lo }

func sequenceMatches(idx int, chk Sequence, target NLPToken, words []tag.Token, history []int) match {
	var text []string

	toks := chk.Tokens

	sizeT := len(toks)
	sizeW := len(words)
	index := 0
	lo, hi := -1, -1
	wordAt := map[int]int{}

	for jdx, tok := range words {
		if tokensMatch(target, tok) && !core.IntInSlice(jdx, history) {
			index = jdx
			// We've found our context.
			//
			// The *first* token with a `pattern` becomes the anchor of our
			// search. From there, we must check both its left- and right-hand
			// sides to ensure the sequence matches.
			if idx > 0 {
				// Check the left-end of the sequence:
				//
				// If the anchor is the first token, then there's no left-hand
				// side to check -- hence, `idx > 0`.
				ti, wi := idx-1, jdx-1
				for ti >= 0 {
					if wi < 0 {
						if negatedToBoundary(toks[:ti+1]) {
							break
						}
						return match{index: index, lo: -1, hi: -1}
					}
					tok := toks[ti]

					word := words[wi]
					text = append([]string{word.Text}, text...)
					lo = wi
					wordAt[ti] = wi

					// NOTE: We have to perform this conversion because the token slice is made
					// with the right-hand orientation in mind. For example,
					//
					// optional (start), optional, required (end) -> required, optional, optional
					//
					// (from right to left).
					if tok.Skip > 0 {
						tok.optional = (tok.optional || tok.end) && !tok.start
					}

					mat := tokensMatch(tok, word)
					switch {
					case !mat && !tok.optional:
						return match{index: index, lo: -1, hi: -1}
					case mat && tok.optional:
						// The token was found inside its window: the group's
						// spare positions are done with, but the tokens past
						// them still have to hold.
						for ti >= 0 && toks[ti].group == tok.group {
							ti--
						}
						wi--
					default:
						ti--
						wi--
					}
				}
			}
			if idx < sizeT {
				// Check the right-end of the sequence
				//
				// If the anchor is the last token, then there's no right-hand
				// side to check.
				ti, wi := idx, jdx
				for ti < sizeT {
					if wi >= sizeW {
						if negatedToBoundary(toks[ti:]) {
							break
						}
						return match{index: index, lo: -1, hi: -1}
					}
					tok := toks[ti]

					word := words[wi]
					text = append(text, word.Text)
					if lo < 0 || wi < lo {
						lo = wi
					}
					hi = wi
					wordAt[ti] = wi

					mat := tokensMatch(tok, word)
					switch {
					case !mat && !tok.optional:
						return match{index: index, lo: -1, hi: -1}
					case mat && tok.optional:
						for ti < sizeT && toks[ti].group == tok.group {
							ti++
						}
						wi++
					default:
						ti++
						wi++
					}
				}
			}
			break
		}
	}

	return match{text: text, index: index, lo: lo, hi: hi, wordAt: wordAt}
}

func stepsToString(steps []string) string {
	var sb strings.Builder

	for i, step := range steps {
		switch {
		case step == "." || step == "," || step == ":" || step == ";" || step == "!" || step == "?" || step == "'" || step == `"` || step == ")":
			// No space before punctuation or closing parenthesis
			sb.WriteString(step)
		case step == "(":
			// No space before or after an opening parenthesis
			if i > 0 && sb.Len() > 0 {
				lastChar := sb.String()[sb.Len()-1]
				if lastChar != ' ' {
					sb.WriteString(" ")
				}
			}
			sb.WriteString(step)
		case strings.HasPrefix(step, "'"):
			// If the step starts with an apostrophe, attach it without space
			sb.WriteString(step)
		default:
			// Otherwise, add space before the word
			if sb.Len() > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(step)
		}
	}

	return strings.TrimSpace(sb.String())
}

// locate returns the span of a match within txt, plus the matched text.
//
// When the tokens carry offsets, the span is taken straight from them, so it
// is exact even when the sequence occurs more than once or the source spacing
// is irregular. Without offsets we fall back to rebuilding the text and
// searching for it, which is what Vale did before tokens had positions; that
// path returns nil rather than a negative span when the search fails.
func (s Sequence) locate(txt string, words []tag.Token, m match, positioned bool) ([]int, string) {
	if positioned && m.hi < len(words) {
		lo, hi := m.lo, m.hi
		if tlo, thi, ok := s.targetRange(m); ok {
			lo, hi = tlo, thi
		}

		start := words[lo].Start
		end := words[hi].Start + len(words[hi].Text)
		if start >= 0 && end <= len(txt) && start < end {
			return []int{start, end}, txt[start:end]
		}
	}

	seq := stepsToString(m.text)
	if ssp := strings.Index(txt, seq); ssp >= 0 {
		return []int{ssp, ssp + len(seq)}, seq
	}
	return nil, seq
}

// targetRange returns the span of words covered by the rule's `target`
// tokens.
//
// Several tokens may be marked, in which case the range runs from the first to
// the last -- a target of two words reports both, not just one. Unmarked
// tokens between them are included, since the result has to be a single
// contiguous span.
func (s Sequence) targetRange(m match) (int, int, bool) {
	lo, hi := -1, -1
	for i := range s.Tokens {
		if !s.Tokens[i].Target {
			continue
		}
		w, ok := m.wordAt[i]
		if !ok {
			// A targeted token that matched nothing -- an optional or skipped
			// one. Narrowing to a range we cannot fully resolve would report
			// the wrong span, so fall back to the whole match.
			return 0, 0, false
		}
		if lo < 0 || w < lo {
			lo = w
		}
		if w > hi {
			hi = w
		}
	}
	if lo < 0 {
		return 0, 0, false
	}
	return lo, hi, true
}

// Run looks for the user-defined sequence of tokens.
// sentenceScope narrows the scopes a `sequence` rule was given to the
// sentences within them.
//
// The rule reads part-of-speech data, which is tagged a sentence at a time, so
// it always runs on sentences -- but *which* sentences is the author's to say.
// Declaring `list` means the sentences of list items, not the whole document's.
// An undeclared scope means all of them.
//
// A scope that already names sentences is left as it is. Any other is joined
// to `sentence` with the grammar's own conjunction, which reads the same set
// of blocks as `sentence.list` would and holds for a term the dot cannot
// qualify, such as a selector.
func sentenceScope(declared []string) []string {
	if len(declared) == 0 {
		return []string{"sentence"}
	}

	scopes := make([]string, 0, len(declared))
	for _, s := range declared {
		if s == "sentence" || strings.HasPrefix(s, "sentence.") {
			scopes = append(scopes, s)
			continue
		}
		// Negation applies to the part being excluded, so it stays in front:
		// `~list` narrows to sentences outside a list, not to something
		// outside `sentence.list`.
		if neg, found := strings.CutPrefix(s, "~"); found {
			scopes = append(scopes, "~"+neg)
			continue
		}
		// `paragraph` names no block of its own. Splitting wraps every block
		// as `paragraph.<scope>` -- headings and list items included -- so the
		// scope already describes the same blocks an undeclared one does.
		// Narrowing it to `sentence.paragraph` asked for blocks that are never
		// built, and the rule matched nothing without saying so. See #1126.
		if rest, found := strings.CutPrefix(s, "paragraph"); found &&
			(rest == "" || strings.HasPrefix(rest, ".")) {
			scopes = append(scopes, "sentence"+rest)
			continue
		}
		scopes = append(scopes, "sentence & "+s)
	}

	return scopes
}

func (s Sequence) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert
	var offset []string
	var history []int

	// Rule the sequence out before tagging, which is the expensive part.
	if len(s.filter) > 0 && !containsAny(blk.Lower, s.filter) {
		return nil, nil
	}

	// This is *always* sentence-scoped.
	words, terr := f.TokensWith(s.Model, blk.Text)
	if terr != nil {
		return nil, terr
	}

	// A remote NLP endpoint returns text and tags only, so we have no offsets
	// to work from and have to fall back to locating the match by its text.
	positioned := f.NLP.Endpoint == ""

	txt := blk.Text
	excluded := s.exceptionSpans(txt)

	idx, tok, ok := s.anchor()
	if ok {
		{
			// Each candidate position for the anchor is one possible
			// violation. A `pattern` anchor enumerates them by searching the
			// text; a tag-only anchor has nothing to search for, so we let
			// sequenceMatches walk the words and stop when it runs out.
			for _, loc := range s.candidates(txt, tok, len(words)) {
				// These are all possible violations in `txt`:
				m := sequenceMatches(idx, s, tok, words, history)
				history = append(history, m.index)

				if m.ok() {
					span, seq := s.locate(txt, words, m, positioned)
					if span == nil {
						// We matched but cannot say where; reporting a bogus
						// span is worse than reporting nothing.
						continue
					}

					if beginsInside(excluded, span[0]) {
						continue
					}

					// When the block knows where it sits in the document, hand
					// back an absolute offset. Otherwise the span is
					// block-relative and has to be located by searching, which
					// resolves every repeat of a sentence to the first one.
					absolute := blk.Offset >= 0
					if absolute {
						span = []int{blk.Offset + span[0], blk.Offset + span[1]}
					}

					action := s.Action
					if s.MatchCase && action.Name == "replace" {
						action.Params = recase(action.Params, seq)
					}

					a := core.Alert{
						Check: s.Name, Severity: s.Level, Link: s.Link,
						Span: span, Hide: false, HasByteOffsets: absolute,
						Match: seq, Action: action}

					a.Message, a.Description = formatMessages(s.Message,
						s.Description, m.text...)
					a.Offset = offset

					if err := resolveFix(&a, cfg); err != nil {
						return alerts, err
					}

					alerts = append(alerts, a)
					offset = []string{}
				} else if loc != nil {
					converted, err := re2Loc(txt, loc)
					if err != nil {
						return alerts, err
					}
					offset = append(offset, converted)
				}
			}
		}
	}

	return alerts, nil
}

// anchor picks the token the search starts from.
//
// A `pattern` token is preferred because it can be located in the text
// directly. Failing that any tagged token will do -- without this, a rule made
// only of tags matched nothing at all, silently.
func (s Sequence) anchor() (int, NLPToken, bool) {
	for i, tok := range s.Tokens {
		if !tok.Negate && tok.Pattern != "" {
			return i, tok, true
		}
	}
	for i, tok := range s.Tokens {
		if !tok.Negate && tok.Tag != "" {
			return i, tok, true
		}
	}
	return 0, NLPToken{}, false
}

// candidates returns one entry per position the anchor might occupy.
//
// For a `pattern` anchor each entry is the match's location in the text, which
// the caller reports as an offset when the surrounding sequence does not pan
// out. A tag-only anchor has no such location, so the entries are nil and the
// count simply bounds how many times to try.
func (s Sequence) candidates(txt string, tok NLPToken, words int) [][]int {
	if tok.re != nil {
		return tok.re.FindAllStringIndex(txt, -1)
	}
	return make([][]int, words)
}

// containsAny reports whether lowered holds any of the literals.
func containsAny(lowered string, lits []string) bool {
	for _, l := range lits {
		if strings.Contains(lowered, l) {
			return true
		}
	}
	return false
}
