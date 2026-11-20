package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/errata-ai/ini"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

func TestInitCmd_CreatesValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".vale.ini")

	if system.FileExists(cfgPath) {
		t.Fatalf("expected config to not exist initially: %s", cfgPath)
	}

	err := initCmd([]string{cfgPath}, &core.CLIFlags{})
	if err != nil {
		t.Fatalf("initCmd failed: %v", err)
	}

	if !system.FileExists(cfgPath) {
		t.Fatalf("expected config file to be created: %s", cfgPath)
	}

	stylesDir := filepath.Join(tmpDir, "styles")
	if !system.IsDir(stylesDir) {
		t.Fatalf("expected styles directory to be created: %s", stylesDir)
	}

	// Validate INI syntax and keys
	uCfg, err := ini.Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to parse generated config with ini: %v", err)
	}

	coreSec := uCfg.Section("")
	if coreSec.Key("StylesPath").String() != "styles" {
		t.Errorf("expected StylesPath = styles, got %s", coreSec.Key("StylesPath").String())
	}
	if coreSec.Key("MinAlertLevel").String() != "suggestion" {
		t.Errorf("expected MinAlertLevel = suggestion, got %s", coreSec.Key("MinAlertLevel").String())
	}
	if coreSec.Key("Packages").String() != "Google" {
		t.Errorf("expected Packages = Google, got %s", coreSec.Key("Packages").String())
	}

	formatSec := uCfg.Section("*.{md,txt}")
	if formatSec.Key("BasedOnStyles").String() != "Vale, Google" {
		t.Errorf("expected BasedOnStyles = Vale, Google, got %s", formatSec.Key("BasedOnStyles").String())
	}

	// Validate with Vale pipeline loader
	cfg, err := core.ReadPipeline(&core.CLIFlags{Path: cfgPath, IgnoreGlobal: true}, true)
	if err != nil {
		t.Fatalf("failed to read generated config through ReadPipeline: %v", err)
	}

	if cfg.MinAlertLevel != core.LevelToInt["suggestion"] {
		t.Errorf("expected MinAlertLevel %d, got %d", core.LevelToInt["suggestion"], cfg.MinAlertLevel)
	}

	pkgs, err := core.GetPackages(cfgPath)
	if err != nil {
		t.Fatalf("failed to get packages from generated config: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "Google" {
		t.Errorf("expected packages [Google], got %v", pkgs)
	}

	baseStyles := cfg.SBaseStyles["*.{md,txt}"]
	if len(baseStyles) != 2 || baseStyles[0] != "Vale" || baseStyles[1] != "Google" {
		t.Errorf("expected base styles [Vale Google], got %v", baseStyles)
	}
}

func TestInitCmd_ExistingConfigFails(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".vale.ini")

	err := os.WriteFile(cfgPath, []byte("StylesPath = styles\n"), 0o600)
	if err != nil {
		t.Fatalf("failed to create existing config: %v", err)
	}

	err = initCmd([]string{cfgPath}, &core.CLIFlags{})
	if err == nil {
		t.Fatal("expected initCmd to fail when config already exists, got nil")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error message mentioning 'already exists', got: %v", err)
	}
}

func TestInitCmd_AlternativeConfigInDirFails(t *testing.T) {
	tmpDir := t.TempDir()
	altPath := filepath.Join(tmpDir, "vale.ini")

	err := os.WriteFile(altPath, []byte("StylesPath = styles\n"), 0o600)
	if err != nil {
		t.Fatalf("failed to create existing alt config: %v", err)
	}

	targetPath := filepath.Join(tmpDir, ".vale.ini")
	err = initCmd([]string{targetPath}, &core.CLIFlags{})
	if err == nil {
		t.Fatal("expected initCmd to fail when alternative config exists in directory, got nil")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error message mentioning 'already exists', got: %v", err)
	}
}

func TestInitCmd_DefaultTargetInCwd(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory to temp dir: %v", err)
	}

	err = initCmd(nil, &core.CLIFlags{})
	if err != nil {
		t.Fatalf("initCmd without args failed: %v", err)
	}

	expectedFile := filepath.Join(tmpDir, ".vale.ini")
	if !system.FileExists(expectedFile) {
		t.Fatalf("expected .vale.ini in cwd, file not found: %s", expectedFile)
	}

	expectedStyles := filepath.Join(tmpDir, "styles")
	if !system.IsDir(expectedStyles) {
		t.Fatalf("expected styles dir in cwd, dir not found: %s", expectedStyles)
	}
}

func TestInitCmd_WithTargetDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subproject")
	if err := os.MkdirAll(subDir, os.ModePerm); err != nil {
		t.Fatalf("failed to create subproject dir: %v", err)
	}

	err := initCmd([]string{subDir}, &core.CLIFlags{})
	if err != nil {
		t.Fatalf("initCmd with directory argument failed: %v", err)
	}

	expectedFile := filepath.Join(subDir, ".vale.ini")
	if !system.FileExists(expectedFile) {
		t.Fatalf("expected .vale.ini inside subproject, file not found: %s", expectedFile)
	}

	expectedStyles := filepath.Join(subDir, "styles")
	if !system.IsDir(expectedStyles) {
		t.Fatalf("expected styles inside subproject, dir not found: %s", expectedStyles)
	}
}

func TestInitCmd_WithConfigFlag(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".custom.ini")

	err := initCmd(nil, &core.CLIFlags{Path: cfgPath})
	if err != nil {
		t.Fatalf("initCmd with --config flag failed: %v", err)
	}

	if !system.FileExists(cfgPath) {
		t.Fatalf("expected custom config file to exist: %s", cfgPath)
	}

	expectedStyles := filepath.Join(tmpDir, "styles")
	if !system.IsDir(expectedStyles) {
		t.Fatalf("expected styles dir in config dir, dir not found: %s", expectedStyles)
	}
}

func TestInitCmd_StylesDirAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".vale.ini")
	stylesDir := filepath.Join(tmpDir, "styles")

	if err := os.MkdirAll(stylesDir, os.ModePerm); err != nil {
		t.Fatalf("failed to pre-create styles dir: %v", err)
	}

	err := initCmd([]string{cfgPath}, &core.CLIFlags{})
	if err != nil {
		t.Fatalf("initCmd failed when styles dir already exists: %v", err)
	}

	if !system.FileExists(cfgPath) {
		t.Fatalf("expected config file to be created: %s", cfgPath)
	}
}

func TestInitCmd_StylesPathIsFileFails(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".vale.ini")
	stylesConflict := filepath.Join(tmpDir, "styles")

	if err := os.WriteFile(stylesConflict, []byte("conflict"), 0o600); err != nil {
		t.Fatalf("failed to create conflicting styles file: %v", err)
	}

	err := initCmd([]string{cfgPath}, &core.CLIFlags{})
	if err == nil {
		t.Fatal("expected initCmd to fail when styles exists as regular file, got nil")
	}
}

func TestInitCmd_ValidatesFixture(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "init", ".vale.ini")
	if !system.FileExists(fixturePath) {
		t.Fatalf("expected fixture to exist: %s", fixturePath)
	}

	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	if string(content) != core.DefaultConfigTemplate {
		t.Errorf("expected fixture content to match DefaultConfigTemplate")
	}

	cfg, err := core.ReadPipeline(&core.CLIFlags{Path: fixturePath, IgnoreGlobal: true}, true)
	if err != nil {
		t.Fatalf("failed to read fixture pipeline: %v", err)
	}

	if cfg.MinAlertLevel != core.LevelToInt["suggestion"] {
		t.Errorf("expected MinAlertLevel %d, got %d", core.LevelToInt["suggestion"], cfg.MinAlertLevel)
	}
}
