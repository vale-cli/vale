package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A broken/looping symlink must surface an error that names the offending
// path, rather than a bare "EvalSymlinks: too many links". See #968.
func TestWalkSymlinkErrorNamesPath(t *testing.T) {
	root := t.TempDir()

	// A mutual symlink loop (a -> b -> a) makes EvalSymlinks fail with
	// "too many links", as in #968.
	if err := os.Symlink("b", filepath.Join(root, "a")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink("a", filepath.Join(root, "b")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	err := Walk(root, func(_ string, _ os.FileInfo, _ error) error { return nil })
	if err == nil {
		t.Fatal("expected an error walking a symlink loop")
	}
	// The message must name the offending path (one of the looping links),
	// not just "EvalSymlinks: too many links".
	if !strings.Contains(err.Error(), filepath.Join(root, "a")) &&
		!strings.Contains(err.Error(), filepath.Join(root, "b")) {
		t.Errorf("error should name the offending symlink path, got: %v", err)
	}
}
