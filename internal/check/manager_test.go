package check

import (
	"testing"
)

/*var checktests = []struct {
	check string
	msg   string
}{
	{"NoExtends.yml", "YAML.NoExtends: missing extension point!"},
	{"NoMsg.yml", "YAML.NoMsg: missing message!"},
}

func TestAddCheck(t *testing.T) {
	cfg, err := config.New()
	if err != nil {
		panic(err)
	}

	mgr := Manager{
		AllChecks: make(map[string]Check),
		Config:    cfg,
		Scopes:    make(map[string]struct{})}

	for _, tt := range checktests {
		path, err := filepath.Abs(filepath.Join("../fixtures/YAML", tt.check))
		if err != nil {
			panic(err)
		}
		s := mgr.loadCheck(tt.check, path)
		if s.Error() != tt.msg {
			t.Errorf("%q != %q", s.Error(), tt.msg)
		}
	}
}*/

// scopeBases feeds HasScope, which switches on paragraph splitting, sentence
// segmentation, and inline capture. A term that goes unrecorded leaves its
// rule silently matching nothing -- the failure #1133 reported.
func TestScopeBases(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"paragraph", []string{"paragraph"}},
		{"paragraph.md", []string{"paragraph"}},
		{"paragraph & ~heading", []string{"paragraph"}},
		{"sentence & ~blockquote & ~list", []string{"sentence"}},
		{"paragraph & sentence", []string{"paragraph", "sentence"}},
		{"sentence.heading & ~h2", []string{"sentence"}},

		// A negated term asks for a family's absence: nothing to build.
		{"~heading", []string{}},
		{"~blockquote & ~heading", []string{}},
	}

	for _, c := range cases {
		got := scopeBases(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("scopeBases(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("scopeBases(%q)[%d] = %q, want %q",
					c.in, i, got[i], c.want[i])
			}
		}
	}
}

var msgtests = []struct {
	in   string
	args []string
	out  string
}{
	{"Avoid using '%s'", []string{"foo", "bar"}, "Avoid using 'foo'"},
	{"Avoid using 'foo'", []string{"foo", "bar"}, "Avoid using 'foo'"},
	{"Use '%s', not '%s'", []string{"foo", "bar"}, "Use 'foo', not 'bar'"},
}

func TestFormatMessage(t *testing.T) {
	for _, tt := range msgtests {
		s, _ := formatMessages(tt.in, tt.in, tt.args...)
		if s != tt.out {
			t.Errorf("(%q, %v) => %q != %q", tt.in, tt.args, s, tt.out)
		}
	}
}

// RuleForAlert resolves an alert name to its defining rule by longest known
// prefix: a consistency alert carries a matched term, and a nested rule's own
// name spans subdirectories, so neither dot-count nor position can say where
// the rule ends.
func TestRuleForAlert(t *testing.T) {
	mgr := Manager{rules: map[string]Rule{
		"Std.OxfordComma":      Existence{},
		"Std.dates.TimeFormat": Existence{},
		"S.Consistency":        Existence{},
	}}

	cases := []struct{ in, want string }{
		{"Std.OxfordComma", "Std.OxfordComma"},
		{"Std.dates.TimeFormat", "Std.dates.TimeFormat"},
		{"S.Consistency.center", "S.Consistency"},
		{"Std.dates.TimeFormat.term", "Std.dates.TimeFormat"},
		{"Un.Known.deep.name", "Un.Known"},
		{"Un.Known", "Un.Known"},
	}
	for _, tt := range cases {
		if got := mgr.RuleForAlert(tt.in); got != tt.want {
			t.Errorf("RuleForAlert(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}
