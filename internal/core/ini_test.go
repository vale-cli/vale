package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func Test_processConfig_commentDelimiters(t *testing.T) {
	cases := []struct {
		description string
		body        string
		expected    map[string][2]string
	}{
		{
			description: "custom comment delimiters for markdown",
			body: `[*.md]
CommentDelimiters = "{/*,*/}"
`,
			expected: map[string][2]string{
				"*.md": {"{/*", "*/}"},
			},
		},
		{
			description: "not set",
			body: `[*.md]
TokenIgnores = (\$+[^\n$]+\$+)
`,
			expected: map[string][2]string{},
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			uCfg, err := shadowLoad([]byte(c.body))
			if err != nil {
				t.Fatal(err)
			}

			conf, err := NewConfig(&CLIFlags{})
			if err != nil {
				t.Fatal(err)
			}

			_, err = processConfig(uCfg, conf, false)
			if err != nil {
				t.Fatal(err)
			}

			actual := conf.CommentDelimiters
			for k, v := range c.expected {
				if actual[k] != v {
					t.Errorf("expected %v, but got %v", v, actual[k])
				}
			}
		})
	}
}

func Test_processConfig_commentDelimiters_error(t *testing.T) {
	cases := []struct {
		description string
		body        string
		expectedErr string
	}{
		{
			description: "global custom comment delimiters",
			body: `[*]
CommentDelimiters = "{/*,*/}"
`,
			expectedErr: "syntax-specific option",
		},
		{
			description: "more than two delimiters",
			body: `[*.md]
CommentDelimiters = "{/*,*/},<<,>>"
`,
			expectedErr: "CommentDelimiters must be a comma-separated list of two delimiters, but got 4 items",
		},
		{
			description: "more than two delimiters (shadow)",
			body: `[*.md]
CommentDelimiters = "{/*,*/}"

[*.md]
CommentDelimiters = "<<,>>"
`,
			expectedErr: "CommentDelimiters must be a comma-separated list of two delimiters, but got 4 items",
		},
		{
			description: "one delimiter is empty",
			body: `[*.md]
CommentDelimiters = "{/*"
`,
			expectedErr: "CommentDelimiters must be a comma-separated list of two delimiters, but got 1 items",
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			uCfg, err := shadowLoad([]byte(c.body))
			if err != nil {
				t.Fatal(err)
			}

			conf, err := NewConfig(&CLIFlags{})
			if err != nil {
				t.Fatal(err)
			}

			_, err = processConfig(uCfg, conf, false)
			if !strings.Contains(err.Error(), c.expectedErr) {
				t.Errorf("expected %v, but got %v", c.expectedErr, err.Error())
			}
		})
	}
}
func Test_processConfig_transform(t *testing.T) {
	body := `[*.xml]
Transform = transform.xsl
`
	uCfg, err := shadowLoad([]byte(body))
	if err != nil {
		t.Fatal(err)
	}

	conf, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	// Use a path that works on both Unix and Windows
	projectDir, _ := filepath.Abs(filepath.Join("Source", "project"))
	cfgFile := filepath.Join(projectDir, ".vale.ini")
	conf.AddConfigFile(cfgFile)

	_, err = processConfig(uCfg, conf, false)
	if err != nil {
		t.Fatal(err)
	}

	actual := conf.Stylesheets["*.xml"]

	// Logic: DeterminePath joins relative 'transform.xsl' with the config dir
	expected := filepath.Join(projectDir, "transform.xsl")

	if actual != expected {
		t.Errorf("expected %v, but got %v", expected, actual)
	}
}

func Test_processConfig_transform_abs(t *testing.T) {
	// 1. Get a clean absolute path for the current OS
	absPath, _ := filepath.Abs("transform.xsl")

	// 2. Use a raw string format.
	body := fmt.Sprintf(`[*.xml]
Transform = %s
`, absPath)

	uCfg, err := shadowLoad([]byte(body))
	if err != nil {
		t.Fatal(err)
	}

	conf, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Ensure the project directory is also absolute/clean
	projectDir, _ := filepath.Abs(filepath.Join("Source", "project"))
	conf.AddConfigFile(filepath.Join(projectDir, ".vale.ini"))

	_, err = processConfig(uCfg, conf, false)
	if err != nil {
		t.Fatal(err)
	}

	actual := conf.Stylesheets["*.xml"]

	// 4. Normalize both paths before comparison to account for
	// any trailing slashes or separator inconsistencies.
	if filepath.Clean(actual) != filepath.Clean(absPath) {
		t.Errorf("expected %v, but got %v", absPath, actual)
	}
}

// Test_shadowLoad_sectionsDoNotInherit covers a config split across two
// sources, which is the ordinary case: a global .vale.ini plus the project's.
//
// Sections here are glob patterns, and nearly all of them contain a dot. The
// ini library reads a dot as section nesting, so a key lookup that missed in
// `[*.txt]` used to fall back to `[*]` and return its key. Merging the second
// source then wrote one section's styles into the other, and a style scoped to
// `.txt` applied to every file. See #1129.
func Test_shadowLoad_sectionsDoNotInherit(t *testing.T) {
	global := []byte("MinAlertLevel = suggestion\n")
	local := []byte("[*]\nBasedOnStyles = A\n\n[*.txt]\nBasedOnStyles = B\n")

	uCfg, err := shadowLoad(global, local)
	if err != nil {
		t.Fatal(err)
	}

	conf, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = processConfig(uCfg, conf, false); err != nil {
		t.Fatal(err)
	}

	if got := conf.GBaseStyles; len(got) != 1 || got[0] != "A" {
		t.Errorf("[*] should hold only its own styles; got %v", got)
	}
	if got := conf.SBaseStyles["*.txt"]; len(got) != 1 || got[0] != "B" {
		t.Errorf("[*.txt] should hold only its own styles; got %v", got)
	}
}

// A level set under a section belongs to that section. Collecting them all in
// one map let the last section in the file decide the level everywhere. See
// #965.
func Test_processConfig_sectionLevels(t *testing.T) {
	cases := []struct {
		description string
		body        string
		levels      map[string]map[string]string
		global      map[string]string
	}{
		{
			description: "one section overrides a shared style",
			body: `[*.{html,md}]
BasedOnStyles = Vale

[*.md]
Vale.Spelling = warning
`,
			levels: map[string]map[string]string{
				"*.{html,md}": {},
				"*.md":        {"Vale.Spelling": "warning"},
			},
			global: map[string]string{},
		},
		{
			description: "each section keeps its own level",
			body: `[*.html]
Vale.Spelling = error

[*.md]
Vale.Spelling = warning
`,
			levels: map[string]map[string]string{
				"*.html": {"Vale.Spelling": "error"},
				"*.md":   {"Vale.Spelling": "warning"},
			},
			global: map[string]string{},
		},
		{
			description: "a level under [*] stays global",
			body: `[*]
Vale.Spelling = suggestion

[*.md]
BasedOnStyles = Vale
`,
			levels: map[string]map[string]string{
				"*.md": {},
			},
			global: map[string]string{"Vale.Spelling": "suggestion"},
		},
		{
			description: "YES and NO carry no level",
			body: `[*.md]
Vale.Spelling = YES
Vale.Repetition = NO
`,
			levels: map[string]map[string]string{
				"*.md": {},
			},
			global: map[string]string{},
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			uCfg, err := shadowLoad([]byte(c.body))
			if err != nil {
				t.Fatal(err)
			}

			conf, err := NewConfig(&CLIFlags{})
			if err != nil {
				t.Fatal(err)
			}

			if _, err = processConfig(uCfg, conf, false); err != nil {
				t.Fatal(err)
			}

			for sec, want := range c.levels {
				got := conf.SLevels[sec]
				if len(got) != len(want) {
					t.Fatalf("SLevels[%q] = %v, want %v", sec, got, want)
				}
				for k, v := range want {
					if got[k] != v {
						t.Errorf("SLevels[%q][%q] = %q, want %q", sec, k, got[k], v)
					}
				}
			}

			if len(conf.RuleToLevel) != len(c.global) {
				t.Fatalf("RuleToLevel = %v, want %v", conf.RuleToLevel, c.global)
			}
			for k, v := range c.global {
				if conf.RuleToLevel[k] != v {
					t.Errorf("RuleToLevel[%q] = %q, want %q", k, conf.RuleToLevel[k], v)
				}
			}
		})
	}
}

// A file reports a rule at the level its own sections gave it, falling back to
// the level the rule was compiled with.
func TestFileLevel(t *testing.T) {
	f := File{Levels: map[string]string{
		"Vale.Spelling": "warning",
		"proselint":     "suggestion",
	}}

	tests := []struct {
		name     string
		rule     string
		compiled string
		want     string
	}{
		{"the rule itself", "Vale.Spelling", "error", "warning"},
		{"its style", "proselint.Typography", "error", "suggestion"},
		{"neither", "Microsoft.Wordiness", "error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Level(tt.rule, tt.compiled); got != tt.want {
				t.Errorf("Level(%q, %q) = %q, want %q",
					tt.rule, tt.compiled, got, tt.want)
			}
		})
	}
}

// `Style.Rule[param]` is the php.ini-style parameter key; anything else is
// left for the level/toggle path, and structural fields are refused with a
// pointer at inheritance.
func TestAsRuleParam(t *testing.T) {
	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		key, val string
		isParam  bool
	}{
		{"Std.SentenceLength[max]", "30", true},
		{"Std.dates.TimeFormat[ignorecase]", "true", true},
		{"Std.SentenceLength", "error", false},
		{"BasedOnStyles", "Std", false},
		{"NoDot[max]", "1", false},
	} {
		got, pErr := asRuleParam(tt.key, tt.val, cfg)
		if pErr != nil {
			t.Fatalf("%s: %v", tt.key, pErr)
		}
		if got != tt.isParam {
			t.Errorf("asRuleParam(%q) = %v; want %v", tt.key, got, tt.isParam)
		}
	}

	if cfg.RuleToParams["Std.SentenceLength"]["max"] != "30" {
		t.Errorf("param not stored: %v", cfg.RuleToParams)
	}

	if _, pErr := asRuleParam("Std.Passive[tokens]", "x", cfg); pErr == nil ||
		!strings.Contains(pErr.Error(), "extend") {
		t.Errorf("structural param: got %v; want the extend guidance", pErr)
	}

	// One spelling per setting: the classic key owns levels.
	if _, pErr := asRuleParam("S.R[level]", "error", cfg); pErr == nil ||
		!strings.Contains(pErr.Error(), "S.R = error") {
		t.Errorf("bracketed level: got %v; want a pointer at the classic key", pErr)
	}
}

// A later *configuration* wins -- package fragments and the local ini load
// sequentially, each calling this once per key. Within one file the ini
// library collapses duplicate keys to the first before this layer runs.
func TestRuleParamLastWins(t *testing.T) {
	cfg, _ := NewConfig(&CLIFlags{})
	_, _ = asRuleParam("S.R[max]", "10", cfg)
	_, _ = asRuleParam("S.R[max]", "20", cfg)
	if cfg.RuleToParams["S.R"]["max"] != "20" {
		t.Errorf("got %v; want the later value", cfg.RuleToParams["S.R"])
	}
}

// An escaped comma stays inside its pattern; a bare one still separates
// patterns. See #1164.
func Test_processConfig_ignoresKeepEscapedCommas(t *testing.T) {
	body := `[*]
TokenIgnores = \b[a-z]{2\,}\b, (\$+[^\n$]+\$+)
BlockIgnores = BEGIN.{2\,}END
TokenIgnores = \\, \d{1\,3}

[*.md]
TokenIgnores = \b[a-z]{2\,}\b
BlockIgnores = BEGIN.{2\,}END, (?s)<!--.*?-->
`
	uCfg, err := shadowLoad([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	conf, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = processConfig(uCfg, conf, false); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"global TokenIgnores", conf.TokenIgnores["*"], []string{
			`\b[a-z]{2,}\b`, `(\$+[^\n$]+\$+)`, `\\`, `\d{1,3}`,
		}},
		{"global BlockIgnores", conf.BlockIgnores["*"], []string{`BEGIN.{2,}END`}},
		{"section TokenIgnores", conf.TokenIgnores["*.md"], []string{`\b[a-z]{2,}\b`}},
		{"section BlockIgnores", conf.BlockIgnores["*.md"], []string{
			`BEGIN.{2,}END`, `(?s)<!--.*?-->`,
		}},
	}
	for _, c := range cases {
		if fmt.Sprint(c.got) != fmt.Sprint(c.want) {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

// A package's configuration is read before the project's, and a key both set
// takes the project's value: the later source wins, for a level, a parameter,
// and a switch alike.
func Test_processConfig_laterSourceWins(t *testing.T) {
	pkg := []byte("[*]\nS.G = error\n\n[*.md]\nS.R = error\nS.R[max] = 10\n")
	local := []byte("[*]\nS.G = NO\n\n[*.md]\nS.R = suggestion\nS.R[max] = 20\n")

	uCfg, err := shadowLoad(pkg, local)
	if err != nil {
		t.Fatal(err)
	}

	conf, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = processConfig(uCfg, conf, false); err != nil {
		t.Fatal(err)
	}

	if got := conf.SLevels["*.md"]["S.R"]; got != "suggestion" {
		t.Errorf("level = %q, want the project's", got)
	}
	if got := conf.RuleToParams["S.R"]["max"]; got != "20" {
		t.Errorf("param = %q, want the project's", got)
	}
	if conf.GChecks["S.G"] {
		t.Error("S.G is on; the project switched it off")
	}
}
