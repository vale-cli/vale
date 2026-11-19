package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/system"
	"github.com/vale-cli/vale/v3/internal/textfsm"
)

// DaselValue is the decoded document root passed to dasel. It may be a map
// (object root), a slice (array root, see #1017), or any other JSON/YAML/TOML
// value -- dasel navigates all of them.
type DaselValue = any

var viewEngines = []string{"tree-sitter", "dasel", "textfsm"}

// A Scope is a single query that we want to run against a document.
type Scope struct {
	Name string `yaml:"name"`
	Expr string `yaml:"expr"`
	Type string `yaml:"type"`

	// Join, when set, is the separator a selected list of strings is
	// joined with, so the list reads as one value rather than one value
	// per element. A notebook cell's source is such a list.
	Join *string `yaml:"join"`
}

// A View is a named, virtual representation of a subset of a file's
// structured content. It is defined by a set of queries that can be
// used to extract specific information from the file.
//
// The supported engines are:
//
//   - `dasel`, which queries the parsed value of a data file;
//   - `tree-sitter`, which queries the syntax tree of a source file; and
//   - `textfsm`, which reads a plain-text file through a template -- a state
//     machine of regular expressions -- and names what it captures.
type View struct {
	Engine string  `yaml:"engine"`
	Scopes []Scope `yaml:"scopes"`

	// Template is the TextFSM template a `textfsm` view reads with. Each
	// scope's `expr` names one of its values.
	Template string `yaml:"template"`

	fsm *textfsm.Template
}

// A ScopedValue is a single value extracted from a scope, along with the
// source position it was parsed from. Line/Column are 1-based; both are 0
// when the source format does not provide position information (e.g., TOML)
// or when the value could not be located in the parse tree.
type ScopedValue struct {
	Text   string
	Line   int
	Column int

	// Joined marks a value made from a list, so its own line breaks are
	// real ones and not an escape in a scalar.
	Joined bool

	// Parts maps the value back to the scalars it was decoded from. It is
	// empty when the parser gave no positions.
	Parts []Part
}

// A Part is the run of a value that came from one scalar: where it starts
// in the value and in the source, and the source text it was decoded from.
type Part struct {
	Offset int // rune offset in the value
	Line   int
	Column int

	// Quote is the scalar's quote character, or 0 for a plain or block
	// scalar. A quoted scalar decodes escapes, so a column in its text is
	// not a column in the source.
	Quote byte

	// Raw is the source line from Column on; the walk through its escapes
	// stops there, so a quoted scalar folded over several lines is placed
	// by its first.
	Raw string
}

// Locate maps a rune offset in the value to its line and column in the
// source.
func (v ScopedValue) Locate(off int) (int, int) {
	if len(v.Parts) == 0 {
		return v.Line, v.Column + off
	}
	p := v.Parts[0]
	for _, q := range v.Parts[1:] {
		if q.Offset > off {
			break
		}
		p = q
	}
	d := max(off-p.Offset, 0)

	if p.Quote != 0 {
		return p.Line, p.Column + rawWidth(p.Raw, p.Quote, d)
	}

	// A plain or block scalar spans source lines as its text does, and every
	// line of a block starts at the indentation of its first.
	text := []rune(v.Text)
	if p.Offset+d > len(text) {
		d = len(text) - p.Offset
	}
	seg := string(text[p.Offset : p.Offset+d])
	if nl := strings.LastIndex(seg, "\n"); nl >= 0 {
		return p.Line + strings.Count(seg, "\n"), p.Column + nlp.StrLen(seg[nl+1:])
	}
	return p.Line, p.Column + d
}

// rawWidth returns how many runes of a quoted scalar's source text decode
// to its first d runes. An escape is several runes in the source and one in
// the value; a doubled quote in a single-quoted scalar is two and one.
func rawWidth(raw string, quote byte, d int) int {
	runes := []rune(raw)
	i, decoded := 0, 0
	for i < len(runes) && decoded < d {
		switch r := runes[i]; {
		case quote == '\'' && r == '\'':
			if i+1 < len(runes) && runes[i+1] == '\'' {
				i += 2
			} else {
				return i // the closing quote
			}
		case quote == '"' && r == '"':
			return i
		case quote == '"' && r == '\\' && i+1 < len(runes):
			i += escapeWidth(runes[i+1:])
		default:
			i++
		}
		decoded++
	}
	return i
}

// escapeWidth returns the width of the escape whose kind is at the start of
// runes, counting the backslash before it.
func escapeWidth(runes []rune) int {
	switch runes[0] {
	case 'x':
		return 4
	case 'u':
		return 6
	case 'U':
		return 10
	default:
		return 2
	}
}

// A ScopedValues is a value that has been assigned a scope.
type ScopedValues struct {
	Scope  string
	Format string
	Values []ScopedValue
}

// NewView creates a new blueprint from the given path.
func NewView(path string, fsys system.FS) (*View, error) {
	data, err := fsys.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return NewViewFromString(string(data))
}

// NewViewFromString parses and validates a View from its YAML text.
func NewViewFromString(text string) (*View, error) {
	var view View

	err := yaml.Unmarshal([]byte(text), &view)
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

	if view.Engine == "textfsm" {
		if err = view.compileTemplate(); err != nil {
			return nil, err
		}
	}

	return &view, nil
}

// compileTemplate parses a `textfsm` view's template and checks that every
// scope names one of its values.
func (b *View) compileTemplate() error {
	if strings.TrimSpace(b.Template) == "" {
		return errors.New("a textfsm view needs a template")
	}

	fsm, err := textfsm.Parse(b.Template)
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}

	for _, s := range b.Scopes {
		found := false
		for _, v := range fsm.Values {
			if v.Name == s.Expr {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("scope %q names no Value in the template", s.Expr)
		}
	}

	b.fsm = fsm
	return nil
}

// applyTemplate runs a `textfsm` view over a file.
//
// A List value's captures on consecutive lines are joined into one value, so
// a body reads as the paragraphs it is rather than one block per line. A gap
// in the lines starts another value, and so does a change of column: a value
// is placed by one line and one column, so every line in it has to start
// where its first line does.
func (b *View) applyTemplate(f *File) ([]ScopedValues, error) {
	records, err := b.fsm.Run(f.Content)
	if err != nil {
		return nil, err
	}

	found := make([]ScopedValues, 0, len(b.Scopes))
	for _, s := range b.Scopes {
		var values []ScopedValue
		for _, rec := range records {
			values = append(values, joinCaptures(rec[s.Expr])...)
		}
		if len(values) == 0 {
			// A value the template never filled is still a scope, so a rule
			// that requires something of it can report that it is missing.
			// Every other rule sees no text and stays quiet.
			values = []ScopedValue{{Text: "", Line: 1, Column: 1}}
		}
		found = append(found, ScopedValues{Scope: s.Name, Values: values, Format: s.Type})
	}

	return found, nil
}

func joinCaptures(caps []textfsm.Capture) []ScopedValue {
	var out []ScopedValue
	var run []string
	var start textfsm.Capture

	flush := func() {
		text := strings.TrimRight(strings.Join(run, "\n"), "\n")
		if strings.TrimSpace(text) != "" {
			out = append(out, ScopedValue{Text: text, Line: start.Line, Column: start.Column})
		}
		run = nil
	}

	for i, c := range caps {
		if i > 0 && (c.Line != caps[i-1].Line+1 || c.Column != caps[i-1].Column) {
			flush()
		}
		if run == nil {
			start = c
		}
		run = append(run, c.Text)
	}
	if run != nil {
		flush()
	}
	return out
}

func (b *View) Apply(f *File) ([]ScopedValues, error) {
	if b.Engine == "textfsm" {
		return b.applyTemplate(f)
	}

	value, scalars, err := fileToValue(f)
	if err != nil {
		return nil, err
	}

	resolver := newScalarResolver(scalars)
	srcLines := strings.Split(f.Content, "\n")
	part := func(sp scalarPos, offset int) Part { return newPart(srcLines, sp, offset) }

	found := make([]ScopedValues, 0, len(b.Scopes))
	for _, s := range b.Scopes {
		items, serr := selectValues(value, s.Expr)
		if serr != nil {
			return nil, fmt.Errorf("processing scope %q: %w", s.Name, serr)
		}
		values := make([]ScopedValue, 0, len(items))
		for _, item := range items {
			switch v := item.(type) {
			case string:
				sv := ScopedValue{Text: v}
				if sp, ok := resolver.locate(v); ok {
					sv.Line, sv.Column = sp.Line, sp.Column
					sv.Parts = []Part{part(sp, 0)}
				}
				values = append(values, sv)
			case []any:
				parts, ok := stringList(v)
				if !ok || s.Join == nil || len(parts) == 0 {
					continue
				}
				// Each element is placed on its own, so the list may be
				// written one per line or all on one; and each is consumed,
				// so a later scope is not placed on a line this value covers.
				sv := ScopedValue{Text: strings.Join(parts, *s.Join), Joined: true}
				offset := 0
				for i, p := range parts {
					if sp, found := resolver.locate(p); found {
						if i == 0 {
							sv.Line, sv.Column = sp.Line, sp.Column
						}
						sv.Parts = append(sv.Parts, part(sp, offset))
					}
					offset += nlp.StrLen(p) + nlp.StrLen(*s.Join)
				}
				if len(sv.Parts) > 0 && sv.Parts[0].Offset != 0 {
					// The first element was not found; the value has no
					// place of its own, so it is searched for instead.
					sv.Parts = nil
				}
				values = append(values, sv)
			}
		}
		found = append(found, ScopedValues{
			Scope:  s.Name,
			Values: values,
			Format: s.Type,
		})
	}

	return found, nil
}

// selectStrings returns the strings a selector lands on; anything else it
// lands on is dropped.
func selectStrings(value DaselValue, expr string) ([]string, error) {
	items, err := selectValues(value, expr)
	if err != nil {
		return nil, err
	}
	results := make([]string, 0, len(items))
	for _, v := range items {
		if str, isStr := v.(string); isStr {
			results = append(results, str)
		}
	}
	return results, nil
}

// stringList reports whether every element of a list is a string.
func stringList(list []any) ([]string, bool) {
	parts := make([]string, 0, len(list))
	for _, v := range list {
		str, ok := v.(string)
		if !ok {
			return nil, false
		}
		parts = append(parts, str)
	}
	return parts, true
}

// scalarPos records a single scalar value's source position, and the quote
// it was written with, if any.
type scalarPos struct {
	Value  string
	Line   int
	Column int
	Quote  byte
}

// newPart places one scalar's run of a value in the source.
func newPart(srcLines []string, sp scalarPos, offset int) Part {
	raw := ""
	if sp.Line-1 < len(srcLines) {
		if runes := []rune(srcLines[sp.Line-1]); sp.Column-1 < len(runes) {
			raw = string(runes[sp.Column-1:])
		}
	}
	return Part{Offset: offset, Line: sp.Line, Column: sp.Column, Quote: sp.Quote, Raw: raw}
}

// scalarResolver maps extracted string values back to source positions by
// consuming entries from a flat, document-ordered list of scalars.
// A scalarResolver hands out scalars by value, each once, in document order.
// The scalars are indexed by value up front: scanning the list for every
// value made a large document quadratic to place.
type scalarResolver struct {
	byValue map[string][]scalarPos
}

func newScalarResolver(scalars []scalarPos) *scalarResolver {
	r := &scalarResolver{byValue: make(map[string][]scalarPos)}
	for _, s := range scalars {
		r.byValue[s.Value] = append(r.byValue[s.Value], s)
	}
	return r
}

// locate returns the first unconsumed scalar matching value, and false when
// nothing matches -- callers must fall back to a textual search in that case.
func (r *scalarResolver) locate(value string) (scalarPos, bool) {
	if r == nil {
		return scalarPos{}, false
	}
	queue := r.byValue[value]
	if len(queue) == 0 {
		return scalarPos{}, false
	}
	r.byValue[value] = queue[1:]
	return queue[0], true
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
			out = append(out, scalarAt(node, srcLines))
		case yaml.AliasNode:
			walk(node.Alias)
		}
	}
	walk(n)
	return out
}

// scalarAt records where a scalar's content starts.
func scalarAt(node *yaml.Node, srcLines []string) scalarPos {
	line, col := node.Line, node.Column
	var quote byte
	switch {
	case node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		line, col = blockScalarContentStart(srcLines, node.Line)
	case node.Style&yaml.DoubleQuotedStyle != 0:
		// Step past the opening quote so callers point at
		// the first content character.
		col++
		quote = '"'
	case node.Style&yaml.SingleQuotedStyle != 0:
		col++
		quote = '\''
	}
	return scalarPos{Value: node.Value, Line: line, Column: col, Quote: quote}
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
	case ".toml":
		if err := unmarshalTOML(contents, &raw); err != nil {
			return nil, nil, err
		}
	default:
		// YAML, which reads JSON too, for `.yml`, `.yaml`, and anything
		// the section matched that is not named for its format.
		//
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
	}

	if raw == nil {
		return nil, nil, errors.New("document root is empty")
	}

	return raw, scalars, nil
}
