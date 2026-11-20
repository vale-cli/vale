package regex

import (
	"regexp/syntax"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// required finds literals a subject must contain for the pattern to match.
//
// Running a rule's regular expression is far more expensive than searching for
// a word, and almost every rule is asked about text it cannot match: linting an
// 82 KB file with a 545-rule style ran the engine 1.2 million times to produce
// 274 alerts, a hit rate of 0.022%. A cheap test that rules a pattern out
// before the engine sees it removes nearly all of that.
//
// The result is a disjunction: the subject must contain at least one of the
// returned literals. An empty result means no useful test could be derived and
// the pattern has to be run.
//
// Correctness rests on only ever returning literals that are genuinely
// required. Anything uncertain -- a pattern that will not parse, an optional
// branch, a character class -- yields nothing, so the pattern runs as before. A
// prefilter can then only skip work, never change a result.
// lookaroundAt reports whether a lookahead or lookbehind group opens at i:
// `(?=`, `(?!`, `(?<=`, or `(?<!`.
func lookaroundAt(expr string, i int) bool {
	rest := expr[i:]
	if !strings.HasPrefix(rest, "(?") || len(rest) < 3 {
		return false
	}
	c := rest[2]
	if c == '<' && len(rest) > 3 {
		c = rest[3]
	}
	return c == '=' || c == '!'
}

// withoutLookarounds removes every lookahead and lookbehind from expr,
// reporting whether it removed any.
//
// The group is found by counting parentheses rather than by pattern, so that a
// nested one is removed with its parent, and an escaped parenthesis inside it
// is not mistaken for structure.
func withoutLookarounds(expr string) (string, bool) {
	var out []byte
	changed := false

	for i := 0; i < len(expr); {
		if lookaroundAt(expr, i) {
			depth, j := 0, i
			for ; j < len(expr); j++ {
				switch expr[j] {
				case '\\':
					j++
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth == 0 && expr[j] == ')' {
					j++
					break
				}
			}
			i = j
			changed = true

			continue
		}
		out = append(out, expr[i])
		i++
	}

	return string(out), changed
}

// Required returns the strongest single test Requirements finds: the subject
// must contain at least one of the returned literals.
func Required(expr string) []string {
	var best []string
	for _, clause := range Requirements(expr) {
		if Weight(clause) > Weight(best) {
			best = clause
		}
	}
	return best
}

// Requirements finds what a subject must contain for the pattern to match,
// as a list of tests that all have to pass. Each test is a set of literals,
// lower-cased, one of which must be present. No tests means the pattern has
// to be run.
func Requirements(expr string) [][]string {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		// regexp2 accepts lookarounds and backreferences; regexp/syntax does
		// not. Dropping the lookarounds gives a pattern this can read, and one
		// that matches at least everything the original does: a lookaround
		// constrains a match without contributing to it, so removing it can
		// only widen the language. A literal the wider pattern requires is
		// therefore required by the original too, which is what keeps this
		// sound -- half of a real style's rules reach here.
		stripped, changed := withoutLookarounds(expr)
		if !changed {
			return nil
		}
		re, err = syntax.Parse(stripped, syntax.Perl)
		if err != nil {
			re = nil
		}
	}

	var out [][]string
	if re != nil {
		for _, clause := range clauses(re.Simplify()) {
			if clause = usable(clause); clause != nil {
				out = append(out, clause)
			}
		}
	}
	// What a positive lookaround has to find is in the subject too, wherever
	// every match passes through it.
	for _, body := range requiredLookarounds(expr) {
		out = append(out, Requirements(body)...)
	}
	if len(out) == 0 {
		return nil
	}
	// The most selective test first, so a miss is found early.
	sort.SliceStable(out, func(i, j int) bool { return Weight(out[i]) > Weight(out[j]) })
	if len(out) > maxClauses {
		out = out[:maxClauses]
	}
	return out
}

// requiredLookarounds returns the bodies of the positive lookaheads and
// lookbehinds every match of expr has to pass: those outside any alternation
// and any optional group. A lookaround must match for the pattern to, so what
// it requires is in the subject; one that a branch or a quantifier can skip
// promises nothing.
func requiredLookarounds(expr string) []string {
	type frame struct {
		alt    bool
		bodies []string
	}
	stack := []*frame{{}}
	optional := func(rest string) bool {
		return strings.HasPrefix(rest, "?") || strings.HasPrefix(rest, "*") ||
			strings.HasPrefix(rest, "{0") || strings.HasPrefix(rest, "{,")
	}

	for i := 0; i < len(expr); i++ {
		switch c := expr[i]; {
		case c == '\\':
			i++
		case c == '[':
			i = classEnd(expr, i)
		case c == '(' && lookaroundAt(expr, i):
			end := groupEnd(expr, i)
			if end < 0 {
				return nil
			}
			start := i + 3
			if expr[i+2] == '<' {
				start++
			}
			if expr[start-1] == '=' && !optional(expr[end+1:]) {
				top := stack[len(stack)-1]
				top.bodies = append(top.bodies, expr[start:end])
			}
			i = end
		case c == '(':
			stack = append(stack, &frame{})
		case c == '|':
			stack[len(stack)-1].alt = true
		case c == ')':
			if len(stack) < 2 {
				return nil
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !f.alt && !optional(expr[i+1:]) {
				parent := stack[len(stack)-1]
				parent.bodies = append(parent.bodies, f.bodies...)
			}
		}
	}
	if len(stack) != 1 || stack[0].alt {
		return nil
	}
	return stack[0].bodies
}

// groupEnd returns the index of the parenthesis that closes the group
// opening at i, or -1.
func groupEnd(expr string, i int) int {
	depth := 0
	for j := i; j < len(expr); j++ {
		switch expr[j] {
		case '\\':
			j++
		case '[':
			j = classEnd(expr, j)
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// classEnd returns the index of the bracket closing the character class that
// opens at i, or the end of expr.
func classEnd(expr string, i int) int {
	j := i + 1
	if j < len(expr) && expr[j] == '^' {
		j++
	}
	if j < len(expr) && expr[j] == ']' {
		j++
	}
	for ; j < len(expr); j++ {
		switch expr[j] {
		case '\\':
			j++
		case ']':
			return j
		}
	}
	return len(expr) - 1
}

// maxClauses bounds the tests kept for one pattern: past the first few, a
// test rarely rules out what the others let through.
const maxClauses = 4

// usable lower-cases a clause and drops repeats. A clause holding a single
// character rules almost nothing out and costs a scan of the subject to
// discover it, so it yields nothing.
func usable(clause []string) []string {
	out := make([]string, 0, len(clause))
	for _, l := range clause {
		if len(l) < 2 {
			return nil
		}
		if l = strings.ToLower(l); !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// oneRune reports the rune a node always matches, compared lower-cased: a
// literal of one character, or a class of one letter's cases, such as `[Ii]`.
func oneRune(re *syntax.Regexp) (rune, bool) {
	if re.Op == syntax.OpLiteral && len(re.Rune) == 1 {
		return unicode.ToLower(re.Rune[0]), true
	}
	if re.Op != syntax.OpCharClass {
		return 0, false
	}

	var r rune
	n := 0
	for i := 0; i+1 < len(re.Rune); i += 2 {
		lo, hi := re.Rune[i], re.Rune[i+1]
		if hi-lo > 3 {
			return 0, false
		}
		for c := lo; c <= hi; c++ {
			if l := unicode.ToLower(c); n == 0 {
				r = l
			} else if l != r {
				return 0, false
			}
			n++
		}
	}
	return r, n > 0
}

// clauses returns tests that all hold for any match of re.
func clauses(re *syntax.Regexp) [][]string {
	switch re.Op {
	case syntax.OpLiteral:
		// Literals are compared lower-case, which is what the subject is
		// lower-cased for.
		return [][]string{{strings.ToLower(string(re.Rune))}}

	case syntax.OpCapture:
		return clauses(re.Sub[0])

	case syntax.OpConcat:
		// Every part must match, so each part's tests are the whole
		// expression's. Parts that are fixed text and sit side by side make
		// one literal: `[Ii]t is` requires "it is", not "t is".
		var out [][]string
		var run []rune
		flush := func() {
			if len(run) > 0 {
				out = append(out, []string{string(run)})
				run = nil
			}
		}
		for _, sub := range re.Sub {
			if sub.Op == syntax.OpLiteral {
				run = append(run, []rune(strings.ToLower(string(sub.Rune)))...)
				continue
			}
			if r, ok := oneRune(sub); ok {
				run = append(run, r)
				continue
			}
			flush()
			out = append(out, clauses(sub)...)
		}
		flush()
		return out

	case syntax.OpAlternate:
		// One branch matches, so a test holds only if every branch supplies
		// one; the subject must contain some member of the union.
		var all []string
		for _, sub := range re.Sub {
			var best []string
			for _, clause := range clauses(sub) {
				if Weight(clause) > Weight(best) {
					best = clause
				}
			}
			if len(best) == 0 {
				return nil
			}
			all = append(all, best...)
		}
		return [][]string{all}

	case syntax.OpPlus, syntax.OpRepeat:
		// One repetition is mandatory when Min > 0; OpPlus always is.
		if re.Op == syntax.OpPlus || re.Min > 0 {
			return clauses(re.Sub[0])
		}
		return nil

	case syntax.OpCharClass:
		if r, ok := oneRune(re); ok {
			return [][]string{{string(r)}}
		}
		// A set of alternatives, not a string to search for. Enumerating a
		// class would produce a filter slower than the match it replaces.
		return nil

	case syntax.OpStar, syntax.OpQuest:
		// Zero repetitions satisfy these, so nothing is guaranteed.
		return nil

	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return nil

	case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText,
		syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary,
		syntax.OpEmptyMatch:
		// Assertions match a position rather than any text.
		return nil

	case syntax.OpNoMatch:
		// Matches nothing, so the rule can never fire. Returning no test
		// leaves it to the engine rather than making this the one place that
		// decides a rule is dead.
		return nil
	}

	return nil
}

// weight scores a literal set by its weakest member, since the subject only has
// to contain one of them.
func Weight(lits []string) int {
	if len(lits) == 0 {
		return 0
	}
	shortest := len(lits[0])
	for _, l := range lits[1:] {
		if len(l) < shortest {
			shortest = len(l)
		}
	}
	return shortest
}
