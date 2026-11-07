package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsRelease(t *testing.T) {
	for version, want := range map[string]bool{
		"v3.2.3":                               true,
		"v3.3.0-rc.1":                          true,
		"v3.2.4-0.20261106120000-5e77b40ab1c2": false,
		"v3.3.0-rc.1.0.20261106120000-5e77b40ab1c2": false,
		"v2.0.0+incompatible":                       false,
		"":                                          false,
	} {
		if got := isRelease(version); got != want {
			t.Errorf("isRelease(%q) = %t, want %t", version, got, want)
		}
	}
}

// VALE_MODELS_URL names a mirror or a local directory; a directory may hold
// archives or unpacked packages.
func TestModelSource(t *testing.T) {
	t.Setenv(modelsURLVar, "https://example.com/models/")
	if got, err := modelSource("de"); err != nil || got != "https://example.com/models/de.zip" {
		t.Errorf("modelSource = %q, %v", got, err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fr"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(modelsURLVar, dir)
	if got, _ := modelSource("de"); got != filepath.Join(dir, "de.zip") {
		t.Errorf("an archive in a directory: got %q", got)
	}
	if got, _ := modelSource("fr"); got != filepath.Join(dir, "fr") {
		t.Errorf("an unpacked package in a directory: got %q", got)
	}
}
