package check

import (
	"fmt"
	"strings"

	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Conditional ensures that the present of First ensures the present of Second.
type Conditional struct {
	Definition `mapstructure:",squash"`
	Exceptions []string
	patterns   []*rx.Regexp
	First      string
	Second     string
	// In names the View scope Second is looked for in. Without it, Second
	// is looked for in the same block as First.
	In         string
	exceptRe   *rx.Regexp
	phraseRe   *rx.Regexp
	Ignorecase bool
	Vocab      bool

	// secondHasGroup records whether `Second` has a capture group. When it
	// does, a `First` match is allowed only if its value was captured by a
	// `Second` match (e.g. an acronym defined as `... (WHO)`). When it doesn't,
	// the rule is a plain presence check: any `First` requires `Second` to
	// appear somewhere in the same block. See #1048.
	secondHasGroup bool
}

// hasCaptureGroup reports whether `pattern` contains a capturing group -- an
// unescaped `(` that doesn't begin a non-capturing/extension group `(?...)`.
func hasCaptureGroup(pattern string) bool {
	inClass := false
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			// An escape takes the next character with it: `\(` is a literal
			// paren, and `\[` never opens a class.
			i++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '(':
			if inClass {
				// A paren inside a character class, `[^()]`, is a member of
				// the class and not a group.
				continue
			}
			if i+1 < len(pattern) && pattern[i+1] == '?' {
				// `(?:`, `(?=`, `(?<=`, `(?i)`: not captures. A named group,
				// `(?<name>` or `(?P<name>`, is one.
				rest := pattern[i+2:]
				if strings.HasPrefix(rest, "P<") {
					return true
				}
				if strings.HasPrefix(rest, "<") && !strings.HasPrefix(rest, "<=") && !strings.HasPrefix(rest, "<!") {
					return true
				}
				continue
			}
			return true
		}
	}
	return false
}

// NewConditional creates a new `conditional`-based rule.
func NewConditional(cfg *core.Config, generic baseCheck, path string) (Conditional, error) {
	var expression []*rx.Regexp
	rule := Conditional{Vocab: true}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	if rule.In != "" && !viewDefinesScope(cfg, rule.In) {
		return rule, core.NewE201FromTarget(
			fmt.Sprintf("no View defines a scope named '%s'", rule.In), "in", path)
	}

	re, err := updateExceptions(cfg, rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)
	if err != nil {
		return rule, patternError(err, path, rule.Exceptions...)
	}
	rule.exceptRe = re
	rule.phraseRe = buildPhraseRe(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)

	re, err = rx.Compile(rule.Second)
	if err != nil {
		return rule, patternError(err, path, rule.Second)
	}
	expression = append(expression, re)
	rule.secondHasGroup = hasCaptureGroup(rule.Second)

	re, err = rx.Compile(rule.First)
	if err != nil {
		return rule, patternError(err, path, rule.First)
	}
	expression = append(expression, re)

	// TODO: How do we support multiple patterns?
	rule.patterns = expression
	return rule, nil
}

// viewDefinesScope reports whether any View names a scope `name`.
func viewDefinesScope(cfg *core.Config, name string) bool {
	for _, view := range cfg.Views {
		for _, s := range view.Scopes {
			if s.Name == name {
				return true
			}
		}
	}
	return false
}

// Run evaluates the given conditional statement.
func (c Conditional) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}

	txt := blk.Text

	// Every alert this rule raises, on either branch below, comes from a match
	// of the antecedent -- so if that cannot match, there is nothing to flag.
	//
	// It has to be the antecedent and not the consequent: the rule fires when
	// the consequent is *absent*, so ruling the block out on the consequent
	// would suppress exactly the alerts it exists to raise. Same reason
	// `occurrence` is left unfiltered, where a `min` alerts on a count of zero.
	if !c.patterns[1].MightMatch(blk.Lower) {
		return alerts, nil
	}

	// The consequent is looked for in this block, or, with `in`, in the
	// values of another of the View's scopes.
	sources := []string{txt}
	if c.In != "" {
		sources = f.Scoped[c.In]
	}

	// When `Second` has no capture group, the rule is a plain presence check:
	// if `First` appears, `Second` must appear somewhere in the same block. If
	// it does, there's nothing to flag; otherwise every `First` match is a
	// violation. See #1048.
	if !c.secondHasGroup {
		for _, src := range sources {
			if c.patterns[0].MatchStringStd(src) {
				return alerts, nil
			}
		}
		return c.flagAntecedents(blk, cfg)
	}

	// We first look for the consequent of the conditional statement.
	// For example, if we're ensuring that abbreviations have been defined
	// parenthetically, we'd have something like:
	//
	//     "WHO" [antecedent], "World Health Organization (WHO)" [consequent]
	//
	// In other words: if "WHO" exists, it must also have a definition -- which
	// we're currently looking for.
	for _, src := range sources {
		for _, mat := range c.patterns[0].FindAllStringSubmatch(src, -1) {
			if len(mat) > 1 {
				// If we find one, we store it in a slice associated with
				// this particular file.
				for _, m := range mat[1:] {
					if len(m) > 0 {
						f.Sequences = append(f.Sequences, m)
					}
				}
			}
		}
	}

	// Now we look for the antecedent.
	locs := c.patterns[1].FindAllStringIndex(txt, -1)
	for _, loc := range locs {
		s, err := re2Loc(blk, loc)
		if err != nil {
			return alerts, err
		}

		if !core.StringInSlice(s, f.Sequences) && !isMatch(c.exceptRe, s) && !withinPhrase(c.phraseRe, txt, loc) {
			// If we've found one (e.g., "WHO") and we haven't marked it as
			// being defined previously, send an Alert.
			a, erra := makeAlert(c.Definition, loc, blk, cfg)
			if erra != nil {
				return alerts, erra
			}
			alerts = append(alerts, a)
		}
	}

	return alerts, nil
}

// flagAntecedents reports every `First` match as a violation (used by the
// presence check when `Second` is absent), honoring the rule's exceptions and
// accepted phrases.
func (c Conditional) flagAntecedents(blk nlp.Block, cfg *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}
	txt := blk.Text
	for _, loc := range c.patterns[1].FindAllStringIndex(txt, -1) {
		s, err := re2Loc(blk, loc)
		if err != nil {
			return alerts, err
		}
		if isMatch(c.exceptRe, s) || withinPhrase(c.phraseRe, txt, loc) {
			continue
		}

		a, erra := makeAlert(c.Definition, loc, blk, cfg)
		if erra != nil {
			return alerts, erra
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (c Conditional) Fields() Definition {
	return c.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (c Conditional) Pattern() string {
	return ""
}
