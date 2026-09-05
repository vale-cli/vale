package check

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
	"github.com/jdkato/prose/v3/strcase"

	"github.com/vale-cli/vale/v3/internal/core"
	rx "github.com/vale-cli/vale/v3/internal/regex"
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

// editArity is how many parameters each `edit` operation takes, counting
// the operation's own name. A flat parameter list is read as a pipeline by
// consuming that many at a time, so the shape of `Params` never changes.
var editArity = map[string]int{
	"replace":      3,
	"remove":       2,
	"regex":        3,
	"trim_right":   2,
	"trim_left":    2,
	"trim":         2,
	"truncate":     2,
	"split":        3,
	"wrap":         2,
	"unwrap":       2,
	"prefix":       2,
	"suffix":       2,
	"squeeze":      1,
	"capitalize":   1,
	"uncapitalize": 1,
	"smart":        1,
	"dumb":         1,
	"ascii":        1,
	"words":        1,
	"digits":       1,
	"lower":        1,
	"upper":        1,
	"title":        1,
	"sentence":     1,
	"simple":       1,
	"kebab":        1,
	"snake":        1,
	"camel":        1,
	"pascal":       1,
}

var (
	titleCase    = strcase.NewTitleConverter(strcase.APStyle)
	sentenceCase = strcase.NewSentenceConverter()
)

// scripts holds each `suggest` script compiled once; a run clones it.
var scripts sync.Map

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

// FixAlert resolves the alert's action to its suggestions.
func FixAlert(alert core.Alert, cfg *core.Config) ([]string, error) {
	action := alert.Action.Name
	if f, found := fixers[action]; found {
		return f(alert, cfg)
	}
	return []string{}, fmt.Errorf("unknown action '%s'", action)
}

// resolveFix fills in the suggestions an alert's action produces, so that
// output carries the answer rather than the recipe.
func resolveFix(a *core.Alert, cfg *core.Config) error {
	if a.Action.Name == "" {
		return nil
	}

	fixed, err := FixAlert(*a, cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", a.Check, err)
	}
	a.Suggestions = fixed

	return nil
}

// checkAction reports what is wrong with a rule's action before any alert
// depends on it: an unknown name, the wrong number of parameters, or a
// script that is not on disk.
func checkAction(cfg *core.Config, rule Rule) error {
	info := rule.Fields()
	action, params := info.Action, info.Action.Params

	switch action.Name {
	case "":
		return nil
	case "replace", "remove":
		return nil
	case "suggest":
		// A spelling rule's bare `suggest` has always meant its dictionaries.
		if _, spells := rule.(Spelling); spells && len(params) == 0 {
			return nil
		}
		if len(params) != 1 {
			return errors.New("'suggest' takes one parameter")
		}
		if params[0] != "spellings" && scriptPath(cfg, params[0], info.Name) == "" {
			return fmt.Errorf("script '%s' not found", params[0])
		}
	case "convert":
		if len(params) != 1 || params[0] != "simple" {
			return errors.New("'convert' takes one parameter: simple")
		}
	case "edit":
		steps, err := editSteps(params)
		if err != nil {
			return err
		}
		for _, step := range steps {
			switch step[0] {
			case "regex":
				if _, err = rx.Compile(step[1]); err != nil {
					return err
				}
			case "split":
				if _, err = strconv.Atoi(step[2]); err != nil {
					return fmt.Errorf("'split' index '%s' is not a number", step[2])
				}
			}
		}
	default:
		return fmt.Errorf("unknown action '%s'", action.Name)
	}
	return nil
}

func suggest(alert core.Alert, cfg *core.Config) ([]string, error) {
	name := "spellings"
	if len(alert.Action.Params) > 0 {
		name = alert.Action.Params[0]
	}

	switch name {
	case "spellings":
		return spelling(alert, cfg)
	default:
		return script(name, alert, cfg)
	}
}

// scriptPath finds a `suggest` script under config/actions, then in the
// style's own directory.
func scriptPath(cfg *core.Config, name, check string) string {
	file := core.FindConfigAsset(cfg, name, core.ActionDir)
	if file == "" {
		parts := strings.SplitN(check, ".", 2)
		if len(parts) == 2 {
			file = core.FindAsset(cfg, filepath.Join(parts[0], name))
		}
	}
	return file
}

func script(name string, alert core.Alert, cfg *core.Config) ([]string, error) {
	var suggestions = []string{}

	file := scriptPath(cfg, name, alert.Check)
	if file == "" {
		return suggestions, fmt.Errorf("script '%s' not found", name)
	}

	compiled, err := compileAction(file)
	if err != nil {
		return suggestions, err
	}

	run := compiled.Clone()
	if err = run.Set("match", alert.Match); err != nil {
		return suggestions, err
	}
	if err = run.Run(); err != nil {
		return suggestions, err
	}

	for _, s := range run.Get("suggestions").Array() {
		if str, ok := s.(string); ok {
			suggestions = append(suggestions, str)
		}
	}

	return suggestions, nil
}

func compileAction(file string) (*tengo.Compiled, error) {
	if cached, ok := scripts.Load(file); ok {
		return cached.(*tengo.Compiled), nil
	}

	source, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	script := tengo.NewScript(source)
	script.SetImports(stdlib.GetModuleMap("text", "fmt", "math"))

	if err = script.Add("match", ""); err != nil {
		return nil, err
	}

	compiled, err := script.Compile()
	if err != nil {
		return nil, err
	}

	scripts.Store(file, compiled)
	return compiled, nil
}

func spelling(alert core.Alert, cfg *core.Config) ([]string, error) {
	var suggestions = []string{}

	name := strings.Split(alert.Check, ".")
	if len(name) != 2 {
		return suggestions, fmt.Errorf("unknown check '%s'", alert.Check)
	}
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
	out := make([]string, len(alert.Action.Params))
	for i, p := range alert.Action.Params {
		out[i] = expandGroups(p, alert.Match, alert.Groups)
	}
	return out, nil
}

func remove(_ core.Alert, _ *core.Config) ([]string, error) {
	return []string{""}, nil
}

func convert(alert core.Alert, _ *core.Config) ([]string, error) {
	if len(alert.Action.Params) == 0 {
		return []string{}, errors.New("no parameters")
	}

	switch mode := alert.Action.Params[0]; mode {
	case "simple":
		return []string{strcase.Simple(alert.Match)}, nil
	default:
		return []string{}, fmt.Errorf("unknown conversion '%s'", mode)
	}
}

// editSteps reads a flat parameter list as a sequence of operations, each
// taking as many parameters as its arity says.
func editSteps(params []string) ([][]string, error) {
	var steps [][]string

	for i := 0; i < len(params); {
		name := params[i]
		want, known := editArity[name]
		if !known {
			return nil, fmt.Errorf("unknown edit '%s'", name)
		}
		if i+want > len(params) {
			return nil, fmt.Errorf("'%s' takes %d parameter(s)", name, want-1)
		}
		steps = append(steps, params[i:i+want])
		i += want
	}

	if len(steps) == 0 {
		return nil, errors.New("'edit' needs an operation")
	}
	return steps, nil
}

// flattenAction turns a nested `params` list, one list per operation, into
// the flat list the action carries. A flat list passes through untouched.
func flattenAction(generic baseCheck) {
	action, ok := generic["action"].(map[string]interface{})
	if !ok {
		return
	}
	params, ok := action["params"].([]interface{})
	if !ok {
		return
	}

	flat := make([]interface{}, 0, len(params))
	nested := false
	for _, p := range params {
		if step, isList := p.([]interface{}); isList {
			flat = append(flat, step...)
			nested = true
		} else {
			flat = append(flat, p)
		}
	}
	if nested {
		action["params"] = flat
	}
}

func edit(alert core.Alert, _ *core.Config) ([]string, error) {
	steps, err := editSteps(alert.Action.Params)
	if err != nil {
		return []string{}, err
	}

	match := alert.Match
	for _, step := range steps {
		// A regex step's `$1` means its own pattern's group, not the token's.
		if step[0] != "regex" {
			for i := 1; i < len(step); i++ {
				step[i] = expandGroups(step[i], alert.Match, alert.Groups)
			}
		}
		if match, err = editStep(match, step); err != nil {
			return []string{}, err
		}
	}

	return []string{strings.TrimSpace(match)}, nil
}

func editStep(match string, step []string) (string, error) {
	switch step[0] {
	case "replace":
		return strings.ReplaceAll(match, step[1], step[2]), nil
	case "remove":
		return strings.Map(func(r rune) rune {
			if strings.ContainsRune(step[1], r) {
				return -1
			}
			return r
		}, match), nil
	case "regex":
		// The same engine the rule's own patterns run on, so a lookaround
		// that found the match can also rewrite it.
		regex, err := rx.Compile(step[1])
		if err != nil {
			return "", err
		}
		return regex.Replace(match, step[2], -1, -1)
	case "trim_right":
		return strings.TrimRight(match, step[1]), nil
	case "trim_left":
		return strings.TrimLeft(match, step[1]), nil
	case "trim":
		return strings.Trim(match, step[1]), nil
	case "truncate":
		return strings.Split(match, step[1])[0], nil
	case "split":
		index, err := strconv.Atoi(step[2])
		if err != nil {
			return "", err
		}
		parts := strings.Split(match, step[1])
		if index >= len(parts) {
			return "", errors.New("index out of range")
		}
		return parts[index], nil
	case "wrap":
		return step[1] + match + step[1], nil
	case "unwrap":
		return strings.TrimSuffix(strings.TrimPrefix(match, step[1]), step[1]), nil
	case "prefix":
		return step[1] + match, nil
	case "suffix":
		return match + step[1], nil
	case "squeeze":
		return squeeze(match), nil
	case "capitalize":
		return capitalize(match), nil
	case "uncapitalize":
		return uncapitalize(match), nil
	case "smart":
		return smartPunctuation(match), nil
	case "dumb":
		return dumbPunctuation(match), nil
	case "ascii":
		return toASCII(match), nil
	case "words":
		return numberToWords(match), nil
	case "digits":
		return wordsToNumber(match), nil
	case "lower":
		return strings.ToLower(match), nil
	case "upper":
		return strings.ToUpper(match), nil
	case "title":
		return titleCase.Convert(match), nil
	case "sentence":
		return sentenceCase.Convert(match), nil
	case "simple":
		return strcase.Simple(match), nil
	case "kebab":
		return strcase.Dash(match), nil
	case "snake":
		return strcase.Snake(match), nil
	case "camel":
		return strcase.Camel(match), nil
	case "pascal":
		return strcase.Pascal(match), nil
	}
	return "", fmt.Errorf("unknown edit '%s'", step[0])
}
