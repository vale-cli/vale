package check

import (
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
		{"OAuth2?", "oauth2", "OAuth2"},       // optional char present
		{"OpenAPI", "openapi", "OpenAPI"},     // plain literal
		{`Wi\-?Fi`, "wi-fi", "Wi-Fi"},         // escaped literal hyphen, aligned
		{"Wi-?Fi", "wifi", "Wi-?Fi"},          // optional char absent -> fall back
		{"[Pp]ython", "python", "[Pp]ython"},  // class -> fall back
		{"(?:foo|bar)", "foo", "(?:foo|bar)"}, // group/alternation -> fall back
	}
	for _, c := range cases {
		if got := recaseToTerm(c.term, c.observed); got != c.want {
			t.Errorf("recaseToTerm(%q, %q) = %q, want %q", c.term, c.observed, got, c.want)
		}
	}
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
