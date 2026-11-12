package core

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Languages lists the languages the configuration names with `Lang`, other
// than English, sorted. Each is lower-cased with a hyphen, as `pt-br`.
func (c *Config) Languages() []string {
	seen := map[string]bool{}
	for _, lang := range c.FormatToLang {
		lang = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
		if lang == "" || nlp.IsEnglish(lang) {
			continue
		}
		seen[lang] = true
	}

	out := make([]string, 0, len(seen))
	for lang := range seen {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

// Files a language's model directory may hold; see nlp.LanguageModel.
const (
	taggerFile       = "tagger.bin"
	segmenterFile    = "segmenter.json.gz"
	abbreviationFile = "abbreviations.txt"
)

// LoadLanguageModels hands nlp the models for every language the
// configuration names, read from config/models/<lang> through the
// configuration's own filesystem -- which is how the browser build, with no
// disk, gets them too.
//
// A language with no directory is left alone: it may be one prose segments
// by rules alone, and a rule that needs its tags reports the missing model
// when it runs.
func LoadLanguageModels(cfg *Config) error {
	for _, lang := range cfg.Languages() {
		dir := modelDir(cfg, lang)
		if dir == "" {
			continue
		}

		var model nlp.LanguageModel
		var err error
		if model.Tagger, err = readOptional(cfg, filepath.Join(dir, taggerFile)); err != nil {
			return err
		}
		if model.Segmenter, err = readOptional(cfg, filepath.Join(dir, segmenterFile)); err != nil {
			return err
		}
		abbrevs, err := readOptional(cfg, filepath.Join(dir, abbreviationFile))
		if err != nil {
			return err
		}
		model.Abbreviations = strings.Fields(string(abbrevs))

		nlp.RegisterLanguage(lang, model)
	}
	return nil
}

// modelDir finds the model directory for lang: the full tag's, or the base
// language's for a tag with a region ("pt-br" reads "pt").
func modelDir(cfg *Config, lang string) string {
	if dir := FindConfigAsset(cfg, lang, ModelDir); dir != "" {
		return dir
	}
	if base, _, ok := strings.Cut(lang, "-"); ok {
		return FindConfigAsset(cfg, base, ModelDir)
	}
	return ""
}

// readOptional reads a file that need not exist.
func readOptional(cfg *Config, path string) ([]byte, error) {
	if !cfg.FS.Exists(path) {
		return nil, nil
	}
	return cfg.FS.ReadFile(path)
}

// DictionaryNames lists the file names a language's Hunspell dictionary may
// go by under config/dictionaries: the tag itself, Hunspell's underscore
// form with the region in capitals, the base language, and the base doubled,
// as de_DE.
func DictionaryNames(lang string) []string {
	lang = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
	base, region, _ := strings.Cut(lang, "-")
	names := []string{lang}
	if region != "" {
		names = append(names, base+"_"+strings.ToUpper(region), base)
	}
	return append(names, base+"_"+strings.ToUpper(base))
}

// DictionaryFor finds a language's dictionary on the StylesPath: the paths
// of its .dic and .aff files, or two empty strings.
func DictionaryFor(cfg *Config, lang string) (string, string) {
	for _, name := range DictionaryNames(lang) {
		dic := FindConfigAsset(cfg, name+".dic", DictDir)
		aff := FindConfigAsset(cfg, name+".aff", DictDir)
		if dic != "" && aff != "" {
			return dic, aff
		}
	}
	return "", ""
}
