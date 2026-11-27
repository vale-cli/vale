package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// syncIfNeeded runs `vale sync --if-needed` against cfgPath and reports
// whether it reinstalled the local package: the rule it installs is marked
// beforehand, so a sync that ran overwrites the mark.
func syncIfNeeded(t *testing.T, cfgPath, stylesPath string) bool {
	t.Helper()

	rule := filepath.Join(stylesPath, "TestStyle", "Rule.yml")
	if err := os.WriteFile(rule, []byte("marked"), 0o600); err != nil {
		t.Fatalf("mark %s: %v", rule, err)
	}
	if err := sync(nil, &core.CLIFlags{Path: cfgPath, IgnoreGlobal: true, IfNeeded: true}); err != nil {
		t.Fatalf("sync --if-needed: %v", err)
	}
	b, err := os.ReadFile(rule)
	if err != nil {
		t.Fatalf("read %s: %v", rule, err)
	}
	return string(b) != "marked"
}

func firstSync(t *testing.T) (string, string) {
	t.Helper()

	stylesPath, cfgPath := setupLocalSyncTestPackage(t, t.TempDir())
	if err := sync(nil, &core.CLIFlags{Path: cfgPath, IgnoreGlobal: true, IfNeeded: true}); err != nil {
		t.Fatalf("first sync --if-needed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stylesPath, "TestStyle", "Rule.yml")); err != nil {
		t.Fatalf("first sync --if-needed installed nothing: %v", err)
	}
	return stylesPath, cfgPath
}

func TestSyncIfNeededSkipsUnchanged(t *testing.T) {
	stylesPath, cfgPath := firstSync(t)
	if syncIfNeeded(t, cfgPath, stylesPath) {
		t.Fatal("expected an unchanged configuration to skip the sync")
	}
}

func TestSyncIfNeededRunsWhenPackagesChange(t *testing.T) {
	stylesPath, cfgPath := firstSync(t)

	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	// The same package, named again: a change to `Packages` all the same.
	pkg := filepath.Join(filepath.Dir(cfgPath), "local-package")
	if _, err = f.WriteString("Packages = " + pkg + ", " + pkg + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if !syncIfNeeded(t, cfgPath, stylesPath) {
		t.Fatal("expected a change to Packages to sync")
	}
}

func TestSyncIfNeededRunsForNewVale(t *testing.T) {
	stylesPath, cfgPath := firstSync(t)

	old := version
	version = "99.0.0"
	t.Cleanup(func() { version = old })

	if !syncIfNeeded(t, cfgPath, stylesPath) {
		t.Fatal("expected a different Vale version to sync")
	}
}

func TestSyncIfNeededRunsWithoutStamp(t *testing.T) {
	stylesPath, cfgPath := firstSync(t)

	if err := os.Remove(filepath.Join(stylesPath, stampFile)); err != nil {
		t.Fatal(err)
	}
	if !syncIfNeeded(t, cfgPath, stylesPath) {
		t.Fatal("expected a missing stamp to sync")
	}
}

func TestSyncFailureClearsStamp(t *testing.T) {
	stylesPath, cfgPath := firstSync(t)

	// The package is gone, so the next sync fails partway.
	if err := os.RemoveAll(filepath.Join(filepath.Dir(cfgPath), "local-package")); err != nil {
		t.Fatal(err)
	}
	if err := sync(nil, &core.CLIFlags{Path: cfgPath, IgnoreGlobal: true}); err == nil {
		t.Fatal("expected sync of a missing package to fail")
	}
	if _, err := os.Stat(filepath.Join(stylesPath, stampFile)); !os.IsNotExist(err) {
		t.Fatalf("expected the failed sync to remove the stamp, got %v", err)
	}
}

func TestSyncRecordsInstalledStyles(t *testing.T) {
	stylesPath, _ := firstSync(t)

	b, err := os.ReadFile(filepath.Join(stylesPath, stampFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || lines[1] != "TestStyle" {
		t.Fatalf("expected the stamp to list TestStyle after its digest, got %q", lines)
	}
}
