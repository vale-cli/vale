package check

import (
	"strings"

	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
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

	exceptRe *rx.Regexp
	phraseRe *rx.Regexp
	pattern  *rx.Regexp
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
		return rule, patternError(err, path, rule.Exceptions...)
	}
	rule.exceptRe = re
	rule.phraseRe = buildPhraseRe(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)

	regex := ""
	if rule.Ignorecase {
		regex += ignoreCase
	}
	regex += `(` + strings.Join(rule.Tokens, "|") + `)`

	made, err := rx.Compile(regex)
	if err != nil {
		return rule, patternError(err, path, rule.Tokens...)
	}

	rule.pattern = made
	return rule, nil
}

// Run executes the `repetition`-based rule.
//
// The rule looks for repeated matches of its regex -- such as "this this".
func (o Repetition) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	vocab := vocabFor(cfg, f)
	var curr, prev string
	var hit bool
	var ploc []int
	var count int
	var alerts []core.Alert

	txt := blk.Text
	for _, loc := range o.pattern.FindAllStringIndex(txt, -1) {
		converted, err := re2Loc(blk, loc)
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

			converted, err = re2Loc(blk, pos)
			if err != nil {
				return alerts, err
			}

			if sents := nlp.Segment(f.NLP.Lang, converted); len(sents) == 1 {
				// If we have more than one sentence, we're likely looking at
				// a false positive:
				//
				// I almost forgot about that. That is important.
				//
				// All plans except a Personal plan can use Redis. Redis ...
				floc := []int{ploc[0], loc[1]}
				if !isMatch(o.exceptRe, converted) && !withinPhrase(o.phraseRe, txt, floc) &&
					!vocab.accepts(converted, txt, floc) {
					a, erra := makeAlert(o.Definition, floc, blk, cfg)
					if erra != nil {
						return alerts, erra
					}

					a.Message, a.Description = formatMessages(o.Message,
						o.Description, curr)

					anchor(&a, blk)
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
