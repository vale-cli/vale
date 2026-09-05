package check

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

func TestExpandGroups(t *testing.T) {
	groups := []string{"alpha", "beta"}
	cases := []struct{ arg, want string }{
		{"plain", "plain"},
		{"$0", "whole"},
		{"$1-$2", "alpha-beta"},
		{"${2} then ${1}", "beta then alpha"},
		{"$3", ""},
		{"cost: $$5", "cost: $5"},
	}
	for _, tc := range cases {
		if got := expandGroups(tc.arg, "whole", groups); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.arg, got, tc.want)
		}
	}
}

func TestNewEditOperations(t *testing.T) {
	cases := []struct {
		match  string
		params []string
		want   string
	}{
		{"too   many  spaces", []string{"squeeze"}, "too many spaces"},
		{"élan vital", []string{"capitalize"}, "Élan vital"},
		{"Élan Vital", []string{"uncapitalize"}, "élan Vital"},
		{`He said "it's fine" -- really...`, []string{"smart"}, "He said “it’s fine” – really…"},
		{`'quoted' --- yes`, []string{"smart"}, "‘quoted’ — yes"},
		{"He said “it’s fine” – really…", []string{"dumb"}, `He said "it's fine" -- really...`},
		{"“Crème brûlée” — naïve", []string{"ascii"}, `"Creme brulee" --- naive`},
		{"Æsop, Straße, Øresund", []string{"ascii"}, "AEsop, Strasse, Oresund"},
		{"21", []string{"words"}, "twenty-one"},
		{"1,205", []string{"words"}, "one thousand two hundred five"},
		{"100", []string{"words"}, "one hundred"},
		{"3rd", []string{"words"}, "third"},
		{"22nd", []string{"words"}, "twenty-second"},
		{"40th", []string{"words"}, "fortieth"},
		{"112th", []string{"words"}, "one hundred twelfth"},
		{"-7", []string{"words"}, "minus seven"},
		{"lots", []string{"words"}, "lots"},
		{"twenty-one", []string{"digits"}, "21"},
		{"One Hundred and Five", []string{"digits"}, "105"},
		{"two thousand", []string{"digits"}, "2000"},
		{"one million two hundred thousand", []string{"digits"}, "1200000"},
		{"third", []string{"digits"}, "3rd"},
		{"twenty-second", []string{"digits"}, "22nd"},
		{"one hundred eleventh", []string{"digits"}, "111th"},
		{"minus seven", []string{"digits"}, "-7"},
		{"first light", []string{"digits"}, "first light"},
		{"several", []string{"digits"}, "several"},
	}
	for _, tc := range cases {
		got, err := FixAlert(core.Alert{Match: tc.match,
			Action: core.Action{Name: "edit", Params: tc.params}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{tc.want}) {
			t.Errorf("%v on %q: got %v, want %q", tc.params, tc.match, got, tc.want)
		}
	}
}

func TestNumberRoundTrip(t *testing.T) {
	for _, n := range []int64{0, 5, 13, 20, 47, 99, 100, 101, 999, 1000, 1001,
		12345, 1_000_000, 2_000_001, 1_000_000_000_000} {
		words := numberToWords(itoa(n))
		if back := wordsToNumber(words); back != itoa(n) {
			t.Errorf("%d -> %q -> %q", n, words, back)
		}
	}
}

func itoa(n int64) string {
	return wordsToNumber(cardinalWords(n))
}

func TestEditStepsUseGroups(t *testing.T) {
	alert := core.Alert{Match: "foo_bar", Groups: []string{"foo", "bar"}}

	cases := []struct {
		action core.Action
		want   string
	}{
		{core.Action{Name: "edit", Params: []string{"prefix", "$2 "}}, "bar foo_bar"},
		{core.Action{Name: "edit", Params: []string{"replace", "$1", "$2"}}, "bar_bar"},
		{core.Action{Name: "replace", Params: []string{"$2-$1"}}, "bar-foo"},
		// A regex step keeps `$1` for its own pattern.
		{core.Action{Name: "edit", Params: []string{"regex", `(\w+)_(\w+)`, "$2 $1"}}, "bar foo"},
	}
	for _, tc := range cases {
		alert.Action = tc.action
		got, err := FixAlert(alert, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{tc.want}) {
			t.Errorf("%v: got %v, want %q", tc.action, got, tc.want)
		}
	}
}

func TestExistenceGroups(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	rule, err := NewExistence(cfg, baseCheck{
		"extends": "existence",
		"message": "'%s' -> '%s'",
		"nonword": true,
		"action":  map[string]interface{}{"name": "replace", "params": []interface{}{"$2, $1"}},
		"tokens": []interface{}{
			`(\w+) (?P<last>\w+) Jr\.`, // unnamed then named
			`Dr\. (\w+)`,               // a second token with its own $1
			`Mx\. \w+`,                 // no groups at all
		},
	}, "T/Names.yml")
	if err != nil {
		t.Fatal(err)
	}

	file, err := core.NewFile("", cfg)
	if err != nil {
		t.Fatal(err)
	}

	alerts, err := rule.Run(nlp.NewBlock("", "Sammy Davis Jr., Dr. Who, Mx. Anyone", ""), file, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 3 {
		t.Fatalf("got %d alerts, want 3", len(alerts))
	}

	want := [][]string{{"Sammy", "Davis"}, {"Who"}, nil}
	wantFix := []string{"Davis, Sammy", ", Who", ", "}
	for i, a := range alerts {
		if !reflect.DeepEqual(a.Groups, want[i]) {
			t.Errorf("alert %d groups = %q, want %q", i, a.Groups, want[i])
		}
		if !reflect.DeepEqual(a.Suggestions, []string{wantFix[i]}) {
			t.Errorf("alert %d suggestions = %q, want %q", i, a.Suggestions, wantFix[i])
		}
	}
}

func TestGroupsSurviveFixRoundTrip(t *testing.T) {
	// An editor hands the alert back as JSON; the groups have to ride along
	// for the fix to come out the same.
	dir := t.TempDir()
	cfg := actionTestConfig(t, dir)

	alert := `{"Action":{"Name":"replace","Params":["$2 $1"]},"Match":"a b","Groups":["a","b"],"Check":"T.R"}`
	path := filepath.Join(dir, "alert.json")
	if err := os.WriteFile(path, []byte(alert), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ParseAlert(alert, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Suggestions, []string{"b a"}) {
		t.Errorf("got %v", got.Suggestions)
	}
}
