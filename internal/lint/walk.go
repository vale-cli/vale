package lint

import (
	"bytes"
	"net/url"
	"strings"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/net/html"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// inlineCapture is the text of one inline element, gathered as the block that
// contains it is read.
type inlineCapture struct {
	tag   string
	scope string
	text  string

	// masked records that the block will not hold this text: a skipped
	// element (inline code) is written out of it, so the fragment has to be
	// located in the source rather than within the block.
	masked bool
}

type walker struct {
	lines   int
	context []byte

	activeTag string
	activeCls string

	idx int
	z   *html.Tokenizer

	// cursor is how far into the context we have already emitted blocks.
	//
	// Blocks come out in document order, so searching forward from here finds
	// the right occurrence of a repeated phrase and keeps the total work
	// linear in the document rather than linear per block.
	cursor int

	// queue holds each segment of text we encounter in a block, which we then
	// use to sequentially update our context.
	queue []string

	// tagHistory holds the HTML tags we encounter in a given block -- e.g.,
	// if we see <ul>, <li>, <p>, we'd get tagHistory = [ul li p]. It's reset
	// on every non-inline end tag.
	tagHistory []string

	// clsHistory holds the `class` of each tag in tagHistory, at the same
	// index, so that a block can be scoped by the classes enclosing it. Both
	// are reset together, so this is a handful of strings at a time rather
	// than anything that accumulates across the document.
	clsHistory []string

	// enclosing holds every block container still open -- pushed on its start
	// tag, popped on its end tag. Unlike clsHistory, it is not reset per
	// block: a container's class stays in scope for every block inside it,
	// however many blocks that is. A MyST directive holding three paragraphs
	// scopes all three, not just the first.
	enclosing []openTag

	// spans maps this block's text back to the source, and srcCursor is how far
	// the search for the next run has already gone. Separate from `cursor`,
	// which places whole blocks and must not be moved by this.
	spans     []nlp.Run
	srcCursor int

	// cmtCursor is how far the search for the next comment directive has
	// gone. Comments arrive in document order but ahead of the block holding
	// them -- `cursor` moves only when a block is placed -- so they need a
	// cursor of their own, floored at the block cursor.
	cmtCursor int

	// inline holds the inline elements captured within the current block --
	// link text, bold, and so on -- to be linted once the block itself is,
	// since their position is derived from its own. Reset with the block.
	inline []inlineCapture

	// pending holds the attribute values of the element just opened, waiting
	// to be masked out of the context. They can't be masked on the start tag
	// itself: an autolink's `href` and its text are the same source bytes, so
	// masking the attribute leaves the text token to find its string somewhere
	// else in the document -- and `idx`, which only moves forward, then places
	// every later block on that line. See #847.
	pending []string

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
			if !found && w.ext != ".adoc" {
				for _, f := range strings.Fields(s) {
					_ = w.sub(f, '@')
				}
			}
		}
	}
}

// flush masks the attribute values held since the last start tag.
//
// `text` is the element's own text, if it has arrived; an attribute equal to it
// is dropped rather than masked, because the two are one run of source and
// masking it twice consumes an occurrence that belongs to something else.
func (w *walker) flush(text string) {
	for _, val := range w.pending {
		if val != text {
			w.update(val, html.TextToken)
		}
	}
	w.pending = nil
}

func (w *walker) reset() {
	w.flush("")
	for _, s := range w.queue {
		w.update(s, html.TextToken)
	}
	w.queue = []string{}
	w.tagHistory = []string{}
	w.clsHistory = []string{}
	w.inline = nil
	w.spans = nil
}

func (w *walker) getCtx() string {
	return byteSlice2String(w.context)
}

func (w *walker) append(text string) {
	if text != "" {
		// Before the search below, so the text finds its own occurrence, and
		// after the drop, so an autolink's `href` doesn't take it first.
		w.flush(text)

		pos := w.advance(text)
		if pos > -1 {
			w.idx = pos
		}
		w.queue = append(w.queue, text)
	}
}

func (w *walker) addTag(tag, class string) {
	w.tagHistory = append(w.tagHistory, tag)
	w.clsHistory = append(w.clsHistory, class)
	w.activeTag = tag
}

// An openTag is one still-open block container.
type openTag struct {
	tag string
	cls string
}

// enclose records a block container as open.
func (w *walker) enclose(tag, cls string) {
	w.enclosing = append(w.enclosing, openTag{tag, cls})
}

// unclose closes the innermost open container with the given tag.
func (w *walker) unclose(tag string) {
	for i := len(w.enclosing) - 1; i >= 0; i-- {
		if w.enclosing[i].tag == tag {
			w.enclosing = append(w.enclosing[:i], w.enclosing[i+1:]...)
			return
		}
	}
}

// classes returns the classes enclosing the current block, in the order they
// were opened and without repeats.
func (w *walker) classes() []string {
	var found []string

	add := func(cls string) {
		for _, name := range strings.Fields(cls) {
			if scopeSafe(name) && !core.StringInSlice(name, found) {
				found = append(found, name)
			}
		}
	}

	for _, o := range w.enclosing {
		add(o.cls)
	}
	for _, cls := range w.clsHistory {
		add(cls)
	}

	return found
}

// scopeSafe reports whether a class can be used as part of a scope.
//
// A scope is split on `.`, combined with `&` and negated with `~`, so a class
// holding one of those would not survive the round trip. Those are dropped
// rather than rewritten: a silently altered name is harder to explain than an
// absent one.
func scopeSafe(class string) bool {
	return class != "" && !strings.ContainsAny(class, ".&~")
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

// block builds the block for text, which begins `shift` bytes into the runs
// recorded as it was read -- the space `clean` prefixes and lintScope trims.
func (w *walker) block(text, scope string, shift int) nlp.Block {
	line := w.idx

	pos := w.advance(text)
	if pos != line && pos > -1 {
		line = pos
	}

	b := nlp.NewLinedBlock(w.getCtx(), text, scope, line)
	b.Offset = w.locate(text)

	// A block whose text is in the source verbatim needs nothing more; one
	// holding inline markup is not there to be found, and only its runs can
	// place a match inside it. See #502.
	if b.Offset < 0 {
		b.Runs = w.runs(shift, len(text))
	}

	return b
}

// runs returns the recorded runs covering [shift, shift+n) of the walker's
// buffer, rebased onto a block that starts there.
func (w *walker) runs(shift, n int) []nlp.Run {
	var out []nlp.Run
	for _, r := range w.spans {
		lo, hi := max(r.At, shift), min(r.At+r.N, shift+n)
		if lo >= hi {
			continue
		}
		out = append(out, nlp.Run{At: lo - shift, Src: r.Src + (lo - r.At), N: hi - lo})
	}
	return out
}

// locate returns where text begins in the context, advancing the cursor past
// it, or -1 if it cannot be found from the cursor onwards.
//
// Recording the position here is what lets checks report byte offsets. Without
// it every alert has to be placed by searching the document and masking what
// it matched, which costs a copy of the document per alert.
func (w *walker) locate(text string) int {
	if text == "" {
		return -1
	}

	ctx := w.getCtx()
	if w.cursor > len(ctx) {
		return -1
	}

	// Look a bounded distance ahead rather than to the end of the file.
	//
	// A block that will be found sits just past the cursor -- the gap is the
	// markup between it and the previous block. A block that extraction has
	// rewritten is not in the source at all, and scanning the rest of the
	// document to discover that costs a pass per block: on an 870 KB generated
	// reference, where every block is rewritten, this was the largest single
	// cost in the run.
	//
	// The window only has to exceed the longest run of markup between two
	// blocks. 64 KB is far past that, and a block beyond it takes the same
	// search-based placement a miss has always taken.
	const window = 64 << 10

	hay := ctx[w.cursor:]
	if len(hay) > window {
		hay = hay[:window]
	}

	i := strings.Index(hay, text)
	if i < 0 {
		// Extraction can rewrite text -- stripping markup, decoding entities --
		// leaving something that is not a substring of the source. Those blocks
		// keep the old search-based placement.
		return -1
	}

	off := w.cursor + i
	w.cursor = off + len(text)

	return off
}

// commentAt returns where the comment's text begins in the source, or -1 if
// it cannot be found -- a format whose comment syntax rewrites the text, or a
// directive quoted earlier in a way the search cannot tell apart.
func (w *walker) commentAt(txt string) int {
	if txt == "" {
		return -1
	}
	if w.cmtCursor < w.cursor {
		w.cmtCursor = w.cursor
	}

	ctx := w.getCtx()
	if w.cmtCursor > len(ctx) {
		return -1
	}

	i := strings.Index(ctx[w.cmtCursor:], txt)
	if i < 0 {
		return -1
	}

	off := w.cmtCursor + i
	w.cmtCursor = off + len(txt)

	return off
}

// mapRun records that the run of text at `at` in the current block came from
// `raw` in the source, if that can be found from where the last run ended.
func (w *walker) mapRun(at int, raw string) {
	ctx := w.getCtx()
	if raw == "" || w.srcCursor > len(ctx) {
		return
	}

	i := strings.Index(ctx[w.srcCursor:], raw)
	if i < 0 {
		return
	}

	src := w.srcCursor + i
	w.srcCursor = src + len(raw)
	w.spans = append(w.spans, nlp.Run{At: at, Src: src, N: len(raw)})
}

// sourceOffset returns where index `i` of the block's text sits in the source,
// or -1 if that run was never mapped.
func (w *walker) sourceOffset(i int) int {
	for _, s := range w.spans {
		if i >= s.At && i < s.At+s.N {
			return s.Src + (i - s.At)
		}
	}
	return -1
}

func (w *walker) walk() (html.TokenType, html.Token, string) {
	tokt := w.z.Next()
	tok := w.z.Token()
	return tokt, tok, html.UnescapeString(strings.TrimSpace(tok.Data))
}

func (w *walker) replaceToks(tok html.Token) {
	tags := core.StringInSlice(tok.Data, []string{
		"img", "a", "p", "script", "h1", "h2", "h3", "h4", "h5", "h6", "span"})
	// Anything still waiting belongs to an element that had no text of its own.
	w.flush("")

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
				w.pending = append(w.pending, a.Val)
			}
		}
	}
}

// advance reports the line `text` sits on, or -1 if that is not ahead of the
// line we are already on.
//
// Only the last line of `text` decides the result. The loop this replaced ran
// a search for every line, and then for every word of any line that missed,
// but each pass overwrote the previous one -- so every search but the final
// one was computed and discarded. On a 118 KB file that made this function a
// fifth of the whole run.
//
// The remaining search still starts at the beginning of the document, so cost
// still grows faster than length. Three ways out were tried and measured:
//
//   - Skipping the masked prefix. `sub` overwrites consumed text with '@', but
//     never touches the markup around it, so the first unmasked byte is the
//     '#' of the first heading and the watermark cannot move.
//   - Resuming from the previous match. Extraction rewrites text often enough
//     that the forward search misses, and the fallback made the common path
//     two scans instead of one: 47% slower at 112 KB.
//   - Taking the line from the offset `locate` already found. This does remove
//     the search -- string matching drops from 30% of the profile to 4% -- but
//     `locate` gives where a block *starts* and this gives where it *ends*, and
//     the change to blk.Line makes alert placement in core.assignLoc do much
//     more work: 38% slower overall. Worth revisiting with assignLoc rather
//     than on its own; the two are coupled.
//
// The line this returns is a fallback, and core.assignLoc has a standing NOTE
// saying so. It matters less than it did: an alert in a block holding inline
// markup is now placed by the runs the block was read from rather than by
// searching from this line, which is what moved the rst alert in
// `testdata/e2e/frontmatter.yaml` out of a literal block and onto the prose it
// belongs to. What still comes through here is a block no run could place.
func (w *walker) advance(text string) int {
	ctx := w.getCtx()

	last := text
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		last = text[i+1:]
	}

	pos := strings.Index(ctx, last)
	if pos < 0 {
		// Extraction rewrites text, so a line is not always a substring of the
		// source; a single word from it usually still is. The longest word,
		// though: the final one is as often a short `it.` that appears all
		// over the document, landing the block on someone else's line -- and
		// an alert placed by that line is an alert placed on the wrong text.
		longest := ""
		for _, f := range strings.Fields(last) {
			if len(f) > len(longest) {
				longest = f
			}
		}
		if longest != "" {
			pos = strings.Index(ctx, longest)
		}
	}

	if pos >= 0 {
		if l := strings.Count(ctx[:pos], "\n"); l > w.idx {
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
