package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

var knownConfig = filepath.Join(testData, "fixtures", "formats", ".vale.ini")

// TestNoBaseConfig tests that we raise an error if we can't find a base
// config.
func TestNoBaseConfig(t *testing.T) {
	cfg, err := NewConfig(&CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	err = os.Chdir(homeDir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FromFile(cfg, false)
	if err == nil {
		t.Fatal("Expected error, got nil", cfg.ConfigFiles)
	}
}

// TestDefaultConfigOnly tests that a dry run -- e.g., `vale sync` -- succeeds
// when the user's only config file is the default one.
//
// See https://github.com/errata-ai/vale/issues/1128.
func TestDefaultConfigOnly(t *testing.T) {
	defaultCfg := onlyDefaultConfig(t)

	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = FromFile(cfg, true); err != nil {
		t.Fatal(err)
	}

	// `sync` reads its packages from the root config file.
	found, err := cfg.Root()
	if err != nil {
		t.Fatal(err)
	} else if found != defaultCfg {
		t.Fatalf("expected root '%s', got '%s'", defaultCfg, found)
	}

	// `sync` also has to write to the `StylesPath` that linting reads from.
	styles := filepath.Join(filepath.Dir(defaultCfg), "styles")
	if cfg.StylesPath() != styles {
		t.Fatalf("expected StylesPath '%s', got '%s'", styles, cfg.StylesPath())
	}
}

// TestDefaultConfigIgnored tests that `--no-global` leaves a dry run without a
// root config, rather than falling back to the file it just excluded.
func TestDefaultConfigIgnored(t *testing.T) {
	onlyDefaultConfig(t)

	cfg, err := NewConfig(&CLIFlags{IgnoreGlobal: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = FromFile(cfg, true); err != nil {
		t.Fatal(err)
	}

	if found, rootErr := cfg.Root(); rootErr == nil {
		t.Fatalf("expected no root, got '%s'", found)
	}
}

// onlyDefaultConfig points Vale at a config directory holding nothing but the
// default config file, from a working directory that has none.
func onlyDefaultConfig(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	configHome := filepath.Join(root, "config")
	defaultCfg := filepath.Join(configHome, "vale", ".vale.ini")

	if err := os.MkdirAll(filepath.Dir(defaultCfg), os.ModePerm); err != nil {
		t.Fatal(err)
	} else if err = os.WriteFile(defaultCfg, []byte("StylesPath = styles\nPackages = write-good\n"), 0o644); err != nil { //nolint:gosec
		t.Fatal(err)
	}

	unsetEnv(t, "VALE_CONFIG_PATH", "VALE_STYLES_PATH")

	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	xdg.Reload()
	t.Cleanup(xdg.Reload)

	// A directory without a project-specific config file.
	t.Chdir(t.TempDir())

	return defaultCfg
}

func unsetEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if old, found := os.LookupEnv(name); found {
			os.Unsetenv(name)                          //nolint:errcheck
			t.Cleanup(func() { os.Setenv(name, old) }) //nolint:errcheck
		}
	}
}

// TestFlagBase tests that we respect the `--config` option for setting a base
// config.
func TestFlagBase(t *testing.T) {
	cfg, err := NewConfig(&CLIFlags{Path: knownConfig})
	if err != nil {
		t.Fatal(err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	err = os.Chdir(homeDir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FromFile(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
}

// TestEnvBase tests that we respect the `VALE_CONFIG_PATH` option for setting
// a base config.
func TestEnvBase(t *testing.T) {
	t.Setenv("VALE_CONFIG_PATH", knownConfig)

	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	err = os.Chdir(homeDir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FromFile(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
}

// A package's .vale.ini keeps its own StylesPath. Read from the pipeline
// directory, that path resolved to a directory beside the file and became
// the last path, where the next sync then installed.
func TestPipelineStylesPathDropped(t *testing.T) {
	root := t.TempDir()
	styles := filepath.Join(root, "styles")
	pipe := filepath.Join(styles, PipeDir)
	if err := os.MkdirAll(pipe, os.ModePerm); err != nil {
		t.Fatal(err)
	}

	pkg := []byte("StylesPath = styles\n\n[*]\nBasedOnStyles = A\n")
	if err := os.WriteFile(filepath.Join(pipe, "0-pkg.ini"), pkg, 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".vale.ini")
	if err := os.WriteFile(cfgPath, []byte("StylesPath = styles\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadPipeline(&CLIFlags{Path: cfgPath, IgnoreGlobal: true}, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Paths) != 1 || cfg.Paths[0] != styles {
		t.Fatalf("Paths = %v, want only %s", cfg.Paths, styles)
	}
	if got := cfg.GBaseStyles; len(got) != 1 || got[0] != "A" {
		t.Errorf("the package's other settings should still load; got %v", got)
	}
}
