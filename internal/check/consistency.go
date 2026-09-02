package check

import (
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

type step struct {
	pattern *rx.Regexp
	subs    []string
}

// Consistency ensures that the keys and values of Either don't both exist.
type Consistency struct {
	Definition `mapstructure:",squash"`
	steps      []step
	// `either` (`map`): A map of `option 1: option 2` pairs, of which only one
	// may appear.
	Either map[string]string
	// `nonword` (`bool`): Removes the default word boundaries (`\b`).
	Nonword bool
	// `ignorecase` (`bool`): Makes all matches case-insensitive.
	Ignorecase bool
}

// NewConsistency creates a new `consistency`-based rule.
func NewConsistency(cfg *core.Config, generic baseCheck, path string) (Consistency, error) {
	var chkRE string

	rule := Consistency{}
	name, _ := generic["name"].(string)

	err := mapstructure.WeakDecode(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	regex := makeRegexp(
		cfg.WordTemplate,
		rule.Ignorecase,
		func() bool { return !rule.Nonword },
		func() string { return "" }, true)

	// The capture-group stem is the rule's base name -- the last segment,
	// since the rule may sit in a subdirectory.
	parts := strings.Split(name, ".")
	chkKey := parts[len(parts)-1]
	count := 0
	for v1, v2 := range rule.Either {
		count += 2

		subs := []string{
			fmt.Sprintf("%s%d", chkKey, count),
			fmt.Sprintf("%s%d", chkKey, count+1)}

		chkRE = fmt.Sprintf("(?P<%s>%s)|(?P<%s>%s)", subs[0], v1, subs[1], v2)
		chkRE = fmt.Sprintf(regex, chkRE)

		re, errc := rx.Compile(chkRE)
		if errc != nil {
			return rule, core.NewE201FromPosition(errc.Error(), path, 1)
		}

		rule.Extends = name
		rule.Name = fmt.Sprintf("%s.%s", name, v1)
		rule.steps = append(rule.steps, step{pattern: re, subs: subs})
	}

	return rule, nil
}

// Run looks for inconsistent use of a user-defined regex.
func (o Consistency) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}

	loc := []int{}
	txt := blk.Text

	for _, s := range o.steps {
		matches := s.pattern.FindAllStringSubmatchIndex(txt, -1)
		for _, submat := range matches {
			for idx, mat := range submat {
				if mat != -1 && idx > 0 && idx%2 == 0 {
					loc = []int{mat, submat[idx+1]}
					f.Sequences = append(
						f.Sequences,
						s.pattern.SubexpNames()[idx/2])
				}
			}
		}

		if matches != nil && core.AllStringsInSlice(s.subs, f.Sequences) {
			o.Name = o.Extends

			// Not anchored, deliberately. `loc` is whatever the submatch loop
			// above left behind, which is the *last* match in the block rather
			// than the one being reported; searching for the matched text
			// instead lands on the first occurrence, which is what this check
			// has always reported. Anchoring would promote that leftover into
			// the output. The rule fires at most once per block, so there is
			// nothing to gain by it either.
			a, err := makeAlert(o.Definition, loc, txt, cfg)
			if err != nil {
				return alerts, err
			}
			alerts = append(alerts, a)
		}
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (o Consistency) Fields() Definition {
	return o.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (o Consistency) Pattern() string {
	return ""
}
