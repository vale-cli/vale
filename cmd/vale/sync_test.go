package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/system"
)

func setupLocalSyncTestPackage(t *testing.T, root string) (string, string) {
	t.Helper()

	stylesPath := filepath.Join(root, "missing-styles")
	cfgPath := filepath.Join(root, ".vale.ini")
	pkgRoot := filepath.Join(root, "local-package")
	pkgStyles := filepath.Join(pkgRoot, "styles", "TestStyle")

	if err := os.MkdirAll(pkgStyles, os.ModePerm); err != nil {
		t.Fatalf("Failed to create local package styles directory %q: %v", pkgStyles, err)
	}

	rulePath := filepath.Join(pkgStyles, "Rule.yml")
	if err := os.WriteFile(rulePath, []byte("extends: existence\nmessage: test\nlevel: warning\n"), 0o600); err != nil {
		t.Fatalf("Failed to write local package rule file %q: %v", rulePath, err)
	}

	body := []byte("StylesPath = missing-styles\nPackages = " + pkgRoot + "\n")
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		t.Fatalf("Failed to write test config file %q: %v", cfgPath, err)
	}

	return stylesPath, cfgPath
}

func TestSyncCreatesConfiguredMissingStylesPath(t *testing.T) {
	root := t.TempDir()
	stylesPath, cfgPath := setupLocalSyncTestPackage(t, root)

	if system.IsDir(stylesPath) {
		t.Fatalf("Expected StylesPath to be missing before sync: %s", stylesPath)
	}

	err := sync(nil, &core.CLIFlags{
		Path:         cfgPath,
		IgnoreGlobal: true,
	})
	if err != nil {
		t.Fatalf("Command 'sync' failed while creating configured StylesPath %q from config %q: %v", stylesPath, cfgPath, err)
	}

	if !system.IsDir(stylesPath) {
		t.Fatalf("Expected sync to create configured StylesPath: %s", stylesPath)
	}
}

func TestSyncInstallsPackageIntoConfiguredStylesPath(t *testing.T) {
	root := t.TempDir()
	stylesPath, cfgPath := setupLocalSyncTestPackage(t, root)

	err := sync(nil, &core.CLIFlags{
		Path:         cfgPath,
		IgnoreGlobal: true,
	})
	if err != nil {
		t.Fatalf("Command 'sync' failed while installing local package into configured StylesPath %q: %v", stylesPath, err)
	}

	installedRule := filepath.Join(stylesPath, "TestStyle", "Rule.yml")
	if !system.FileExists(installedRule) {
		t.Fatalf("Expected package asset to be installed into configured StylesPath: %s", installedRule)
	}
}

func TestSyncDoesNotInstallPackageIntoGlobalStylesPath(t *testing.T) {
	root := t.TempDir()
	globalStylesPath := filepath.Join(root, "global-styles")
	stylesPath, cfgPath := setupLocalSyncTestPackage(t, root)

	if err := os.MkdirAll(globalStylesPath, os.ModePerm); err != nil {
		t.Fatalf("Failed to create global StylesPath %q: %v", globalStylesPath, err)
	}
	t.Setenv("VALE_STYLES_PATH", globalStylesPath)

	err := sync(nil, &core.CLIFlags{
		Path: cfgPath,
	})
	if err != nil {
		t.Fatalf("Command 'sync' failed with configured StylesPath %q and global StylesPath %q: %v", stylesPath, globalStylesPath, err)
	}

	installedRule := filepath.Join(stylesPath, "TestStyle", "Rule.yml")
	if !system.FileExists(installedRule) {
		t.Fatalf("Expected package asset to be installed into configured StylesPath: %s", installedRule)
	}

	wrongGlobalRule := filepath.Join(globalStylesPath, "TestStyle", "Rule.yml")
	if system.FileExists(wrongGlobalRule) {
		t.Fatalf("Expected package asset not to be installed into global StylesPath: %s", wrongGlobalRule)
	}
}

func TestSyncPreservesLocalPackageINI(t *testing.T) {
	// A local directory package's own .vale.ini must not be renamed/moved out
	// of the user's source directory during sync. See #991 (regression of
	// #583).
	root := t.TempDir()
	pkgRoot := filepath.Join(root, "local-package")
	pkgStyles := filepath.Join(pkgRoot, "styles", "TestStyle")
	if err := os.MkdirAll(pkgStyles, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgStyles, "Rule.yml"),
		[]byte("extends: existence\nmessage: test\nlevel: warning\ntokens: [foo]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	pkgINI := filepath.Join(pkgRoot, ".vale.ini")
	if err := os.WriteFile(pkgINI,
		[]byte("StylesPath = styles\n[*]\nBasedOnStyles = TestStyle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(root, ".vale.ini")
	if err := os.WriteFile(cfgPath,
		[]byte("StylesPath = missing-styles\nPackages = "+pkgRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := sync(nil, &core.CLIFlags{Path: cfgPath, IgnoreGlobal: true}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if !system.FileExists(pkgINI) {
		t.Fatalf("sync renamed/removed the local package's .vale.ini: %s", pkgINI)
	}

	// The config should still have been installed into the pipeline directory.
	installed := filepath.Join(root, "missing-styles", core.PipeDir, "0-local-package.ini")
	if !system.FileExists(installed) {
		t.Fatalf("expected package config in the pipeline directory: %s", installed)
	}
}

func TestSyncDoesNotInstallPackageIntoConfigRoot(t *testing.T) {
	root := t.TempDir()
	_, cfgPath := setupLocalSyncTestPackage(t, root)

	err := sync(nil, &core.CLIFlags{
		Path:         cfgPath,
		IgnoreGlobal: true,
	})
	if err != nil {
		t.Fatalf("Command 'sync' failed while checking that package is not installed into config root %q: %v", root, err)
	}

	wrongConfigRootRule := filepath.Join(root, "TestStyle", "Rule.yml")
	if system.FileExists(wrongConfigRootRule) {
		t.Fatalf("Expected package asset not to be installed into config root: %s", wrongConfigRootRule)
	}
}
