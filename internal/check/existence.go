package check

import (
	"fmt"
	"strings"

	"github.com/errata-ai/regexp2"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// Existence checks for the present of Tokens.
type Existence struct {
	Definition `mapstructure:",squash"`
	Raw        []string
	Tokens     []string
	// `exceptions` (`array`): An array of strings to be ignored.
	Exceptions []string
	exceptRe   *regexp2.Regexp
	pattern    *regexp2.Regexp
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

	// `Vocab` is a list of accepted *words*, so it only makes sense to treat
	// it as a set of exceptions for word-based rules. For `nonword` rules --
	// whose tokens match arbitrary spans (e.g. `"[^"]+"[.,]`) -- a vocab word
	// would suppress any match that merely *contains* it (e.g. `"plugh",`).
	//
	// See https://github.com/errata-ai/vale/issues/1058.
	re, err := updateExceptions(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab && !rule.Nonword)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}
	rule.exceptRe = re

	regex := makeRegexp(
		cfg.WordTemplate,
		rule.IgnoreCase,
		func() bool { return !rule.Nonword && len(rule.Tokens) > 0 },
		func() string { return strings.Join(rule.Raw, "") },
		rule.Append)

	parsed := []string{}
	for _, token := range rule.Tokens {
		if strings.TrimSpace(token) != "" {
			parsed = append(parsed, token)
		}
	}
	regex = fmt.Sprintf(regex, strings.Join(parsed, "|"))

	re, err = regexp2.CompileStd(regex)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}
	rule.pattern = re

	return rule, nil
}

// Run executes the `existence`-based rule.
//
// This is simplest of the available extension points: it looks for any matches
// of its internal `pattern` (calculated from `NewExistence`) against the
// provided text.
func (e Existence) Run(blk nlp.Block, _ *core.File, cfg *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}

	for _, loc := range e.pattern.FindAllStringIndex(blk.Text, -1) {
		converted, err := re2Loc(blk.Text, loc)
		if err != nil {
			return alerts, err
		}

		observed := strings.TrimSpace(converted)
		if !isMatch(e.exceptRe, observed) {
			a, erra := makeAlert(e.Definition, loc, blk.Text, cfg)
			if erra != nil {
				return alerts, erra
			}
			alerts = append(alerts, a)
		}
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (e Existence) Fields() Definition {
	return e.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (e Existence) Pattern() string {
	return e.pattern.String()
}
