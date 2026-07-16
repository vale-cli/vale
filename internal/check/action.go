package check

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
	"github.com/jdkato/twine/strcase"

	"github.com/errata-ai/vale/v3/internal/core"
)

// Solution is a potential solution to an alert.
type Solution struct {
	Suggestions []string `json:"suggestions"`
	Error       string   `json:"error"`
}

type fixer func(core.Alert, *core.Config) ([]string, error)

var fixers = map[string]fixer{
	"suggest": suggest,
	"replace": replace,
	"remove":  remove,
	"convert": convert,
	"edit":    edit,
}

// ParseAlert returns a slice of suggestions for the given Vale alert.
func ParseAlert(s string, cfg *core.Config) (Solution, error) {
	body := core.Alert{}
	resp := Solution{}

	err := json.Unmarshal([]byte(s), &body)
	if err != nil {
		return Solution{}, err
	}

	suggestions, err := FixAlert(body, cfg)
	if err != nil {
		resp.Error = err.Error()
	}
	resp.Suggestions = suggestions

	return resp, nil
}

func FixAlert(alert core.Alert, cfg *core.Config) ([]string, error) {
	action := alert.Action.Name
	if f, found := fixers[action]; found {
		return f(alert, cfg)
	}
	return []string{}, fmt.Errorf("unknown action '%s'", action)
}

func suggest(alert core.Alert, cfg *core.Config) ([]string, error) {
	if len(alert.Action.Params) == 0 {
		return []string{}, errors.New("no parameters")
	}
	name := alert.Action.Params[0]

	switch name {
	case "spellings":
		return spelling(alert, cfg)
	default:
		return script(name, alert, cfg)
	}
}

func script(name string, alert core.Alert, cfg *core.Config) ([]string, error) {
	var suggestions = []string{}

	file := core.FindConfigAsset(cfg, name, core.ActionDir)
	if file == "" {
		// Fall back to the style's own directory.
		parts := strings.SplitN(alert.Check, ".", 2)
		if len(parts) == 2 {
			file = core.FindAsset(cfg, filepath.Join(parts[0], name))
		}
	}
	if file == "" {
		return suggestions, fmt.Errorf("script '%s' not found", name)
	}

	source, err := os.ReadFile(file)
	if err != nil {
		return suggestions, err
	}

	script := tengo.NewScript(source)
	script.SetImports(stdlib.GetModuleMap("text", "fmt", "math"))

	err = script.Add("match", alert.Match)
	if err != nil {
		return suggestions, err
	}

	compiled, err := script.Compile()
	if err != nil {
		return suggestions, err
	}

	if err = compiled.Run(); err != nil {
		return suggestions, err
	}

	computed := compiled.Get("suggestions").Array()
	for _, s := range computed {
		suggestions = append(suggestions, s.(string))
	}

	return suggestions, nil
}

func spelling(alert core.Alert, cfg *core.Config) ([]string, error) {
	var suggestions = []string{}

	name := strings.Split(alert.Check, ".")
	path := filepath.Join(cfg.StylesPath(), name[0], name[1]+".yml")

	mgr, err := NewManager(cfg)
	if err != nil {
		return suggestions, err
	}

	if _, ok := mgr.Rules()[alert.Check]; !ok {
		err = mgr.AddRuleFromFile(alert.Check, path)
		if err != nil {
			return suggestions, err
		}
	}

	rule, ok := mgr.Rules()[alert.Check].(Spelling)
	if !ok {
		return suggestions, fmt.Errorf("unknown check '%s'", alert.Check)
	}

	return rule.Suggest(alert.Match), nil
}

func replace(alert core.Alert, _ *core.Config) ([]string, error) {
	return alert.Action.Params, nil
}

func remove(_ core.Alert, _ *core.Config) ([]string, error) {
	return []string{""}, nil
}

func convert(alert core.Alert, _ *core.Config) ([]string, error) {
	match := alert.Match
	if alert.Action.Params[0] == "simple" {
		match = strcase.Simple(match)
	}
	return []string{match}, nil
}

func edit(alert core.Alert, _ *core.Config) ([]string, error) {
	match := alert.Match

	if len(alert.Action.Params) == 0 {
		return []string{}, errors.New("no parameters")
	}

	switch name := alert.Action.Params[0]; name {
	case "regex":
		if len(alert.Action.Params) != 3 {
			return []string{}, errors.New("invalid number of parameters")
		}

		regex, err := regexp.Compile(alert.Action.Params[1])
		if err != nil {
			return []string{}, err
		}

		match = regex.ReplaceAllString(match, alert.Action.Params[2])
	case "trim_right":
		if len(alert.Action.Params) != 2 {
			return []string{}, errors.New("invalid number of parameters")
		}
		match = strings.TrimRight(match, alert.Action.Params[1])
	case "trim_left":
		if len(alert.Action.Params) != 2 {
			return []string{}, errors.New("invalid number of parameters")
		}
		match = strings.TrimLeft(match, alert.Action.Params[1])
	case "trim":
		if len(alert.Action.Params) != 2 {
			return []string{}, errors.New("invalid number of parameters")
		}
		match = strings.Trim(match, alert.Action.Params[1])
	case "truncate":
		if len(alert.Action.Params) != 2 {
			return []string{}, errors.New("invalid number of parameters")
		}
		match = strings.Split(match, alert.Action.Params[1])[0]
	case "split":
		if len(alert.Action.Params) != 3 {
			return []string{}, errors.New("invalid number of parameters")
		}

		index, err := strconv.Atoi(alert.Action.Params[2])
		if err != nil {
			return []string{}, err
		}

		parts := strings.Split(match, alert.Action.Params[1])
		if index >= len(parts) {
			return []string{}, errors.New("index out of range")
		}

		match = parts[index]
	}

	return []string{strings.TrimSpace(match)}, nil
}
