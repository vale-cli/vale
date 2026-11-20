package regex

import (
	"reflect"
	"testing"
)

// A group that did not take part is -1, -1; an empty match at the start of
// the string is 0, 0. Both have index 0 and length 0 in regexp2, so the
// capture list is what tells them apart.
func TestFindAllStringSubmatchIndexEmptyMatch(t *testing.T) {
	cases := []struct {
		name, expr, s string
		want          [][]int
	}{
		{"empty at start", `\A(?!x)`, "ab", [][]int{{0, 0}}},
		{"empty everywhere", `(?!zzz)`, "ab", [][]int{{0, 0}, {1, 1}, {2, 2}}},
		{"empty at end", `\z`, "ab", [][]int{{2, 2}}},
		{"empty group at start", `(\A)b?`, "b", [][]int{{0, 1, 0, 0}}},
		{"absent group", `(a)|(b)`, "b", [][]int{{0, 1, -1, -1, 0, 1}}},
		{"absent group after a match", `(x)?b`, "b", [][]int{{0, 1, -1, -1}}},
		{"multibyte", `(?<=é)`, "éa", [][]int{{1, 1}}},
		{"no match", `zzz`, "ab", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MustCompile(c.expr).FindAllStringSubmatchIndex(c.s, -1)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("%q on %q = %v, want %v", c.expr, c.s, got, c.want)
			}
		})
	}
}

// A pattern's prefilter is worked out when it is first asked, not when the
// pattern is compiled: a rule that splits its tokens never asks the joined
// pattern, and reading it again cost a quarter of a large style's load.
func TestPrefilterIsDerivedOnFirstUse(t *testing.T) {
	re := MustCompile(`\bfoo(?:bar|baz)\b`)
	if re.tests.set != nil {
		t.Fatal("the prefilter was derived at compile time")
	}
	if re.MightMatch("nothing here") {
		t.Error("a subject without the literal passed")
	}
	if !re.MightMatch("a foobar here") {
		t.Error("a subject with the literal was ruled out")
	}
	if re.tests.set == nil {
		t.Error("the prefilter was not kept after its first use")
	}
	if !(&Regexp{}).MightMatch("anything") {
		t.Error("an unset pattern ruled a subject out")
	}
}
