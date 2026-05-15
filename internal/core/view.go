package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
	v2dasel "github.com/tomwright/dasel/v2"
	"github.com/tomwright/dasel/v3"
	"gopkg.in/yaml.v3"
)

type DaselValue = map[string]any

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

// A ScopedValue is a single value extracted from a scope, along with the
// source position it was parsed from. Line/Column are 1-based; both are 0
// when the source format does not provide position information (e.g., TOML)
// or when the value could not be located in the parse tree.
type ScopedValue struct {
	Text   string
	Line   int
	Column int
}

// A ScopedValues is a value that has been assigned a scope.
type ScopedValues struct {
	Scope  string
	Format string
	Values []ScopedValue
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
	value, scalars, err := fileToValue(f)
	if err != nil {
		return nil, err
	}

	resolver := newScalarResolver(scalars)
	found := make([]ScopedValues, 0, len(b.Scopes))
	for _, s := range b.Scopes {
		strs, serr := selectStrings(value, s.Expr)
		if serr != nil {
			return nil, fmt.Errorf("processing scope %q: %w", s.Name, serr)
		}
		values := make([]ScopedValue, 0, len(strs))
		for _, str := range strs {
			line, col := resolver.locate(str)
			values = append(values, ScopedValue{Text: str, Line: line, Column: col})
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

// scalarPos records a single scalar value's source position.
type scalarPos struct {
	Value  string
	Line   int
	Column int
}

// scalarResolver maps extracted string values back to source positions by
// consuming entries from a flat, document-ordered list of scalars.
type scalarResolver struct {
	scalars []scalarPos
	used    map[int]bool
}

func newScalarResolver(scalars []scalarPos) *scalarResolver {
	return &scalarResolver{scalars: scalars, used: map[int]bool{}}
}

// locate returns the (line, column) of the first unconsumed scalar matching
// value. Returns (0, 0) when nothing matches — callers must fall back to a
// textual search in that case.
func (r *scalarResolver) locate(value string) (int, int) {
	if r == nil {
		return 0, 0
	}
	for i, s := range r.scalars {
		if r.used[i] {
			continue
		}
		if s.Value == value {
			r.used[i] = true
			return s.Line, s.Column
		}
	}
	return 0, 0
}

// walkYAMLScalars flattens a yaml.v3 node tree into a document-ordered list
// of scalar values with their source positions. Mapping keys are skipped —
// only the values are included, which is what dasel returns from queries.
//
// For block scalars (|, >), yaml.v3 reports the position of the indicator
// rather than the first content line; we resolve it to the first content
// line/column by inspecting srcLines so callers can offset alerts by the
// node's source position directly.
func walkYAMLScalars(n *yaml.Node, srcLines []string) []scalarPos {
	var out []scalarPos
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node == nil {
			return
		}
		switch node.Kind {
		case yaml.DocumentNode:
			for _, c := range node.Content {
				walk(c)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				walk(node.Content[i+1])
			}
		case yaml.SequenceNode:
			for _, c := range node.Content {
				walk(c)
			}
		case yaml.ScalarNode:
			line, col := node.Line, node.Column
			switch {
			case node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
				line, col = blockScalarContentStart(srcLines, node.Line)
			case node.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0:
				// Step past the opening quote so callers point at
				// the first content character.
				col++
			}
			out = append(out, scalarPos{Value: node.Value, Line: line, Column: col})
		case yaml.AliasNode:
			walk(node.Alias)
		}
	}
	walk(n)
	return out
}

// foldedToLiteral parses src once with yaml.v3, finds every folded (`>`)
// scalar, and rewrites the indicator byte to `|` so a subsequent parse
// preserves newlines in the value. Returns src unmodified when there are
// no folded scalars or when parsing fails.
func foldedToLiteral(src []byte) ([]byte, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(src, &node); err != nil {
		return src, err
	}

	type indicator struct{ line, col int }
	var hits []indicator
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.ScalarNode && n.Style&yaml.FoldedStyle != 0 {
			hits = append(hits, indicator{line: n.Line, col: n.Column})
		}
		for _, c := range n.Content {
			walk(c)
		}
		if n.Kind == yaml.AliasNode && n.Alias != nil {
			walk(n.Alias)
		}
	}
	walk(&node)
	if len(hits) == 0 {
		return src, nil
	}

	// Build a line-offset table so we can convert (line, col) → byte index.
	offsets := []int{0}
	for i, b := range src {
		if b == '\n' {
			offsets = append(offsets, i+1)
		}
	}

	out := append([]byte(nil), src...)
	for _, h := range hits {
		if h.line-1 >= len(offsets) {
			continue
		}
		idx := offsets[h.line-1] + (h.col - 1)
		if idx >= 0 && idx < len(out) && out[idx] == '>' {
			out[idx] = '|'
		}
	}
	return out, nil
}

// blockScalarContentStart returns the 1-based line and column of the first
// non-blank content line for a block scalar whose indicator is on
// markerLine. Falls back to (markerLine+1, 1) when no content line can be
// located (e.g., empty block).
func blockScalarContentStart(srcLines []string, markerLine int) (int, int) {
	for idx := markerLine; idx < len(srcLines); idx++ {
		line := srcLines[idx]
		trim := 0
		for trim < len(line) && (line[trim] == ' ' || line[trim] == '\t') {
			trim++
		}
		if trim == len(line) {
			continue
		}
		return idx + 1, trim + 1
	}
	return markerLine + 1, 1
}

func fileToValue(f *File) (DaselValue, []scalarPos, error) {
	var raw any
	var scalars []scalarPos

	contents := []byte(f.Content)
	switch f.RealExt {
	case ".json":
		if err := json.Unmarshal(contents, &raw); err != nil {
			return nil, nil, err
		}
		// JSON is a strict YAML subset; reparse for positions.
		var node yaml.Node
		if err := yaml.Unmarshal(contents, &node); err == nil {
			scalars = walkYAMLScalars(&node, strings.Split(f.Content, "\n"))
		}
	case ".yml", ".yaml":
		// Rewrite folded `>` indicators to literal `|` so the parsed
		// value preserves newlines. Position-mapping into source then
		// works on a line-for-line basis instead of having to guess
		// where folds happened. We use the parser itself to locate
		// the indicators so the rewrite is precise.
		rewritten, ferr := foldedToLiteral(contents)
		if ferr != nil {
			return nil, nil, ferr
		}
		var node yaml.Node
		if uerr := yaml.Unmarshal(rewritten, &node); uerr != nil {
			return nil, nil, uerr
		}
		if derr := node.Decode(&raw); derr != nil {
			return nil, nil, derr
		}
		scalars = walkYAMLScalars(&node, strings.Split(string(rewritten), "\n"))
	case ".toml":
		if err := toml.Unmarshal(contents, &raw); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, errors.New("unsupported file type")
	}

	value, isMap := raw.(map[string]any)
	if !isMap {
		return nil, nil, errors.New("document root is not an object")
	}

	return value, scalars, nil
}
