package core

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/errata-ai/ini"

	"github.com/errata-ai/vale/v3/internal/glob"
	"github.com/errata-ai/vale/v3/internal/system"
)

var coreError = "'%s' is a core option; it should be defined above any syntax-specific options (`[...]`)."

func mergeValues(shadows []string) []string {
	values := []string{}
	for _, v := range shadows {
		entry := strings.TrimSpace(v)
		if entry != "" && !StringInSlice(entry, values) {
			values = append(values, entry)
		}
	}
	return values
}

func loadVocab(root string, cfg *Config) error {
	target := ""
	for _, p := range cfg.SearchPaths() {
		opt := filepath.Join(p, VocabDir, root)
		if system.IsDir(opt) {
			target = opt
			break
		}
	}

	if target == "" {
		return NewE100("vocab", fmt.Errorf(
			"'%s/%s' directory does not exist", VocabDir, root))
	}

	err := system.Walk(target, func(fp string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if name == "accept.txt" {
			return cfg.AddWordListFile(fp, true)
		} else if name == "reject.txt" {
			return cfg.AddWordListFile(fp, false)
		}
		return nil
	})

	return err
}

func validateLevel(key, val string, cfg *Config) bool {
	options := []string{"YES", "suggestion", "warning", "error"}
	if val == "NO" || !StringInSlice(val, options) {
		return false
	} else if val != "YES" {
		cfg.RuleToLevel[key] = val
	}
	return true
}

var syntaxOpts = map[string]func(string, *ini.Section, *Config) error{
	"BasedOnStyles": func(lbl string, sec *ini.Section, cfg *Config) error {
		pat, err := glob.Compile(lbl)
		if err != nil {
			return NewE201FromTarget(
				fmt.Sprintf("The glob pattern '%s' could not be compiled.", lbl),
				lbl,
				cfg.Flags.Path)
		} else if _, found := cfg.SecToPat[lbl]; !found {
			cfg.SecToPat[lbl] = pat
		}
		sStyles := mergeValues(sec.Key("BasedOnStyles").StringsWithShadows(","))

		cfg.Styles = append(cfg.Styles, sStyles...)
		cfg.StyleKeys = append(cfg.StyleKeys, lbl)
		cfg.SBaseStyles[lbl] = sStyles

		return nil
	},
	"IgnorePatterns": func(label string, sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.BlockIgnores[label] = sec.Key("IgnorePatterns").Strings(",")
		return nil
	},
	"BlockIgnores": func(label string, sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.BlockIgnores[label] = mergeValues(sec.Key("BlockIgnores").StringsWithShadows(","))
		return nil
	},
	"CommentDelimiters": func(label string, sec *ini.Section, cfg *Config) error {
		d := mergeValues(sec.Key("CommentDelimiters").StringsWithShadows(","))
		if len(d) != 2 {
			return NewE201FromTarget(
				fmt.Sprintf("CommentDelimiters must be a comma-separated list of two delimiters, but got %v items", len(d)),
				label,
				cfg.Flags.Path)
		}
		var c [2]string
		c[0], c[1] = d[0], d[1]
		cfg.CommentDelimiters[label] = c
		return nil

	},
	"TokenIgnores": func(label string, sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.TokenIgnores[label] = mergeValues(sec.Key("TokenIgnores").StringsWithShadows(","))
		return nil
	},
	"Transform": func(label string, sec *ini.Section, cfg *Config) error { //nolint:unparam
		candidate := sec.Key("Transform").String()
		cfg.Stylesheets[label] = system.DeterminePath(cfg.ConfigFile(), candidate)
		return nil

	},
	"Lang": func(label string, sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.FormatToLang[label] = sec.Key("Lang").String()
		return nil
	},
	"View": func(label string, sec *ini.Section, cfg *Config) error {
		name := sec.Key("View").String()

		path := FindConfigAsset(cfg, name+".yml", ViewDir)
		if path == "" {
			return fmt.Errorf("view '%s' not found", name)
		}

		view, err := NewView(path)
		if err != nil {
			return err
		}

		cfg.Views[label] = view
		return nil
	},
}

var globalOpts = map[string]func(*ini.Section, *Config){
	"BasedOnStyles": func(sec *ini.Section, cfg *Config) {
		cfg.GBaseStyles = mergeValues(sec.Key("BasedOnStyles").StringsWithShadows(","))
		cfg.Styles = append(cfg.Styles, cfg.GBaseStyles...)
	},
	"IgnorePatterns": func(sec *ini.Section, cfg *Config) {
		cfg.BlockIgnores["*"] = sec.Key("IgnorePatterns").Strings(",")
	},
	"BlockIgnores": func(sec *ini.Section, cfg *Config) {
		cfg.BlockIgnores["*"] = mergeValues(sec.Key("BlockIgnores").StringsWithShadows(","))
	},
	"TokenIgnores": func(sec *ini.Section, cfg *Config) {
		cfg.TokenIgnores["*"] = mergeValues(sec.Key("TokenIgnores").StringsWithShadows(","))
	},
	"Lang": func(sec *ini.Section, cfg *Config) {
		cfg.FormatToLang["*"] = sec.Key("Lang").String()
	},
}

var coreOpts = map[string]func(*ini.Section, *Config) error{
	"StylesPath": func(sec *ini.Section, cfg *Config) error {
		// NOTE: The order of these paths is important. They represent the load
		// order of the configuration files -- not `cfg.Paths`.
		paths := sec.Key("StylesPath").ValueWithShadows()
		files := cfg.ConfigFiles
		if cfg.Flags.Local && len(files) == 2 {
			// This represents the case where we have a default `.vale.ini`
			// file and a local `.vale.ini` file.
			//
			// In such a case, there are three options: (1) both files define a
			// `StylesPath`, (2) only one file defines a `StylesPath`, or (3)
			// neither file defines a `StylesPath`.
			basePath := system.DeterminePath(files[0], filepath.FromSlash(paths[0]))
			mockPath := system.DeterminePath(files[1], filepath.FromSlash(paths[0]))
			// ^ This case handles the situation where both configs define the
			// same StylesPath (e.g., `StylesPath = styles`).
			if len(paths) == 2 {
				basePath = system.DeterminePath(files[0], filepath.FromSlash(paths[0]))
				mockPath = system.DeterminePath(files[1], filepath.FromSlash(paths[1]))
			}
			cfg.AddStylesPath(basePath)
			cfg.AddStylesPath(mockPath)
		} else if len(paths) > 0 {
			// In this case, we have a local configuration file (no default)
			// that defines a `StylesPath`.
			candidate := filepath.FromSlash(paths[len(paths)-1])
			path := system.DeterminePath(cfg.ConfigFile(), candidate)

			cfg.AddStylesPath(path)
			if !system.FileExists(path) {
				return NewE201FromTarget(
					fmt.Sprintf("The path '%s' does not exist.", path),
					candidate,
					cfg.Flags.Path)
			}
		}
		return nil
	},
	"MinAlertLevel": func(sec *ini.Section, cfg *Config) error {
		if !StringInSlice(cfg.Flags.AlertLevel, AlertLevels) {
			level := sec.Key("MinAlertLevel").String()

			values := sec.Key("MinAlertLevel").StringsWithShadows(",")
			if len(values) > 0 {
				level = values[len(values)-1]
			}

			if index, found := LevelToInt[level]; found {
				cfg.MinAlertLevel = index
			} else {
				return NewE201FromTarget(
					"MinAlertLevel must be 'suggestion', 'warning', or 'error'.",
					level,
					cfg.Flags.Path)
			}
		}
		return nil
	},
	"IgnoredScopes": func(sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.IgnoredScopes = mergeValues(sec.Key("IgnoredScopes").StringsWithShadows(","))
		return nil
	},
	"WordTemplate": func(sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.WordTemplate = sec.Key("WordTemplate").String()
		return nil
	},
	"SkippedScopes": func(sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.SkippedScopes = mergeValues(sec.Key("SkippedScopes").StringsWithShadows(","))
		return nil
	},
	"IgnoredClasses": func(sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.IgnoredClasses = mergeValues(sec.Key("IgnoredClasses").StringsWithShadows(","))
		return nil
	},
	"Vocab": func(sec *ini.Section, cfg *Config) error {
		cfg.Vocab = mergeValues(sec.Key("Vocab").StringsWithShadows(","))
		for _, v := range cfg.Vocab {
			if err := loadVocab(v, cfg); err != nil {
				return err
			}
		}
		return nil
	},
	"NLPEndpoint": func(sec *ini.Section, cfg *Config) error { //nolint:unparam
		cfg.NLPEndpoint = sec.Key("NLPEndpoint").MustString("")

		values := sec.Key("NLPEndpoint").StringsWithShadows(",")
		if len(values) > 0 {
			cfg.NLPEndpoint = values[len(values)-1]
		}

		return nil
	},
}

func shadowLoad(source interface{}, others ...interface{}) (*ini.File, error) {
	return ini.LoadSources(ini.LoadOptions{
		AllowShadows:             true,
		SpaceBeforeInlineComment: true}, source, others...)
}

func processSources(cfg *Config, sources []string) (*ini.File, error) {
	var err error

	uCfg := ini.Empty(ini.LoadOptions{
		AllowShadows:             true,
		Loose:                    true,
		SpaceBeforeInlineComment: true,
	})

	if len(sources) == 0 {
		return uCfg, errors.New("no sources provided")
	} else if len(sources) == 1 {
		cfg.Flags.Path = sources[0]
		return shadowLoad(cfg.Flags.Path)
	}

	t := sources[1:]
	s := make([]interface{}, len(t))
	for i, v := range t {
		s[i] = v
	}

	uCfg, err = shadowLoad(sources[0], s...)
	cfg.Flags.Path = sources[len(sources)-1]

	return uCfg, err
}

func processConfig(uCfg *ini.File, cfg *Config, dry bool) (*ini.File, error) {
	core := uCfg.Section("")
	global := uCfg.Section("*")

	formats := uCfg.Section("formats")
	adoc := uCfg.Section("asciidoctor")

	// Default settings
	for _, k := range core.KeyStrings() {
		if f, found := coreOpts[k]; found {
			if err := f(core, cfg); err != nil && !dry {
				return nil, err
			}
		} else if _, found = syntaxOpts[k]; found {
			msg := fmt.Sprintf("'%s' is a syntax-specific option", k)
			return nil, NewE201FromTarget(msg, k, cfg.RootINI)
		}
	}

	// Format mappings
	for _, k := range formats.KeyStrings() {
		cfg.Formats[k] = formats.Key(k).String()
	}

	// Asciidoctor attributes
	for _, k := range adoc.KeyStrings() {
		cfg.Asciidoctor[k] = adoc.Key(k).String()
	}

	// Global settings
	for _, k := range global.KeyStrings() {
		if _, option := coreOpts[k]; option {
			return nil, NewE201FromTarget(fmt.Sprintf(coreError, k), k, cfg.RootINI)
		} else if f, found := globalOpts[k]; found {
			f(global, cfg)
		} else if _, found = syntaxOpts[k]; found {
			msg := fmt.Sprintf("'%s' is a syntax-specific option", k)
			return nil, NewE201FromTarget(msg, k, cfg.RootINI)
		} else {
			cfg.GChecks[k] = validateLevel(k, global.Key(k).String(), cfg)
			cfg.Checks = append(cfg.Checks, k)
		}
	}

	// Syntax-specific settings
	for _, sec := range uCfg.SectionStrings() {
		if StringInSlice(sec, []string{"*", "DEFAULT", "formats", "asciidoctor"}) {
			continue
		}

		pat, err := glob.Compile(sec)
		if err != nil {
			return nil, err
		}
		cfg.SecToPat[sec] = pat

		syntaxMap := make(map[string]bool)
		for _, k := range uCfg.Section(sec).KeyStrings() {
			if _, option := coreOpts[k]; option {
				return nil, NewE201FromTarget(fmt.Sprintf(coreError, k), k, cfg.RootINI)
			} else if f, found := syntaxOpts[k]; found {
				if err = f(sec, uCfg.Section(sec), cfg); err != nil && !dry {
					return nil, err
				}
			} else {
				syntaxMap[k] = validateLevel(k, uCfg.Section(sec).Key(k).String(), cfg)
				cfg.Checks = append(cfg.Checks, k)
			}
		}
		cfg.RuleKeys = append(cfg.RuleKeys, sec)
		cfg.SChecks[sec] = syntaxMap
	}

	return uCfg, nil
}
