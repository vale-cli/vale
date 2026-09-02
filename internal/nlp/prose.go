package nlp

import (
	"fmt"
	"os"
	"strings"
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

// SegmentWith splits text into sentences, making the same local-vs-remote
// choice Info.Compute already makes for structural paragraph splitting (see
// usesRemoteSegmentation in provider.go): local Punkt for English text, or
// when info names no remote endpoint at all, and otherwise the endpoint's own
// `/segment` response for non-English text.
//
// Local Punkt's own Sentence values already carry accurate offsets (see
// segment.Sentence). A remote `/segment` response does not -- it is text
// only -- so each returned piece is located by searching text for it,
// advancing a cursor past every earlier piece so a sentence that recurs
// verbatim resolves to its own occurrence rather than always the first (the
// same technique offsetOf, in provider.go, uses for structural splitting).
//
// A remote endpoint's request can fail -- a network error, a timeout, a
// non-JSON body -- so this reports that as a real error rather than
// panicking: there is no recover() anywhere in Vale, so a panic here would
// crash the whole run instead of surfacing as one file's lint error.
func SegmentWith(text string, info *Info) ([]segment.Sentence, error) {
	if !usesRemoteSegmentation(info) {
		return punktSegmenter().Segment(text), nil
	}

	ret, err := doSegment(text, info.Lang, info.Endpoint)
	if err != nil {
		return nil, err
	}

	var sents []segment.Sentence
	cursor := 0
	for _, piece := range ret.Sents {
		piece = strings.TrimSpace(piece)
		if piece == "" || cursor > len(text) {
			continue
		}
		i := strings.Index(text[cursor:], piece)
		if i < 0 {
			// A remote segmenter can rewrite text (normalize whitespace,
			// say) so that a piece is no longer a literal substring; there
			// is no honest span to report for it, so it is dropped rather
			// than guessed at.
			continue
		}
		start := cursor + i
		cursor = start + len(piece)
		sents = append(sents, segment.Sentence{Text: piece, Start: start})
	}
	return sents, nil
}

// TextToTokens converts a string to a slice of tagged tokens.
//
// Tokens from the built-in tagger carry their byte offset within text, so
// text[tok.Start:tok.Start+len(tok.Text)] == tok.Text. Tokens from a remote
// NLP endpoint do not: that API returns text and tags only, so Start is zero
// throughout and callers needing positions must locate the tokens themselves.
//
// A remote endpoint's request can fail -- a network error, a timeout, a
// non-2xx status (see post, in http.go) -- so this reports that as a real
// error rather than panicking: there is no recover() anywhere in Vale, so a
// panic here would crash the whole run instead of surfacing as a normal,
// reportable error. TextToContext (internal/core/util.go) and its own
// caller, the `tag` CLI command (cmd/vale/command.go's runTag), already
// return errors the same way; both are updated to thread this one through
// rather than let it panic.
func TextToTokens(text string, nlp *Info) ([]tag.Token, error) {
	// Determine if (and how) we need to do POS tagging.
	if nlp == nil || nlp.Endpoint == "" {
		// Fall back to our internal library (English-only).
		return tagText(text), nil
	}
	result, err := pos(text, nlp.Lang, nlp.Endpoint)
	if err != nil {
		return nil, err
	}
	return result.Tokens, nil
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
	tagged    map[string][]tag.Token
	sentences map[string][]segment.Sentence
}

// Sentences returns the sentence spans of text -- each one's byte offset
// included, not just its bytes -- segmenting it only the first time.
//
// Segmentation does not depend on which tagger a rule names, unlike
// TokensWith, so there is only one cache for it rather than one per model:
// two rules asking for the same block's sentences, whatever tagger either of
// them uses, get the same segmentation pass.
//
// info drives the same local-vs-remote dispatch Info.Compute already makes
// (see SegmentWith): a file configured with a non-English remote endpoint
// gets that endpoint's own sentence boundaries here too, not always local
// Punkt's.
//
// A non-nil error means segmentation itself failed (a remote endpoint's
// request errored); the caller reports that rather than proceeding with a
// partial or stale result.
func (c *TokenCache) Sentences(text string, info *Info) ([]segment.Sentence, error) {
	if c == nil {
		return SegmentWith(text, info)
	}

	if sents, ok := c.sentences[text]; ok {
		return sents, nil
	}

	sents, err := SegmentWith(text, info)
	if err != nil {
		return nil, err
	}
	if c.sentences == nil {
		c.sentences = map[string][]segment.Sentence{}
	}
	c.sentences[text] = sents

	return sents, nil
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
