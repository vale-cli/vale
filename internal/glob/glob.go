// Package glob implements pure-Go globbing utilities.
package glob

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/gobwas/glob"
)

type Glob struct {
	Pattern string
	Negated bool

	// compiled is the `gobwas/glob` form of Pattern, built once by NewGlob.
	// It's nil for a `**` pattern, which doublestar matches instead.
	compiled glob.Glob
}

// Match returns whether or not the Glob g matches the string query.
func (g Glob) Match(query string) bool {
	q := filepath.ToSlash(query)

	if g.compiled == nil {
		matched, _ := doublestar.Match(g.Pattern, q)
		return matched != g.Negated
	}

	return g.compiled.Match(q) != g.Negated
}

// MatchAny returns whether or not the Glob g matches any of the strings in
// query.
func (g Glob) MatchAny(query []string) bool {
	for _, q := range query {
		if g.Match(q) {
			return true
		}
	}
	return false
}

// NewGlob creates a Glob from the string pat.
func NewGlob(pat string) (Glob, error) {
	negate := false
	if strings.HasPrefix(pat, "!") {
		pat = strings.TrimLeft(pat, "!")
		negate = true
	}
	_, err := doublestar.PathMatch(pat, "")
	if err != nil {
		return Glob{}, err
	}

	if strings.Contains(pat, "**") {
		return Glob{Pattern: pat, Negated: negate}, nil
	}

	// doublestar accepts patterns gobwas/glob rejects, and Match used to
	// compile there with MustCompile -- so `--glob='[a-]*'` panicked mid-walk.
	compiled, err := glob.Compile(pat)
	if err != nil {
		return Glob{}, err
	}
	return Glob{Pattern: pat, Negated: negate, compiled: compiled}, nil
}

// Compile is a wrapper around NewGlobal for backwards compatibility.
func Compile(pat string) (Glob, error) {
	return NewGlob(pat)
}
