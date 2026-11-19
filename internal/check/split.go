package check

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	rx "github.com/vale-cli/vale/v3/internal/regex"
)

// A tokenSplit runs a rule's tokens as patterns of their own.
//
// A rule's pattern is one alternation of every token, and the engine tries
// each branch at each position of a block. Almost none can match: a token
// needs its literal in the text, and a block holds a few of a rule's
// hundreds. Running only those, and taking their matches in the order the
// alternation would have found them, gives the same result for a fraction of
// the work.
//
// The order is the alternation's own: the leftmost start wins, and at one
// start the earlier token does. That holds only while nothing but the token
// differs between its pattern and the joined one, which is what splittable
// checks.
type tokenSplit struct {
	template string        // the rule's pattern, with %s where a token goes
	tokens   []string      // each token, as joined
	filters  *rx.FilterSet // what each token requires; nothing means it always runs

	once     []sync.Once
	compiled []*rx.Regexp
}

// splitFloor is the fewest tokens worth splitting: below it the alternation
// is cheap, and a pattern per token is not.
const splitFloor = 4

// splitCeiling is the most tokens a block may wake before the alternation is
// the better way to run them. Measured over five styles, the gain levels off
// between 24 and 48.
const splitCeiling = 32

// reUnsplittable matches what ties a token to the pattern around it: a group
// it captures, which the joined pattern numbers; a reference to one; and a
// bare inline flag, which reaches into the tokens after it.
var reUnsplittable = regexp.MustCompile(`\\[1-9]|\\k[<'{]|\(\?[a-zA-Z-]+\)|\(\?P?<[A-Za-z_]|\(\?'|\((?:[^?]|$)`)

// newTokenSplit returns the split form of a rule's tokens, or nil when the
// tokens have to stay one pattern.
func newTokenSplit(template string, tokens []string) *tokenSplit {
	if len(tokens) < splitFloor {
		return nil
	}

	filters := make([][][]string, len(tokens))
	for i, tok := range tokens {
		if reUnsplittable.MatchString(stripEscapes(tok)) {
			return nil
		}
		filters[i] = rx.Requirements(fmt.Sprintf(template, tok))
	}

	return &tokenSplit{
		template: template,
		tokens:   tokens,
		filters:  rx.NewFilterSet(filters),
		once:     make([]sync.Once, len(tokens)),
		compiled: make([]*rx.Regexp, len(tokens)),
	}
}

// stripEscapes drops escaped characters and character classes, so a literal
// parenthesis is not read as a group.
func stripEscapes(expr string) string {
	var b strings.Builder
	class := false
	for i := 0; i < len(expr); i++ {
		switch c := expr[i]; {
		case c == '\\' && i+1 < len(expr):
			// A back-reference is the one escape that matters here.
			if !class && (expr[i+1] == 'k' || (expr[i+1] >= '1' && expr[i+1] <= '9')) {
				b.WriteString(expr[i : i+2])
			}
			i++
		case class:
			class = c != ']'
		case c == '[':
			class = true
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// active lists the tokens that could match text, by its lower-cased form.
func (s *tokenSplit) active(lowered string) []int {
	return s.filters.Active(lowered)
}

// pattern returns token i's own pattern, compiled the first time it wakes.
func (s *tokenSplit) pattern(i int) *rx.Regexp {
	s.once[i].Do(func() {
		// The joined pattern compiled, and this is one branch of it.
		s.compiled[i], _ = rx.Compile(fmt.Sprintf(s.template, s.tokens[i]))
	})
	return s.compiled[i]
}

// find returns the rune bounds of every match the tokens in active make in
// text, in the order their alternation finds them, and which token made each.
// ok is false when a token has no pattern of its own and the alternation has
// to run instead.
func (s *tokenSplit) find(text string, active []int) ([][]int, []int, bool) {
	res := make([]*rx.Regexp, len(active))
	for j, i := range active {
		if res[j] = s.pattern(i); res[j] == nil {
			return nil, nil, false
		}
	}

	var (
		found [][]int
		by    []int
	)
	runes := []rune(text)
	next := make([]*rx.Match, len(active))
	seek := func(j, from int) {
		m, err := res[j].FindRunesMatchStartingAt(runes, from)
		if err != nil {
			panic(&rx.MatchError{Err: err})
		}
		next[j] = m
	}
	for j := range active {
		seek(j, 0)
	}

	for {
		best := -1
		for j, m := range next {
			if m != nil && (best < 0 || m.RuneIndex < next[best].RuneIndex) {
				best = j
			}
		}
		if best < 0 {
			return found, by, true
		}

		m := next[best]
		end := m.RuneIndex + m.RuneLength
		found = append(found, []int{m.RuneIndex, end})
		by = append(by, active[best])

		// The engine resumes where a match ended, a rune on if it was empty.
		if m.RuneLength == 0 {
			if end == len(runes) {
				return found, by, true
			}
			end++
		}
		for j, m := range next {
			if m != nil && m.RuneIndex < end {
				seek(j, end)
			}
		}
	}
}
