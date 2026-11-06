package nlp

import (
	"fmt"
	"strings"
	"sync"

	"github.com/jdkato/prose/v3/segment"
	"github.com/jdkato/prose/v3/tag"
	"github.com/jdkato/prose/v3/tokenize"

	// Every language prose ships a model for, since the language is chosen
	// in configuration, at run time.
	_ "github.com/jdkato/prose/v3/segment/lang/all"
	_ "github.com/jdkato/prose/v3/tag/lang/all"

	"github.com/vale-cli/vale/v3/internal/system"
)

// TaggedWord is a word with an NLP context.
type TaggedWord struct {
	Token tag.Token
	Line  int
	Span  []int
}

// WordTokenizer splits text into words.
var WordTokenizer = NewIterTokenizer()

// segmenters holds one sentence segmenter per language, built on first use:
// a model costs time and memory to load, and most Vale runs never segment
// anything.
var segmenters sync.Map // lang -> *segment.Segmenter

// segmenterFor returns the segmenter for lang, loading its model once.
//
// English is the default. A language without a trained model gets prose's
// rule-based segmenter, which is never an error, so the only failure here is
// a model that will not load -- a bug, not a configuration mistake.
func segmenterFor(lang string) *segment.Segmenter {
	lang = normalizeLang(lang)
	if s, ok := segmenters.Load(lang); ok {
		return s.(*segment.Segmenter)
	}
	s, err := segment.ForLanguage(lang)
	if err != nil {
		panic("nlp: loading the sentence segmentation model for " + lang + ": " + err.Error())
	}
	actual, _ := segmenters.LoadOrStore(lang, s)
	return actual.(*segment.Segmenter)
}

// Segment splits text into the sentences of the given language.
func Segment(lang, text string) []string {
	return segmenterFor(lang).SegmentText(text)
}

// sentenceTokenizer defers loading the English model until something is
// actually segmented, while keeping the `SentenceTokenizer.Segment(...)` call
// shape.
type sentenceTokenizer struct{}

// Segment splits English text into sentences. Callers that know the
// language should use the Segment function instead.
func (sentenceTokenizer) Segment(text string) []string {
	return Segment("en", text)
}

// SentenceTokenizer splits English text into sentences.
var SentenceTokenizer sentenceTokenizer

// normalizeLang lower-cases a language tag, treating an empty one as English.
func normalizeLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return "en"
	}
	return lang
}

// IsEnglish reports whether lang names English, the language Vale assumes
// when none is set.
func IsEnglish(lang string) bool {
	lang = normalizeLang(lang)
	return lang == "en" || strings.HasPrefix(lang, "en-") || strings.HasPrefix(lang, "en_")
}

// taggers holds one tagger per model, built on first use.
//
// A style may name more than one: rules ported from another checker want that
// checker's idea of a noun, while everything already written expects prose's.
var (
	taggersMu sync.Mutex
	taggers   = map[string]tag.Interface{}
)

// RegisterModel makes a tagger available to rules under the given name,
// reading its dictionary from path.
//
// The dictionary is Vale's existing asset kind, so a model ships, syncs and
// resolves the same way a spelling dictionary does. Words the dictionary does
// not know fall back to prose's English tagger, since a dictionary carries no
// language of its own.
func RegisterModel(name, path string) error {
	taggersMu.Lock()
	defer taggersMu.Unlock()

	if _, built := taggers[name]; built {
		return nil
	}

	f, err := system.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	entries, err := tag.ReadDictionary(f)
	if err != nil {
		return err
	}

	base, err := tag.New()
	if err != nil {
		return err
	}

	lex, err := tag.NewLexical(entries, base)
	if err != nil {
		return err
	}
	taggers[name] = lex

	return nil
}

// TaggerFor returns the tagger a rule asked for, or the language's own when
// the rule named none.
//
// An unknown name is an error rather than a quiet fallback: a rule written
// against one tagger reads differently under another, so substituting one
// would change what the rule means. So is a language prose has no model for:
// English tags on German text are not tags, they are noise.
func TaggerFor(name, lang string) (tag.Interface, error) {
	if name == "" {
		return tag.ForLanguage(normalizeLang(lang))
	}

	taggersMu.Lock()
	defer taggersMu.Unlock()

	t, ok := taggers[name]
	if !ok {
		return nil, fmt.Errorf("no tagger model named %q", name)
	}

	return t, nil
}

// tagTextWith splits text into sentences, tags each one with the named tagger
// (or the language's own), and returns the tokens with offsets relative to
// text.
//
// Tagging is per sentence because the tagger conditions on the previous two
// tags; letting that context run across a sentence boundary would condition
// the first word of each sentence on the last word of the one before it.
//
// The tokenizer is the language's: French "l'homme" is two tokens, and
// Japanese has no spaces to split on. prose's tokenizers rather than the
// Treebank one, which rewrites the text as it splits and so cannot carry
// offsets. Callers such as the `sequence` check need to know where a token
// actually is.
func tagTextWith(model, lang, text string) ([]tag.Token, error) {
	t, err := TaggerFor(model, lang)
	if err != nil {
		return nil, err
	}

	words := tokenize.ForLanguage(normalizeLang(lang))

	var tokens []tag.Token
	for _, sent := range segmenterFor(lang).Segment(text) {
		found := words.Tokenize(sent.Text)
		t.TagTokens(found)

		// Tokenize reported offsets within the sentence; shift them so they
		// address the text the caller passed in.
		for i := range found {
			found[i].Start += sent.Start
		}
		tokens = append(tokens, found...)
	}

	return tokens, nil
}

// TextToTokens converts a string to a slice of tagged tokens.
//
// Tokens from the built-in tagger carry their byte offset within text, so
// text[tok.Start:tok.Start+len(tok.Text)] == tok.Text. Tokens from a remote
// NLP endpoint do not: that API returns text and tags only, so Start is zero
// throughout and callers needing positions must locate the tokens themselves.
//
// The error is a language with no model, or an endpoint that did not answer.
func TextToTokens(text string, nlp *Info) ([]tag.Token, error) {
	return textToTokensWith("", text, nlp)
}

// textToTokensWith converts text to tagged tokens with the named tagger.
func textToTokensWith(model, text string, info *Info) ([]tag.Token, error) {
	if info == nil {
		return tagTextWith(model, "en", text)
	}
	if info.Endpoint == "" {
		return tagTextWith(model, info.Lang, text)
	}

	// A remote endpoint does its own tagging, so there is no model to choose.
	result, err := pos(text, info.Lang, info.Endpoint)
	if err != nil {
		return nil, err
	}

	return result.Tokens, nil
}

// TokenCache remembers the tagging of each block within one document.
//
// Every `sequence` rule tags the sentence it is given, and a style may hold
// hundreds of them -- so the same sentence was tagged once per rule. The
// result depends only on the text, so it is computed once and shared.
//
// Scoped to a document: a cache living longer would hold every sentence a run
// has ever seen, and one shared between documents would need locking on a path
// that is otherwise free of it.
type TokenCache struct {
	tagged map[string][]tag.Token
}

// Tokens returns the tagged tokens of text, tagging it only the first time.
func (c *TokenCache) Tokens(text string, info *Info) []tag.Token {
	toks, _ := c.TokensWith("", text, info)
	return toks
}

// TokensWith is Tokens, read with the named tagger.
func (c *TokenCache) TokensWith(model, text string, info *Info) ([]tag.Token, error) {
	if c == nil {
		return textToTokensWith(model, text, info)
	}

	if toks, ok := c.tagged[text]; ok {
		return toks, nil
	}

	toks, err := textToTokensWith(model, text, info)
	if err != nil {
		return nil, err
	}
	if c.tagged == nil {
		c.tagged = map[string][]tag.Token{}
	}
	c.tagged[text] = toks

	return toks, nil
}
