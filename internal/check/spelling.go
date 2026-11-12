package check

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mitchellh/mapstructure"
	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/spell"
)

// defaultFilters is the reference the scans in spellfilter.go are tested
// against; the linter runs the scans.
var defaultFilters = []*regexp.Regexp{
	regexp.MustCompile(`[A-Z]{1}[a-z]+[A-Z]+\w+`),
	regexp.MustCompile(`[A-Z]+$`),
	regexp.MustCompile(`[^a-zA-Z_']`),
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

	// byLanguage is set when the rule names no dictionary of its own, so it
	// reads the built-in English one, and a file in another language reads
	// that language's instead.
	byLanguage bool

	// `split` (`bool`): Check the parts of an identifier -- `recieveMessage`,
	// `recieve_message`, `RecieveMessage` -- rather than skipping it or
	// checking it whole. A part is reported at its own position.
	Split bool
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
		term = termPattern(term)
		if !strings.HasPrefix(term, "\b") && !strings.HasSuffix(term, "\b") {
			term = `\b` + term + `\b`
		}
		s.Exceptions = append(s.Exceptions, term)
	}
	if len(s.Exceptions) > 0 {
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
		ignored, readErr := core.IgnoreFiles(cfg.FS, cfg.StylesPath())
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
				if err = model.AddWordListFile(p); err != nil && cfg.FS.Exists(p) {
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
	rule.byLanguage = len(rule.Dictionaries) == 0 && rule.Dic == "" && rule.Dicpath == ""

	return rule, nil
}

// Run performs spell-checking on the provided text.
func (s Spelling) Run(blk nlp.Block, f *core.File, cfg *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert
	vocab := vocabFor(cfg, f)

	// The built-in dictionary is English; a file in another language reads
	// the one its StylesPath has for it, or nothing, since checking German
	// against en_US flags every word.
	if s.byLanguage && f != nil && !nlp.IsEnglish(f.NLP.Lang) {
		gs := spellerFor(cfg, f.NLP.Lang)
		if gs == nil {
			// The directory is shown with slashes on every platform: it is
			// a place in the StylesPath, not a path on this machine.
			warnOnce(s.Name+":"+f.NLP.Lang, fmt.Sprintf(
				"%s has no dictionary for '%s'; add <name>.dic and <name>.aff under %s, or set %s = NO",
				s.Name, f.NLP.Lang, filepath.ToSlash(core.DictDir), s.Name))
			return alerts, nil
		}
		s.gs = gs
	}

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

		if s.stdFilters && s.skipped(word) {
			continue
		}
		for _, filter := range s.Filters {
			if filter.MatchString(word) {
				continue OUTER
			}
		}

		if s.gs.Spell(word) || isMatch(s.exceptRe, word) || vocab.accepts(word, checkTxt, []int{offsets[i], offsets[i] + len(found)}) {
			continue
		}

		// The extent is the word as it appears, not as it converts: the
		// offset is a position in the block's own text, so the length that
		// goes with it has to be measured there too.
		offset := offsets[i]
		if s.Split {
			// A plain word is reported whole; anything else is an identifier,
			// and only its parts are.
			if parts := splitIdentifier(found); len(parts) != 1 || parts[0].text != found {
				for _, part := range parts {
					if s.checkPart(part.text) || vocab.accepts(part.text, "", nil) {
						continue
					}
					a := s.alert(part.text, offset+part.at, len(part.text))
					// A part is not a whole word, so a search for it would
					// land elsewhere; the block knows where it is. Where it
					// does not, the whole identifier is reported instead.
					if at := blk.SourceOffset(offset + part.at); at >= 0 {
						a.Span = []int{at, at + len(part.text)}
						a.HasByteOffsets = true
					} else {
						a.Match, a.Span = found, []int{offset, offset + len(found)}
					}
					alerts = append(alerts, a)
				}
				continue
			}
		}
		a := s.alert(word, offset, len(found))
		s.explain(&a, word, found, f)
		// The block knows where it is, so the word need not be searched
		// for, which found an earlier copy of it or one in markup.
		if at := blk.SourceOffset(offset); at >= 0 {
			a.Span = []int{at, at + len(found)}
			a.HasByteOffsets = true
		}
		alerts = append(alerts, a)
	}

	return alerts, nil
}

// skipped reports whether the built-in filters skip a word. With `split`,
// an identifier is not skipped but taken apart, so only a word with a
// character no identifier holds is.
func (s Spelling) skipped(word string) bool {
	if s.Split {
		return skipsNonIdentifier(word)
	}
	return skippedByDefault(word)
}

// checkPart reports whether one part of an identifier passes: a short part
// or an acronym is not checked.
func (s Spelling) checkPart(part string) bool {
	letters := 0
	for _, r := range part {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < minPartLetters || strings.ToUpper(part) == part {
		return true
	}
	return s.gs.Spell(s.gs.Convert(part)) || isMatch(s.exceptRe, part)
}

// minPartLetters is the shortest part of an identifier that is checked.
const minPartLetters = 3

// alert reports a misspelling at a position in the block.
func (s Spelling) alert(word string, at, length int) core.Alert {
	a := core.Alert{Check: s.Name, Severity: s.Level, Span: []int{at, at + length},
		Link: s.Link, Match: word, Action: s.Action}
	a.Message, a.Description = formatMessages(s.Message, s.Description, word)
	return a
}

// inlineCode wraps text as inline code, by format.
var inlineCode = map[string]string{
	".md": "`%s`", ".mdx": "`%s`", ".myst": "`%s`", ".qmd": "`%s`", ".ipynb": "`%s`",
	".adoc": "`%s`", ".typ": "`%s`", ".rst": "``%s``", ".org": "~%s~",
	".html": "<code>%s</code>",
}

// identifierShape matches snake_case, and lowerCamel with two or more
// leading lower-case letters: `apiVersion`, but not `vSphere` or `iPhone`,
// which are names.
var identifierShape = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+|[a-z]{2,}[A-Z][A-Za-z0-9]*)$`)

// explain rewrites a misspelling alert when more is known about the word,
// with a fix that is certain: the dictionary spells it in another case, or
// it is shaped like an identifier in a format with inline code.
func (s Spelling) explain(a *core.Alert, word, found string, f *core.File) {
	if cased, ok := s.gs.Recase(word); ok && certainRecase(word, cased) {
		a.Message = fmt.Sprintf("Use '%s' instead of '%s'.", cased, found)
		a.Action = core.Action{Name: "replace", Params: []string{cased}}
		a.Suggestions = a.Action.Params
		return
	}

	if s.Split || f == nil || !identifierShape.MatchString(found) {
		return
	}
	if wrap, ok := inlineCode[f.NormedExt]; ok {
		code := fmt.Sprintf(wrap, found)
		a.Message = fmt.Sprintf("'%s' looks like code; format it as code.", found)
		a.Action = core.Action{Name: "replace", Params: []string{code}}
		a.Suggestions = a.Action.Params
	}
}

// certainRecase reports whether the dictionary's other-case spelling is a
// fix to offer: one that adds capitals inside a word of three or more
// letters with at most one, as in GitHub or JSON. A capitalized-only name
// (Kubernetes) can't be told from a word in another language (los, Los),
// and a word with more capitals (CNs) chose them.
func certainRecase(word, cased string) bool {
	upper := func(s string) int {
		n := 0
		for _, r := range s {
			if unicode.IsUpper(r) {
				n++
			}
		}
		return n
	}
	inner := strings.IndexFunc(cased[1:], unicode.IsUpper) >= 0
	return utf8.RuneCountInString(word) >= 3 && upper(word) <= 1 && upper(cased) > upper(word) && inner
}

// Fields provides access to the internal rule definition.
func (s Spelling) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s Spelling) Pattern() string {
	return ""
}

// Suggest returns the closest spellings of word from the dictionaries.
func (s Spelling) Suggest(word string) []string {
	return s.SuggestFor(word, nil)
}

// SuggestFor is Suggest with the project's vocabularies as candidates too:
// an accepted term as close as a dictionary word is suggested first, in
// the case the vocabulary spells it.
func (s Spelling) SuggestFor(word string, cfg *core.Config) []string {
	ranked := s.gs.Rank(word)
	if cfg == nil {
		return suggestionWords(ranked)
	}

	lower := strings.ToLower(word)
	floor := 0.0
	if len(ranked) > 0 {
		floor = ranked[len(ranked)-1].Score
	}

	terms := append([]string(nil), cfg.AcceptedTokens...)
	for _, v := range cfg.Vocabularies {
		if v != nil {
			terms = append(terms, v.Accepted...)
		}
	}
	merged := make([]spell.Suggestion, 0, len(terms)+len(ranked))
	for _, term := range terms {
		if strings.EqualFold(term, word) {
			continue
		}
		if score := spell.Similarity(strings.ToLower(term), lower); score >= floor && score > 0 {
			merged = append(merged, spell.Suggestion{Word: term, Score: score})
		}
	}

	// Stable, so a vocabulary term keeps its place ahead of a dictionary
	// word it ties with, or repeats.
	merged = append(merged, ranked...)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	out := make([]string, 0, 6)
	seen := map[string]bool{}
	for _, m := range merged {
		if seen[m.Word] {
			continue
		}
		seen[m.Word] = true
		if out = append(out, m.Word); len(out) == 6 {
			break
		}
	}
	return out
}

func suggestionWords(ranked []spell.Suggestion) []string {
	out := make([]string, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, r.Word)
	}
	return out
}

func makeSpeller(s *Spelling, cfg *core.Config, rulePath string) (*spell.Checker, error) {
	var options []spell.CheckerOption
	var found bool

	affloc := core.FindAsset(cfg, s.Aff)
	dicloc := core.FindAsset(cfg, s.Dic)

	if cfg.FS.Exists(affloc) && cfg.FS.Exists(dicloc) {
		return spell.NewCheckerFS(cfg.FS, spell.UsingDictionaryByPath(dicloc, affloc))
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
			if cfg.FS.IsDir(p) {
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
		return spell.NewCheckerFS(cfg.FS, options...)
	}

	if rulePath == "internal" {
		// NOTE: New in v3.0 -- if we aren't given a `dicpath` or specific
		// dictionaries, we use the default one.
		options = append(options, spell.WithDefaultPath(
			filepath.Join(cfg.StylesPath(), core.DictDir)))
	}

	return spell.NewCheckerFS(cfg.FS, options...)
}
