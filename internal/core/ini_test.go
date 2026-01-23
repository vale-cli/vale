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
