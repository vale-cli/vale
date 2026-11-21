package check

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/exp/maps"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
	rx "github.com/vale-cli/vale/v3/internal/regex"
)

// Substitution switches the values of Swap for its keys.
type Substitution struct {
	Definition `mapstructure:",squash"`
	Exceptions []string
	repl       []string
	Swap       map[string]string
	// `affix` names the affix file whose flags a `word/FLAGS` key uses.
	Affix      string
	exceptRe   *rx.Regexp
	phraseRe   *rx.Regexp
	pattern    *rx.Regexp
	split      *tokenSplit
	Ignorecase bool
	Nonword    bool
	Vocab      bool
	Capitalize bool

	// terms marks a rule built from a vocabulary, whose swap values may be
	// patterns: a match is offered as a fix only when the expected form is
	// plain text.
	terms bool

	msgMap []string

	// Deprecated
	POS string
}

// NewSubstitution creates a new `substitution`-based rule.
func NewSubstitution(cfg *core.Config, generic baseCheck, path string) (Substitution, error) {
	rule := Substitution{Vocab: true}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}
	if rule.Affix != "" {
		swap := make(map[string]string, len(rule.Swap))
		for key, value := range rule.Swap {
			expanded, aerr := affixTokens(cfg, rule.Affix, []string{key}, path)
			if aerr != nil {
				return rule, aerr
			}
			swap[expanded[0]] = value
		}
		rule.Swap = swap
	}
	var tokens strings.Builder

	re, err := updateExceptions(cfg, rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)
	if err != nil {
		return rule, patternError(err, path, rule.Exceptions...)
	}
	rule.exceptRe = re
	rule.phraseRe = buildPhraseRe(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)

	regex := makeRegexp(
		cfg.WordTemplate,
		rule.Ignorecase,
		func() bool { return !rule.Nonword },
		func() string { return "" }, true)

	terms := maps.Keys(rule.Swap)
	sort.Slice(terms, func(p, q int) bool {
		return len(terms[p]) > len(terms[q])
	})

	shared := sharedLookaround(terms)

	replacements := []string{}
	parts := []string{}
	for _, regexstr := range terms {
		rule.msgMap = append(rule.msgMap, regexstr)
		replacement := rule.Swap[regexstr]

		regexstr = wrapSpaces(strings.TrimPrefix(regexstr, shared), rule.Scope)

		opens := strings.Count(regexstr, "(")
		if opens != strings.Count(regexstr, "(?")+strings.Count(regexstr, `\(`) {
			// We have a capture group, so we need to make it non-capturing.
			regexstr, err = convertCaptureGroups(regexstr)
			if err != nil {
				return rule, core.NewE201FromTarget(err.Error(), regexstr, path)
			}
		}
		if tokens.Len() > 0 {
			tokens.WriteByte('|')
		}
		tokens.WriteString(`(` + regexstr + `)`)
		parts = append(parts, regexstr)
		replacements = append(replacements, replacement)
	}

	joined := tokens.String()
	each := `(%s)`
	if shared != "" {
		joined = shared + `(?:` + joined + `)`
		each = strings.ReplaceAll(shared, "%", "%%") + `(?:(%s))`
	}
	template := strings.Replace(regex, "%s", each, 1)
	regex = fmt.Sprintf(regex, joined)

	re, err = rx.CompileList(regex, len(terms))
	if err != nil {
		return rule, patternError(err, path, terms...)
	}

	rule.pattern = re
	rule.repl = replacements
	// A template of the user's own may match in more than one way around a
	// term, and then the alternation's order is not the terms' alone.
	if cfg.WordTemplate == "" || rule.Nonword {
		rule.split = newTokenSplit(template, parts)
	}
	return rule, nil
}

// sharedLookaround returns a leading lookaround every term begins with.
//
// A lookaround is zero-width, so asserting it once ahead of the alternation is
// the same as asserting it in every branch -- and costs one evaluation per
// position instead of one per term. Nothing capturing is hoisted, so the group
// numbers the replacements are keyed on stay put.
func sharedLookaround(terms []string) string {
	if len(terms) < 2 {
		return ""
	}

	lead := leadingGroup(terms[0])
	if lead == "" || strings.Count(lead, "(") != strings.Count(lead, "(?")+strings.Count(lead, `\(`) {
		return ""
	}

	for _, t := range terms[1:] {
		if !strings.HasPrefix(t, lead) {
			return ""
		}
	}

	return lead
}

// leadingGroup returns the lookaround expr opens with, brackets included.
func leadingGroup(expr string) string {
	if !strings.HasPrefix(expr, "(?=") && !strings.HasPrefix(expr, "(?!") &&
		!strings.HasPrefix(expr, "(?<=") && !strings.HasPrefix(expr, "(?<!") {
		return ""
	}

	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return expr[:i+1]
			}
		}
	}

	return ""
}

// Run executes the `substitution`-based rule.
//
// The rule looks for one pattern and then suggests a replacement.
func (s Substitution) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	vocab := vocabFor(cfg, f)
	var alerts []core.Alert

	txt := blk.Text
	// A substring search is far cheaper than the pattern, and a false answer
	// is definitive. A split rule asks each token instead, in matches.
	if s.split == nil && !s.pattern.MightMatch(blk.Lower) {
		return alerts, nil
	}

	locs, terms := s.matches(blk)
	for n, loc := range locs {
		converted, err := re2Loc(blk, loc)
		if err != nil {
			return alerts, err
		}

		observed := converted
		expected, msgErr := subMsg(s, terms[n], observed)
		if msgErr != nil {
			return alerts, msgErr
		}

		// Determine whether `observed` is already in an acceptable
		// form (a no-op suggestion). For a `replace` action the swap
		// value is literal replacement text -- one or more `|`-separated
		// suggestions (the same split used to build `action.Params`
		// below) -- so we compare each option literally. Otherwise an
		// option like `...` would be read as a regex matching any three
		// characters (e.g. `,,,`), suppressing a real finding (#1038).
		//
		// For non-`replace` rules the value may instead be a regex that
		// describes the acceptable forms (e.g. a vocab term `[pP]y.*\b`,
		// or a `LookAround`-style pattern), which we still match as one.
		var same bool
		switch {
		case s.Fields().Action.Name == "replace":
			same = core.StringInSlice(observed, getOptions(expected))
		case s.terms:
			// A phrase may wrap across a line where the term has a space.
			same = matchToken(termPattern(expected), observed, false)
		default:
			same = matchToken(expected, observed, false)
		}
		if !same && !isMatch(s.exceptRe, observed) && !withinPhrase(s.phraseRe, txt, loc) &&
			!vocab.accepts(observed, txt, loc) {
			action := s.Fields().Action
			if action.Name == "replace" && len(action.Params) == 0 {
				action.Params = getOptions(expected)
				if s.Capitalize && observed == core.CapFirst(observed) {
					cased := []string{}
					for _, param := range action.Params {
						cased = append(cased, core.CapFirst(param))
					}
					action.Params = cased
				}

				if s.MatchCase {
					action.Params = recase(action.Params, observed)
				}

				expected = core.ToSentence(action.Params, "or")
				// NOTE: For backwards-compatibility, we need to ensure
				// that we don't double quote.
				s.Message = convertMessage(s.Message)
			} else if action.Name != "replace" {
				// For non-`replace` rules (e.g. Vale.Terms built from a
				// vocab), the swap value may be a regex describing the
				// term. Show the observed text re-cased to the term's
				// canonical form instead of the raw pattern -- e.g.
				// `OAuth2?` -> `OAuth2`. See #997.
				expected = recaseToTerm(expected, observed)
			}

			a, aerr := alertFor(s.Definition, loc, observed, cfg)
			if aerr != nil {
				return alerts, aerr
			}

			a.Message, a.Description = formatMessages(s.Message,
				s.Description, expected, observed)
			a.Action = action
			if action.Name == "replace" {
				a.Suggestions = action.Params
			} else if s.terms && expected != observed && literalTerm(expected) {
				a.Action = core.Action{Name: "replace", Params: []string{keepWrap(expected, observed)}}
				a.Suggestions = a.Action.Params
			}

			anchor(&a, blk)
			alerts = append(alerts, a)
		}
	}

	return alerts, nil
}

// matches returns the rune bounds of each match in the block and the index
// of the term that made it. A rule whose terms can run apart runs only those
// the block could match; see tokenSplit.
func (s Substitution) matches(blk nlp.Block) ([][]int, []int) {
	if s.split != nil {
		active := s.split.active(blk.Lower)
		if len(active) == 0 {
			return nil, nil
		}
		if len(active) <= splitCeiling {
			if found, by, ok := s.split.find(blk.Text, active); ok {
				return found, by
			}
		}
	}

	// Leave early if we can to avoid calling `FindAllStringSubmatchIndex`
	// unnecessarily.
	//
	// This looks like a wasted scan on a block that does match, and it was
	// measured as the cheaper arrangement anyway: MatchString stops at the
	// first hit, and a block with no match skips the allocation the Find below
	// makes for its submatch slices.
	if !s.pattern.MatchStringStd(blk.Text) {
		return nil, nil
	}

	var (
		locs  [][]int
		terms []int
	)
	for _, submat := range s.pattern.FindAllStringSubmatchIndex(blk.Text, -1) {
		// An alternation sets exactly one group, so the rest of the slots
		// only hold -1.
		for idx := 2; idx < len(submat); idx += 2 {
			if submat[idx] != -1 {
				locs = append(locs, []int{submat[idx], submat[idx+1]})
				terms = append(terms, (idx/2)-1)
				break
			}
		}
	}
	return locs, terms
}

// Fields provides access to the internal rule definition.
func (s Substitution) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s Substitution) Pattern() string {
	return s.pattern.String()
}

// literalSkeleton returns the literal characters of a regex pattern, in order,
// or "" if the pattern uses constructs that can't be reduced to a single
// fixed-length literal (character classes, groups, alternation, wildcards, or
// `*`/`+`/`{}` repetition). A trailing `?` (optional single char) is allowed;
// the optional char is kept, and callers fall back when the length doesn't
// line up with the observed text.
func literalSkeleton(pattern string) string {
	var b strings.Builder
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '\\':
			// Escaped literal -- take the next rune verbatim.
			if i+1 >= len(runes) {
				return ""
			}
			b.WriteRune(runes[i+1])
			i++
		case '?':
			// Optional previous char: already written, nothing to add.
		case '[', ']', '(', ')', '{', '}', '|', '.', '*', '+', '^', '$':
			return ""
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Alternations multiply out fast; past a handful the term is no longer one
// word spelled several ways.
const maxExpansions = 64

// expandPattern enumerates every spelling a vocab term can match -- `OAuth2?`
// gives OAuth2 and OAuth -- or nil if the pattern is not a finite set.
func expandPattern(pattern string) []string {
	runes := []rune(pattern)

	// parse reads an alternation at i, returning its spellings and the index
	// just past what it consumed.
	var parse func(i int, nested bool) (out []string, next int, ok bool)
	parse = func(i int, nested bool) ([]string, int, bool) {
		branches := []string{}  // completed alternatives
		current := []string{""} // spellings of the branch being read

		// cross appends every element of add to every spelling so far.
		cross := func(add []string) bool {
			if len(current)*len(add) > maxExpansions {
				return false
			}
			next := make([]string, 0, len(current)*len(add))
			for _, prefix := range current {
				for _, suffix := range add {
					next = append(next, prefix+suffix)
				}
			}
			current = next
			return true
		}

		for i < len(runes) {
			var atom []string

			switch r := runes[i]; r {
			case ')':
				if !nested {
					return nil, 0, false // unbalanced
				}
				return append(branches, current...), i + 1, true
			case '|':
				branches = append(branches, current...)
				current = []string{""}
				i++
				continue
			case '(':
				start := i + 1
				// Capturing or not makes no difference to the spellings.
				if strings.HasPrefix(string(runes[start:]), "?:") {
					start += 2
				} else if start < len(runes) && runes[start] == '?' {
					return nil, 0, false // lookaround, named group, flags
				}
				inner, next, ok := parse(start, true)
				if !ok {
					return nil, 0, false
				}
				atom, i = inner, next
			case '\\':
				if i+1 >= len(runes) {
					return nil, 0, false
				}
				// `\d`, `\w`, `\b` are not fixed spellings.
				next := runes[i+1]
				if unicode.IsLetter(next) || unicode.IsDigit(next) {
					return nil, 0, false
				}
				atom = []string{string(next)}
				i += 2
			case '[', ']', '{', '}', '.', '*', '+', '^', '$':
				return nil, 0, false
			default:
				atom = []string{string(r)}
				i++
			}

			// `?` makes the atom optional: add the empty spelling too.
			if i < len(runes) && runes[i] == '?' {
				atom = append(append([]string{}, atom...), "")
				i++
			} else if i < len(runes) && (runes[i] == '*' || runes[i] == '+' || runes[i] == '{') {
				return nil, 0, false
			}

			if !cross(atom) {
				return nil, 0, false
			}
		}

		if nested {
			return nil, 0, false // unterminated group
		}
		return append(branches, current...), i, true
	}

	out, next, ok := parse(0, false)
	if !ok || next != len(runes) || len(out) == 0 {
		return nil
	}

	return out
}

// Computed once per term, not once per alert; a run can raise hundreds of
// thousands against the same few terms.
var expansions sync.Map // pattern -> []string

func expansionsFor(term string) []string {
	if cached, ok := expansions.Load(term); ok {
		return cached.([]string)
	}

	out := expandPattern(term)
	if out == nil {
		if skel := literalSkeleton(term); skel != "" {
			out = []string{skel}
		}
	}

	expansions.Store(term, out)
	return out
}

// recaseToTerm re-cases observed to a vocab term's canonical spelling, e.g.
// `OAuth2?` against `Oauth` yields `OAuth`. It returns term unchanged when no
// spelling matches, so the raw regex is only ever a last resort. See #997.
func recaseToTerm(term, observed string) string {
	unwrapped := strings.Join(strings.Fields(observed), " ")
	for _, candidate := range expansionsFor(term) {
		if strings.EqualFold(candidate, unwrapped) {
			return candidate
		}
	}
	return term
}

// keepWrap returns expected with the whitespace observed has between its
// words, so a fix to a phrase that wraps across a line keeps the wrap.
func keepWrap(expected, observed string) string {
	words := strings.Fields(expected)
	gaps := wrapGaps.FindAllString(observed, -1)
	if len(gaps) != len(words)-1 {
		return expected
	}
	var b strings.Builder
	for i, w := range words {
		if i > 0 {
			b.WriteString(gaps[i-1])
		}
		b.WriteString(w)
	}
	return b.String()
}

var wrapGaps = regexp.MustCompile(`\s+`)

func convertMessage(s string) string {
	for _, spec := range []string{"'%s'", "\"%s\""} {
		if strings.Count(s, spec) == 2 {
			s = strings.Replace(s, spec, "%s", 1)
		}
	}
	return s
}

func convertCaptureGroups(msg string) (string, error) {
	captureOpen := rx.MustCompile(`(?<!\\)\((?!\?)`)
	return captureOpen.Replace(msg, "(?:", -1, -1)
}

func subMsg(s Substitution, index int, observed string) (string, error) {
	// Based on the current capture group (`idx`), we can determine
	// the associated replacement string by using the `repl` slice:
	expected := s.repl[index]
	if s.Capitalize && observed == core.CapFirst(observed) {
		expected = core.CapFirst(expected)
	}

	// TODO: Why do we need to check for this?
	//
	// This feels like a bug in `regexp2`.
	hasIndex := rx.MustCompile(`\$\d+`)
	if !hasIndex.MatchStringStd(expected) {
		return expected, nil
	}

	msg := s.msgMap[index]
	if s.Ignorecase {
		msg = `(?i)` + msg
	}

	msgRe := rx.MustCompile(msg)
	return msgRe.Replace(observed, expected, -1, -1)
}

// reUnescapedPipe separates a suggestion's options: a `|` not preceded by a
// backslash. An escaped `\|` is a literal pipe in the option's text.
var reUnescapedPipe = rx.MustCompile(`(?<!\\)\|`)

// getOptions returns a slice of options from a match.
//
// For example, given the match "a|b|c", this function will return
// []string{"a", "b", "c"}.
//
// This allows the user to specify multiple options for a single match.
//
// https://vale.sh/docs/checks/substitution#multiple-suggestions
func getOptions(match string) []string {
	options := []string{}

	for _, option := range reUnescapedPipe.Split(match, -1) {
		if option != "" {
			options = append(options, strings.ReplaceAll(option, `\|`, `|`))
		}
	}

	return options
}
