package core

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/errata-ai/ini"
	"github.com/errata-ai/vale/v3/internal/system"
)

// ConfigSrc is a source of configuration values.
//
// This could be a local file, a string, or a remote URL.
type ConfigSrc int

const (
	FileSrc ConfigSrc = iota
	StringSrc
)

// ReadPipeline loads Vale's configuration according to the local search
// process.
//
// A `dry` run means that we can't expect the `StylesPath` to fully formed yet.
// For example, some assets may not have been downloaded yet via the `sync`
// command.
func ReadPipeline(flags *CLIFlags, dry bool) (*Config, error) {
	config, err := NewConfig(flags)
	if err != nil {
		return config, err
	} else if err = validateFlags(config); err != nil {
		return config, err
	}

	_, err = FromFile(config, dry)
	if err != nil {
		return config, err
	}

	sources, err := pipeConfig(config)
	if err != nil {
		return config, err
	}

	if len(sources) > 0 {
		config.Flags.Sources = strings.Join(sources, ",")

		_, err = FromFile(config, true)
		if err != nil {
			return config, err
		}
	}

	return config, nil
}

// from updates an existing configuration with values From a user-provided
// source.
func from(provider ConfigSrc, src string, cfg *Config, dry bool) (*ini.File, error) {
	switch provider {
	case FileSrc:
		return loadINI(cfg, dry)
	case StringSrc:
		return loadStdin(src, cfg, dry)
	default:
		return nil, NewE100(
			"source/From", fmt.Errorf("unknown provider '%v'", provider))
	}
}

// FromFile loads an INI configuration from a file.
func FromFile(cfg *Config, dry bool) (*ini.File, error) {
	return from(FileSrc, "", cfg, dry)
}

// FromString loads an INI configuration from a string.
func FromString(src string, cfg *Config, dry bool) (*ini.File, error) {
	return from(StringSrc, src, cfg, dry)
}

func validateFlags(cfg *Config) error {
	if cfg.Flags.Path != "" && !system.FileExists(cfg.Flags.Path) {
		return NewE100(
			"--config",
			fmt.Errorf("path '%s' does not exist", cfg.Flags.Path))
	}
	return nil
}

func loadStdin(src string, cfg *Config, dry bool) (*ini.File, error) {
	uCfg, err := shadowLoad([]byte(src))
	if err != nil {
		return nil, NewE100("loadStdin", err)
	}
	return processConfig(uCfg, cfg, dry)
}

func loadINI(cfg *Config, dry bool) (*ini.File, error) {
	var sources []string
	var uCfg *ini.File

	// NOTE: In v3.0, we now use the user's config directory as the default
	// location.
	//
	// This is different from the other config-defining options (`--config`,
	// `VALE_CONFIG_PATH`, etc.) in that it's loaded in addition to, rather
	// than instead of, any other configuration sources.
	//
	// In other words, this config file is *always* loaded and is read after
	// any other sources to allow for project-agnostic customization.
	defaultCfg, _ := DefaultConfig()

	if system.FileExists(defaultCfg) && !cfg.Flags.IgnoreGlobal && !dry {
		sources = append(sources, defaultCfg)
		cfg.Flags.Local = true
		cfg.AddConfigFile(defaultCfg)
	}

	base, err := loadConfig(configNames)
	if err != nil {
		return nil, NewE100("loadINI/homedir", err)
	}
	cfg.RootINI = base

	if cfg.Flags.Sources != "" {
		// NOTE: This case shouldn't be accessible from the CLI, but it can
		// still be triggered by packages that include config files.

		for _, source := range strings.Split(cfg.Flags.Sources, ",") {
			abs, _ := filepath.Abs(source)
			sources = append(sources, abs)
		}
	} else if cfg.Flags.Path != "" {
		// We've been given a value through `--config`.
		sources = append(sources, cfg.Flags.Path)
		cfg.AddConfigFile(cfg.Flags.Path)
	} else if fromEnv, hasEnv := os.LookupEnv("VALE_CONFIG_PATH"); hasEnv {
		// We've been given a value through `VALE_CONFIG_PATH`.
		sources = append(sources, fromEnv)
		cfg.AddConfigFile(fromEnv)
	} else if base != "" {
		// We're using a config file found using a local search process.
		sources = append(sources, base)
		cfg.AddConfigFile(base)
	}

	if StringInSlice(cfg.Flags.AlertLevel, AlertLevels) {
		cfg.MinAlertLevel = LevelToInt[cfg.Flags.AlertLevel]
	}

	if base == "" && len(cfg.ConfigFiles) == 0 && !dry {
		return nil, NewE100(".vale.ini not found", errors.New("no config file found"))
	}

	uCfg, err = processSources(cfg, sources)
	if err != nil {
		return nil, NewE100("config pipeline failed", err)
	}
	return processConfig(uCfg, cfg, dry)
}

// loadConfig loads the .vale file. It checks the ancestors of the current
// directory, stopping on the first occurrence of a .vale or _vale file. If
// no ancestor of the current directory has a configuration file, it checks
// the user's home directory for a configuration file.
func loadConfig(names []string) (string, error) {
	var parent string

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		parent = filepath.Dir(cwd)

		for _, name := range names {
			loc := path.Join(cwd, name)
			if system.FileExists(loc) && !system.IsDir(loc) {
				return loc, nil
			}
		}

		if cwd == parent {
			break
		}
		cwd = parent
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return "", nil
	}

	for _, name := range names {
		loc := path.Join(homeDir, name)
		if system.FileExists(loc) && !system.IsDir(loc) {
			return loc, nil
		}
	}

	return "", nil
}
