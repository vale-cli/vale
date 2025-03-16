package nlp

import (
	"strings"
)

type segmenter func(string) []string

// A Block represents a section of text.
type Block struct {
	Context string // parent content - e.g., sentence -> paragraph
	Line    int    // line of the block
	Scope   string // section selector
	Parent  string // parent (fully-qualfied) selector
	Text    string // text content
}

// NewBlock makes a new Block with prepared text and a Selector.
func NewBlock(ctx, txt, sel string) Block {
	return NewLinedBlock(ctx, txt, sel, -1)
}

// NewBlockWithParent makes a new Block with prepared text, a Selector, and a parent.
func NewBlockWithParent(ctx, txt, sel, parent string) Block {
	if ctx == "" {
		ctx = txt
	}

	return Block{
		Context: ctx,
		Text:    txt,
		Scope:   sel,
		Parent:  parent,
		Line:    -1}
}

// NewLinedBlock creates a Block with an already-known location.
func NewLinedBlock(ctx, txt, sel string, line int) Block {
	if ctx == "" {
		ctx = txt
	}

	return Block{
		Context: ctx,
		Text:    txt,
		Scope:   sel,
		Parent:  sel,
		Line:    line}
}

// Info handles NLP-related tasks.
//
// Assigning this on a per-file basis allows us to handle multi-language
// projects -- one file might be `en` while another is `ja`, for example.
type Info struct {
	Lang         string // Language of the file.
	Endpoint     string // API endpoint (optional); TODO: should this be per-file?
	Scope        string // The file's ext scope.
	Tagging      bool   // Does the file need POS tagging?
	Segmentation bool   // Does the file need sentence segmentation?
	Splitting    bool   // Does the file need paragraph splitting?
}

// An NLP provider is a library to implements part-of-speech tagging, sentence
// segmentation, and word tokenization.
//
// The default implementation is the pure-Go prose library, but the goal is to
// allow (fairly) seamless integration with non-Go libraries too (such as
// spaCy).
func (n *Info) Compute(block *Block) ([]Block, error) {
	seg := SentenceTokenizer.Segment
	if n.Endpoint != "" && n.Lang != "en" {
		// We only use external segmentation for non-English text since prose
		// (our native library) is more efficient.
		seg = func(text string) []string {
			ret, err := doSegment(text, n.Lang, n.Endpoint)
			if err != nil {
				panic(err)
			}
			return ret.Sents
		}
	}
	return n.doNLP(block, seg)
}

func (n *Info) doNLP(blk *Block, seg segmenter) ([]Block, error) {
	blks := []Block{}

	ctx := blk.Context
	idx := blk.Line

	if n.Splitting {
		for _, p := range strings.SplitAfter(blk.Text, "\n\n") {
			blks = append(
				blks, NewLinedBlock(ctx, p, "paragraph."+blk.Scope, idx))
		}
	}

	if n.Segmentation {
		for _, s := range seg(blk.Text) {
			s = strings.TrimSpace(s)
			if s != "" {
				blks = append(
					blks, NewLinedBlock(ctx, s, "sentence."+blk.Scope, idx))
			}
		}
	}

	blks = append(
		blks, NewLinedBlock(ctx, blk.Text, blk.Scope, idx))

	return blks, nil
}
