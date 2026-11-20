package regex

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWithoutLookarounds(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{"none", `\bfoo\b`, `\bfoo\b`, false},
		{"lookbehind", `(?<=\bthe\s)foo`, `foo`, true},
		{"negative lookbehind", `(?<!\bthe\s)foo`, `foo`, true},
		{"lookahead", `foo(?=\sbar)`, `foo`, true},
		{"negative lookahead", `foo(?!\sbar)`, `foo`, true},
		{"both ends", `(?<=a)foo(?!b)`, `foo`, true},
		{"nested group inside", `(?<=(?:a|b)\s)foo`, `foo`, true},
		{"escaped paren inside", `(?<=\(x\))foo`, `foo`, true},
		{"keeps ordinary groups", `(?:a|b)foo`, `(?:a|b)foo`, false},
		{"keeps named group", `(?P<x>a)foo`, `(?P<x>a)foo`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := withoutLookarounds(tt.in)
			if got != tt.want || changed != tt.changed {
				t.Errorf("withoutLookarounds(%q) = (%q, %v), want (%q, %v)",
					tt.in, got, changed, tt.want, tt.changed)
			}
		})
	}
}

// The filter may only ever skip work. A literal it reports as required must be
// present in every string the pattern matches, or a real alert disappears.
func TestRequiredThroughLookaroundsStaysSound(t *testing.T) {
	cases := []struct {
		expr    string
		matches []string
	}{
		{`(?<=\bfascinated\s)(?:about|above)`, []string{"fascinated about", "fascinated above"}},
		{`\bself[-\s]+(?!harm\b)(?:censor|censors)`, []string{"self-censor", "self censors"}},
		{`(?i)\b(?:do|does)\s+(?=a lot)`, []string{"do a lot", "does a lot"}},
		{`foo(?!bar)`, []string{"foo", "foobaz"}},
	}

	for _, c := range cases {
		lits := Required(c.expr)
		if len(lits) == 0 {
			continue // no claim made, nothing to check
		}
		for _, subject := range c.matches {
			lowered := strings.ToLower(subject)
			ok := false
			for _, lit := range lits {
				if strings.Contains(lowered, lit) {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%q: filter %v rejects %q, which the pattern matches",
					c.expr, lits, subject)
			}
		}
	}
}

// Every test Requirements reports must hold for every string the pattern
// matches, and side-by-side parts must read as one literal.
func TestRequirements(t *testing.T) {
	cases := []struct {
		expr    string
		want    [][]string // nil to check soundness alone
		matches []string
	}{
		{`[Ii]t is`, [][]string{{"it is"}}, []string{"It is", "it is"}},
		{`\bThe \w+ (?:is|are) (?:thin|clean)`, [][]string{{"the "}, {"thin", "clean"}, {"is", "are"}},
			[]string{"The api is clean", "The walls are thin"}},
		{`(?i)\b(?:very|really)\b`, [][]string{{"very", "really"}}, []string{"VERY", "Really"}},
		{`[Ii]t'?s not just [^;,]+[;,] it'?s [^.!?]+`, nil, []string{"It's not just a; its b", "its not just a, it's b"}},
		{`a(?:bc|de)?f`, [][]string(nil), []string{"af", "abcf"}},
		{`[A-Z][a-z]+ are [^,]+, not [^.]+`, [][]string{{", not "}, {" are "}}, []string{"Dogs are loud, not quiet"}},
		{`colou?r`, [][]string{{"colo"}}, []string{"color", "colour"}},
		{`x|[a-z]+`, [][]string(nil), []string{"x", "word"}},
	}

	for _, c := range cases {
		tests := Requirements(c.expr)
		if c.want != nil || len(c.matches) == 0 {
			if len(tests) != len(c.want) {
				t.Errorf("Requirements(%q) = %v, want %v", c.expr, tests, c.want)
			}
		}
		for i := range c.want {
			if i < len(tests) && strings.Join(tests[i], "|") != strings.Join(c.want[i], "|") {
				t.Errorf("Requirements(%q)[%d] = %v, want %v", c.expr, i, tests[i], c.want[i])
			}
		}
		for _, subject := range c.matches {
			if !MustCompile(c.expr).MatchStringStd(subject) {
				t.Fatalf("%q does not match %q, so this proves nothing", c.expr, subject)
			}
			if !Holds(tests, strings.ToLower(subject)) {
				t.Errorf("%q: tests %v reject %q, which the pattern matches", c.expr, tests, subject)
			}
		}
	}
}

// Stripping lookarounds reads the pattern once: a generated rule runs to
// hundreds of kilobytes, and a rescan per byte took seconds on one.
func TestWithoutLookaroundsIsLinear(t *testing.T) {
	expr := strings.Repeat(`(?:alpha|beta)\s+`, 40_000) + `(?=gamma)delta`

	start := time.Now()
	got, changed := withoutLookarounds(expr)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v on a %d KB pattern", took, len(expr)/1024)
	}
	if !changed || !strings.HasSuffix(got, `\s+delta`) || strings.Contains(got, "gamma") {
		t.Errorf("the lookaround was not removed: ...%q", got[len(got)-24:])
	}
}

// A positive lookaround every match passes through adds its literals; one in
// a branch, behind a quantifier, or negated adds nothing.
func TestRequiredLookarounds(t *testing.T) {
	tests := []struct {
		expr string
		want []string
	}{
		{`(?<=\bimitat\w+\s+\S+\s+)(?:from)`, []string{`\bimitat\w+\s+\S+\s+`}},
		{`(?:(?<=after)x)y(?=before)`, []string{`after`, `before`}},
		{`foo(?=(?<=o)bar)`, []string{`(?<=o)bar`}},
		{`(?:(?<=abc)x|y)`, nil},
		{`(?<=abc)x|y`, nil},
		{`(?:(?<=abc)x)?y`, nil},
		{`(?:(?<=abc)x)*y`, nil},
		{`(?:(?<=abc)x){0,2}y`, nil},
		{`(?=abc)?y`, nil},
		{`(?<!abc)x(?!def)`, nil},
		{`[(?<=abc)]x`, nil},
		{`\(?<=abc\)x`, nil},
	}
	for _, tt := range tests {
		if got := requiredLookarounds(tt.expr); !slices.Equal(got, tt.want) {
			t.Errorf("requiredLookarounds(%q) = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// What the lookarounds add holds for every string the pattern matches.
func TestRequirementsThroughLookaroundsStaySound(t *testing.T) {
	cases := []struct {
		expr    string
		matches []string
	}{
		{`(?:(?<=\bimitat\w+\s+\S+\s+)(?:from))`, []string{"imitates them from", "imitating him from"}},
		{`(?:(?<=abc)x|y)`, []string{"y", "abcx"}},
		{`(?:(?<=abc)x)?y`, []string{"y", "abcxy"}},
		{`(?<!abc)x(?=de|fg)`, []string{"xde", "xfg"}},
	}
	for _, c := range cases {
		re := MustCompile(c.expr)
		for _, subject := range c.matches {
			if !re.MatchStringStd(subject) {
				t.Fatalf("%q does not match %q, so this proves nothing", c.expr, subject)
			}
			lowered := strings.ToLower(subject)
			for _, clause := range Requirements(c.expr) {
				if !slices.ContainsFunc(clause, func(l string) bool { return strings.Contains(lowered, l) }) {
					t.Errorf("%q: test %v rejects %q, which the pattern matches", c.expr, clause, subject)
				}
			}
		}
	}
}
