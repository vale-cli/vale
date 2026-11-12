package core

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
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

// loadVocab adds a vocabulary's terms to the project's, for a top-level
// `Vocab`.
func loadVocab(root string, cfg *Config) error {
	vocab, err := loadVocabulary(root, cfg)
	if err != nil {
		return err
	}
	cfg.AcceptedTokens = append(cfg.AcceptedTokens, vocab.Accepted...)
	cfg.RejectedTokens = append(cfg.RejectedTokens, vocab.Rejected...)
	return nil
}

// loadVocabulary reads a vocabulary by name, once.
func loadVocabulary(root string, cfg *Config) (*Vocabulary, error) {
	if vocab, ok := cfg.Vocabularies[root]; ok {
		return vocab, nil
	}

	target := ""
	tried := []string{}
	for _, p := range cfg.SearchPaths() {
		opt := filepath.Join(p, VocabDir, root)
		tried = append(tried, opt)
		if cfg.FS.IsDir(opt) {
			target = opt
			break
		}
	}

	if target == "" {
		return nil, NewE100("vocab", fmt.Errorf(
			"'%s' vocabulary not found; searched: %s",
			root, strings.Join(tried, ", ")))
	}

	vocab := &Vocabulary{}
	err := cfg.FS.Walk(target, func(fp string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		switch info.Name() {
		case "accept.txt":
			terms, rerr := readWordList(cfg.FS, fp)
			vocab.Accepted = append(vocab.Accepted, terms...)
			return rerr
		case "reject.txt":
			terms, rerr := readWordList(cfg.FS, fp)
			vocab.Rejected = append(vocab.Rejected, terms...)
			return rerr
		case vocabFiles[0], vocabFiles[1]:
			return readVocabYAML(fp, vocab, cfg)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	cfg.Vocabularies[root] = vocab
	return vocab, nil
}

// readWordList reads a vocabulary file: one term per line, `# ` a comment.
func readWordList(fsys system.FS, path string) ([]string, error) {
	fd, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	var terms []string
	scanner := bufio.NewScanner(fd)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if len(word) == 0 || strings.HasPrefix(word, "# ") {
			continue
		}
		terms = append(terms, word)
	}
	return terms, scanner.Err()
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

// unsetValue drops what earlier sections said about a rule or style, so it
// follows BasedOnStyles and its own level again.
const unsetValue = "UNSET"

// noneValue is `BasedOnStyles = NONE` or `Vocab = NONE`, the spelled-out
// form of an empty value: nothing listed, and nothing inherited.
const noneValue = "NONE"

// baseStyles reads a BasedOnStyles key.
func baseStyles(key *ini.Key) []string {
	return valuesAfterNone(key)
}

// valuesAfterNone reads a list key. Its values arrive merged across the
// configuration's layers, so NONE clears whatever came before it and the
// last layer to say NONE wins.
func valuesAfterNone(key *ini.Key) []string {
	values := key.StringsWithShadows(",")
	for i := len(values) - 1; i >= 0; i-- {
		if strings.TrimSpace(values[i]) == noneValue {
			values = values[i+1:]
			break
		}
	}
	return mergeValues(values)
}

func validateLevel(key, val string, levels map[string]string) bool {
	options := []string{"YES", "suggestion", "warning", "error"}
	switch {
	case val == "NO":
		return false
	case !StringInSlice(val, options):
		// Read as off, as before, but said: a typo here silently disabled
		// the rule, and a misspelled key was never read at all.
		if strings.Contains(key, ".") {
			Warn(fmt.Sprintf("'%s = %s' isn't YES, NO, UNSET, or a level; Vale is switching the rule off.", key, val))
		} else {
			Warn(fmt.Sprintf("'%s' isn't a section option; Vale is ignoring it.", key))
		}
		return false
	case val != "YES":
		levels[key] = val
	}
	return true
}

var syntaxOpts = map[string]func(string, *ini.Section, *Config) error{
	"Vocab": func(lbl string, sec *ini.Section, cfg *Config) error {
		names := valuesAfterNone(sec.Key("Vocab"))
		for _, name := range names {
			if _, err := loadVocabulary(name, cfg); err != nil {
				return err
			}
			// The vocabulary's own Terms and Avoid run only where a section
			// names it; NewFile turns them on for its files.
			cfg.GChecks["Vale."+name+".Terms"] = false
			cfg.GChecks["Vale."+name+".Avoid"] = false
		}
		cfg.SVocab[lbl] = append(cfg.SVocab[lbl], names...)
		return nil
	},
	"BasedOnStyles": func(lbl string, sec *ini.Section, cfg *Config) error {
		g, _ := SplitSection(lbl)
		pat, err := glob.Compile(g)
		if err != nil {
			return NewE201FromTarget(
				fmt.Sprintf("The glob pattern '%s' could not be compiled.", lbl),
				lbl,
				cfg.Flags.Path)
		} else if _, found := cfg.SecToPat[lbl]; !found {
			cfg.SecToPat[lbl] = pat
		}
		sStyles := baseStyles(sec.Key("BasedOnStyles"))

		cfg.Styles = append(cfg.Styles, sStyles...)
		cfg.StyleKeys = append(cfg.StyleKeys, lbl)
		cfg.SBaseStyles[lbl] = sStyles

		return nil
	},
	"IgnorePatterns": func(label string, sec *ini.Section, cfg *Config) error {
		cfg.BlockIgnores[label] = sec.Key("IgnorePatterns").Strings(",")
		return nil
	},
	"BlockIgnores": func(label string, sec *ini.Section, cfg *Config) error {
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
	"TokenIgnores": func(label string, sec *ini.Section, cfg *Config) error {
		cfg.TokenIgnores[label] = patternsWithShadows(sec.Key("TokenIgnores"))
		return nil
	},
	"Transform": func(label string, sec *ini.Section, cfg *Config) error {
		candidate := sec.Key("Transform").String()
		cfg.Stylesheets[label] = system.DeterminePath(cfg.FS, cfg.ConfigFile(), candidate)
		return nil

	},
	"Lang": func(label string, sec *ini.Section, cfg *Config) error {
		cfg.FormatToLang[label] = sec.Key("Lang").String()
		return nil
	},
	"View": func(label string, sec *ini.Section, cfg *Config) error {
		name := sec.Key("View").String()

		path := FindConfigAsset(cfg, name+".yml", ViewDir)
		if path == "" {
			return fmt.Errorf("view '%s' not found", name)
		}

		view, err := NewView(path, cfg.FS)
		if err != nil {
			return err
		}

		cfg.Views[label] = view
		return nil
	},
}

var globalOpts = map[string]func(*ini.Section, *Config){
	"BasedOnStyles": func(sec *ini.Section, cfg *Config) {
		cfg.GBaseStyles = baseStyles(sec.Key("BasedOnStyles"))
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
	// The language of every file a section doesn't set one for.
	"Lang": func(sec *ini.Section, cfg *Config) error {
		cfg.FormatToLang["*"] = sec.Key("Lang").String()
		return nil
	},
	"StylesPath": func(sec *ini.Section, cfg *Config) error {
		paths := sec.Key("StylesPath").ValueWithShadows()
		for _, path := range paths {
			cfg.AddStylesPath(path)

			if !cfg.FS.Exists(path) {
				// The value was expanded on load, so the line is found by
				// the path it was written as.
				written := func(v string) bool {
					v = filepath.Clean(filepath.FromSlash(v))
					return v != "." && strings.HasSuffix(path, v)
				}
				return NewE201FromKey(cfg, "StylesPath", written,
					fmt.Sprintf("The StylesPath '%s' doesn't exist.", path))
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
	"IgnoredScopes": func(sec *ini.Section, cfg *Config) error {
		cfg.IgnoredScopes = mergeValues(sec.Key("IgnoredScopes").StringsWithShadows(","))
		return nil
	},
	"WordTemplate": func(sec *ini.Section, cfg *Config) error {
		cfg.WordTemplate = sec.Key("WordTemplate").String()
		return nil
	},
	"SkippedScopes": func(sec *ini.Section, cfg *Config) error {
		cfg.SkippedScopes = mergeValues(sec.Key("SkippedScopes").StringsWithShadows(","))
		return nil
	},
	"IgnoredClasses": func(sec *ini.Section, cfg *Config) error {
		cfg.IgnoredClasses = mergeValues(sec.Key("IgnoredClasses").StringsWithShadows(","))
		return nil
	},
	"Grammars": func(sec *ini.Section, cfg *Config) error {
		// Present but empty, `Grammars = NONE`, is an empty list rather
		// than nil: sync infers grammars only when the key is absent.
		cfg.Grammars = valuesAfterNone(sec.Key("Grammars"))
		return nil
	},
	"Vocab": func(sec *ini.Section, cfg *Config) error {
		cfg.Vocab = valuesAfterNone(sec.Key("Vocab"))
		for _, v := range cfg.Vocab {
			if err := loadVocab(v, cfg); err != nil {
				return err
			}
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
		// A configuration given as text has no file to resolve against.
		// Its paths are read as written: an absolute path is itself, and a
		// relative one is relative to wherever the host's filesystem puts
		// it, as `StylesPath = styles` over an in-memory filesystem is
		// `/styles`.
		return
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

// loadINISource parses an INI source: a path, read from the active
// filesystem, or the bytes of one. A missing file is empty when the options
// are Loose, as go-ini reads it.
func loadINISource(fsys system.FS, options ini.LoadOptions, source interface{}) (*ini.File, error) {
	if path, ok := source.(string); ok {
		b, err := fsys.ReadFile(path)
		if err != nil {
			if options.Loose && errors.Is(err, fs.ErrNotExist) {
				b = nil
			} else {
				return nil, err
			}
		}
		return ini.LoadSources(options, b)
	}
	return ini.LoadSources(options, source)
}

func shadowLoad(fsys system.FS, source interface{}, others ...interface{}) (*ini.File, error) {
	options := ini.LoadOptions{
		AllowShadows:             true,
		Loose:                    true,
		SpaceBeforeInlineComment: true,
		ChildSectionDelimiter:    noChildSections,
	}

	primary, err := loadINISource(fsys, options, source)
	if err != nil {
		return nil, err
	}
	expandPaths(primary, source)

	for _, other := range others {
		var shadow *ini.File

		shadow, err = loadINISource(fsys, options, other)
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
		return shadowLoad(cfg.FS, cfg.Flags.Path)
	}

	t := sources[1:]
	s := make([]interface{}, len(t))
	for i, v := range t {
		s[i] = v
	}

	uCfg, err = shadowLoad(cfg.FS, sources[0], s...)
	cfg.Flags.Path = sources[len(sources)-1]
	cfg.StyleSources = styleSources(cfg.FS, sources)

	return uCfg, err
}

// styleSources maps each section that sets BasedOnStyles to the last
// configuration file that sets it there.
func styleSources(fsys system.FS, sources []string) map[string]string {
	options := ini.LoadOptions{Loose: true, AllowShadows: true, ChildSectionDelimiter: noChildSections}
	out := map[string]string{}
	for _, src := range sources {
		f, err := loadINISource(fsys, options, src)
		if err != nil {
			continue
		}
		for _, sec := range f.Sections() {
			if sec.HasKey("BasedOnStyles") {
				out[sec.Name()] = src
			}
		}
	}
	return out
}

// packageOf returns the package a configuration file came from, or "" for
// one of the project's own: `vale sync` writes a package's `.vale.ini` to
// `.vale-config/<n>-<Name>.ini`.
func packageOf(path string) string {
	if filepath.Base(filepath.Dir(path)) != PipeDir {
		return ""
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if _, rest, found := strings.Cut(name, "-"); found {
		return rest
	}
	return name
}

// sphinxKeys are the keys a `[sphinx]` section may set: the directives whose
// body is code rather than prose, and the roles whose text is prose rather
// than an identifier. Both the reStructuredText and the MyST reader use them.
var sphinxKeys = []string{"CodeDirectives", "ProseRoles"}

func processConfig(uCfg *ini.File, cfg *Config, dry bool) (*ini.File, error) {
	core := uCfg.Section("")
	global := uCfg.Section("*")

	formats := uCfg.Section("formats")
	adoc := uCfg.Section("asciidoctor")
	sphinx := uCfg.Section("sphinx")

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

	// A Sphinx project's directives and roles
	for _, k := range sphinx.KeyStrings() {
		if !StringInSlice(k, sphinxKeys) {
			Warn(fmt.Sprintf("'%s' isn't a sphinx option; Vale is ignoring it.", k))
			continue
		}
		cfg.Sphinx[k] = sphinx.Key(k).String()
	}

	// Global settings
	for _, k := range global.KeyStrings() {
		if k == "Vocab" {
			continue // read below, once every rule setting is known
		} else if f, found := globalOpts[k]; found {
			// Before the core check: `Lang` is both, and `[*]` is one of
			// its places.
			f(global, cfg)
		} else if _, found = syntaxOpts[k]; found {
			msg := fmt.Sprintf("'%s' is a syntax-specific option", k)
			return nil, NewE201FromTarget(msg, k, cfg.RootINI)
		} else if isParam, pErr := asRuleParam(k, lastValue(global.Key(k)), cfg); pErr != nil {
			return nil, pErr
		} else if lastValue(global.Key(k)) == unsetValue {
			// Nothing precedes the global section, so there is nothing to unset.
			continue
		} else if !isParam {
			cfg.GChecks[k] = validateLevel(k, lastValue(global.Key(k)), cfg.RuleToLevel)
			cfg.Checks = append(cfg.Checks, k)
		}
	}

	// `[*]` names vocabularies for every file, with rules of their own that
	// are on unless a setting in `[*]` says otherwise.
	if global.HasKey("Vocab") {
		names := valuesAfterNone(global.Key("Vocab"))
		for _, name := range names {
			if _, err := loadVocabulary(name, cfg); err != nil && !dry {
				return nil, err
			}
			for _, rule := range []string{".Terms", ".Avoid"} {
				if _, set := cfg.GChecks["Vale."+name+rule]; !set {
					cfg.GChecks["Vale."+name+rule] = true
				}
			}
		}
		cfg.SVocab["*"] = names
	}

	// Syntax-specific settings
	for _, sec := range uCfg.SectionStrings() {
		if StringInSlice(sec, []string{"*", "DEFAULT", "formats", "asciidoctor", "sphinx"}) {
			continue
		}

		// `[*.md if .Meta.draft]` applies to the files its glob matches for
		// which its condition holds.
		g, cond := SplitSection(sec)
		pat, err := glob.Compile(g)
		if err != nil {
			return nil, err
		}
		cfg.SecToPat[sec] = pat
		if cond != "" {
			c, cErr := NewCondition(cond)
			if cErr != nil {
				return nil, NewE201FromTarget(fmt.Sprintf("[%s]: %s", sec, cErr), sec, cfg.RootINI)
			}
			cfg.SecToCond[sec] = c
		}

		syntaxMap := make(map[string]bool)
		levelMap := make(map[string]string)
		for _, k := range uCfg.Section(sec).KeyStrings() {
			if f, found := syntaxOpts[k]; found {
				if err = f(sec, uCfg.Section(sec), cfg); err != nil && !dry {
					return nil, err
				}
			} else if _, option := coreOpts[k]; option {
				return nil, NewE201FromTarget(fmt.Sprintf(coreError, k), k, cfg.RootINI)
			} else if isParam, pErr := asRuleParam(k, lastValue(uCfg.Section(sec).Key(k)), cfg); pErr != nil {
				return nil, pErr
			} else if lastValue(uCfg.Section(sec).Key(k)) == unsetValue {
				cfg.SUnsets[sec] = append(cfg.SUnsets[sec], k)
				cfg.Checks = append(cfg.Checks, k)
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
