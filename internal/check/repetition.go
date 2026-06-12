package check

import (
	"strings"

	"github.com/errata-ai/regexp2"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// Repetition looks for repeated uses of Tokens.
type Repetition struct {
	Definition `mapstructure:",squash"`
	Tokens     []string
	Max        int
	Ignorecase bool
	Alpha      bool
	Vocab      bool
	Exceptions []string

	exceptRe *regexp2.Regexp
	phraseRe *regexp2.Regexp
	pattern  *regexp2.Regexp
}

// NewRepetition creates a new `repetition`-based rule.
func NewRepetition(cfg *core.Config, generic baseCheck, path string) (Repetition, error) {
	rule := Repetition{Vocab: true}

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

	regex := ""
	if rule.Ignorecase {
		regex += ignoreCase
	}
	regex += `(` + strings.Join(rule.Tokens, "|") + `)`

	made, err := regexp2.CompileStd(regex)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}

	rule.pattern = made
	return rule, nil
}

// Run executes the `repetition`-based rule.
//
// The rule looks for repeated matches of its regex -- such as "this this".
func (o Repetition) Run(blk nlp.Block, _ *core.File, cfg *core.Config) ([]core.Alert, error) {
	var curr, prev string
	var hit bool
	var ploc []int
	var count int
	var alerts []core.Alert

	txt := blk.Text
	for _, loc := range o.pattern.FindAllStringIndex(txt, -1) {
		converted, err := re2Loc(txt, loc)
		if err != nil {
			return alerts, err
		}
		curr = strings.TrimSpace(converted)

		if o.Ignorecase {
			hit = strings.EqualFold(curr, prev) && curr != ""
		} else {
			hit = curr == prev && curr != ""
		}

		hit = hit && (!o.Alpha || core.IsLetter(curr))
		if hit {
			count++
		}

		if hit && count > o.Max {
			pos := []int{ploc[0], loc[1]}

			converted, err = re2Loc(txt, pos)
			if err != nil {
				return alerts, err
			}

			if sents := nlp.SentenceTokenizer.Segment(converted); len(sents) == 1 {
				// If we have more than one sentence, we're likely looking at
				// a false positive:
				//
				// I almost forgot about that. That is important.
				//
				// All plans except a Personal plan can use Redis. Redis ...
				floc := []int{ploc[0], loc[1]}
				if !isMatch(o.exceptRe, converted) && !withinPhrase(o.phraseRe, txt, floc) {
					a, erra := makeAlert(o.Definition, floc, txt, cfg)
					if erra != nil {
						return alerts, erra
					}

					a.Message, a.Description = formatMessages(o.Message,
						o.Description, curr)
					alerts = append(alerts, a)
					count = 0
				}
			}
		}
		ploc = loc
		prev = curr
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (o Repetition) Fields() Definition {
	return o.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (o Repetition) Pattern() string {
	return o.pattern.String()
}
