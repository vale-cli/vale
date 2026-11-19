package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// A pattern too large for the engine's backtracking stack fails every match
// that reaches it. The run reports the rule, where it used to panic.
func TestOversizedPatternNamesItsRule(t *testing.T) {
	words := make([]string, 16_000)
	for i := range words {
		words[i] = fmt.Sprintf("w%dx?y", i)
	}
	dir := t.TempDir()
	rule := "extends: existence\nmessage: \"'%s'\"\nlevel: error\nnonword: true\ntokens:\n  - '(?:" +
		strings.Join(words, "|") + ")'\n"
	if err := os.MkdirAll(filepath.Join(dir, "Big"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Big", "Huge.yml"), []byte(rule), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := core.LoadConfigFromString(
		"StylesPath = " + filepath.ToSlash(dir) + "\nMinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = Big\n")
	if err != nil {
		t.Fatal(err)
	}

	for _, floor := range []int{0, 1 << 20} { // both the concurrent and serial paths
		withFloor(t, floor)

		linter, lerr := NewLinter(cfg)
		if lerr != nil {
			t.Fatal(lerr)
		}
		_, err = linter.LintStringAs("Some words, then w5y and more words.\n", "input.md")
		if err == nil {
			t.Fatal("the lint succeeded, so the pattern is not oversized")
		}
		if !strings.Contains(err.Error(), "Big.Huge") || !strings.Contains(err.Error(), "backtracking") {
			t.Errorf("floor %d: the error does not name the rule and the cause: %v", floor, err)
		}
	}
}
