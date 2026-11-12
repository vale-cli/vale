package testsuite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func touch(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestPackageRoot finds the nearest directory with both a `.vale.ini` and a
// `styles` directory, the layout sync installs.
func TestPackageRoot(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Pkg/styles/Pkg/Sub", "Bare/Style")
	touch(t, filepath.Join(root, "Pkg", ".vale.ini"), "")
	touch(t, filepath.Join(root, "Bare", ".vale.ini"), "")

	if got := packageRoot(filepath.Join(root, "Pkg/styles/Pkg/Sub")); got != filepath.Join(root, "Pkg") {
		t.Errorf("packageRoot(nested) = %q, want the Pkg directory", got)
	}
	// A .vale.ini with no styles directory beside it is a project, not a package.
	if got := packageRoot(filepath.Join(root, "Bare/Style")); got == filepath.Join(root, "Bare") {
		t.Errorf("packageRoot(no styles) = %q, want no package root", got)
	}
}

// TestIsolatedSearch puts installed dependencies first and the rule's own
// package last, since the last path is where its dictionaries are read.
func TestIsolatedSearch(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Pkg/styles/Pkg", "deps/Base")
	touch(t, filepath.Join(root, "Pkg", ".vale.ini"), "Packages = Base\n")
	touch(t, filepath.Join(root, "deps", "Base", "Words.yml"), "extends: existence\n")

	var installed []string
	r := NewRunner(&core.CLIFlags{})
	r.pathsSet = true // no project configuration
	r.Install = func(pkgs []string, styles string) error {
		installed = append(installed, pkgs...)
		return os.MkdirAll(filepath.Join(styles, "Base"), 0o755)
	}
	defer r.Close()

	c := Case{Path: filepath.Join(root, "Pkg/styles/Pkg/Rule.yml"), Rule: "Rule.yml"}
	search, err := r.isolatedSearch(c)
	if err != nil {
		t.Fatal(err)
	}

	own := filepath.Join(root, "Pkg", "styles")
	if len(search) != 2 || search[1] != own {
		t.Fatalf("isolatedSearch = %q, want [<installed> %s]", search, own)
	}
	if len(installed) != 1 || installed[0] != "Base" {
		t.Errorf("installed %q, want [Base]", installed)
	}

	// A second case from the same package reuses the install.
	if _, err = r.isolatedSearch(c); err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 {
		t.Errorf("installed %d times, want once", len(installed))
	}
}

// TestIsolatedSearchSkipsInstalled leaves a dependency already on the search
// path alone, so a synced project never reaches the network.
func TestIsolatedSearchSkipsInstalled(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Pkg/styles/Pkg", "Pkg/styles/Base")
	touch(t, filepath.Join(root, "Pkg", ".vale.ini"), "Packages = Base\n")

	r := NewRunner(&core.CLIFlags{})
	r.pathsSet = true
	r.Install = func([]string, string) error {
		t.Error("Install called for a package already on the search path")
		return nil
	}
	defer r.Close()

	c := Case{Path: filepath.Join(root, "Pkg/styles/Pkg/Rule.yml"), Rule: "Rule.yml"}
	if _, err := r.isolatedSearch(c); err != nil {
		t.Fatal(err)
	}
}

// A failing sequence case carries the input as tagged, in the case's
// language; `lang` without `rule` is refused, since a project case has a
// language already.
func TestLangAndTags(t *testing.T) {
	if got := tagged(Case{Input: "The old house.\n"}); got != "The/DT old/JJ house/NN ./." {
		t.Errorf("tagged = %q", got)
	}

	r := NewRunner(&core.CLIFlags{})
	defer r.Close()
	res := r.Run(Case{Name: "x", Path: "x.test.yml", Lang: "de", Input: "Das Haus."})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "`lang` needs `rule`") {
		t.Errorf("lang without rule: err = %v", res.Err)
	}
}
