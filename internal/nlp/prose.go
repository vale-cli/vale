package nlp

import (
	"fmt"
	"os"
	"sync"

	"github.com/jdkato/prose/v3/segment"
	"github.com/jdkato/prose/v3/tag"
	"github.com/jdkato/prose/v3/tokenize"
)

// TaggedWord is a word with an NLP context.
type TaggedWord struct {
	Token tag.Token
	Line  int
	Span  []int
}

// WordTokenizer splits text into words.
var WordTokenizer = NewIterTokenizer()

// segmenter is loaded on first use: the punkt model costs time and memory to
// build, and most Vale runs never segment anything.
var punktSegmenter = sync.OnceValue(func() *segment.Segmenter {
	s, err := segment.New()
	if err != nil {
		panic("nlp: loading the sentence segmentation model: " + err.Error())
	}
	return s
})

// sentenceTokenizer defers loading the punkt model until something is actually
// segmented, while keeping the `SentenceTokenizer.Segment(...)` call shape.
type sentenceTokenizer struct{}

// Segment splits text into sentences.
func (sentenceTokenizer) Segment(text string) []string {
	return punktSegmenter().SegmentText(text)
}

// SentenceTokenizer splits text into sentences.
var SentenceTokenizer sentenceTokenizer

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
// resolves the same way a spelling dictionary does.
func RegisterModel(name, path string) error {
	taggersMu.Lock()
	defer taggersMu.Unlock()

	if _, built := taggers[name]; built {
		return nil
	}

	f, err := os.Open(path)
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

// TaggerFor returns the tagger a rule asked for.
//
// An unknown name is an error rather than a quiet fallback: a rule written
// against one tagger reads differently under another, so substituting one
// would change what the rule means.
func TaggerFor(name string) (tag.Interface, error) {
	if name == "" {
		return tagger()
	}

	taggersMu.Lock()
	defer taggersMu.Unlock()

	t, ok := taggers[name]
	if !ok {
		return nil, fmt.Errorf("no tagger model named %q", name)
	}

	return t, nil
}

// tagger is prose's own, used by every rule that does not name a model.
//
// sync.OnceValues rather than a nil check: Vale lints files concurrently, so
// a plain check-then-assign here is a data race.
var tagger = sync.OnceValues(func() (tag.Interface, error) { return tag.Open("") })

// wordTokenizer splits a sentence into positioned words for tagging.
//
// prose's tokenizer rather than the Treebank one: Treebank rewrites the text
// as it splits (quotes become “ and ”), so its tokens are not substrings of
// the source and cannot carry offsets. Callers such as the `sequence` check
// need to know where a token actually is.
var wordTokenizer = sync.OnceValue(func() *tokenize.Tokenizer {
	return tokenize.New()
})

// tagText splits text into sentences, tags each one, and returns the tokens
// with offsets relative to text.
//
// Tagging is per sentence because the tagger conditions on the previous two
// tags; letting that context run across a sentence boundary would condition
// the first word of each sentence on the last word of the one before it.
func tagText(text string) []tag.Token {
	toks, err := tagTextWith("", text)
	if err != nil {
		panic("nlp: loading the part-of-speech model: " + err.Error())
	}
	return toks
}

// tagTextWith is tagText, read with the named tagger.
//
// An unknown model is returned as an error rather than panicked on: the name
// comes from a rule, so it is the author's mistake to report, not a bug.
func tagTextWith(model, text string) ([]tag.Token, error) {
	t, err := TaggerFor(model)
	if err != nil {
		return nil, err
	}

	var tokens []tag.Token
	for _, sent := range punktSegmenter().Segment(text) {
		found := wordTokenizer().Tokenize(sent.Text)
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
func TextToTokens(text string, nlp *Info) []tag.Token {
	// Determine if (and how) we need to do POS tagging.
	if nlp == nil || nlp.Endpoint == "" {
		// Fall back to our internal library (English-only).
		return tagText(text)
	}
	result, err := pos(text, nlp.Lang, nlp.Endpoint)
	if err != nil {
		panic(err)
	}
	return result.Tokens
}

// textToTokensWith converts text to tagged tokens with the named tagger.
func textToTokensWith(model, text string, info *Info) ([]tag.Token, error) {
	if info == nil || info.Endpoint == "" {
		return tagTextWith(model, text)
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

// TokensWith returns the tagged tokens of text, read with the named tagger,
// tagging it only the first time.
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
