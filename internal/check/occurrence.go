package check

import (
	"strings"

	"github.com/errata-ai/regexp2"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// Occurrence counts the number of times Token appears.
type Occurrence struct {
	Definition `mapstructure:",squash"`
	Token      string
	Max        int
	Min        int
	pattern    *regexp2.Regexp
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
	re, err := regexp2.CompileStd(regex)
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
			// NOTE: We might not have a location to report -- i.e., by
			// definition, having zero instances of a token may break a rule.
			//
			// In a case like this, the check essentially becomes
			// document-scoped (like `readability`), so we mark the issue at
			// the first line.
			a = core.Alert{
				Check: o.Name, Severity: o.Level, Span: []int{1, 1},
				Link: o.Link}
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
