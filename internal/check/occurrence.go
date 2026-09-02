package check

import (
	"strings"

	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// reFirstWord finds the word a zero-occurrence shortfall is anchored to.
var reFirstWord = rx.MustCompile(`\S+`)

// Occurrence counts the number of times Token appears.
type Occurrence struct {
	Definition `mapstructure:",squash"`
	Token      string
	Max        int
	Min        int
	pattern    *rx.Regexp
	Ignorecase bool
}

// NewOccurrence creates a new `occurrence`-based rule.
func NewOccurrence(_ *core.Config, generic baseCheck, path string) (Occurrence, error) {
	rule := Occurrence{}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	regex := ""
	if rule.Ignorecase {
		regex += ignoreCase
	}

	regex += `(?:` + rule.Token + `)`
	re, err := rx.Compile(regex)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}

	rule.pattern = re
	return rule, nil
}

// Run checks the number of occurrences of a user-defined regex against a
// certain threshold.
func (o Occurrence) Run(blk nlp.Block, _ *core.File, cfg *core.Config) ([]core.Alert, error) {
	var a core.Alert
	var err error
	var alerts []core.Alert

	txt := blk.Text
	locs := o.pattern.FindAllStringIndex(txt, -1)

	occurrences := len(locs)
	if (o.Max > 0 && occurrences > o.Max) || (o.Min > 0 && occurrences < o.Min) {
		if occurrences == 0 {
			// Zero matches leave no occurrence to point at, but the scope
			// that fell short has a position of its own. Anchored to its
			// first word, the alert lands on the deficient paragraph;
			// unlocated, it collapsed onto line one, where one report hid
			// every other scope that fell short too.
			a = core.Alert{
				Check: o.Name, Severity: o.Level, Span: []int{1, 1},
				Link: o.Link}

			if word := reFirstWord.FindAllStringIndex(txt, 1); len(word) == 1 {
				a, err = makeAlert(o.Definition, word[0], txt, cfg)
				if err != nil {
					return alerts, err
				}
				anchor(&a, blk)
			}
		} else {
			span := []int{}

			// We look for the first non-code match.
			//
			// Previously, we would just use the first match, but this could
			// lead to false positives if the first match was in a code-like
			// token.
			//
			// We also can't use the entire scope (`txt`) without risking
			// having to fall back to string matching.
			for _, loc := range locs {
				m, rErr := re2Loc(txt, loc)
				if rErr != nil || strings.TrimSpace(m) == "" {
					continue
				} else if !core.IsCode(m) {
					span = loc
					break
				}
			}

			// If we can't find a non-code match, we return early.
			//
			// The alternative here is to use `scope: raw`.
			if len(span) == 0 {
				return alerts, nil
			}

			a, err = makeAlert(o.Definition, span, txt, cfg)
			if err != nil {
				return alerts, err
			}
			// Only this branch: the zero-occurrence case above reports a line
			// number, not a span into the text.
			anchor(&a, blk)
		}

		// Pass the count as an int (not a string) so messages can use either
		// `%d` or `%s` for it; a string `%d` would otherwise render as
		// `%!d(string=0)`. See #1048.
		a.Message = core.CondSprintf(o.Message, occurrences)
		a.Description = core.CondSprintf(o.Description, occurrences)
		alerts = append(alerts, a)
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (o Occurrence) Fields() Definition {
	return o.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (o Occurrence) Pattern() string {
	return o.pattern.String()
}
