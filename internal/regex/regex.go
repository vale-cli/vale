// Package regex adapts github.com/dlclark/regexp2 to the API Vale uses.
//
// Vale needs a backtracking engine: user-authored rules rely on lookarounds
// and backreferences, which regexp/syntax rejects outright. regexp2 provides
// that, but its matching methods return errors and its Find surface is
// narrower than the standard library's, so this package supplies the missing
// convenience layer.
//
// # Offsets are rune-based
//
// FindAllStringIndex and friends report positions in the *rune* slice, not
// byte offsets. Callers converting to byte positions must keep doing so; see
// core.re2Loc.
//
// # Errors
//
// regexp2 can fail at match time, most often by exceeding its backtracking
// budget on a pathological pattern. The wrappers here panic in that case,
// matching the behaviour Vale has always had. Anything that should degrade
// gracefully needs to call the error-returning method directly.
package regex

import (
	"sync"

	"github.com/jdkato/regexp2/v2"
)

// Match is a single regular-expression match.
type Match = regexp2.Match

// Regexp is a compiled regular expression.
//
// It embeds *regexp2.Regexp, so the engine's own API — String, Replace,
// ReplaceFunc, MatchString, FindStringMatch, FindNextMatch — is available
// directly.
type Regexp struct {
	*regexp2.Regexp

	// tests works out, the first time it is asked, what a subject must
	// contain to match; see prefilter.go. Working it out reads the whole
	// pattern again, and a pattern that is never asked -- one a rule keeps
	// only to fall back on -- has no need of it.
	tests *lazyTests
}

// lazyTests is a pattern's prefilter, derived on first use. A nil set means
// the pattern has to be run to find out.
type lazyTests struct {
	once sync.Once
	expr string
	set  *FilterSet
}

func (l *lazyTests) get() *FilterSet {
	l.once.Do(func() {
		if tests := Requirements(l.expr); len(tests) > 0 {
			l.set = NewFilterSet([][][]string{tests})
		}
	})
	return l.set
}

// String returns the source text of the pattern.
//
// The zero value is usable and reports "". Vale relies on that: an unset
// pattern is represented by &Regexp{} and detected with String() == "", so
// this must not dereference the embedded pointer.
func (re *Regexp) String() string {
	if re == nil || re.Regexp == nil {
		return ""
	}
	return re.Regexp.String()
}

// IsZero reports whether re carries no pattern.
func (re *Regexp) IsZero() bool {
	return re == nil || re.Regexp == nil
}

// Compile parses a regular expression in RE2 compatibility mode.
func Compile(expr string) (*Regexp, error) {
	re, err := regexp2.Compile(expr, regexp2.RE2)
	if err != nil {
		return nil, err
	}
	return &Regexp{Regexp: re, tests: &lazyTests{expr: expr}}, nil
}

// MightMatch reports whether lowered could contain a match.
//
// lowered is the subject lower-cased, which the caller does once per block
// rather than once per rule. A false return is definitive: the pattern cannot
// match. A true return means the pattern still has to be run.
func (re *Regexp) MightMatch(lowered string) bool {
	if re == nil || re.tests == nil {
		return true
	}
	set := re.tests.get()
	return set == nil || set.Any(lowered)
}

// A MatchError is what the wrappers here panic with when the engine fails
// during a match, most often on a pattern too large for its backtracking
// stack. A caller that knows which rule was running recovers it to say so.
type MatchError struct{ Err error }

func (e *MatchError) Error() string { return e.Err.Error() }

func (e *MatchError) Unwrap() error { return e.Err }

// MustCompile is Compile but panics on an invalid pattern. It is meant for
// patterns fixed at compile time.
func MustCompile(expr string) *Regexp {
	re, err := Compile(expr)
	if err != nil {
		panic(err)
	}
	return re
}

// MatchStringStd reports whether s contains a match, panicking if the match
// itself fails.
func (re *Regexp) MatchStringStd(s string) bool {
	matched, err := re.MatchString(s)
	if err != nil {
		panic(&MatchError{Err: err})
	}
	return matched
}

// FindAllString returns up to n successive matches, or all of them if n < 0.
//
// A nil return means no match.
func (re *Regexp) FindAllString(s string, n int) []string {
	var result []string
	re.eachMatch(s, n, func(m *regexp2.Match) {
		result = append(result, m.Group.String())
	})
	return result
}

// FindAllStringMatches returns every successive match.
func (re *Regexp) FindAllStringMatches(s string) []*Match {
	var result []*Match
	re.eachMatch(s, -1, func(m *regexp2.Match) {
		result = append(result, m)
	})
	return result
}

// FindAllStringIndex returns the rune-index bounds of up to n successive
// matches, or all of them if n < 0.
//
// A nil return means no match.
func (re *Regexp) FindAllStringIndex(s string, n int) [][]int {
	var result [][]int
	re.eachMatch(s, n, func(m *regexp2.Match) {
		result = append(result, []int{
			m.Group.RuneIndex,
			m.Group.RuneIndex + m.Group.RuneLength,
		})
	})
	return result
}

// FindAllStringSubmatch returns up to n successive matches and their
// submatches, or all of them if n < 0.
//
// A nil return means no match.
func (re *Regexp) FindAllStringSubmatch(s string, n int) [][]string {
	var result [][]string
	re.eachMatch(s, n, func(m *regexp2.Match) {
		groups := m.Groups()
		subs := make([]string, 0, len(groups))
		for i := range groups {
			subs = append(subs, groups[i].String())
		}
		result = append(result, subs)
	})
	return result
}

// FindAllStringSubmatchIndex returns the rune-index bounds of up to n
// successive matches and their submatches, or all of them if n < 0.
//
// A group that did not participate in the match is reported as -1, -1, the
// same convention the standard library uses.
//
// A nil return means no match.
func (re *Regexp) FindAllStringSubmatchIndex(s string, n int) [][]int {
	var result [][]int
	re.eachMatch(s, n, func(m *regexp2.Match) {
		groups := m.Groups()
		subs := make([]int, 0, len(groups)*2)
		for i := range groups {
			g := &groups[i]
			// An empty match at the start of the string also has index 0 and
			// length 0, so only the capture list says whether the group took
			// part.
			if len(g.Captures) == 0 {
				subs = append(subs, -1, -1)
				continue
			}
			subs = append(subs, g.RuneIndex, g.RuneIndex+g.RuneLength)
		}
		result = append(result, subs)
	})
	return result
}

// SubexpNames returns the names of the parenthesized subexpressions.
//
// The name for the first sub-expression is names[1], so for a match slice m
// the name for m[i] is SubexpNames()[i]. The expression as a whole cannot be
// named, so names[0] is always empty.
func (re *Regexp) SubexpNames() []string {
	names := re.GetGroupNames()
	result := make([]string, 0, len(names))
	for i, name := range names {
		if i == 0 {
			result = append(result, "")
			continue
		}
		result = append(result, name)
	}
	return result
}

// Split slices s around each match, returning the pieces between them.
//
// n limits the number of pieces; n < 0 means no limit. A pattern that does
// not match returns s unchanged, as a single element.
func (re *Regexp) Split(s string, n int) []string {
	if n == 0 {
		return []string{s}
	}
	parts, err := re.Regexp.Split(s, n)
	if err != nil {
		panic(&MatchError{Err: err})
	}
	return parts
}

// eachMatch walks up to n matches, calling fn for each. n < 0 means all.
//
// Centralised so every Find* variant shares one iteration and one error
// policy, rather than repeating the walk with subtly different bounds.
func (re *Regexp) eachMatch(s string, n int, fn func(*regexp2.Match)) {
	if n == 0 {
		return
	}

	m, err := re.FindStringMatch(s)
	if err != nil {
		panic(&MatchError{Err: err})
	}

	count := 0
	for m != nil {
		fn(m)
		count++
		if n > 0 && count >= n {
			return
		}

		m, err = re.FindNextMatch(m)
		if err != nil {
			panic(&MatchError{Err: err})
		}
	}
}
