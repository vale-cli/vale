package check

import (
	"fmt"
	"os"
	"strings"

	"github.com/expr-lang/expr"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

func filter(mgr *Manager) (map[string]Rule, error) {
	var filter string

	stringOrPath := mgr.Config.Flags.Filter
	if stringOrPath == "" {
		return mgr.rules, nil
	}

	if system.FileExists(stringOrPath) {
		// Case 1: The user has provided a valid path to a filter file.
		b, err := os.ReadFile(stringOrPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read filter '%s': %w", stringOrPath, err)
		}
		filter = string(b)
	} else if found := core.FindAsset(mgr.Config, stringOrPath); found != "" {
		// Case 2: The user has referenced a filter stored on the `StylesPath`.
		b, err := os.ReadFile(found)
		if err != nil {
			return nil, fmt.Errorf("failed to read filter '%s': %w", found, err)
		}
		filter = string(b)
	} else {
		// Case 3: Assume the  user has provided a string.
		filter = stringOrPath
	}

	// .Name, .Level -> override
	// .Scope, .Message, .Description, .Extends, .Link
	//
	// The idea here should be simple: we read the ini file and apply overrides
	// (where needed) from the user-given filter. The order is always:
	//
	// ini -> filter
	//
	// The key is that the *filter* always has the last say -- in terms of what
	// rules run and at what level.
	//
	// NOTE: This means that filtered results can only ever be a *subset* of
	// the would-be results since we're filtering on checks loaded based on the
	// ini config.

	env := FilterEnv{}
	for _, rule := range mgr.rules {
		env.Rules = append(env.Rules, rule.Fields())
	}
	code := fmt.Sprintf(`filter(Rules, {%s})`, filter)

	program, err := expr.Compile(code, expr.Env(env))
	if err != nil {
		return mgr.rules, err
	}

	output, err := expr.Run(program, env)
	if err != nil {
		return mgr.rules, err
	}

	filtered := map[string]Rule{}
	for _, entry := range output.([]interface{}) {
		rule, _ := entry.(Definition)

		name := mgr.RuleForAlert(rule.Name)

		// NOTE: We can't simply assume that what the filter returns should be
		// run -- it depends on the *intent* of the filter.
		//
		// If the filter *only* sets `.Level`, then, for example, the output
		// could contain rules that match the new level but are disabled in the
		// `.vale.ini`.
		//
		// TODO: If checking for the existence of, e.g., `.Level` enough?
		// Should we use `program.Constants`?

		if strings.Contains(code, ".Level") {
			lvl := core.LevelToInt[rule.Level]
			if lvl < mgr.Config.MinAlertLevel {
				mgr.Config.MinAlertLevel = lvl
			}
		}

		/*
			if strings.Contains(code, ".Name") {
				mgr.Config.GChecks[name] = true
				for _, v := range mgr.Config.SChecks {
					v[name] = true
				}
			}*/

		filtered[name] = mgr.rules[name]
	}

	return filtered, nil
}
