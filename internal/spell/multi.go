package spell

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/errata-ai/vale/v3/internal/system"
)

//go:embed data/en_US-web.aff
var defaultAff []byte

//go:embed data/en_US-web.dic
var defaultDic []byte

var defaultOpts = Options{
	path: os.Getenv("DICPATH"),
	load: false,

	system: os.Getenv("DICPATH"),
}

// Options controls the checker-creation process:
type Options struct {
	path        string
	defaultPath string
	system      string
	names       []string
	dics        []dictionary
	load        bool
}

// A CheckerOption is a setting that changes the checker-creation process.
type CheckerOption func(opts *Options)

// WithPath specifies the location of Hunspell-compatible dictionaries.
func WithPath(path string) CheckerOption {
	return func(opts *Options) {
		opts.path = path
	}
}

// WithDefault specifies if Vale's default dictionary should be loaded.
func WithDefault(load bool) CheckerOption {
	return func(opts *Options) {
		opts.load = load
	}
}

// WithDefaultPath specifies a path from which all dictionaries should be
// loaded.
func WithDefaultPath(path string) CheckerOption {
	return func(opts *Options) {
		opts.defaultPath = path
	}
}

// UsingDictionary loads the given Hunspell-compatible dictionary.
func UsingDictionary(name string) CheckerOption {
	return func(opts *Options) {
		opts.names = append(opts.names, name)
	}
}

// UsingDictionaryByPath loads the given Hunspell-compatible dictionary using
// the given local paths.
func UsingDictionaryByPath(dic, aff string) CheckerOption {
	return func(opts *Options) {
		opts.dics = append(opts.dics, dictionary{dic, aff})
	}
}

// Checker is a spell-checker based on multiple dictionaries.
type Checker struct {
	options  Options
	checkers []*goSpell
}

// NewChecker creates a spell checker from multiple
// Hunspell-compatible dictionaries.
func NewChecker(options ...CheckerOption) (*Checker, error) {
	base := defaultOpts
	for _, applyOpt := range options {
		applyOpt(&base)
	}

	checker := Checker{options: base}
	for _, name := range base.names {
		if err := checker.loadDic(name); err != nil {
			return &checker, err
		}
	}

	for _, entry := range base.dics {
		c, err := newGoSpell(entry.aff, entry.dic)
		if err != nil {
			return &checker, err
		}
		checker.checkers = append(checker.checkers, c)
	}

	if len(checker.checkers) == 0 || base.load {
		// use default dictionary ...
		aff := bytes.NewReader(defaultAff)
		dic := bytes.NewReader(defaultDic)

		c, err := newGoSpellReader(aff, dic)
		if err != nil {
			return &checker, err
		}

		checker.checkers = append(checker.checkers, c)
	}

	if base.defaultPath != "" {
		// load all dictionaries from the given path ...
		files, err := filepath.Glob(base.defaultPath + "/*.dic")
		if err != nil {
			return &checker, err
		}

		for _, f := range files {
			name := filepath.Base(f)
			name = name[:len(name)-4]
			if loadErr := checker.loadDic(name); loadErr != nil {
				return &checker, loadErr
			}
		}
	}

	return &checker, nil
}

// Spell checks to see if a given word is in the internal dictionaries.
func (m *Checker) Spell(word string) bool {
	for _, checker := range m.checkers {
		if checker.spell(word) {
			return true
		}
	}
	return false
}

// Suggest returns a list of suggestions for a given word.
func (m *Checker) Suggest(word string) []string {
	ranks := []wordMatch{}
	for _, checker := range m.checkers {
		ranks = append(ranks, checker.suggest(word)...)
	}

	sort.Slice(ranks, func(i, j int) bool {
		return ranks[i].score > ranks[j].score
	})

	suggestions := []string{}
	for i, r := range ranks {
		if i > 5 {
			break
		}
		suggestions = append(suggestions, r.word)
	}

	return suggestions
}

// Dict returns the underlying dictionary for the provided index.
func (m *Checker) Dict(i int) map[string]struct{} {
	return m.checkers[i].dict
}

// Convert performs character substitutions (ICONV).
func (m *Checker) Convert(s string) string {
	for _, checker := range m.checkers {
		s = checker.inputConversion([]byte(s))
	}
	return s
}

// AddWordListFile reads in a word list file
func (m *Checker) AddWordListFile(name string) error {
	for _, checker := range m.checkers {
		_, err := checker.addWordListFile(name)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Checker) readAsset(name string) (string, error) {
	roots := []string{
		m.options.defaultPath,
		m.options.path,
		m.options.system,
	}

	for _, p := range roots {
		if p == "" {
			continue
		}

		option := filepath.Join(p, name)
		if system.FileExists(option) {
			return option, nil
		}

		// The asset may be a symlink (e.g. a Nix store path exposed via
		// $DICPATH). A failed Readlink just means this root doesn't have it,
		// so keep trying the remaining roots instead of bailing out -- the
		// early return here previously made Vale ignore $DICPATH (#1014).
		if ln, err := os.Readlink(option); err == nil && system.FileExists(ln) {
			return ln, nil
		}
	}

	return "", fmt.Errorf("'%s' not found in %v", name, roots)
}

func (m *Checker) loadDic(name string) error {
	dicPath, err := m.readAsset(name + ".dic")
	if err != nil {
		return err
	}

	dic, err := os.Open(dicPath)
	if err != nil {
		return err
	}

	affPath, err := m.readAsset(name + ".aff")
	if err != nil {
		return err
	}

	aff, err := os.Open(affPath)
	if err != nil {
		return err
	}

	s, err := newGoSpellReader(aff, dic)
	if err != nil {
		return err
	}
	m.checkers = append(m.checkers, s)

	return nil
}
