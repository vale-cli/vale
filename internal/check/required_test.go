package check

import (
	"strings"
	"testing"
)

func TestRequireFields(t *testing.T) {
	cases := []struct {
		extends string
		rule    baseCheck
		want    string // "" for a rule that has what it needs
	}{
		{"existence", baseCheck{}, "'existence' needs 'tokens' or 'raw'"},
		{"existence", baseCheck{"tokens": []interface{}{}}, "'existence' needs 'tokens' or 'raw'"},
		{"existence", baseCheck{"raw": []interface{}{"x"}}, ""},
		{"substitution", baseCheck{"swap": map[string]interface{}{}}, "'substitution' needs 'swap'"},
		{"substitution", baseCheck{"swap": map[string]interface{}{"a": "b"}}, ""},
		{"capitalization", baseCheck{"match": "  "}, "'capitalization' needs 'match'"},
		{"occurrence", baseCheck{"token": "x"}, "'occurrence' needs 'max' or 'min'"},
		{"occurrence", baseCheck{"token": "x", "max": 3}, ""},
		{"conditional", baseCheck{"first": "x"}, "'conditional' needs 'second'"},
		{"metric", baseCheck{"formula": "x"}, "'metric' needs 'condition'"},
		{"readability", baseCheck{"metrics": []interface{}{"SMOG"}, "grade": 0}, ""},
		{"spelling", baseCheck{}, ""},
	}
	for _, c := range cases {
		err := requireFields(c.extends, c.rule, "Rule.yml")
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s %v: unexpected error %v", c.extends, c.rule, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s %v: expected %q, got %v", c.extends, c.rule, c.want, err)
		}
	}
}
