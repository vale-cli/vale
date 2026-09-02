package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

func managerWithStyles(t *testing.T, files map[string]string) (*Manager, string) {
	t.Helper()
	sp := t.TempDir()

	for path, body := range files {
		full := filepath.Join(sp, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddStylesPath(sp)

	return &Manager{Config: cfg, rules: map[string]Rule{}}, sp
}

const parentRule = `extends: existence
message: "parent: '%s'"
level: suggestion
ignorecase: true
tokens:
  - alpha
  - beta
tests:
  - name: parent case
    input: "alpha"
    contains: parent
`

func flattenChild(t *testing.T, mgr *Manager, sp, body string) (baseCheck, error) {
	t.Helper()
	path := filepath.Join(sp, "Child", "Rule.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	generic, err := parse([]byte(body), path)
	if err != nil {
		t.Fatal(err)
	}
	return mgr.flatten(generic, path, nil)
}

func TestFlattenOverlay(t *testing.T) {
	mgr, sp := managerWithStyles(t, map[string]string{"Base/Words.yml": parentRule})

	got, err := flattenChild(t, mgr, sp, `extends: Base.Words
message: "child: '%s'"
level: error
tokens+:
  - gamma
tokens-:
  - alpha
`)
	if err != nil {
		t.Fatal(err)
	}

	if got["extends"] != "existence" {
		t.Errorf("extends = %v; want the root extension point", got["extends"])
	}
	if got["message"] != "child: '%s'" || got["level"] != "error" {
		t.Errorf("child scalars did not win: %v", got)
	}
	if got["ignorecase"] != true {
		t.Error("un-overridden parent key was lost")
	}
	if _, has := got["tests"]; has {
		t.Error("a parent's cases must not be inherited")
	}

	tokens, _ := got["tokens"].([]interface{})
	if len(tokens) != 2 || tokens[0] != "beta" || tokens[1] != "gamma" {
		t.Errorf("tokens = %v; want [beta gamma]", tokens)
	}
}

func TestFlattenMapEdits(t *testing.T) {
	mgr, sp := managerWithStyles(t, map[string]string{"Base/Swaps.yml": `extends: substitution
message: "use '%s'"
swap:
  aa: one
  bb: two
`})

	got, err := flattenChild(t, mgr, sp, `extends: Base.Swaps
swap+:
  bb: TWO
  cc: three
swap-:
  - aa
`)
	if err != nil {
		t.Fatal(err)
	}

	swap, _ := got["swap"].(map[string]interface{})
	if len(swap) != 2 || swap["bb"] != "TWO" || swap["cc"] != "three" {
		t.Errorf("swap = %v; want map[bb:TWO cc:three]", swap)
	}
}

func TestFlattenErrors(t *testing.T) {
	cases := []struct{ name, body, wants string }{
		{"missing parent", "extends: Base.Missing\n", "does not exist on the search path"},
		{"builtin parent", "extends: Vale.Terms\n", "generated at runtime"},
		{"remove absent entry", "extends: Base.Words\ntokens-: [omega]\n", "no entry 'omega'"},
		{"replace and edit", "extends: Base.Words\ntokens: [x]\ntokens+: [y]\n", "pick one"},
		{"op on scalar", "extends: Base.Words\nmessage+: [x]\n", "assign it directly"},
		{"op without parent", "extends: existence\nmessage: x\ntokens+: [x]\n", "needs a parent"},
		{"cycle", "extends: Loop.A\n", "cycle"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mgr, sp := managerWithStyles(t, map[string]string{
				"Base/Words.yml": parentRule,
				"Loop/A.yml":     "extends: Loop.B\nmessage: x\n",
				"Loop/B.yml":     "extends: Loop.A\nmessage: x\n",
			})
			_, err := flattenChild(t, mgr, sp, tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.wants) {
				t.Fatalf("got %v; want %q", err, tt.wants)
			}
		})
	}
}

// A chain flattens top-down: the grandparent's machinery, each descendant's
// opinions in order.
func TestFlattenChain(t *testing.T) {
	mgr, sp := managerWithStyles(t, map[string]string{
		"Base/Words.yml": parentRule,
		"Mid/Words.yml":  "extends: Base.Words\nlevel: warning\ntokens+: [gamma]\n",
	})

	got, err := flattenChild(t, mgr, sp, `extends: Mid.Words
message: "leaf"
`)
	if err != nil {
		t.Fatal(err)
	}

	tokens, _ := got["tokens"].([]interface{})
	if got["extends"] != "existence" || got["level"] != "warning" ||
		got["message"] != "leaf" || len(tokens) != 3 {
		t.Errorf("chain flattened wrong: %v", got)
	}
}
