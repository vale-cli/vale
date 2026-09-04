package check

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"sync"

	"github.com/andybalholm/cascadia"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Parsed scopes and selectors are cached rather than rebuilt.
//
// Both are derived from strings that are fixed for the life of a run: a rule's
// scope comes from its definition, and a block's from the parser. But they were
// parsed afresh for every rule against every block, which for a large style is
// millions of times. Together they accounted for roughly 70% of everything Vale
// allocated -- 13 GB of 18 GB on a 650 KB file -- and the resulting garbage
// collection cost more than the regular-expression matching did.
//
// The parsed values are read-only, so one copy can serve every caller.
var (
	scopeCache    sync.Map // string -> Scope
	selectorCache sync.Map // string -> Selector
)

// cachedSelector parses a dotted scope string, reusing an earlier parse.
func cachedSelector(value string) Selector {
	if hit, ok := selectorCache.Load(value); ok {
		return hit.(Selector) //nolint:errcheck // only Selectors are stored
	}
	sel := NewSelector(strings.Split(value, "."))
	selectorCache.Store(value, sel)
	return sel
}

// A Selector represents a named section of text.
type Selector struct {
	Value   []string // e.g., text.comment.line.py
	Negated bool

	// sections is Value split on ".", computed once.
	//
	// Contains calls Sections on both operands, so a single scope comparison
	// re-split both selectors; with a rule run against every block that was
	// 40% of everything Vale allocated after the scope cache landed. Value
	// never changes after construction, so the split is done with it.
	//
	// A Selector built as a literal rather than through NewSelector leaves
	// this nil, and Sections falls back to splitting on demand.
	sections []string
}

type Scope struct {
	Selectors map[string][]Selector
}

func NewSelector(value []string) Selector {
	negated := false

	parts := []string{}
	for i, m := range value {
		m = strings.TrimSpace(m)
		if i == 0 && strings.HasPrefix(m, "~") {
			m = strings.TrimPrefix(m, "~")
			negated = true
		}
		parts = append(parts, m)
	}

	return Selector{Value: parts, Negated: negated, sections: split(parts)}
}

// split flattens dotted parts into their sections.
func split(value []string) []string {
	parts := make([]string, 0, len(value))
	for _, m := range value {
		parts = append(parts, strings.Split(m, ".")...)
	}
	return parts
}

func NewScope(value []string) Scope {
	key := strings.Join(value, "\x00")
	if hit, ok := scopeCache.Load(key); ok {
		return hit.(Scope) //nolint:errcheck // only Scopes are stored
	}

	scope := map[string][]Selector{}
	for _, v := range value {
		selectors := []Selector{}
		parts := splitOutside(v, '&')
		for _, part := range parts {
			selectors = append(selectors, newPart(part, len(parts) == 1))
		}
		scope[v] = selectors
	}

	built := Scope{Selectors: scope}
	scopeCache.Store(key, built)

	return built
}

// Macthes the scope `s` matches `s2`.
func (s Scope) Matches(blk nlp.Block) bool {
	candidate := cachedSelector(blk.Scope)
	parent := cachedSelector(blk.Parent)

	// A sentence fragment's scope is its parent's plus `sentence`, so any
	// selector it satisfies without naming `sentence` is satisfied by the
	// parent block too -- and that is the copy such a rule must see: a
	// `scope: heading` rule reading a heading one fragment at a time reported
	// `a.` as a whole heading (#1150).
	fragment := strings.HasPrefix(blk.Scope, "sentence.")

	for _, sel := range s.Selectors {
		if fragment && !asksForSentence(sel) {
			continue
		}
		if s.partMatches(candidate, parent, sel) {
			return true
		}
	}

	return false
}

// asksForSentence reports whether any of the AND-ed parts names `sentence`
// without negating it.
func asksForSentence(options []Selector) bool {
	for _, part := range options {
		if !part.Negated && part.Has("sentence") {
			return true
		}
	}
	return false
}

func (s Scope) partMatches(target, parent Selector, options []Selector) bool {
	for _, part := range options {
		tm := target.Contains(part)
		pm := parent.Contains(part)
		if part.Negated && !pm {
			if target.Has("raw") || target.Has("summary") || target.Has("doc") {
				// This can't apply to sized scopes, nor to a selection, whose
				// text is linted where it lies.
				return false
			}
		} else if (!part.Negated && !tm) || (part.Negated && pm) {
			return false
		}
	}
	return true
}

// Sections splits a Selector into its parts -- e.g., text.comment.line.py ->
// []string{"text", "comment", "line", "py"}.
func (s *Selector) Sections() []string {
	if s.sections != nil {
		return s.sections
	}
	// Not built by NewSelector. Computed rather than stored, so a Selector
	// shared between goroutines is not written to behind their backs.
	return split(s.Value)
}

// Contains determines if all if sel's sections are in s.
func (s *Selector) Contains(sel Selector) bool {
	return core.AllStringsInSlice(sel.Sections(), s.Sections())
}

// ContainsString determines if all if sel's sections are in s.
func (s *Selector) ContainsString(scope []string) bool {
	for _, option := range scope {
		sel := Selector{Value: []string{option}, sections: split([]string{option})}
		if !s.Contains(sel) {
			return false
		}
	}
	return true
}

// Equal determines if sel == s.
func (s *Selector) Equal(sel Selector) bool {
	if len(s.Value) == len(sel.Value) {
		for i, v := range s.Value {
			if sel.Value[i] != v {
				return false
			}
		}
		return true
	}
	return false
}

// Has determines if s has a part equal to scope.
func (s *Selector) Has(scope string) bool {
	return core.StringInSlice(scope, s.Sections())
}

// newPart parses one `&`-separated term of a scope.
//
// A `doc(...)` term names elements of the document view by CSS selector. On
// its own it selects the matched element as a block, `doc.<id>`; beside
// another term, or negated, it narrows to blocks inside the element, which
// carry `in.<id>`.
func newPart(part string, standalone bool) Selector {
	term := strings.TrimSpace(part)
	negated := strings.HasPrefix(term, "~")
	term = strings.TrimPrefix(term, "~")

	sel, ok := docSelection(term)
	if !ok {
		return NewSelector(strings.Split(part, "."))
	}

	kind := "in"
	if standalone && !negated {
		kind = "doc"
	}
	return Selector{Value: []string{term}, Negated: negated, sections: []string{kind, docID(sel)}}
}

// docSelection returns the selector inside a `doc(...)` term.
func docSelection(term string) (string, bool) {
	if strings.HasPrefix(term, "doc(") && strings.HasSuffix(term, ")") {
		return term[len("doc(") : len(term)-1], true
	}
	return "", false
}

// docID names a selector. It is derived from the selector's text so that a
// rule and the walker agree on it without sharing state.
func docID(sel string) string {
	h := fnv.New32a()
	h.Write([]byte(sel)) //nolint:errcheck // hash writes cannot fail
	return fmt.Sprintf("%08x", h.Sum32())
}

// reHasChild is the standard spelling of "has a child matching", which
// cascadia only knows by its own name.
var reHasChild = regexp.MustCompile(`:has\(\s*>\s*`)

// compileSelector parses a `doc(...)` selector.
func compileSelector(sel string) (cascadia.Sel, error) {
	return cascadia.Parse(reHasChild.ReplaceAllString(sel, ":haschild("))
}

// DocSelectors returns the selectors named by `doc(...)` terms in a scope,
// each under its id.
func DocSelectors(scope string) map[string]string {
	found := map[string]string{}
	for _, part := range splitOutside(scope, '&') {
		term := strings.TrimPrefix(strings.TrimSpace(part), "~")
		if sel, ok := docSelection(term); ok {
			found[docID(sel)] = sel
		}
	}
	return found
}

// splitOutside splits s on sep, leaving alone any sep inside parentheses or
// quotes -- the `&`, `.` and `~` a selector may contain are its own.
func splitOutside(s string, sep rune) []string {
	var parts []string
	var quote rune
	depth, start := 0, 0

	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			if depth > 0 {
				depth--
			}
		case r == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
