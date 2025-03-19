package check

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/exp/maps"

	"github.com/errata-ai/regexp2"
	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// Substitution switches the values of Swap for its keys.
type Substitution struct {
	Definition `mapstructure:",squash"`
	Exceptions []string
	repl       []string
	Swap       map[string]string
	exceptRe   *regexp2.Regexp
	pattern    *regexp2.Regexp
	Ignorecase bool
	Nonword    bool
	Vocab      bool
	Capitalize bool

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
	tokens := ""

	re, err := updateExceptions(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}
	rule.exceptRe = re

	regex := makeRegexp(
		cfg.WordTemplate,
		rule.Ignorecase,
		func() bool { return !rule.Nonword },
		func() string { return "" }, true)

	terms := maps.Keys(rule.Swap)
	sort.Slice(terms, func(p, q int) bool {
		return len(terms[p]) > len(terms[q])
	})

	replacements := []string{}
	for _, regexstr := range terms {
		rule.msgMap = append(rule.msgMap, regexstr)
		replacement := rule.Swap[regexstr]

		opens := strings.Count(regexstr, "(")
		if opens != strings.Count(regexstr, "(?")+strings.Count(regexstr, `\(`) {
			// We have a capture group, so we need to make it non-capturing.
			regexstr, err = convertCaptureGroups(regexstr)
			if err != nil {
				return rule, core.NewE201FromTarget(err.Error(), regexstr, path)
			}
		}
		tokens += `(` + regexstr + `)|`
		replacements = append(replacements, replacement)
	}
	regex = fmt.Sprintf(regex, strings.TrimRight(tokens, "|"))

	re, err = regexp2.CompileStd(regex)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}

	rule.pattern = re
	rule.repl = replacements
	return rule, nil
}

// Run executes the `substitution`-based rule.
//
// The rule looks for one pattern and then suggests a replacement.
func (s Substitution) Run(blk nlp.Block, _ *core.File, cfg *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert

	txt := blk.Text
	// Leave early if we can to avoid calling `FindAllStringSubmatchIndex`
	// unnecessarily.
	if !s.pattern.MatchStringStd(txt) {
		return alerts, nil
	}

	for _, submat := range s.pattern.FindAllStringSubmatchIndex(txt, -1) {
		for idx, mat := range submat {
			if mat != -1 && idx > 0 && idx%2 == 0 {
				loc := []int{mat, submat[idx+1]}

				converted, err := re2Loc(txt, loc)
				if err != nil {
					return alerts, err
				}

				observed := converted
				expected, msgErr := subMsg(s, (idx/2)-1, observed)
				if msgErr != nil {
					return alerts, msgErr
				}

				same := matchToken(expected, observed, false)
				if !same && !isMatch(s.exceptRe, observed) {
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

						expected = core.ToSentence(action.Params, "or")
						// NOTE: For backwards-compatibility, we need to ensure
						// that we don't double quote.
						s.Message = convertMessage(s.Message)
					}

					a, aerr := makeAlert(s.Definition, loc, txt, cfg)
					if aerr != nil {
						return alerts, aerr
					}

					a.Message, a.Description = formatMessages(s.Message,
						s.Description, expected, observed)
					a.Action = action

					alerts = append(alerts, a)
				}
			}
		}
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (s Substitution) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s Substitution) Pattern() string {
	return s.pattern.String()
}

func convertMessage(s string) string {
	for _, spec := range []string{"'%s'", "\"%s\""} {
		if strings.Count(s, spec) == 2 {
			s = strings.Replace(s, spec, "%s", 1)
		}
	}
	return s
}

func convertCaptureGroups(msg string) (string, error) {
	captureOpen := regexp2.MustCompileStd(`(?<!\\)\((?!\?)`)
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
	hasIndex := regexp2.MustCompileStd(`\$\d+`)
	if !hasIndex.MatchStringStd(expected) {
		return expected, nil
	}

	msg := s.msgMap[index]
	if s.Ignorecase {
		msg = `(?i)` + msg
	}

	msgRe := regexp2.MustCompileStd(msg)
	return msgRe.Replace(observed, expected, -1, -1)
}

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

	// We want to ignore any escaped pipes, so make a temporary substitution:
	//
	// TODO: Add support for `.Split` in `regexp2`.
	temp := strings.ReplaceAll(match, `\|`, "PIPE")

	for _, option := range strings.Split(temp, "|") {
		if option != "" {
			options = append(options, strings.ReplaceAll(option, "PIPE", `|`))
		}
	}

	return options
}
