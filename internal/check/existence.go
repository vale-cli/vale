package check

import (
	"fmt"
	"strconv"
	"strings"

	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Existence checks for the present of Tokens.
type Existence struct {
	Definition `mapstructure:",squash"`
	Raw        []string
	Tokens     []string
	// `affix` names the affix file whose flags a `word/FLAGS` token uses.
	Affix string
	// `exceptions` (`array`): An array of strings to be ignored.
	Exceptions []string
	exceptRe   *rx.Regexp
	phraseRe   *rx.Regexp
	pattern    *rx.Regexp
	split      *tokenSplit
	groups     []groupSpan
	Append     bool
	IgnoreCase bool
	Nonword    bool
	Vocab      bool
}

// NewExistence creates a new `Rule` that extends `Existence`.
func NewExistence(cfg *core.Config, generic baseCheck, path string) (Existence, error) {
	rule := Existence{Vocab: true}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	rule.Tokens, err = affixTokens(cfg, rule.Affix, rule.Tokens, path)
	if err != nil {
		return rule, err
	}

	// `Vocab` is a list of accepted *words*, so it only makes sense to treat
	// it as a set of exceptions for word-based rules. For `nonword` rules --
	// whose tokens match arbitrary spans (e.g. `"[^"]+"[.,]`) -- a vocab word
	// would suppress any match that merely *contains* it (e.g. `"plugh",`).
	//
	// See https://github.com/errata-ai/vale/issues/1058.
	re, err := updateExceptions(cfg, rule.Exceptions, cfg.AcceptedTokens, rule.Vocab && !rule.Nonword)
	if err != nil {
		return rule, patternError(err, path, rule.Exceptions...)
	}
	rule.exceptRe = re
	rule.phraseRe = buildPhraseRe(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab && !rule.Nonword)

	regex := makeRegexp(
		cfg.WordTemplate,
		rule.IgnoreCase,
		func() bool { return !rule.Nonword && len(rule.Tokens) > 0 },
		func() string { return strings.Join(rule.Raw, "") },
		rule.Append)

	parsed := []string{}
	for _, token := range rule.Tokens {
		if strings.TrimSpace(token) != "" {
			parsed = append(parsed, wrapSpaces(token, rule.Scope))
		}
	}
	template := regex
	regex = fmt.Sprintf(regex, strings.Join(parsed, "|"))

	re, err = rx.CompileList(regex, len(parsed))
	if err != nil {
		return rule, patternError(err, path, append(parsed, rule.Raw...)...)
	}
	rule.pattern = re
	// Raw text or a template of the user's own may match in more than one
	// way around a token, and then the alternation's order is not the
	// tokens' alone.
	if len(rule.Raw) == 0 && (cfg.WordTemplate == "" || rule.Nonword) {
		rule.split = newTokenSplit(template, parsed)
	}
	// Counting each token's groups compiles it again, which a pattern with
	// no groups at all has no need of.
	if len(re.SubexpNames()) > 1 {
		rule.groups = groupSpans(parsed, strings.Join(rule.Raw, ""), rule.Append)
	}

	return rule, nil
}

// A groupSpan is where one token's capture groups sit in the joined pattern:
// its unnamed groups from `unnamed`, its named groups from `named`.
type groupSpan struct {
	unnamed, unnamedCount int
	named, namedCount     int
}

// groupSpans maps each token's groups into the joined pattern. The engine
// numbers every unnamed group before any named one, so a token's groups are
// two runs rather than one.
func groupSpans(tokens []string, raw string, rawLast bool) []groupSpan {
	pieces := append([]string{raw}, tokens...)
	if rawLast {
		pieces = append(append([]string{}, tokens...), raw)
	}

	spans := make([]groupSpan, len(pieces))
	unnamed, named := 0, 0
	for i, piece := range pieces {
		u, n := groupCounts(piece)
		spans[i] = groupSpan{unnamed: unnamed, unnamedCount: u, named: named, namedCount: n}
		unnamed += u
		named += n
	}
	for i := range spans {
		spans[i].named += unnamed
	}

	if rawLast {
		return spans[:len(tokens)]
	}
	return spans[1:]
}

func groupCounts(expr string) (int, int) {
	if expr == "" {
		return 0, 0
	}
	re, err := rx.Compile(expr)
	if err != nil {
		return 0, 0
	}

	unnamed, named := 0, 0
	for _, name := range re.SubexpNames()[1:] {
		if _, numeric := strconv.Atoi(name); numeric == nil {
			unnamed++
		} else {
			named++
		}
	}
	return unnamed, named
}

// groupsFor returns the capture groups of whichever token matched, in that
// token's own numbering, or nil when the token has none.
func (e Existence) groupsFor(blk nlp.Block, sub []int) []string {
	for _, span := range e.groups {
		indices := make([]int, 0, span.unnamedCount+span.namedCount)
		for k := 1; k <= span.unnamedCount; k++ {
			indices = append(indices, span.unnamed+k)
		}
		for k := 1; k <= span.namedCount; k++ {
			indices = append(indices, span.named+k)
		}

		matched := false
		groups := make([]string, 0, len(indices))
		for _, idx := range indices {
			lo, hi := sub[2*idx], sub[2*idx+1]
			if lo < 0 {
				groups = append(groups, "")
				continue
			}
			text, err := re2Loc(blk, []int{lo, hi})
			if err != nil {
				return nil
			}
			groups = append(groups, text)
			matched = true
		}
		if matched {
			return groups
		}
	}
	return nil
}

// Run executes the `existence`-based rule.
//
// This is simplest of the available extension points: it looks for any matches
// of its internal `pattern` (calculated from `NewExistence`) against the
// provided text.
func (e Existence) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	vocab := vocabFor(cfg, f)
	alerts := []core.Alert{}

	// Rule out the pattern before the engine sees it: almost every rule is
	// asked about text it cannot match, and a substring search is far cheaper
	// than a regular expression. A false answer here is definitive. A split
	// rule asks each token instead, in matches.
	if e.split == nil && !e.pattern.MightMatch(blk.Lower) {
		return alerts, nil
	}

	for _, sub := range e.matches(blk) {
		loc := sub[:2]
		converted, err := re2Loc(blk, loc)
		if err != nil {
			return alerts, err
		}

		observed := strings.TrimSpace(converted)
		if !isMatch(e.exceptRe, observed) && !withinPhrase(e.phraseRe, blk.Text, loc) &&
			!vocab.accepts(observed, blk.Text, loc) {
			a, erra := alertWithGroups(e.Definition, loc, converted,
				e.groupsFor(blk, sub), cfg)
			if erra != nil {
				return alerts, erra
			}
			anchor(&a, blk)
			alerts = append(alerts, a)
		}
	}

	return alerts, nil
}

// matches returns the rune bounds of each match in the block, and of its
// groups. A rule whose tokens can run apart runs only those the block could
// match; see tokenSplit.
func (e Existence) matches(blk nlp.Block) [][]int {
	if e.split != nil {
		active := e.split.active(blk.Lower)
		if len(active) == 0 {
			return nil
		}
		if len(active) <= splitCeiling {
			if found, _, ok := e.split.find(blk.Text, active); ok {
				return found
			}
		}
	}
	return e.pattern.FindAllStringSubmatchIndex(blk.Text, -1)
}

// Fields provides access to the internal rule definition.
func (e Existence) Fields() Definition {
	return e.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (e Existence) Pattern() string {
	return e.pattern.String()
}
