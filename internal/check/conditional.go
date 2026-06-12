package check

import (
	"strings"

	"github.com/errata-ai/regexp2"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// Conditional ensures that the present of First ensures the present of Second.
type Conditional struct {
	Definition `mapstructure:",squash"`
	Exceptions []string
	patterns   []*regexp2.Regexp
	First      string
	Second     string
	exceptRe   *regexp2.Regexp
	phraseRe   *regexp2.Regexp
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
	opens := strings.Count(pattern, "(")
	noncap := strings.Count(pattern, "(?") + strings.Count(pattern, `\(`)
	return opens > noncap
}

// NewConditional creates a new `conditional`-based rule.
func NewConditional(cfg *core.Config, generic baseCheck, path string) (Conditional, error) {
	var expression []*regexp2.Regexp
	rule := Conditional{Vocab: true}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	re, err := updateExceptions(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}
	rule.exceptRe = re
	rule.phraseRe = buildPhraseRe(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)

	re, err = regexp2.CompileStd(rule.Second)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}
	expression = append(expression, re)
	rule.secondHasGroup = hasCaptureGroup(rule.Second)

	re, err = regexp2.CompileStd(rule.First)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}
	expression = append(expression, re)

	// TODO: How do we support multiple patterns?
	rule.patterns = expression
	return rule, nil
}

// Run evaluates the given conditional statement.
func (c Conditional) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}

	txt := blk.Text

	// When `Second` has no capture group, the rule is a plain presence check:
	// if `First` appears, `Second` must appear somewhere in the same block. If
	// it does, there's nothing to flag; otherwise every `First` match is a
	// violation. See #1048.
	if !c.secondHasGroup {
		if c.patterns[0].MatchStringStd(txt) {
			return alerts, nil
		}
		return c.flagAntecedents(txt, cfg)
	}

	// We first look for the consequent of the conditional statement.
	// For example, if we're ensuring that abbreviations have been defined
	// parenthetically, we'd have something like:
	//
	//     "WHO" [antecedent], "World Health Organization (WHO)" [consequent]
	//
	// In other words: if "WHO" exists, it must also have a definition -- which
	// we're currently looking for.
	matches := c.patterns[0].FindAllStringSubmatch(txt, -1)
	for _, mat := range matches {
		if len(mat) > 1 {
			// If we find one, we store it in a slice associated with this
			// particular file.
			for _, m := range mat[1:] {
				if len(m) > 0 {
					f.Sequences = append(f.Sequences, m)
				}
			}
		}
	}

	// Now we look for the antecedent.
	locs := c.patterns[1].FindAllStringIndex(txt, -1)
	for _, loc := range locs {
		s, err := re2Loc(txt, loc)
		if err != nil {
			return alerts, err
		}

		if !core.StringInSlice(s, f.Sequences) && !isMatch(c.exceptRe, s) && !withinPhrase(c.phraseRe, txt, loc) {
			// If we've found one (e.g., "WHO") and we haven't marked it as
			// being defined previously, send an Alert.
			a, erra := makeAlert(c.Definition, loc, txt, cfg)
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
func (c Conditional) flagAntecedents(txt string, cfg *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}
	for _, loc := range c.patterns[1].FindAllStringIndex(txt, -1) {
		s, err := re2Loc(txt, loc)
		if err != nil {
			return alerts, err
		}
		if isMatch(c.exceptRe, s) || withinPhrase(c.phraseRe, txt, loc) {
			continue
		}

		a, erra := makeAlert(c.Definition, loc, txt, cfg)
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
