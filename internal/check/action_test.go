package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

const actionScript = `
suggestions := [prefix + "-" + match]
`

func writeActionScript(t *testing.T, path, prefix string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(path, []byte("prefix := \""+prefix+"\"\n"+actionScript), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func actionTestConfig(t *testing.T, stylesPath string) *core.Config {
	t.Helper()

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddStylesPath(stylesPath)

	return cfg
}

func actionTestAlert(scriptName string) core.Alert {
	return core.Alert{
		Action: core.Action{
			Name:   "suggest",
			Params: []string{scriptName},
		},
		Check: "RedHat.NoGerundsInTitles",
		Match: "Running",
	}
}

func TestSuggestActionFindsScriptInStyleDirectory(t *testing.T) {
	stylesPath := t.TempDir()
	scriptName := "NoGerundsInTitles.tengo"
	writeActionScript(t, filepath.Join(stylesPath, "RedHat", scriptName), "style")

	got, err := FixAlert(actionTestAlert(scriptName), actionTestConfig(t, stylesPath))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"style-Running"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestSuggestActionPrefersConfigActionsScript(t *testing.T) {
	stylesPath := t.TempDir()
	scriptName := "NoGerundsInTitles.tengo"
	writeActionScript(t, filepath.Join(stylesPath, core.ActionDir, scriptName), "config")
	writeActionScript(t, filepath.Join(stylesPath, "RedHat", scriptName), "style")

	got, err := FixAlert(actionTestAlert(scriptName), actionTestConfig(t, stylesPath))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"config-Running"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestSuggestActionRequiresStyleQualifiedCheckForFallback(t *testing.T) {
	stylesPath := t.TempDir()
	scriptName := "NoGerundsInTitles.tengo"
	writeActionScript(t, filepath.Join(stylesPath, "RedHat", scriptName), "style")

	alert := actionTestAlert(scriptName)
	alert.Check = "NoGerundsInTitles"

	_, err := FixAlert(alert, actionTestConfig(t, stylesPath))
	if err == nil {
		t.Fatal("expected missing script error")
	}
}

func TestCheckAction(t *testing.T) {
	stylesPath := t.TempDir()
	writeActionScript(t, filepath.Join(stylesPath, "RedHat", "Present.tengo"), "x")
	cfg := actionTestConfig(t, stylesPath)

	cases := []struct {
		name   string
		action core.Action
		want   string
	}{
		{"none", core.Action{}, ""},
		{"replace", core.Action{Name: "replace"}, ""},
		{"remove", core.Action{Name: "remove"}, ""},
		{"unknown", core.Action{Name: "nothing"}, "unknown action 'nothing'"},
		{"convert", core.Action{Name: "convert", Params: []string{"simple"}}, ""},
		{"convert bare", core.Action{Name: "convert"}, "'convert' takes one parameter: simple"},
		{"suggest script", core.Action{Name: "suggest", Params: []string{"Present.tengo"}}, ""},
		{"suggest spellings", core.Action{Name: "suggest", Params: []string{"spellings"}}, ""},
		{"suggest bare", core.Action{Name: "suggest"}, "'suggest' takes one parameter"},
		{"suggest missing", core.Action{Name: "suggest", Params: []string{"Absent.tengo"}}, "script 'Absent.tengo' not found"},
		{"edit trim", core.Action{Name: "edit", Params: []string{"trim_right", ".?!"}}, ""},
		{"edit bare", core.Action{Name: "edit"}, "'edit' needs an operation"},
		{"edit unknown", core.Action{Name: "edit", Params: []string{"strip", ".?!"}}, "unknown edit 'strip'"},
		{"edit arity", core.Action{Name: "edit", Params: []string{"regex", "a"}}, "'regex' takes 2 parameter(s)"},
		{"edit bad regex", core.Action{Name: "edit", Params: []string{"regex", "(", "x"}}, "error parsing regexp: missing closing ) in `(`"},
		{"edit bad index", core.Action{Name: "edit", Params: []string{"split", " ", "one"}}, "'split' index 'one' is not a number"},
		{"edit pipeline", core.Action{Name: "edit", Params: []string{"trim_right", "!", "lower", "wrap", "`"}}, ""},
		{"edit pipeline short", core.Action{Name: "edit", Params: []string{"lower", "wrap"}}, "'wrap' takes 1 parameter(s)"},
		{"edit pipeline unknown", core.Action{Name: "edit", Params: []string{"lower", "shout"}}, "unknown edit 'shout'"},
	}
	for _, tc := range cases {
		err := checkAction(cfg, Existence{Definition: Definition{
			Name: "RedHat.Rule", Action: tc.action}})
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSpellingAcceptsBareSuggest(t *testing.T) {
	rule := Spelling{Definition: Definition{Name: "T.S", Action: core.Action{Name: "suggest"}}}
	if err := checkAction(nil, rule); err != nil {
		t.Errorf("a spelling rule's bare suggest was refused: %v", err)
	}
}

func TestRuleLoadRejectsBadAction(t *testing.T) {
	mgr, sp := managerWithStyles(t, map[string]string{
		"T/Bad.yml": "extends: existence\nmessage: \"'%s' -> '%s'\"\n" +
			"action:\n  name: edit\n  params:\n    - strip\n    - '.?!'\ntokens:\n  - foo\n",
	})

	err := mgr.AddRuleFromFile("T.Bad", filepath.Join(sp, "T", "Bad.yml"))
	if err == nil {
		t.Fatal("expected a load error")
	}
	for _, want := range []string{"E201", "T/Bad.yml", "unknown edit 'strip'"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
}

func TestFixAlertGuards(t *testing.T) {
	cases := []struct {
		action core.Action
		want   string
	}{
		{core.Action{Name: "convert"}, "no parameters"},
		{core.Action{Name: "convert", Params: []string{"upper"}}, "unknown conversion 'upper'"},
		{core.Action{Name: "edit", Params: []string{"strip", "!"}}, "unknown edit 'strip'"},
		{core.Action{Name: "edit", Params: []string{"trim"}}, "'trim' takes 1 parameter(s)"},
	}
	for _, tc := range cases {
		_, err := FixAlert(core.Alert{Match: "x", Action: tc.action}, nil)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v: got %v, want %q", tc.action, err, tc.want)
		}
	}
}

func TestEditRegexUsesRuleEngine(t *testing.T) {
	got, err := FixAlert(core.Alert{Match: "qux", Action: core.Action{
		Name: "edit", Params: []string{"regex", "(?<=q)ux", "UX"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"qUX"}) {
		t.Errorf("got %v", got)
	}
}

func TestEditOperations(t *testing.T) {
	cases := []struct {
		match  string
		params []string
		want   string
	}{
		{"foo!", []string{"trim_right", "!"}, "foo"},
		{"  foo", []string{"trim_left", " "}, "foo"},
		{"*foo*", []string{"trim", "*"}, "foo"},
		{"the the", []string{"truncate", " "}, "the"}, //nolint:dupword
		{"a-b-c", []string{"split", "-", "2"}, "c"},
		{"snake_case", []string{"regex", `(\w+)_(\w+)`, "$1-$2"}, "snake-case"},
		{"endpoint's", []string{"replace", "'", ""}, "endpoints"},
		{"Overview.", []string{"remove", ".?!"}, "Overview"},
		{"a—b", []string{"remove", " "}, "a—b"},
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

func TestSuggestScriptCompilesOnce(t *testing.T) {
	stylesPath := t.TempDir()
	path := filepath.Join(stylesPath, core.ActionDir, "Once.tengo")
	writeActionScript(t, path, "one")
	cfg := actionTestConfig(t, stylesPath)

	for _, match := range []string{"Alpha", "Beta"} {
		alert := actionTestAlert("Once.tengo")
		alert.Match = match

		got, err := FixAlert(alert, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{"one-" + match}) {
			t.Errorf("got %v", got)
		}
	}

	if _, ok := scripts.Load(core.FindConfigAsset(cfg, "Once.tengo", core.ActionDir)); !ok {
		t.Error("script was not cached")
	}
}

func TestResolveFix(t *testing.T) {
	a := core.Alert{Check: "T.R", Match: "foo!",
		Action: core.Action{Name: "edit", Params: []string{"trim_right", "!"}}}
	if err := resolveFix(&a, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Suggestions, []string{"foo"}) {
		t.Errorf("got %v", a.Suggestions)
	}

	bare := core.Alert{Check: "T.R", Match: "foo"}
	if err := resolveFix(&bare, nil); err != nil || bare.Suggestions != nil {
		t.Errorf("an alert without an action was touched: %v %v", err, bare.Suggestions)
	}
}

func TestCapitalizationCarriesSuggestion(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	rule, err := NewCapitalization(cfg, baseCheck{
		"extends": "capitalization",
		"message": "'%s' should be '%s'.",
		"match":   "$title",
		"action":  map[string]interface{}{"name": "replace"},
	}, "T/Title.yml")
	if err != nil {
		t.Fatal(err)
	}

	alerts, err := rule.Run(nlp.NewBlock("", "a title here", ""), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want 1", len(alerts))
	}
	if !reflect.DeepEqual(alerts[0].Suggestions, []string{"A Title Here"}) {
		t.Errorf("got %v", alerts[0].Suggestions)
	}
}

func TestEditSteps(t *testing.T) {
	got, err := editSteps([]string{"trim_right", "!", "lower", "replace", "a", "b", "title"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"trim_right", "!"}, {"lower"}, {"replace", "a", "b"}, {"title"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// An argument that happens to name an operation is still an argument.
	got, err = editSteps([]string{"replace", "lower", "upper"})
	if err != nil || len(got) != 1 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestEditPipeline(t *testing.T) {
	cases := []struct {
		match  string
		params []string
		want   string
	}{
		{"Wow!", []string{"trim_right", "!", "lower"}, "wow"},
		{"foo", []string{"wrap", "`"}, "`foo`"},
		{"`foo`", []string{"unwrap", "`"}, "foo"},
		{"foo", []string{"prefix", "“", "suffix", "”"}, "“foo”"},
		{"the api", []string{"upper"}, "THE API"},
		{"THE API", []string{"lower"}, "the api"},
		{"a title here", []string{"title"}, "A Title Here"},
		{"A Title Here", []string{"sentence"}, "A title here"},
		{"FooBar", []string{"simple"}, "foo bar"},
		{"FooBar", []string{"kebab"}, "foo-bar"},
		{"FooBar", []string{"snake"}, "foo_bar"},
		{"foo bar", []string{"camel"}, "fooBar"},
		{"foo bar", []string{"pascal"}, "FooBar"},
		{"Section Title.", []string{"trim_right", ".", "sentence", "wrap", "*"}, "*Section title*"},
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

func TestNestedEditParamsFlatten(t *testing.T) {
	mgr, sp := managerWithStyles(t, map[string]string{
		"T/Steps.yml": "extends: existence\nmessage: \"'%s' -> '%s'\"\n" +
			"action:\n  name: edit\n  params:\n    - [trim_right, '!']\n    - [lower]\n" +
			"    - [wrap, '`']\ntokens:\n  - 'Wow!'\n",
	})

	path := filepath.Join(sp, "T", "Steps.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rule, _, err := mgr.compileCheck(content, "T.Steps", path)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"trim_right", "!", "lower", "wrap", "`"}
	got := rule.Fields().Action.Params
	if !reflect.DeepEqual(got, want) {
		t.Errorf("params = %v, want %v", got, want)
	}
}
