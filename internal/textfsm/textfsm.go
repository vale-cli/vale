// Package textfsm reads line-oriented text with a template: a state machine
// whose rules are regular expressions, in the form Google's TextFSM defined
// for parsing the output of network devices.
//
// A template declares the values it captures, then the states it moves
// through. Each state is a list of rules, tried in order against the current
// line; the first to match assigns its captures and says what happens next:
// which line to read, whether to emit a record, and which state to enter.
//
//	Value Subject (.+)
//	Value List Body (.*)
//
//	Start
//	  ^${Subject} -> Body
//
//	Body
//	  ^${Body}
//
// The result is a list of records, one per Record action plus the one implied
// at the end of input, each holding the captures that filled it -- with the
// line and column each capture came from, which is what places an alert.
package textfsm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jdkato/regexp2/v2"
)

// A Value is one named capture a template fills.
type Value struct {
	Name  string
	Regex string

	// Filldown keeps the last capture across records; Required drops a
	// record the value is missing from; List gathers every capture rather
	// than the last one.
	Filldown bool
	Required bool
	List     bool
}

// A Capture is one piece of text a value took, and where it came from.
// Line and Column are 1-based, and Column counts runes.
type Capture struct {
	Text   string
	Line   int
	Column int
}

// A Record is one emitted set of captures, keyed by value name. A List value
// holds every capture; any other holds one.
type Record map[string][]Capture

type rule struct {
	re   *regexp2.Regexp
	next string // state to enter, or "" to stay

	cont  bool // Continue: keep trying rules on this line
	rec   string
	errMs string
}

// A Template is a compiled state machine.
type Template struct {
	Values []Value
	states map[string][]rule
	names  map[string]*Value
}

var reValue = regexp2.MustCompile(`^Value\s+(?:((?:\w+,?)+)\s+)?(\w+)\s+\((.*)\)\s*$`, regexp2.RE2)

// Parse compiles a template.
func Parse(src string) (*Template, error) {
	t := &Template{states: map[string][]rule{}, names: map[string]*Value{}}

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	i := 0

	// Values, up to the first blank line.
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if len(t.Values) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		m, _ := reValue.FindStringMatch(line)
		if m == nil && len(t.Values) == 0 {
			return nil, fmt.Errorf("line %d: a template needs at least one Value before its states, got %q", i+1, line)
		} else if m == nil {
			return nil, fmt.Errorf("line %d: expected a Value definition, got %q", i+1, line)
		}
		v := Value{Name: m.GroupByNumber(2).String(), Regex: m.GroupByNumber(3).String()}
		for _, opt := range strings.Split(m.GroupByNumber(1).String(), ",") {
			switch strings.TrimSpace(opt) {
			case "":
			case "Filldown":
				v.Filldown = true
			case "Required":
				v.Required = true
			case "List":
				v.List = true
			case "Key", "Fillup":
				// Accepted for compatibility; neither changes what is captured.
			default:
				return nil, fmt.Errorf("line %d: unknown Value option %q", i+1, opt)
			}
		}
		if _, dup := t.names[v.Name]; dup {
			return nil, fmt.Errorf("line %d: Value %q defined twice", i+1, v.Name)
		}
		t.Values = append(t.Values, v)
		t.names[v.Name] = &t.Values[len(t.Values)-1]
	}
	if len(t.Values) == 0 {
		return nil, errors.New("a template needs at least one Value")
	}

	// States.
	state := ""
	for ; i < len(lines); i++ {
		raw := lines[i]
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") {
			if _, dup := t.states[line]; dup {
				return nil, fmt.Errorf("line %d: state %q defined twice", i+1, line)
			}
			state = line
			t.states[state] = nil
			continue
		}
		if state == "" {
			return nil, fmt.Errorf("line %d: a rule before any state", i+1)
		}
		r, err := t.parseRule(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		t.states[state] = append(t.states[state], r)
	}
	if _, ok := t.states["Start"]; !ok {
		return nil, errors.New("a template needs a Start state")
	}
	for name, rules := range t.states {
		for _, r := range rules {
			if _, ok := t.states[r.next]; r.next != "" && r.next != "End" && r.next != "EOF" && !ok {
				return nil, fmt.Errorf("state %q moves to %q, which is not defined", name, r.next)
			}
		}
	}

	return t, nil
}

var reVar = regexp2.MustCompile(`\$\{(\w+)\}|\$(\w+)`, regexp2.RE2)

// parseRule reads `^pattern -> [Line.][Record] [State]`.
func (t *Template) parseRule(line string) (rule, error) {
	r := rule{rec: "NoRecord"}

	pattern, action, hasAction := strings.Cut(line, "->")
	pattern = strings.TrimSpace(pattern)

	expanded, err := t.expand(pattern)
	if err != nil {
		return r, err
	}

	re, err := regexp2.Compile(expanded, regexp2.RE2)
	if err != nil {
		return r, fmt.Errorf("invalid rule %q: %w", pattern, err)
	}
	r.re = re

	if !hasAction {
		return r, nil
	}
	for _, tok := range strings.Fields(action) {
		// An Error's message, quoted, may run to several words.
		if strings.HasPrefix(tok, `"`) || (r.rec == "Error" && r.errMs != "") {
			r.errMs = strings.TrimSpace(r.errMs + " " + strings.Trim(tok, `"`))
			continue
		}
		for _, part := range strings.Split(tok, ".") {
			switch part {
			case "Next":
			case "Continue":
				r.cont = true
			case "Record", "NoRecord", "Clear", "Clearall", "Error":
				r.rec = part
			default:
				if r.next != "" {
					return r, fmt.Errorf("rule names two states, %q and %q", r.next, part)
				}
				r.next = part
			}
		}
	}
	if r.cont && r.next != "" {
		return r, errors.New("a Continue rule cannot change state")
	}
	return r, nil
}

// expand turns each `${Name}` in a pattern into a named group holding that
// value's regex.
func (t *Template) expand(pattern string) (string, error) {
	var out strings.Builder
	var bad error

	expanded, err := reVar.ReplaceFunc(pattern, func(m regexp2.Match) string {
		name := m.GroupByNumber(1).String()
		if name == "" {
			name = m.GroupByNumber(2).String()
		}
		v, ok := t.names[name]
		if !ok {
			bad = fmt.Errorf("rule references undefined Value %q", name)
			return m.String()
		}
		return "(?<" + name + ">" + v.Regex + ")"
	}, -1, -1)
	if err != nil {
		return "", err
	}
	if bad != nil {
		return "", bad
	}
	out.WriteString(expanded)
	return out.String(), nil
}

// Run reads text through the template and returns its records.
func (t *Template) Run(text string) ([]Record, error) {
	var out []Record
	cur := Record{}
	state := "Start"

	emit := func() {
		if len(cur) == 0 {
			return
		}
		for _, v := range t.Values {
			if v.Required && len(cur[v.Name]) == 0 {
				return
			}
		}
		out = append(out, cur)
		cur = t.carry(cur)
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for n, line := range lines {
		if state == "End" {
			break
		}
	rules:
		for _, r := range t.states[state] {
			m, _ := r.re.FindStringMatch(line)
			if m == nil {
				continue
			}
			t.assign(cur, m, n+1)

			switch r.rec {
			case "Record":
				emit()
			case "Clear":
				cur = t.carry(cur)
			case "Clearall":
				cur = Record{}
			case "Error":
				return out, fmt.Errorf("line %d: %s", n+1, r.errMs)
			}
			if r.next != "" {
				state = r.next
			}
			if !r.cont {
				break rules
			}
		}
	}

	// An EOF state, if defined, replaces the record implied at end of input.
	if eof, ok := t.states["EOF"]; ok {
		for _, r := range eof {
			if r.rec == "Record" {
				emit()
			}
		}
		return out, nil
	}
	emit()
	return out, nil
}

// carry starts the next record with the Filldown values of the last.
func (t *Template) carry(prev Record) Record {
	next := Record{}
	for _, v := range t.Values {
		if v.Filldown && len(prev[v.Name]) > 0 {
			next[v.Name] = prev[v.Name]
		}
	}
	return next
}

// assign copies a match's named groups into the record.
//
// A group that matched the empty string still counts: a blank line in a body
// is a capture, and the paragraph break it marks has to survive.
func (t *Template) assign(rec Record, m *regexp2.Match, n int) {
	for _, g := range m.Groups() {
		v, ok := t.names[g.Name]
		if !ok || len(g.Captures) == 0 {
			continue
		}
		c := Capture{Text: g.String(), Line: n, Column: g.RuneIndex + 1}
		if v.List {
			rec[g.Name] = append(rec[g.Name], c)
		} else {
			rec[g.Name] = []Capture{c}
		}
	}
}
