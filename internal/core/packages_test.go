package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A package's `#sha256:` digest is part of it; a comment after a space
// still isn't. See #1236.
func TestGetPackagesKeepsADigest(t *testing.T) {
	d := strings.Repeat("ab", 32)
	ini := filepath.Join(t.TempDir(), ".vale.ini")
	err := os.WriteFile(ini, []byte("StylesPath = styles\nPackages = https://x.test/Pkg.zip#sha256:"+d+", Google # a comment\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GetPackages(ini)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://x.test/Pkg.zip#sha256:" + d, "Google"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}
