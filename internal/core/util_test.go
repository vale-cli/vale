package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/errata-ai/vale/v3/internal/system"
)

func TestFormatFromExt(t *testing.T) {
	extToFormat := map[string][]string{
		".py":    {".py", "code"},
		".cxx":   {".cpp", "code"},
		".mdown": {".md", "markup"},
	}
	m := map[string]string{}
	for ext, format := range extToFormat {
		normExt, f := FormatFromExt(ext, m)
		if format[0] != normExt {
			t.Errorf("expected = %v, got = %v", format[0], normExt)
		}
		if format[1] != f {
			t.Errorf("expected = %v, got = %v", format[1], f)
		}
	}
}

func TestPrepText(t *testing.T) {
	rawToPrepped := map[string]string{
		"foo\r\nbar":     "foo\nbar",
		"foo\r\n\r\nbar": "foo\n\nbar",
	}
	for raw, prepped := range rawToPrepped {
		if prepped != Sanitize(raw) {
			t.Errorf("expected = %v, got = %v", prepped, Sanitize(raw))
		}
	}
}

func TestPhrase(t *testing.T) {
	rawToPrepped := map[string]bool{
		"test suite":               true,
		"test[ ]?suite":            false,
		"Google":                   true,
		"write-good":               true,
		"https://vale.sh/explorer": false,
		"Google.zip":               false,
	}
	for input, output := range rawToPrepped {
		result := IsPhrase(input)
		if result != output {
			t.Errorf("expected = %v, got = %v", output, result)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Log("os.UserHomeDir failed, will not proceed with tests")
		return
	}
	stylesPathInput := filepath.FromSlash("~/.vale")
	expectedOutput := filepath.Join(homedir, ".vale")
	result := system.NormalizePath(stylesPathInput)
	if result != expectedOutput {
		t.Errorf("expected = %v, got = %v", expectedOutput, result)
	}
	stylesPathInput, err = os.MkdirTemp("", "vale_test")
	if err != nil {
		t.Log("os.MkdirTemp failed, will not proceed with tests")
		return
	}
	expectedOutput = stylesPathInput
	result = system.NormalizePath(stylesPathInput)
	if result != expectedOutput {
		t.Errorf("expected = %v, got = %v", expectedOutput, result)
	}
	stylesPathInput, err = os.MkdirTemp("", "vale~test")
	if err != nil {
		t.Log("os.MkdirTemp failed in second case, will not proceed with tests")
		return
	}
	expectedOutput = stylesPathInput
	result = system.NormalizePath(stylesPathInput)
	if result != expectedOutput {
		t.Errorf("expected = %v, got = %v", expectedOutput, result)
	}
}

func TestShouldIgnoreDirectory(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "empty directory name",
			path:     "",
			expected: false,
		},
		// Direct directory names
		{
			name:     "direct node_modules",
			path:     "node_modules",
			expected: true,
		},
		{
			name:     "direct .git",
			path:     ".git",
			expected: true,
		},
		// Nested paths with ignored directories
		{
			name:     "nested node_modules",
			path:     "plugins/foo/node_modules",
			expected: true,
		},
		{
			name:     "nested .git in worktree",
			path:     "worktree-a/.git",
			expected: true,
		},
		{
			name:     "deeply nested node_modules",
			path:     "project/src/components/node_modules",
			expected: true,
		},
		{
			name:     "node_modules in path with backslashes",
			path:     filepath.Join("project", "src", "node_modules"),
			expected: true,
		},
		// Non-ignored directories
		{
			name:     "regular directory",
			path:     "src",
			expected: false,
		},
		{
			name:     "nested regular directory",
			path:     "plugins/foo",
			expected: false,
		},
		{
			name:     "directory containing node_modules in name",
			path:     "my_node_modules_backup",
			expected: false,
		},
		{
			name:     "directory containing .git in name",
			path:     "my.github",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldIgnoreDirectory(tt.path)
			if result != tt.expected {
				t.Errorf("ShouldIgnoreDirectory(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}
