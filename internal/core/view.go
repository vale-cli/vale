package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
	v2dasel "github.com/tomwright/dasel/v2"
	"github.com/tomwright/dasel/v3"
	"gopkg.in/yaml.v2"
)

type DaselValue = map[string]any

var blockChompingRegex = regexp.MustCompile(`(\w: )>(-?\s*)`)
var viewEngines = []string{"tree-sitter", "dasel"}

// A Scope is a single query that we want to run against a document.
type Scope struct {
	Name string `yaml:"name"`
	Expr string `yaml:"expr"`
	Type string `yaml:"type"`
}

// A View is a named, virtual representation of a subset of a file's
// structured content. It is defined by a set of queries that can be
// used to extract specific information from the file.
//
// The supported engines are:
//
// - `tree-sitter`
// - `dasel`
// - `command`
type View struct {
	Engine string  `yaml:"engine"`
	Scopes []Scope `yaml:"scopes"`
}

// A ScopedValues is a value that has been assigned a scope.
type ScopedValues struct {
	Scope  string
	Format string
	Values []string
}

// NewView creates a new blueprint from the given path.
func NewView(path string) (*View, error) {
	var view View

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(data, &view)
	if err != nil {
		return nil, err
	}

	if view.Engine == "" {
		return nil, fmt.Errorf("missing parser")
	} else if !StringInSlice(view.Engine, viewEngines) {
		return nil, fmt.Errorf("unsupported parser: %s", view.Engine)
	}

	if len(view.Scopes) == 0 {
		return nil, fmt.Errorf("missing queries")
	}

	return &view, nil
}

func (b *View) Apply(f *File) ([]ScopedValues, error) {
	value, err := fileToValue(f)
	if err != nil {
		return nil, err
	}

	found := make([]ScopedValues, 0, len(b.Scopes))
	for _, s := range b.Scopes {
		var values []string
		values, err = selectStrings(value, s.Expr)
		if err != nil {
			return nil, fmt.Errorf("processing scope %q: %w", s.Name, err)
		}
		found = append(found, ScopedValues{
			Scope:  s.Name,
			Values: values,
			Format: s.Type,
		})
	}

	return found, nil
}

func selectStrings(value DaselValue, expr string) ([]string, error) {
	selected, _, err := dasel.Select(context.Background(), value, expr)
	if err != nil {
		return selectStringsV2(value, expr)
	}

	outer, isSlice := selected.([]any)
	if !isSlice {
		return nil, fmt.Errorf("expected []any, got %T", selected)
	}

	// Unwrap single-element wrapper if present.
	if len(outer) == 1 {
		if inner, isInner := outer[0].([]any); isInner {
			outer = inner
		}
	}

	results := make([]string, 0, len(outer))
	for _, v := range outer {
		if str, isStr := v.(string); isStr {
			results = append(results, str)
		}
	}
	return results, nil
}

func selectStringsV2(value DaselValue, expr string) ([]string, error) {
	selected, err := v2dasel.Select(value, expr)
	if err != nil {
		return nil, err
	}
	results := make([]string, 0, len(selected))
	for _, v := range selected {
		results = append(results, v.String())
	}
	return results, nil
}

// normalize recursively converts map[interface{}]interface{} (produced by
// gopkg.in/yaml.v2) to map[string]any so that Dasel v3 can traverse the
// document without choking on interface{} map keys.
func normalize(v any) any {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[fmt.Sprintf("%v", k)] = normalize(v)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[k] = normalize(v)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = normalize(v)
		}
		return out
	default:
		return val
	}
}

func fileToValue(f *File) (DaselValue, error) {
	var raw any

	// We replace block chomping indicators with a pipe to ensure that
	// newlines are preserved.
	//
	// See https://yaml-multiline.info for more information.
	text := blockChompingRegex.ReplaceAllStringFunc(f.Content, func(match string) string {
		return blockChompingRegex.ReplaceAllString(match, `${1}|${2}`)
	})

	contents := []byte(text)
	switch f.RealExt {
	case ".json":
		err := json.Unmarshal(contents, &raw)
		if err != nil {
			return nil, err
		}
	case ".yml", ".yaml":
		err := yaml.Unmarshal(contents, &raw)
		if err != nil {
			return nil, err
		}
	case ".toml":
		err := toml.Unmarshal(contents, &raw)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported file type")
	}

	value, isMap := normalize(raw).(map[string]any)
	if !isMap {
		return nil, errors.New("document root is not an object")
	}

	return value, nil
}
