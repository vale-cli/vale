package lint

import (
	"bytes"
	"net/url"
	"strings"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/net/html"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

type walker struct {
	lines   int
	context []byte

	activeTag string
	activeCls string

	idx int
	z   *html.Tokenizer

	// queue holds each segment of text we encounter in a block, which we then
	// use to sequentially update our context.
	queue []string

	// tagHistory holds the HTML tags we encounter in a given block -- e.g.,
	// if we see <ul>, <li>, <p>, we'd get tagHistory = [ul li p]. It's reset
	// on every non-inline end tag.
	tagHistory []string

	begin int
	end   int

	// ext holds the file extension of the current file.
	ext string
}

func newWalker(f *core.File, raw []byte, offset int) *walker {
	return &walker{
		lines: len(f.Lines) + offset,
		// We keep a private, writable copy of the content so that `sub` can
		// overwrite already-processed segments in place. We must not alias
		// `f.Content` here: its backing array may be read-only (e.g., a
		// compile-time constant), and writing to it crashes (see #1099).
		context: []byte(f.Content),
		z:       html.NewTokenizer(bytes.NewReader(raw)),
		ext:     f.NormedExt,
	}
}

func (w *walker) sub(sub string, char rune) bool {
	return subInplace(w.context, sub, char)
}

func (w *walker) update(txt string, tokt html.TokenType) {
	var found bool
	if (tokt == html.TextToken || tokt == html.CommentToken) && txt != "" {
		for _, s := range strings.Split(txt, "\n") {
			found = w.sub(s, '@')
			if !found {
				for _, f := range strings.Fields(s) {
					_ = w.sub(f, '@')
				}
			}
		}
	}
}

func (w *walker) reset() {
	for _, s := range w.queue {
		w.update(s, html.TextToken)
	}
	w.queue = []string{}
	w.tagHistory = []string{}
}

func (w *walker) getCtx() string {
	return byteSlice2String(w.context)
}

func (w *walker) append(text string) {
	if text != "" {
		pos := w.advance(text)
		if pos > -1 {
			w.idx = pos
		}
		w.queue = append(w.queue, text)
	}
}

func (w *walker) addTag(tag string) {
	w.tagHistory = append(w.tagHistory, tag)
	w.activeTag = tag
}

func (w *walker) setCls(tag string, cls bool) {
	if cls {
		w.activeCls = tag
		w.begin = 1
		w.end = 0
	}
}

func (w *walker) addCls(tag string, start bool) {
	if tag == w.activeCls {
		if start {
			w.begin++
		} else {
			w.end++
		}
	}
}

func (w *walker) canClose() bool {
	return w.activeCls != "" && w.begin == w.end && w.begin > 0
}

func (w *walker) close() {
	w.activeCls = ""
	w.begin = 0
	w.end = 0
}

func (w *walker) block(text, scope string) nlp.Block {
	line := w.idx

	pos := w.advance(text)
	if pos != line && pos > -1 {
		line = pos
	}

	return nlp.NewLinedBlock(w.getCtx(), text, scope, line)
}

func (w *walker) walk() (html.TokenType, html.Token, string) {
	tokt := w.z.Next()
	tok := w.z.Token()
	return tokt, tok, html.UnescapeString(strings.TrimSpace(tok.Data))
}

func (w *walker) replaceToks(tok html.Token) {
	tags := core.StringInSlice(tok.Data, []string{
		"img", "a", "p", "script", "h1", "h2", "h3", "h4", "h5", "h6", "span"})
	if tags {
		names := []string{"href", "id", "src", "alt"}
		if w.ext == ".html" {
			// We need to handle cases in which inline tags include `class` attributes, which may
			// contain substrings that match our actual findings. The challenge is that many of our
			// supported formats inject these *after* converting to HTML, so we can't find them in
			// the original text.
			//
			// See testdata/fixtures/patterns/{test2.rst, test3.html} for examples.
			names = append(names, "class")
		}
		for _, a := range tok.Attr {
			if core.StringInSlice(a.Key, names) {
				if a.Key == "href" {
					a.Val, _ = url.QueryUnescape(a.Val)
				}
				w.update(a.Val, html.TextToken)
			}
		}
	}
}

func (w *walker) advance(text string) int {
	pos := 0
	ctx := w.getCtx()

	for _, s := range strings.Split(text, "\n") {
		pos = strings.Index(ctx, s)
		if pos < 0 {
			for _, ss := range strings.Fields(s) {
				pos = strings.Index(ctx, ss)
			}
		}
	}
	if pos >= 0 {
		l := strings.Count(ctx[:pos], "\n")
		if l > w.idx {
			return l
		}
	}
	return -1
}

func (w *walker) lastTag() string {
	if len(w.tagHistory) > 0 {
		return w.tagHistory[len(w.tagHistory)-1]
	}
	return w.activeTag
}

// isNestedList checks if we're currently inside a nested list.
//
// For example, the following HTML:
//
// ```
//
//	<ul>
//	  <li>
//	    <ol>
//	      <li>...</li>
//	    </ol>
//	  </li>
//	</ul>
//
// ```
// NOTE: We need to do this to ensure that, when we're linting a list item, we
// prepend a space to the item before conatenating it with the previous.
func (w *walker) isNestedList() bool {
	if w.lastTag() == "li" && len(w.tagHistory) > 3 {
		up1 := w.tagHistory[len(w.tagHistory)-2]
		up2 := w.tagHistory[len(w.tagHistory)-3]
		if up1 == "ol" || up1 == "ul" {
			return up2 == "li"
		}
	}
	return false
}

func byteSlice2String(bs []byte) string {
	if len(bs) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(bs), len(bs))
}

// subInplace masks the first occurrence of `sub` in `ctx` by overwriting each
// of its single-byte runes with `char`, mutating `ctx` directly.
//
// The replacement is length-preserving: single-byte runes (other than '\n')
// become `char`, while multi-byte runes and newlines are left untouched. Since
// no bytes are added or removed, positions in `ctx` remain stable -- which is
// the whole point of masking already-processed text rather than removing it.
//
// `ctx` must be a writable buffer owned by the caller; see newWalker.
func subInplace(ctx []byte, sub string, char rune) bool {
	idx := strings.Index(byteSlice2String(ctx), sub)
	if idx < 0 {
		return false
	}

	mask := byte(char)
	for _, r := range sub {
		if r != '\n' && utf8.RuneLen(r) == 1 {
			ctx[idx] = mask
		}
		idx += utf8.RuneLen(r)
	}
	return true
}
