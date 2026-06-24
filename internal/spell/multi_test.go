package spell

import (
	"os"
	"path/filepath"
	"testing"
)

// readAsset must keep searching the remaining roots when an earlier root
// neither contains the file nor is a symlink. A regression made it return the
// Readlink error from the first root, so $DICPATH (the `system` root) was
// never consulted -- see #1014.
func TestReadAssetFallsThroughToLaterRoots(t *testing.T) {
	empty := t.TempDir()   // stands in for StylesPath/config/dictionaries
	dicpath := t.TempDir() // stands in for $DICPATH

	want := filepath.Join(dicpath, "en_GB.dic")
	if err := os.WriteFile(want, []byte("1\nword\n"), 0600); err != nil {
		t.Fatal(err)
	}

	m := &Checker{options: Options{path: empty, system: dicpath}}

	got, err := m.readAsset("en_GB.dic")
	if err != nil {
		t.Fatalf("readAsset: %v", err)
	}
	if got != want {
		t.Errorf("readAsset = %q, want %q", got, want)
	}
}

// A symlinked asset (how Nix typically exposes dictionaries via $DICPATH)
// must be found in its root. os.Stat follows the link, so readAsset returns
// the link path itself, which os.Open then follows to the target.
func TestReadAssetFindsSymlinkedAsset(t *testing.T) {
	target := filepath.Join(t.TempDir(), "real.dic")
	if err := os.WriteFile(target, []byte("1\nword\n"), 0600); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	link := filepath.Join(root, "en_GB.dic")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	m := &Checker{options: Options{system: root}}

	got, err := m.readAsset("en_GB.dic")
	if err != nil {
		t.Fatalf("readAsset: %v", err)
	}
	if got != link {
		t.Errorf("readAsset = %q, want %q", got, link)
	}
}
