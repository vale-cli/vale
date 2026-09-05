package check

import (
	"github.com/jdkato/prose/v3/strcase"
	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Capitalization checks the case of a string.
type Capitalization struct {
	Definition `mapstructure:",squash"`
	// `match` (`string`): $title, $sentence, $lower, $upper, or a pattern.
	Match string
	Check func(s string, re *rx.Regexp) (string, bool)
	// `style` (`string`): AP or Chicago; only applies when match is set to
	// $title.
	Style string
	// `exceptions` (`array`): An array of strings to be ignored.
	Exceptions []string
	// `indicators` (`array`): An array of suffixes that indicate the next
	// token should be ignored.
	Indicators []string
	// `threshold` (`float`): The minimum proportion of words that must be
	// (un)capitalized for a sentence to be considered correct.
	Threshold float64
	// `vocab` (`boolean`): If `true`, use the user's `Vocab` as a list of
	// exceptions.
	Vocab bool
	// `prefix` (`string`): A prefix to be ignored when checking for
	// capitalization.
	Prefix string

	exceptRe *rx.Regexp
}

// NewCapitalization creates a new `capitalization`-based rule.
func NewCapitalization(cfg *core.Config, generic baseCheck, path string) (Capitalization, error) {
	rule := Capitalization{Vocab: true}

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

	// NOTE: This is OK since setting `Threshold` to 0 would mean that the rule
	// would never trigger. In other words, we wouldn't want the default to be
	// 0 because that would be equivalent to disabling the rule.
	//
	// Also, we chose a default of 0.8 because it matches the behavior of the
	// original implementation (pre-threshold).
	if rule.Threshold == 0 {
		rule.Threshold = 0.8
	}

	if rule.Vocab {
		rule.Exceptions = append(rule.Exceptions, cfg.AcceptedTokens...)
	}

	if rule.Match == "$title" {
		var tc *strcase.TitleConverter
		if rule.Style == "Chicago" {
			tc = strcase.NewTitleConverter(
				strcase.ChicagoStyle,
				strcase.UsingVocab(rule.Exceptions),
				strcase.UsingPrefix(rule.Prefix),
			)
		} else {
			tc = strcase.NewTitleConverter(
				strcase.APStyle,
				strcase.UsingVocab(rule.Exceptions),
				strcase.UsingPrefix(rule.Prefix),
			)
		}
		rule.Check = func(s string, re *rx.Regexp) (string, bool) {
			return title(s, re, tc, rule.Threshold)
		}
	} else if rule.Match == "$sentence" {
		sc := strcase.NewSentenceConverter(
			strcase.UsingVocab(rule.Exceptions),
			strcase.UsingPrefix(rule.Prefix),
			strcase.UsingIndicator(wasIndicator(rule.Indicators)),
		)
		rule.Check = func(s string, re *rx.Regexp) (string, bool) {
			return sentence(s, re, sc, rule.Threshold)
		}
	} else if f, ok := varToFunc[rule.Match]; ok {
		rule.Check = f
	} else {
		re2, errc := rx.Compile(rule.Match)
		if errc != nil {
			return rule, core.NewE201FromPosition(errc.Error(), path, 1)
		}
		rule.Check = func(s string, r *rx.Regexp) (string, bool) {
			return re2.String(), re2.MatchStringStd(s) || isMatch(r, s)
		}
	}

	return rule, nil
}

// Run checks the capitalization style of the provided text.
func (c Capitalization) Run(blk nlp.Block, _ *core.File, cfg *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}

	expected, matched := c.Check(blk.Text, c.exceptRe)
	if !matched {
		action := c.Fields().Action
		if action.Name == "replace" && len(action.Params) == 0 {
			// We can only do this for non-regex case styles:
			if c.Match == "$title" || c.Match == "$sentence" {
				action.Params = []string{expected}
			}
		}
		pos := []int{0, nlp.StrLen(blk.Text)}

		a, err := makeAlert(c.Definition, pos, blk.Text, cfg)
		if err != nil {
			return alerts, err
		}

		a.Message, a.Description = formatMessages(c.Message,
			c.Description, blk.Text, expected)
		// Set after makeAlert, so `matchcase` cannot reach it. That is
		// deliberate: this rule decides the case, and re-casing its suggestion
		// to match the text it is correcting would undo the correction.
		a.Action = action
		if action.Name == "replace" {
			a.Suggestions = action.Params
		}

		anchor(&a, blk)
		alerts = append(alerts, a)
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (c Capitalization) Fields() Definition {
	return c.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (c Capitalization) Pattern() string {
	return ""
}
