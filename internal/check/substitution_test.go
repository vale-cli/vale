package check

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

func makeSubstitution(def baseCheck) (*Substitution, error) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return nil, err
	}

	rule, err := NewSubstitution(cfg, def, "")
	if err != nil {
		return nil, err
	}

	return &rule, nil
}

func TestConvertGroups(t *testing.T) {
	converted, err := convertCaptureGroups("change in(?: )?to the (.*) directory")
	if err != nil {
		t.Fatal(err)
	}

	expected := "change in(?: )?to the (?:.*) directory"
	if converted != expected {
		t.Fatalf("Expected '%s', got '%s'", expected, converted)
	}
}

func TestIsDeterministic(t *testing.T) {
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Terms",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"swap": map[string]string{
			"emnify iot supernetwork": "emnify IoT SuperNetwork",
			"emnify":                  "emnify",
		},
	}

	text := "EMnify IoT SuperNetwork"
	for i := 0; i < 100; i++ {
		rule, err := makeSubstitution(swap)
		if err != nil {
			t.Fatal(err)
		}

		actual, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, &core.Config{})
		if err != nil {
			t.Fatal(err)
		}

		if len(actual) != 1 {
			t.Fatalf("expected 1 alert, found %d", len(actual))
		} else if actual[0].Match != "EMnify IoT SuperNetwork" {
			t.Fatalf("Loop %d: expected 'EMnify IoT SuperNetwork', found '%s'", i, actual[0].Match)
		}
	}
}

func TestRegex(t *testing.T) {
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Terms",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"swap": map[string]string{
			`(?:foo|bar)`: "sub",
		},
	}
	text := "foo"
	rule, err := makeSubstitution(swap)
	if err != nil {
		t.Fatal(err)
	}

	actual, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, &core.Config{})
	if err != nil {
		t.Fatal(err)
	}

	expected := "Use 'sub' instead of 'foo'."
	message := actual[0].Message
	if message != expected {
		t.Fatalf("Expected message `%s`, got `%s`", expected, message)
	}
}

func TestSharedLookaround(t *testing.T) {
	tests := []struct {
		name  string
		terms []string
		want  string
	}{
		{"hoisted", []string{`(?<=\b(?:have|has)\s)went\b`, `(?<=\b(?:have|has)\s)ate\b`},
			`(?<=\b(?:have|has)\s)`},
		{"differing assertions", []string{`(?<=have\s)went\b`, `(?<=has\s)ate\b`}, ""},
		{"one term", []string{`(?<=have\s)went\b`}, ""},
		{"no assertion", []string{`went\b`, `ate\b`}, ""},
		{"not leading", []string{`a(?=b)`, `a(?=c)`}, ""},
		{"capturing", []string{`(?<=(have)\s)went`, `(?<=(have)\s)ate`}, ""},
		{"escaped paren inside", []string{`(?<=\)\s)went`, `(?<=\)\s)ate`}, `(?<=\)\s)`},
	}

	for _, tt := range tests {
		if got := sharedLookaround(tt.terms); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Hoisting must not change which term matched, since the replacement is keyed
// on the group number.
func TestHoistKeepsReplacements(t *testing.T) {
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Tense",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"nonword":    true,
		"swap": map[string]string{
			`(?<=\b(?:have|has)\s)went\b`: "gone",
			`(?<=\b(?:have|has)\s)ate\b`:  "eaten",
		},
	}

	rule, err := makeSubstitution(swap)
	if err != nil {
		t.Fatal(err)
	}

	for text, want := range map[string]string{
		"they have went home": "Use 'gone' instead of 'went'.",
		"they have ate lunch": "Use 'eaten' instead of 'ate'.",
	} {
		alerts, rerr := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, &core.Config{})
		if rerr != nil {
			t.Fatal(rerr)
		}
		if len(alerts) != 1 {
			t.Fatalf("%q: got %d alerts, want 1", text, len(alerts))
		}
		if alerts[0].Message != want {
			t.Errorf("%q: got %q, want %q", text, alerts[0].Message, want)
		}
	}

	// The assertion still has to hold once hoisted.
	alerts, err := rule.Run(nlp.NewBlock("they went home", "they went home", "text"),
		&core.File{}, &core.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Fatalf("matched without the auxiliary: %+v", alerts)
	}
}

func TestRegexEscapedParens(t *testing.T) {
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Terms",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"swap": map[string]string{
			`(?!\()(?:foo|bar)(?!\))?`: "sub",
		},
	}
	text := "(foo)"
	rule, err := makeSubstitution(swap)
	if err != nil {
		t.Fatal(err)
	}

	actual, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, &core.Config{})
	if err != nil {
		t.Fatal(err)
	}

	expected := "Use 'sub' instead of 'foo'."
	message := actual[0].Message
	if message != expected {
		t.Fatalf("Expected message `%s`, got `%s`", expected, message)
	}
}

func TestRecaseRegexTerm(t *testing.T) {
	// A vocab term that's a regex should be shown re-cased to its canonical
	// form rather than as the raw pattern. See #997.
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Terms",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"swap": map[string]string{
			"oauth2?": "OAuth2?", // mirrors loadVocabRules: lower(term) -> term
		},
	}
	rule, err := makeSubstitution(swap)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct{ text, want string }{
		{"oauth2", "Use 'OAuth2' instead of 'oauth2'."},
		{"OAUTH2", "Use 'OAuth2' instead of 'OAUTH2'."},
	} {
		actual, rerr := rule.Run(nlp.NewBlock(tt.text, tt.text, "text"), &core.File{}, &core.Config{})
		if rerr != nil {
			t.Fatal(rerr)
		}
		if len(actual) != 1 || actual[0].Message != tt.want {
			t.Fatalf("%q: expected %q, got %v", tt.text, tt.want, actual)
		}
	}
}

func TestRecaseToTerm(t *testing.T) {
	cases := []struct {
		term, observed, want string
	}{
		{"OAuth2?", "oauth2", "OAuth2"},                  // optional char present
		{"OAuth2?", "Oauth", "OAuth"},                    // optional char absent -- see #997
		{"OpenAPI", "openapi", "OpenAPI"},                // plain literal
		{`Wi\-?Fi`, "wi-fi", "Wi-Fi"},                    // escaped literal hyphen
		{"Wi-?Fi", "wifi", "WiFi"},                       // optional char absent
		{"Docker(file|ize)", "dockerfile", "Dockerfile"}, // alternation -- see #997
		{"Docker(file|ize)", "DOCKERIZE", "Dockerize"},
		{"(?:foo|bar)", "foo", "foo"}, // non-capturing group

		// No spelling in the set matches, so the term stands.
		{"Docker(file|ize)", "docker", "Docker(file|ize)"},

		// Not a finite set of spellings: nothing to name.
		{"[Pp]ython", "python", "[Pp]ython"},
		{`Py.*\b`, "pythonic", `Py.*\b`},
		{"Go+gle", "google", "Go+gle"},
	}
	for _, c := range cases {
		if got := recaseToTerm(c.term, c.observed); got != c.want {
			t.Errorf("recaseToTerm(%q, %q) = %q, want %q", c.term, c.observed, got, c.want)
		}
	}
}

func TestExpandPattern(t *testing.T) {
	cases := []struct {
		pattern string
		want    []string
	}{
		{"OAuth", []string{"OAuth"}},
		{"OAuth2?", []string{"OAuth2", "OAuth"}},
		{"Docker(file|ize)", []string{"Dockerfile", "Dockerize"}},
		{"Docker(?:file|ize)", []string{"Dockerfile", "Dockerize"}},
		{"foo|bar", []string{"foo", "bar"}},
		{`Wi\-Fi`, []string{"Wi-Fi"}},
		{"a(b|c)d?", []string{"abd", "ab", "acd", "ac"}},

		// Optional group: the whole group drops out.
		{"Java(Script)?", []string{"JavaScript", "Java"}},

		// Unbounded or class-based -- no finite set of spellings.
		{"[Pp]ython", nil},
		{"Go+gle", nil},
		{"Py.*", nil},
		{`\d+`, nil},
		{"a{2,3}", nil},
		{"(unclosed", nil},
		{"unopened)", nil},
		{"(?=lookahead)", nil},
	}

	for _, c := range cases {
		got := expandPattern(c.pattern)
		if c.want == nil {
			if got != nil {
				t.Errorf("expandPattern(%q) = %v, want nil", c.pattern, got)
			}
			continue
		}
		if !slices.Equal(sorted(got), sorted(c.want)) {
			t.Errorf("expandPattern(%q) = %v, want %v", c.pattern, got, c.want)
		}
	}
}

// A blown budget has to return nil rather than a truncated set: a partial list
// would let recaseToTerm miss the spelling the writer actually used and report
// the raw pattern, which is the bug this exists to avoid.
func TestExpandPatternBudget(t *testing.T) {
	// 2^7 = 128 spellings, past maxExpansions.
	if got := expandPattern(strings.Repeat("(a|b)", 7)); got != nil {
		t.Errorf("expected nil past the budget, got %d spellings", len(got))
	}
	// 2^5 = 32, inside it.
	if got := expandPattern(strings.Repeat("(a|b)", 5)); len(got) != 32 {
		t.Errorf("expected 32 spellings, got %d", len(got))
	}
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

func TestOptions(t *testing.T) {
	cases := map[string][]string{
		"foo|bar":     {"foo", "bar"},
		"foo|bar|baz": {"foo", "bar", "baz"},
		"|foo|":       {"foo"},
		`\|foo\|`:     {"|foo|"},
		`\|foo\||bar`: {"|foo|", "bar"},
		"foo|bar|":    {"foo", "bar"},
		"foo|":        {"foo"},
		"|":           {},
		`\|`:          {"|"},

		// A suggestion is literal replacement text, so an option that happens
		// to contain the letters `PIPE` has to survive the split intact.
		"PIPELINE":         {"PIPELINE"},
		"PIPELINE|conduit": {"PIPELINE", "conduit"},
		`PIPE\|LINE`:       {"PIPE|LINE"},
	}

	for pattern, expected := range cases {
		actual := getOptions(pattern)
		if len(actual) != len(expected) {
			t.Fatalf("Expected %d options, got %v", len(expected), actual)
		}

		for i, opt := range expected {
			if actual[i] != opt {
				t.Fatalf("Expected '%s', got '%s'", opt, actual[i])
			}
		}
	}
}
