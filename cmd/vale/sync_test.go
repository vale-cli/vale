package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
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

func TestSyncAgainInstallsIntoStylesPath(t *testing.T) {
	// A complete package's .vale.ini names its own StylesPath. Read back from
	// the pipeline directory on the next sync, that path became the install
	// target, so the second sync wrote under `.vale-config/styles` and never
	// refreshed the pipeline file.
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
	writePkg := func(body string) {
		if err := os.WriteFile(pkgINI, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writePkg("StylesPath = styles\n[*]\nBasedOnStyles = TestStyle\n")

	styles := filepath.Join(root, "styles")
	cfgPath := filepath.Join(root, ".vale.ini")
	if err := os.WriteFile(cfgPath,
		[]byte("StylesPath = styles\nPackages = "+pkgRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	flags := &core.CLIFlags{Path: cfgPath, IgnoreGlobal: true}

	if err := sync(nil, flags); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	writePkg("StylesPath = styles\nMinAlertLevel = error\n[*]\nBasedOnStyles = TestStyle\n")
	if err := sync(nil, flags); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if nested := filepath.Join(styles, core.PipeDir, "styles"); system.IsDir(nested) {
		t.Fatalf("second sync installed under the pipeline directory: %s", nested)
	}
	installed, err := os.ReadFile(filepath.Join(styles, core.PipeDir, "0-local-package.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installed), "MinAlertLevel = error") {
		t.Fatalf("pipeline file not refreshed:\n%s", installed)
	}
}

// syncZipPkg syncs the given zip fixture into a fresh StylesPath and returns
// that path.
func syncZipPkg(t *testing.T, zipName string) string {
	t.Helper()

	root := t.TempDir()
	stylesPath := filepath.Join(root, "styles")

	pkg, err := filepath.Abs(filepath.Join(TestData, zipName))
	if err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(root, ".vale.ini")
	if err = os.WriteFile(cfgPath,
		[]byte("StylesPath = styles\nPackages = "+pkg+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err = sync(nil, &core.CLIFlags{Path: cfgPath, IgnoreGlobal: true}); err != nil {
		t.Fatalf("sync failed for %s: %v", zipName, err)
	}

	return stylesPath
}

// installedNames lists the entries in dir by their exact on-disk names, so
// that a case-insensitive filesystem cannot hide a wrongly cased install.
func installedNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if name := entry.Name(); name != core.PipeDir && name != "sync" {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestSyncZipTopLevelDirCaseDiffersStyleOnly(t *testing.T) {
	// casefold.zip holds `CaseFold/Rule.yml`; the package is named after the
	// file, `casefold`. See #1181.
	stylesPath := syncZipPkg(t, "casefold.zip")

	names := installedNames(t, stylesPath)
	if len(names) != 1 || names[0] != "CaseFold" {
		t.Fatalf("expected the style to be installed as 'CaseFold', got %v", names)
	}

	rule := filepath.Join(stylesPath, "CaseFold", "Rule.yml")
	if !system.FileExists(rule) {
		t.Fatalf("expected installed rule: %s", rule)
	}
}

func TestSyncZipTopLevelDirCaseDiffersPackage(t *testing.T) {
	// casefold-pkg.zip holds `Casefold-Pkg/styles/CaseFoldStyle` and a
	// `.vale.ini`; the package is named after the file, `casefold-pkg`.
	stylesPath := syncZipPkg(t, "casefold-pkg.zip")

	rule := filepath.Join(stylesPath, "CaseFoldStyle", "Rule.yml")
	if !system.FileExists(rule) {
		t.Fatalf("expected installed rule: %s", rule)
	}

	pipe := filepath.Join(stylesPath, core.PipeDir)
	names := installedNames(t, pipe)
	if len(names) != 1 || names[0] != "0-Casefold-Pkg.ini" {
		t.Fatalf("expected the package config as '0-Casefold-Pkg.ini', got %v", names)
	}
}

func TestSyncZipTopLevelDirNameDiffers(t *testing.T) {
	// renamed-pkg.zip holds `other-root/styles/CaseFoldStyle` and a
	// `.vale.ini`; the package is named after the file, `renamed-pkg`.
	stylesPath := syncZipPkg(t, "renamed-pkg.zip")

	rule := filepath.Join(stylesPath, "CaseFoldStyle", "Rule.yml")
	if !system.FileExists(rule) {
		t.Fatalf("expected installed rule: %s", rule)
	}

	pipe := filepath.Join(stylesPath, core.PipeDir)
	names := installedNames(t, pipe)
	if len(names) != 1 || names[0] != "0-other-root.ini" {
		t.Fatalf("expected the package config as '0-other-root.ini', got %v", names)
	}
}

// localPkg writes a directory package named name under root that installs
// one style and lists the given packages of its own.
func localPkg(t *testing.T, root, name string, pkgs ...string) {
	t.Helper()

	dir := filepath.Join(root, name)
	style := filepath.Join(dir, "styles", name+"Style")
	if err := os.MkdirAll(style, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	rule := []byte("extends: existence\nmessage: test\ntokens: [x]\n")
	if err := os.WriteFile(filepath.Join(style, "Rule.yml"), rule, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := []byte("Packages = " + strings.Join(pkgs, ", ") + "\n")
	if err := os.WriteFile(filepath.Join(dir, ".vale.ini"), ini, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSyncPackagesThatListEachOther(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "A"), filepath.Join(root, "B")
	localPkg(t, root, "A", b)
	localPkg(t, root, "B", a)

	cfgPath := filepath.Join(root, ".vale.ini")
	if err := os.WriteFile(cfgPath, []byte("StylesPath = styles\nPackages = "+a+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sync(nil, &core.CLIFlags{Path: cfgPath, IgnoreGlobal: true}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	for _, style := range []string{"AStyle", "BStyle"} {
		if !system.FileExists(filepath.Join(root, "styles", style, "Rule.yml")) {
			t.Fatalf("expected %s to be installed", style)
		}
	}
}

func TestSyncLeavesNoTempDirs(t *testing.T) {
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}

	syncZipPkg(t, "casefold-pkg.zip")

	if left := installedNames(t, tmp); len(left) != 0 {
		t.Fatalf("expected sync to remove its temp directories, found %v", left)
	}
}
