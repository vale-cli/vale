package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/errata-ai/ini"

	"github.com/vale-cli/vale/v3/internal/glob"
	"github.com/vale-cli/vale/v3/internal/system"
)

var pathKeys = []string{
	"StylesPath",
}

// nonOptKeys are core keys that something other than `coreOpts` reads.
//
// They belong at the top level, so they're not a mistake -- they just aren't
// resolved here. See `GetPackages`.
var nonOptKeys = []string{
	"Packages",
}

// noChildSections disables the ini library's child-section feature.
//
// That feature reads a `.` in a section's name as nesting, so `[*.md]` is
// taken to be a child of `[*]` and inherits its keys. Our sections are glob
// patterns, where a dot is just a dot -- and nearly every one of them contains
// one. Left enabled, a lookup that misses in `[*.md]` silently returns `[*]`'s
// key instead, which is how a style scoped to one file type ended up applying
// to every file. See #1129.
//
// The delimiter has to be a string a section name cannot contain, rather than
// empty: an empty one is replaced by the library's default.
const noChildSections = "\x00"

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

// patternsWithShadows splits a key and its shadows on commas, keeping a
// comma escaped as `\,` inside its pattern so a quantifier like `{2,}`
// survives. Other escapes pass through untouched.
func patternsWithShadows(key *ini.Key) []string {
	var values []string
	for _, v := range key.ValueWithShadows() {
		values = append(values, splitEscaped(v, ',')...)
	}
	return mergeValues(values)
}

func splitEscaped(s string, delim rune) []string {
	var vals []string
	var buf strings.Builder
	escape := false
	for _, r := range s {
		switch {
		case escape:
			if r != delim {
				buf.WriteRune('\\')
			}
			buf.WriteRune(r)
			escape = false
		case r == '\\':
			escape = true
		case r == delim:
			vals = append(vals, buf.String())
			buf.Reset()
		default:
			buf.WriteRune(r)
		}
	}
	if escape {
		buf.WriteRune('\\')
	}
	return append(vals, buf.String())
}

func loadVocab(root string, cfg *Config) error {
	target := ""
	tried := []string{}
	for _, p := range cfg.SearchPaths() {
		opt := filepath.Join(p, VocabDir, root)
		tried = append(tried, opt)
		if system.IsDir(opt) {
			target = opt
			break
		}
	}

	if target == "" {
		return NewE100("vocab", fmt.Errorf(
			"'%s' vocabulary not found; searched: %s",
			root, strings.Join(tried, ", ")))
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

// validateLevel reports whether `key` names a rule that should run, recording
// any level it was given in `levels`.
//
// A level set under a section belongs to that section. Writing every one into
// a single map made the last section in the file decide the level everywhere,
// so `Vale.Spelling = warning` for Markdown quietly downgraded HTML too. See
// #965.
// ruleParam matches a php.ini-style parameter key: `Std.SentenceLength[max]`.
// The name half must be a rule (contain a dot) and must not itself contain
// brackets, which rule names are forbidden to hold.
var ruleParam = regexp.MustCompile(`^([^\[\]]+\.[^\[\]]+)\[([A-Za-z][A-Za-z0-9]*)\]$`)

// structuralKeys are the fields an ini value cannot express or should not
// own: lists, mappings, identity, and the rule's own prose. Changing these is
// authoring, which is what extending a rule in a style is for.
var structuralKeys = []string{
	"extends", "name", "path", "tests", "message", "description", "link",
	"tokens", "swap", "exceptions", "filters", "ignore", "raw", "either",
}

// asRuleParam intercepts a parameter key, storing it on cfg and reporting
// whether it was one. Parameters apply when the rule compiles, so unlike
// levels they hold wherever the rule runs. A later configuration file wins;
// within one file, the ini library keeps a duplicated key's first value.
func asRuleParam(key, val string, cfg *Config) (bool, error) {
	groups := ruleParam.FindStringSubmatch(key)
	if groups == nil {
		return false, nil
	}

	name, param := groups[1], strings.ToLower(groups[2])
	if param == "level" {
		// The classic key already says this, and has for a decade; a second
		// spelling that shadows it helps nobody.
		return true, NewE201FromTarget(fmt.Sprintf(
			"set a level with '%s = %s'", name, val), key, cfg.RootINI)
	}
	if StringInSlice(param, structuralKeys) {
		return true, NewE201FromTarget(fmt.Sprintf(
			"'%s' is not adjustable from configuration; extend '%s' in a style instead",
			param, name), key, cfg.RootINI)
	}

	if _, ok := cfg.RuleToParams[name]; !ok {
		cfg.RuleToParams[name] = map[string]string{}
	}
	cfg.RuleToParams[name][param] = val

	return true, nil
}

// lastValue returns the value a key was given last across the merged
// sources: a package's file, then the user-level file, then the project's.
// `Key.String` is the first, which let a package hold a rule's level or
// parameter against the project's own setting.
func lastValue(key *ini.Key) string {
	values := key.ValueWithShadows()
	return values[len(values)-1]
}

func validateLevel(key, val string, levels map[string]string) bool {
	options := []string{"YES", "suggestion", "warning", "error"}
	if val == "NO" || !StringInSlice(val, options) {
		return false
	} else if val != "YES" {
		levels[key] = val
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
		cfg.BlockIgnores[label] = patternsWithShadows(sec.Key("BlockIgnores"))
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
		cfg.TokenIgnores[label] = patternsWithShadows(sec.Key("TokenIgnores"))
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
		cfg.BlockIgnores["*"] = patternsWithShadows(sec.Key("BlockIgnores"))
	},
	"TokenIgnores": func(sec *ini.Section, cfg *Config) {
		cfg.TokenIgnores["*"] = patternsWithShadows(sec.Key("TokenIgnores"))
	},
	"Lang": func(sec *ini.Section, cfg *Config) {
		cfg.FormatToLang["*"] = sec.Key("Lang").String()
	},
}

var coreOpts = map[string]func(*ini.Section, *Config) error{
	"StylesPath": func(sec *ini.Section, cfg *Config) error {
		paths := sec.Key("StylesPath").ValueWithShadows()
		for _, path := range paths {
			cfg.AddStylesPath(path)

			if !system.FileExists(path) {
				return NewE201FromTarget(
					fmt.Sprintf("The path '%s' does not exist.", path),
					path,
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

func expandPaths(file *ini.File, source interface{}) {
	var path string

	switch s := source.(type) {
	case string:
		abs, _ := filepath.Abs(s)
		path = filepath.Dir(abs)
		if filepath.Base(path) == PipeDir {
			// A package's StylesPath named the styles it shipped with, which
			// sync has since merged into the project's. Resolved from here it
			// is a directory beside this file that does not exist, and, as
			// the last path added, where the next sync would install.
			file.Section("").DeleteKey("StylesPath")
		}
	default:
		path, _ = os.Getwd()
	}

	for _, section := range file.Sections() {
		for _, key := range section.Keys() {
			if StringInSlice(key.Name(), pathKeys) {
				value := key.Value()
				if !filepath.IsAbs(value) {
					key.SetValue(filepath.Join(path, value))
				}
			}
		}
	}
}

func shadowMerge(primary *ini.File, secondary *ini.File) {
	for _, secondarySection := range secondary.Sections() {
		sectionName := secondarySection.Name()

		primarySection, _ := primary.GetSection(sectionName)
		if primarySection == nil {
			primarySection, _ = primary.NewSection(sectionName)
		}

		for _, secondaryKey := range secondarySection.Keys() {
			keyName := secondaryKey.Name()
			keyValue := secondaryKey.Value()

			primaryKey, _ := primarySection.GetKey(keyName)
			if primaryKey == nil {
				primarySection.NewKey(keyName, keyValue)
			} else {
				primaryKey.AddShadow(keyValue)
			}
		}
	}
}

func shadowLoad(source interface{}, others ...interface{}) (*ini.File, error) {
	options := ini.LoadOptions{
		AllowShadows:             true,
		Loose:                    true,
		SpaceBeforeInlineComment: true,
		ChildSectionDelimiter:    noChildSections,
	}

	primary, err := ini.LoadSources(options, source)
	if err != nil {
		return nil, err
	}
	expandPaths(primary, source)

	for _, other := range others {
		var shadow *ini.File

		shadow, err = ini.LoadSources(options, other)
		if err != nil {
			return nil, err
		}

		expandPaths(shadow, other)
		shadowMerge(primary, shadow)
	}

	return primary, nil
}

func processSources(cfg *Config, sources []string) (*ini.File, error) {
	var err error

	uCfg := ini.Empty(ini.LoadOptions{
		AllowShadows:             true,
		Loose:                    true,
		SpaceBeforeInlineComment: true,
		ChildSectionDelimiter:    noChildSections,
	})

	if len(sources) == 0 {
		// A dry run has no sources when the only config file is the default
		// one, which `sync` resolves later via `Config.Root`.
		//
		// Callers that require a config file check for one before we get here.
		return uCfg, nil
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
		} else if !StringInSlice(k, nonOptKeys) {
			// Nothing reads a key we don't recognize here, so leaving it be
			// quietly means the user's config says something Vale never hears.
			//
			// The delimiters include `:`, which is what makes this worth
			// saying out loud: a URL left on a line of its own parses as the
			// key `https` with the rest of itself for a value, and the package
			// it was meant to name is never installed.
			//
			// It's a warning rather than an error because a config that has
			// carried a stale key for years still lints exactly as it did.
			Warn(fmt.Sprintf(
				"'%s' isn't a core option; Vale is ignoring it.", k))
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
		} else if isParam, pErr := asRuleParam(k, lastValue(global.Key(k)), cfg); pErr != nil {
			return nil, pErr
		} else if !isParam {
			cfg.GChecks[k] = validateLevel(k, lastValue(global.Key(k)), cfg.RuleToLevel)
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
		levelMap := make(map[string]string)
		for _, k := range uCfg.Section(sec).KeyStrings() {
			if _, option := coreOpts[k]; option {
				return nil, NewE201FromTarget(fmt.Sprintf(coreError, k), k, cfg.RootINI)
			} else if f, found := syntaxOpts[k]; found {
				if err = f(sec, uCfg.Section(sec), cfg); err != nil && !dry {
					return nil, err
				}
			} else if isParam, pErr := asRuleParam(k, lastValue(uCfg.Section(sec).Key(k)), cfg); pErr != nil {
				return nil, pErr
			} else if !isParam {
				syntaxMap[k] = validateLevel(k, lastValue(uCfg.Section(sec).Key(k)), levelMap)
				cfg.Checks = append(cfg.Checks, k)
			}
		}
		cfg.RuleKeys = append(cfg.RuleKeys, sec)
		cfg.SChecks[sec] = syntaxMap
		cfg.SLevels[sec] = levelMap
	}

	return uCfg, nil
}
