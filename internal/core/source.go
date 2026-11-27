package core

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/errata-ai/ini"
	"github.com/vale-cli/vale/v3/internal/system"
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
			"config", fmt.Errorf("unknown provider '%v'", provider))
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

// LoadConfigFromString returns the configuration an INI document describes,
// with its assets resolved against the active filesystem. It is the entry
// point for a host that has no config file on disk, such as a browser.
func LoadConfigFromString(src string) (*Config, error) {
	return LoadConfigFromStringsFS(nil, src)
}

// LoadConfigFromStringFS is LoadConfigFromString against a filesystem of its
// own; nil means system.Default.
func LoadConfigFromStringFS(fsys system.FS, src string) (*Config, error) {
	return LoadConfigFromStringsFS(fsys, src)
}

// LoadConfigFromStrings resolves a project's configuration with earlier
// layers merged in before it, as the CLI reads the user-level file ahead of
// the project's: list keys union, and a scalar such as MinAlertLevel favors
// the project.
func LoadConfigFromStrings(project string, priors ...string) (*Config, error) {
	return LoadConfigFromStringsFS(nil, project, priors...)
}

// LoadConfigFromStringsFS is LoadConfigFromStrings against a filesystem of
// its own; nil means system.Default.
func LoadConfigFromStringsFS(fsys system.FS, project string, priors ...string) (*Config, error) {
	cfg, err := NewConfig(&CLIFlags{FS: fsys})
	if err != nil {
		return nil, err
	}
	if _, err = loadStdinSources(project, priors, cfg, false); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadStdinSources loads a configuration given as text, with any earlier
// layers, through the pipeline a file gets: a `.vale-config` directory of
// package fragments under its StylesPath first, then the layers, then the
// project's own text, shadow-merged.
func loadStdinSources(src string, priors []string, cfg *Config, dry bool) (*ini.File, error) {
	merge := func(lead [][]byte) (*ini.File, error) {
		sources := []interface{}{}
		for _, b := range lead {
			sources = append(sources, b)
		}
		for _, p := range priors {
			sources = append(sources, []byte(p))
		}
		sources = append(sources, []byte(src))
		return shadowLoad(cfg.FS, sources[0], sources[1:]...)
	}

	uCfg, err := merge(nil)
	if err != nil {
		return nil, NewE100("config", err)
	}

	// The fragments can only be found once StylesPath is known, so a dry
	// pass resolves it and the sources are then read again with them ahead.
	if _, perr := processConfig(uCfg, cfg, true); perr == nil {
		pipeline := filepath.Join(cfg.StylesPath(), PipeDir)
		if frags, gerr := cfg.FS.Glob(filepath.Join(pipeline, "*")); gerr == nil && len(frags) > 0 {
			sortFragments(frags)
			lead := [][]byte{}
			for _, f := range frags {
				if b, rerr := cfg.FS.ReadFile(f); rerr == nil {
					lead = append(lead, b)
				}
			}
			if len(lead) > 0 {
				if merged, merr := merge(lead); merr == nil {
					uCfg = merged
				}
			}
		}
	}
	return processConfig(uCfg, cfg, dry)
}

func validateFlags(cfg *Config) error {
	if cfg.Flags.Path != "" && !cfg.FS.Exists(cfg.Flags.Path) {
		return NewE100(
			"--config",
			fmt.Errorf("path '%s' does not exist", cfg.Flags.Path))
	}
	return nil
}

func loadStdin(src string, cfg *Config, dry bool) (*ini.File, error) {
	uCfg, err := shadowLoad(cfg.FS, []byte(src))
	if err != nil {
		return nil, NewE100("config", err)
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
	//
	// A dry run -- i.e., `sync` -- is the exception: it has to run against a
	// single config file, so we only load this one if it's the only one (see
	// below).
	defaultCfg, _ := DefaultConfig()
	hasDefault := cfg.FS.Exists(defaultCfg) && !cfg.Flags.IgnoreGlobal

	if hasDefault && !dry {
		sources = append(sources, defaultCfg)
		cfg.Flags.Local = true
		cfg.AddConfigFile(defaultCfg)
	}

	base, root, err := loadConfig(configNames)
	if err != nil {
		return nil, NewE100("config", err)
	}
	cfg.RootINI = base
	cfg.RootDir = root

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
		cfg.setRoot(cfg.Flags.Path)
	} else if fromEnv, hasEnv := os.LookupEnv("VALE_CONFIG_PATH"); hasEnv {
		// We've been given a value through `VALE_CONFIG_PATH`.
		sources = append(sources, fromEnv)
		cfg.AddConfigFile(fromEnv)
		cfg.setRoot(fromEnv)
	} else if base != "" {
		// We're using a config file found using a local search process.
		sources = append(sources, base)
		cfg.AddConfigFile(base)
	} else if hasDefault && dry {
		// The default config is the only one available, so a dry run has to
		// read it to resolve values such as `StylesPath`.
		sources = append(sources, defaultCfg)
		cfg.Flags.Local = true
		cfg.AddConfigFile(defaultCfg)
		cfg.setRoot(defaultCfg)
	}

	if StringInSlice(cfg.Flags.AlertLevel, AlertLevels) {
		cfg.MinAlertLevel = LevelToInt[cfg.Flags.AlertLevel]
	}

	if base == "" && len(cfg.ConfigFiles) == 0 && !dry {
		return nil, NewE100(".vale.ini not found", errors.New("no config file found"))
	}

	uCfg, err = processSources(cfg, sources)
	if err != nil {
		return nil, NewE100("config", err)
	}
	return processConfig(uCfg, cfg, dry)
}

// loadConfig finds the project's config file and the directory it was found
// at, which is the project root. It checks the current directory and its
// ancestors for one of names, then `.config/vale.ini`, and falls back to the
// user's home directory.
func loadConfig(names []string) (string, string, error) {
	var parent string

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}

	for {
		parent = filepath.Dir(cwd)

		for _, name := range append(names, nestedConfigNames...) {
			loc := filepath.Join(cwd, name)
			if system.FileExists(loc) && !system.IsDir(loc) {
				return loc, cwd, nil
			}
		}

		if cwd == parent {
			break
		}
		cwd = parent
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return "", "", nil
	}

	for _, name := range names {
		loc := filepath.Join(homeDir, name)
		if system.FileExists(loc) && !system.IsDir(loc) {
			return loc, homeDir, nil
		}
	}

	return "", "", nil
}

// sortFragments orders the pipeline's files by the index sync named them
// with, `<index>-<package>.ini`, as a number: sorted as text, the eleventh
// package's `10-` would come before the third's `2-`.
func sortFragments(frags []string) {
	index := func(path string) int {
		head, _, _ := strings.Cut(filepath.Base(path), "-")
		if n, err := strconv.Atoi(head); err == nil {
			return n
		}
		return math.MaxInt
	}
	sort.SliceStable(frags, func(i, j int) bool {
		if a, b := index(frags[i]), index(frags[j]); a != b {
			return a < b
		}
		return frags[i] < frags[j]
	})
}
