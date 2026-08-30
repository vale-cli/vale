package check

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/mitchellh/mapstructure"
	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/spell"
	"github.com/vale-cli/vale/v3/internal/system"
)

var defaultFilters = []*regexp.Regexp{
	regexp.MustCompile(`[A-Z]{1}[a-z]+[A-Z]+\w+`),
	regexp.MustCompile(`[\p{Lu}]+$`),
	regexp.MustCompile(`[^\p{L}_']`),
}

// Spelling checks text against a Hunspell dictionary.
type Spelling struct {
	Definition `mapstructure:",squash"`
	Filters    []*regexp.Regexp

	// stdFilters applies the built-in filters, which are hand-written rather
	// than compiled. Set unless the rule declares `custom: true`.
	stdFilters   bool
	Ignore       []string
	Exceptions   []string
	Dictionaries []string
	Aff          string
	Dic          string
	Dicpath      string
	Threshold    int
	exceptRe     *rx.Regexp
	phraseRe     *rx.Regexp
	gs           *spell.Checker
	Custom       bool
	Append       bool
}

func addFilters(s *Spelling, generic baseCheck, _ *core.Config) error {
	if generic["filters"] != nil {
		// We pre-compile user-provided filters for efficiency.
		//
		// NOTE: This makes a big difference: ~50s -> ~13s.
		for _, filter := range generic["filters"].([]interface{}) {
			pat, err := regexp.Compile(filter.(string))
			if err != nil {
				return err
			}
			s.Filters = append(s.Filters, pat)
		}
		delete(generic, "filters")
	}
	return nil
}

func addExceptions(s *Spelling, generic baseCheck, cfg *core.Config) error { //nolint:unparam
	if generic["ignore"] != nil {
		// Backwards compatibility: we need to be able to accept a single
		// or an array.
		if reflect.TypeOf(generic["ignore"]).String() == "string" {
			s.Ignore = append(s.Ignore, generic["ignore"].(string))
		} else {
			for _, ignore := range generic["ignore"].([]interface{}) {
				s.Ignore = append(s.Ignore, ignore.(string))
			}
		}
		delete(generic, "ignore")
	}

	for _, term := range cfg.AcceptedTokens {
		// NOTE: This is used to ensure that we are excluding whole words
		// rather than substrings.
		//
		// The assumption is that, for spell checking, we don't want to
		// flag words that are part of a larger word.
		if !strings.HasPrefix(term, "\b") && !strings.HasSuffix(term, "\b") {
			term = `\b` + term + `\b`
		}
		s.Exceptions = append(s.Exceptions, term)
		s.exceptRe = rx.MustCompile(
			ignoreCase + strings.Join(s.Exceptions, "|"))
	}

	// A multi-word term (e.g. `mea culpa`) is accepted only as a phrase; its
	// component words are still spell-checked on their own. We mask these in
	// `Run` via `phraseRe`, built from the same vocabulary as every other
	// Vocab-aware rule. See #1035.
	s.phraseRe = buildPhraseRe(nil, cfg.AcceptedTokens, true)

	return nil
}

// NewSpelling creates a new `spelling`-based rule.
func NewSpelling(cfg *core.Config, generic baseCheck, path string) (Spelling, error) {
	var model *spell.Checker

	rule := Spelling{}
	name, _ := generic["name"].(string)

	err := addFilters(&rule, generic, cfg)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = addExceptions(&rule, generic, cfg)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = mapstructure.WeakDecode(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	model, err = makeSpeller(&rule, cfg, path)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}

	if name == "Vale.Spelling" {
		// NOTE: For `Vale.Spelling`, there's no way to define specific
		// ignore files, so we just check the default `config/ignore`
		// directory.
		//
		// We **can't** add vocabularies here because `AddWordListFile`
		// doesn't support regex.
		ignored, readErr := core.IgnoreFiles(cfg.StylesPath())
		if readErr != nil {
			return rule, readErr
		}

		for _, file := range ignored {
			if err = model.AddWordListFile(file); err != nil {
				return rule, err
			}
		}
	} else {
		for _, ignore := range rule.Ignore {
			fullPath, _ := filepath.Abs(ignore)

			// There are a few cases we need to consider:
			paths := []string{
				// 1. An absolute path (similar to $DICPATH)
				fullPath,
				// 2. Relative to StylesPath
				filepath.Join(cfg.StylesPath(), ignore),
				// 3. Relative to config/ignore
				filepath.Join(cfg.StylesPath(), core.IgnoreDir, ignore),
			}

			for _, p := range paths {
				if err = model.AddWordListFile(p); err != nil && system.FileExists(p) {
					return rule, err
				}
			}
		}
	}

	if !rule.Custom {
		// The defaults are answered by hand rather than by regex -- see
		// spellfilter.go. They are not appended to Filters, so a rule that also
		// declares its own filters runs those as regexes and these as code.
		rule.stdFilters = true
	}
	rule.gs = model

	return rule, nil
}

// Run performs spell-checking on the provided text.
func (s Spelling) Run(blk nlp.Block, _ *core.File, _ *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert

	// Mask any accepted multi-word phrases (e.g. `mea culpa`) so their
	// component words aren't spell-checked individually, while the same words
	// elsewhere still are. We replace each match with an equal-length run of
	// spaces, which preserves the byte offsets of every other word. See #1035.
	//
	// This masks the block's own text rather than a converted copy, so that
	// every offset below is one into `blk.Text` and needs no translation.
	checkTxt := blk.Text
	if s.phraseRe != nil {
		masked, err := s.phraseRe.ReplaceFunc(blk.Text, func(m rx.Match) string {
			return strings.Repeat(" ", len(m.String()))
		}, -1, -1)
		if err == nil {
			checkTxt = masked
		}
	}

	// Each word's position comes back with it, which is what lets the alert be
	// placed by arithmetic instead of by searching the whole context for its
	// text once per alert. Searching also could not tell one occurrence of a
	// repeated word from another, and so reported the first one every time.
	words, offsets := nlp.WordTokenizer.TokenizeWithOffsets(checkTxt)

OUTER:
	for i, found := range words {
		// This ensures that we respect `.aff` entries like `ICONV ’ '`,
		// allowing us to avoid false positives.
		//
		// It is applied per word, which is where hunspell applies it, and what
		// keeps the offsets above measured against the text as written: the
		// conversions change length, so a position taken from a converted copy
		// of the block drifts further out of place with every one that fires.
		//
		// See https://github.com/errata-ai/vale/v2/issues/148.
		word := s.gs.Convert(found)

		if s.stdFilters && skippedByDefault(word) {
			continue
		}
		for _, filter := range s.Filters {
			if filter.MatchString(word) {
				continue OUTER
			}
		}

		if !s.gs.Spell(word) && !isMatch(s.exceptRe, word) {
			// The extent is the word as it appears, not as it converts: the
			// offset is a position in the block's own text, so the length that
			// goes with it has to be measured there too.
			offset := offsets[i]
			loc := []int{offset, offset + len(found)}

			a := core.Alert{Check: s.Name, Severity: s.Level, Span: loc,
				Link: s.Link, Match: word, Action: s.Action}

			a.Message, a.Description = formatMessages(s.Message,
				s.Description, word)

			alerts = append(alerts, a)
		}
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (s Spelling) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s Spelling) Pattern() string {
	return ""
}

// Pattern is the internal regex pattern used by this rule.
func (s Spelling) Suggest(word string) []string {
	return s.gs.Suggest(word)
}

func makeSpeller(s *Spelling, cfg *core.Config, rulePath string) (*spell.Checker, error) {
	var options []spell.CheckerOption
	var found bool

	affloc := core.FindAsset(cfg, s.Aff)
	dicloc := core.FindAsset(cfg, s.Dic)

	if system.FileExists(affloc) && system.FileExists(dicloc) {
		return spell.NewChecker(spell.UsingDictionaryByPath(dicloc, affloc))
	}

	options = append(options, spell.WithDefault(s.Append))
	if s.Dicpath != "" {
		cwd, _ := os.Getwd()

		// There are a few cases we need to consider:
		paths := []string{
			// 1. An absolute path (similar to $DICPATH)
			s.Dicpath,
			// 2. Relative to StylesPath
			filepath.Join(cfg.StylesPath(), s.Dicpath),
			// 4. Relative to cwd
			filepath.Join(cwd, s.Dicpath),
		}

		for _, p := range paths {
			if system.IsDir(p) {
				options = append(options, spell.WithPath(p))
				found = true
				break
			}
		}

		if !found {
			return nil, errors.New("unable to resolve dicpath")
		}
	} else {
		options = append(options, spell.WithPath(
			filepath.Join(cfg.StylesPath(), core.DictDir)))
	}

	if len(s.Dictionaries) > 0 {
		for _, name := range s.Dictionaries {
			options = append(options, spell.UsingDictionary(name))
		}
		return spell.NewChecker(options...)
	}

	if rulePath == "internal" {
		// NOTE: New in v3.0 -- if we aren't given a `dicpath` or specific
		// dictionaries, we use the default one.
		options = append(options, spell.WithDefaultPath(
			filepath.Join(cfg.StylesPath(), core.DictDir)))
	}

	return spell.NewChecker(options...)
}
