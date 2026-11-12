package check

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/spell"
)

// A rule built for English says so once per run when it meets another
// language, rather than once per file.
var warned sync.Map

func warnOnce(key, msg string) {
	if _, seen := warned.LoadOrStore(key, true); !seen {
		core.Warn(msg)
	}
}

// languageSpellers holds one checker per language, built on first use from
// the dictionary the StylesPath has for it.
var languageSpellers sync.Map // lang -> *spell.Checker, or nil for none

// spellerFor returns the dictionary-backed checker for a language other
// than English, or nil when the StylesPath has no dictionary for it.
//
// A dictionary is `config/dictionaries/<name>.dic` with its `.aff`; see
// core.DictionaryNames for what name may be.
func spellerFor(cfg *core.Config, lang string) *spell.Checker {
	key := cfg.StylesPath() + "\x00" + lang
	if c, ok := languageSpellers.Load(key); ok {
		return c.(*spell.Checker) //nolint:errcheck // only checkers are stored
	}

	var checker *spell.Checker
	if dic, aff := core.DictionaryFor(cfg, lang); dic != "" {
		c, err := spell.NewCheckerFS(cfg.FS, spell.WithDefault(false), spell.UsingDictionaryByPath(dic, aff))
		if err != nil {
			warnOnce("dict:"+key, fmt.Sprintf("the dictionary for '%s' at %s did not load: %s", lang, filepath.Dir(dic), err))
		} else {
			if ignored, iErr := core.IgnoreFiles(cfg.FS, cfg.StylesPath()); iErr == nil {
				for _, file := range ignored {
					_ = c.AddWordListFile(file)
				}
			}
			checker = c
		}
	}

	languageSpellers.Store(key, checker)
	return checker
}

// nounsIn tags text in its language and returns the words a sentence-case
// convention leaves capitalized there: nouns, in German, and names.
func nounsIn(text string, info *nlp.Info) ([]string, error) {
	tokens, err := nlp.TextToTokens(text, info)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range tokens {
		if t.Tag == "NOUN" || t.Tag == "PROPN" {
			out = append(out, t.Text)
		}
	}
	return out, nil
}
