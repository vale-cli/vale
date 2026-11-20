package spell

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"

	"github.com/vale-cli/vale/v3/internal/system"
)

//go:embed data/en_US-web.aff
var defaultAff []byte

// ErrNoDictionary reports a spelling check that needs the default
// dictionary in a build without one (vale_nodict).
var ErrNoDictionary = errors.New("this build has no default dictionary")

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
	fs       system.FS
	checkers []*goSpell
	recased  *sync.Map // word -> its other-case spelling, or ""
}

// NewChecker creates a spell checker from multiple
// Hunspell-compatible dictionaries.
func NewChecker(options ...CheckerOption) (*Checker, error) {
	return NewCheckerFS(system.Default, options...)
}

// fsys is the filesystem the checker reads from: its own, or system.Default
// for a Checker built without one.
func (m *Checker) fsys() system.FS {
	if m.fs == nil {
		return system.Default
	}
	return m.fs
}

// NewCheckerFS is NewChecker against a filesystem of its own.
func NewCheckerFS(fsys system.FS, options ...CheckerOption) (*Checker, error) {
	base := defaultOpts
	for _, applyOpt := range options {
		applyOpt(&base)
	}

	checker := Checker{options: base, fs: fsys, recased: &sync.Map{}}
	for _, name := range base.names {
		if err := checker.loadDic(name); err != nil {
			return &checker, err
		}
	}

	for _, entry := range base.dics {
		c, err := sharedDictionaryFS(fsys, entry.aff, entry.dic)
		if err != nil {
			return &checker, err
		}
		checker.checkers = append(checker.checkers, c)
	}

	if len(checker.checkers) == 0 || base.load {
		// use default dictionary ...
		if len(defaultDic) == 0 {
			return &checker, ErrNoDictionary
		}
		c, err := sharedDictionary("embedded\x00en_US-web", func() (*goSpell, error) {
			return newGoSpellReader(bytes.NewReader(defaultAff), bytes.NewReader(defaultDic))
		})
		if err != nil {
			return &checker, err
		}

		checker.checkers = append(checker.checkers, c)
	}

	if base.defaultPath != "" {
		// load all dictionaries from the given path ...
		files, err := fsys.Glob(base.defaultPath + "/*.dic")
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

// Ignored names the `.aff` directives the loaded dictionaries use that the
// checker does not implement, sorted.
func (m *Checker) Ignored() []string {
	seen := map[string]struct{}{}
	var names []string
	for _, checker := range m.checkers {
		for _, name := range checker.ignored {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

// dictionaries holds each loaded dictionary by its source, so that every
// spelling rule that names the same files shares one copy.
var dictionaries sync.Map // key -> *goSpell, pristine

// sharedDictionary returns a checker over the dictionary key names, loading
// it once.
// sharedDictionaryFS is sharedDictionary for the dictionary at affPath and
// dicPath on fsys, keyed by the files' content: two filesystems can hold
// different dictionaries at the same path, and each gets its own.
func sharedDictionaryFS(fsys system.FS, affPath, dicPath string) (*goSpell, error) {
	aff, err := fsys.ReadFile(affPath)
	if err != nil {
		return nil, fmt.Errorf("unable to open aff: %s", err.Error())
	}
	dic, err := fsys.ReadFile(dicPath)
	if err != nil {
		return nil, fmt.Errorf("unable to open dic: %s", err.Error())
	}
	sum := sha256.New()
	sum.Write(aff)
	sum.Write([]byte{0})
	sum.Write(dic)
	key := "content\x00" + hex.EncodeToString(sum.Sum(nil))
	return sharedDictionary(key, func() (*goSpell, error) {
		return newGoSpellReader(bytes.NewReader(aff), bytes.NewReader(dic))
	})
}

func sharedDictionary(key string, load func() (*goSpell, error)) (*goSpell, error) {
	if pristine, ok := dictionaries.Load(key); ok {
		return pristine.(*goSpell).fork(), nil //nolint:errcheck // only *goSpell is stored
	}
	pristine, err := load()
	if err != nil {
		return nil, err
	}
	actual, _ := dictionaries.LoadOrStore(key, pristine)
	return actual.(*goSpell).fork(), nil //nolint:errcheck // only *goSpell is stored
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

// Recase returns the spelling a dictionary has for word in another case,
// when there is exactly one: GitHub for github.
func (m *Checker) Recase(word string) (string, bool) {
	if v, ok := m.recased.Load(word); ok {
		cased := v.(string) //nolint:errcheck // only strings are stored
		return cased, cased != ""
	}
	cased := ""
	for _, checker := range m.checkers {
		if c, ok := checker.recase(word); ok {
			cased = c
			break
		}
	}
	m.recased.Store(word, cased)
	return cased, cased != ""
}

// Suggest returns a list of suggestions for a given word.
func (m *Checker) Suggest(word string) []string {
	suggestions := []string{}
	for _, r := range m.Rank(word) {
		suggestions = append(suggestions, r.Word)
	}
	return suggestions
}

// A Suggestion is a candidate spelling and how close it is to the word, on
// the scale Similarity uses.
type Suggestion struct {
	Word  string
	Score float64
}

// Rank returns the closest spellings of word across the dictionaries, best
// first, at most six.
func (m *Checker) Rank(word string) []Suggestion {
	ranks := []wordMatch{}
	for _, checker := range m.checkers {
		ranks = append(ranks, checker.suggest(word)...)
	}

	sort.SliceStable(ranks, func(i, j int) bool {
		return ranks[i].score > ranks[j].score
	})

	suggestions := []Suggestion{}
	for i, r := range ranks {
		if i > 5 {
			break
		}
		suggestions = append(suggestions, Suggestion{r.word, r.score})
	}
	return suggestions
}

// Similarity is the closeness of two spellings, from 0 to 1, as the
// suggester measures it.
func Similarity(a, b string) float64 {
	return strutil.Similarity(a, b, metrics.NewLevenshtein())
}

// Dict returns the underlying dictionary for the provided index.
func (m *Checker) Dict(i int) map[string]struct{} {
	words := make(map[string]struct{}, len(m.checkers[i].roots))
	for word := range m.checkers[i].roots {
		words[word] = struct{}{}
	}
	return words
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
		_, err := checker.addWordListFile(m.fsys(), name)
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
		if m.fsys().Exists(option) {
			return option, nil
		}

		// The asset may be a symlink (e.g. a Nix store path exposed via
		// $DICPATH). A failed Readlink just means this root doesn't have it,
		// so keep trying the remaining roots instead of bailing out -- the
		// early return here previously made Vale ignore $DICPATH (#1014).
		if ln, err := m.fsys().Readlink(option); err == nil && m.fsys().Exists(ln) {
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

	affPath, err := m.readAsset(name + ".aff")
	if err != nil {
		return err
	}

	s, err := sharedDictionaryFS(m.fsys(), affPath, dicPath)
	if err != nil {
		return err
	}
	m.checkers = append(m.checkers, s)

	return nil
}
