//go:build !vale_nocode

package tsw

import (
	"fmt"
	"regexp"
	"sync"
)

// Predicate step types, as tree-sitter numbers them.
const (
	stepDone    = 0
	stepCapture = 1
	stepString  = 2
)

// Query error types, as tree-sitter numbers them.
var queryErrors = []string{"none", "syntax", "node type", "field", "capture", "structure", "language"}

// A Query is a compiled pattern, belonging to the tree's instance.
type Query struct {
	h          int32
	captures   []string
	predicates [][]step // per pattern
	regexps    sync.Map // pattern -> *regexp.Regexp, for match?
}

type step struct {
	kind, value uint32
}

// CaptureNameForID is the name a capture was given in the query.
func (q *Query) CaptureNameForID(i uint32) string { return q.captures[i] }

// A Match is one occurrence of a query's pattern.
type Match struct {
	PatternIndex uint32
	Captures     []Capture
}

// A Capture is one captured node.
type Capture struct {
	Index uint32
	Node  *Node
}

// Query compiles expr for the tree's grammar, once per instance.
func (t *Tree) Query(expr string) (*Query, error) {
	in := t.inst
	if q, ok := in.queries[expr]; ok {
		return q, nil
	}

	ptr := in.write([]byte(expr))
	h := i32(in.call(in.queryNew, uint64(ptr), uint64(len(expr))))
	in.call(in.release, uint64(ptr))
	if h < 0 {
		offset := in.call(in.lastLen)
		kind := in.call(in.queryError)
		what := "unknown"
		if kind < uint64(len(queryErrors)) {
			what = queryErrors[kind]
		}
		return nil, fmt.Errorf("query: %s error at offset %d", what, offset)
	}

	q := &Query{h: h}
	n := u32(in.call(in.captureCount, handle(h)))
	for i := range n {
		q.captures = append(q.captures, in.str(in.call(in.captureName, handle(h), uint64(i))))
	}
	patterns := u32(in.call(in.patternCount, handle(h)))
	for p := range patterns {
		count := in.call(in.predicates, handle(h), uint64(p))
		var steps []step
		if count > 0 {
			words := in.words()
			for i := 0; i+1 < len(words); i += 2 {
				steps = append(steps, step{kind: words[i], value: words[i+1]})
			}
		}
		q.predicates = append(q.predicates, steps)
	}

	in.queries[expr] = q
	return q, nil
}

// stringValue is a predicate's literal argument.
func (t *Tree) stringValue(q *Query, id uint32) string {
	in := t.inst
	return in.str(in.call(in.stringValue, handle(q.h), uint64(id)))
}

// Matches runs q over the whole tree and returns the matches whose
// predicates hold. src is the parsed source, for the predicates to read.
func (t *Tree) Matches(q *Query, src []byte) []Match {
	in := t.inst
	count := in.call(in.queryExec, handle(q.h), handle(t.root.h))
	words := in.words()

	var out []Match
	i := 0
	for range count {
		m := Match{PatternIndex: words[i]}
		n := words[i+1]
		i += 2
		for range n {
			m.Captures = append(m.Captures, Capture{Index: words[i], Node: &Node{tree: t, h: i32(uint64(words[i+1]))}})
			i += 2
		}
		if t.holds(q, m, src) {
			out = append(out, m)
		}
	}
	return out
}

// holds evaluates a pattern's predicates against a match the way the CGo
// binding did: #eq? and #not-eq? between two captures or a capture and a
// string, #match? and #not-match? between a capture and a pattern.
func (t *Tree) holds(q *Query, m Match, src []byte) bool {
	steps := q.predicates[m.PatternIndex]
	for len(steps) > 0 {
		end := 0
		for end < len(steps) && steps[end].kind != stepDone {
			end++
		}
		pred := steps[:end]
		if end < len(steps) {
			end++
		}
		steps = steps[end:]

		if len(pred) < 3 || pred[0].kind != stepString {
			continue
		}
		op := t.stringValue(q, pred[0].value)
		left := q.captures[pred[1].value]

		switch op {
		case "eq?", "not-eq?":
			want := op == "eq?"
			if pred[2].kind == stepCapture {
				right := q.captures[pred[2].value]
				var a, b *Node
				for _, c := range m.Captures {
					switch q.captures[c.Index] {
					case left:
						a = c.Node
					case right:
						b = c.Node
					}
				}
				if a != nil && b != nil && (a.Content(src) == b.Content(src)) != want {
					return false
				}
			} else {
				value := t.stringValue(q, pred[2].value)
				for _, c := range m.Captures {
					if q.captures[c.Index] == left && (c.Node.Content(src) == value) != want {
						return false
					}
				}
			}
		case "match?", "not-match?":
			want := op == "match?"
			re := t.regexpFor(q, pred[2].value)
			for _, c := range m.Captures {
				if q.captures[c.Index] == left && re.MatchString(c.Node.Content(src)) != want {
					return false
				}
			}
		}
	}
	return true
}

// regexpFor compiles a match? pattern once per query.
func (t *Tree) regexpFor(q *Query, id uint32) *regexp.Regexp {
	if re, ok := q.regexps.Load(id); ok {
		return re.(*regexp.Regexp) //nolint:errcheck // only regexps are stored
	}
	re := regexp.MustCompile(t.stringValue(q, id))
	q.regexps.Store(id, re)
	return re
}
