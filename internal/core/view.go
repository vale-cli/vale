package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
	"github.com/tomwright/dasel/v2"
	"gopkg.in/yaml.v2"
)

type DaselValue = map[string]any

var blockChompingRegex = regexp.MustCompile(`(\w: )>(-?\s*)`)
var viewEngines = []string{"tree-sitter", "dasel"}

// A Scope is a single query that we want to run against a document.
type Scope struct {
	Name   string   `yaml:"name"`
	Expr   string   `yaml:"expr"`
	Type   string   `yaml:"type"`
	Ignore []string `yaml:"ignore"` // rules to skip for this scope
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
	Ignore []string // rules to skip for this scope
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
	found := []ScopedValues{}

	value, err := fileToValue(f)
	if err != nil {
		return nil, err
	}

	for _, s := range b.Scopes {
		selected, verr := dasel.Select(value, s.Expr)
		if verr != nil {
			return found, verr
		}

		values := []string{}
		for _, v := range selected {
			values = append(values, v.String())
		}

		found = append(found, ScopedValues{
			Scope:  s.Name,
			Values: values,
			Format: s.Type,
			Ignore: s.Ignore,
		})
	}

	return found, nil
}

func fileToValue(f *File) (DaselValue, error) {
	var value DaselValue

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
		err := json.Unmarshal(contents, &value)
		if err != nil {
			return nil, err
		}
	case ".yml", ".yaml":
		err := yaml.Unmarshal(contents, &value)
		if err != nil {
			return nil, err
		}
	case ".toml":
		err := toml.Unmarshal(contents, &value)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported file type")
	}

	return value, nil
}
