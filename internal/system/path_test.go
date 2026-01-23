package system

import (
	"path/filepath"
	"testing"
)

func TestDeterminePath(t *testing.T) {
	tests := []struct {
		configPath string
		keyPath    string
		expected   string
	}{
		{
			configPath: "C:\\Source\\project\\.vale.ini",
			keyPath:    "transform.xsl",
			expected:   "C:\\Source\\project\\transform.xsl",
		},
		{
			configPath: "C:\\Source\\project\\.vale.ini",
			keyPath:    "styles/transform.xsl",
			expected:   "C:\\Source\\project\\styles\\transform.xsl",
		},
		{
			configPath: "C:\\Source\\project\\.vale.ini",
			keyPath:    "C:\\Other\\transform.xsl",
			expected:   "C:\\Other\\transform.xsl",
		},
	}

	for _, tt := range tests {
		actual := DeterminePath(tt.configPath, tt.keyPath)
		// Clean the paths to ensure separators match for comparison
		actual = filepath.Clean(actual)
		expected := filepath.Clean(tt.expected)
		if actual != expected {
			t.Errorf("DeterminePath(%q, %q) = %q; want %q", tt.configPath, tt.keyPath, actual, expected)
		}
	}
}
