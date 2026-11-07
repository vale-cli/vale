package lint

import (
	"bytes"
	"index/suffixarray"
	"net/url"
	"sort"
	"strings"
	"unicode"
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

	// begin and end are where the element's text sits in the block's
	// buffer, recorded as it was written.
	begin, end int
}

type walker struct {
	lines   int
	context []byte

	activeTag string
	activeCls string

	idx int
	z   *html.Tokenizer

	// index finds text in the context without scanning it: a suffix array
	// over the bytes as they were before any masking, and, per text asked
	// about, its occurrences in order and how many are already masked. A
	// small context is cheaper to scan, and has no index.
	index *suffixarray.Index
	occ   map[string]*occurrences

	// nlOff and nlCount are where the last line count stopped and what it
	// reached, so the next one counts from there rather than from the top.
	nlOff, nlCount int

	// cursor is how far into the context we have already emitted blocks.
	//
	// Blocks come out in document order, so searching forward from here finds
	// the right occurrence of a repeated phrase and keeps the total work
	// linear in the document rather than linear per block.
	cursor int

	// queue holds each segment of text we encounter in a block, which we then
	// use to sequentially update our context.
	queue []string

	// tagHistory holds the HTML tags open around the current text in a given
	// block -- e.g., if we see <ul>, <li>, <p>, we'd get tagHistory = [ul li
	// p]. An inline tag is popped when it closes; the history is reset on
	// every non-inline end tag.
	tagHistory []string

	// clsHistory holds the `class` of each tag opened in the block, inline
	// ones included after they close, so that a block can be scoped by the
	// classes enclosing it and by those of the elements it holds. Both are
	// reset together, so this is a handful of strings at a time rather than
	// anything that accumulates across the document.
	clsHistory []string

	// idsHistory holds the selections marked on the tags in tagHistory, and
	// is reset with it.
	idsHistory []string

	// enclosing holds every block container still open -- pushed on its start
	// tag, popped on its end tag. Unlike clsHistory, it is not reset per
	// block: a container's class stays in scope for every block inside it,
	// however many blocks that is. A MyST directive holding three paragraphs
	// scopes all three, not just the first.
	enclosing []openTag

	// aggs holds the selections open around the current block, innermost
	// last. Like enclosing, it spans blocks rather than resetting with them.
	// closed is the one whose element just ended, awaiting the flush.
	aggs   []*aggregate
	closed *aggregate

	// seen records every selection the document opened, so the ones it
	// never did can be reported as absent.
	seen map[string]bool

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
	//
	// Each value is a list of the forms it may take in the source: a link
	// target as the converter wrote it and, when it differs, decoded, since a
	// converter percent-encodes a space or a non-ASCII character the author
	// wrote as is. The first form found whole is masked; a form that isn't
	// there must not be masked word by word, since its words land on prose
	// elsewhere in the document. See #1218.
	pending [][]string

	begin int
	end   int

	// ext holds the file extension of the current file.
	ext string
}

// indexMin is the size from which a context is indexed rather than scanned.
const indexMin = 16 << 10

// An occurrences is where one text occurs in the context, in order, and how
// many of those masking has already removed.
type occurrences struct {
	at   []int
	next int
}

func newWalker(f *core.File, raw []byte, offset int) *walker {
	w := &walker{
		lines: len(f.Lines) + offset,
		// We keep a private, writable copy of the content so that `sub` can
		// overwrite already-processed segments in place. We must not alias
		// `f.Content` here: its backing array may be read-only (e.g., a
		// compile-time constant), and writing to it crashes (see #1099).
		context: []byte(f.Content),
		z:       html.NewTokenizer(bytes.NewReader(raw)),
		ext:     f.NormedExt,
	}
	if len(f.Content) >= indexMin {
		w.index = suffixarray.New([]byte(f.Content))
	}
	return w
}

// first returns where needle first occurs in the context as it is now, with
// consumed text masked -- what strings.Index reports -- without scanning
// the masked prefix each time, which made every search a pass over the
// consumed document. Masking only removes occurrences, so the first one
// still present never moves back, and each text keeps its place in its own
// list.
func (w *walker) first(needle string) int {
	ctx := byteSlice2String(w.context)
	if w.index == nil || needle == "" {
		return strings.Index(ctx, needle)
	}

	o, ok := w.occ[needle]
	if !ok {
		at := w.index.Lookup([]byte(needle), -1)
		sort.Ints(at)
		o = &occurrences{at: at}
		if w.occ == nil {
			w.occ = map[string]*occurrences{}
		}
		w.occ[needle] = o
	}

	for o.next < len(o.at) {
		if p := o.at[o.next]; ctx[p:p+len(needle)] == needle {
			return p
		}
		o.next++
	}
	return -1
}

// lineAt counts the newlines before pos, from where the last count stopped.
func (w *walker) lineAt(pos int) int {
	ctx := w.getCtx()
	if pos >= w.nlOff {
		w.nlCount += strings.Count(ctx[w.nlOff:pos], "\n")
	} else {
		w.nlCount -= strings.Count(ctx[pos:w.nlOff], "\n")
	}
	w.nlOff = pos
	return w.nlCount
}

// sub masks the first occurrence of sub still present in the context.
func (w *walker) sub(sub string) bool {
	idx := w.first(sub)
	if idx < 0 {
		return false
	}
	maskAt(w.context, idx, sub, '@')
	return true
}

func (w *walker) update(txt string, tokt html.TokenType) {
	var found bool
	if (tokt == html.TextToken || tokt == html.CommentToken) && txt != "" {
		for _, s := range strings.Split(txt, "\n") {
			found = w.sub(s)
			if !found && w.ext != ".adoc" {
				for _, f := range strings.Fields(s) {
					_ = w.sub(f)
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
	for _, forms := range w.pending {
		if core.StringInSlice(text, forms) {
			continue
		}
		found := false
		for _, form := range forms {
			if w.sub(form) {
				found = true
				break
			}
		}
		if !found {
			w.update(forms[0], html.TextToken)
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
	w.idsHistory = nil
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

func (w *walker) addTag(tag, class, marks string) {
	w.tagHistory = append(w.tagHistory, tag)
	w.clsHistory = append(w.clsHistory, class)
	w.idsHistory = append(w.idsHistory, strings.Fields(marks)...)
	w.activeTag = tag
}

// popTag forgets an inline tag once it closes, so that the text after it is
// not read as inside it: Docutils wraps a hyphenated literal in
// `<tt><span class="pre">`, and with the pair left in the history every
// later text node of the paragraph passed for literal content. The tag's
// class stays in clsHistory: an inline element's class reaches the scope of
// the block holding it, which is how a QDoc `\span {header}` is targeted.
func (w *walker) popTag(tag string) {
	if n := len(w.tagHistory); n > 0 && w.tagHistory[n-1] == tag {
		w.tagHistory = w.tagHistory[:n-1]
	}
}

// An openTag is one still-open block container.
type openTag struct {
	tag string
	cls string

	// ids names the selections the element belongs to, and agg gathers its
	// text for them.
	ids []string
	agg *aggregate
}

// An aggregate gathers the text of an element a `doc(...)` selector matched,
// to be linted as one block when the element closes.
type aggregate struct {
	ids     []string
	line    int
	text    strings.Builder
	metrics map[string]int
}

// enclose records a block container as open. `marks` names the selections
// that matched it, if any.
func (w *walker) enclose(tag, cls, marks string) {
	open := openTag{tag: tag, cls: cls}
	if ids := strings.Fields(marks); len(ids) > 0 {
		open.ids = ids
		open.agg = &aggregate{ids: ids, line: -1}
		w.aggs = append(w.aggs, open.agg)
		if w.seen == nil {
			w.seen = map[string]bool{}
		}
		for _, id := range ids {
			w.seen[id] = true
		}
	}
	w.enclosing = append(w.enclosing, open)
}

// selections returns the ids of every selection the current block is in,
// without repeats: those of the containers still open around it, and those
// of the block's own element, which has closed by the time it is built.
func (w *walker) selections() []string {
	var found []string
	add := func(id string) {
		if !core.StringInSlice(id, found) {
			found = append(found, id)
		}
	}
	for _, o := range w.enclosing {
		for _, id := range o.ids {
			add(id)
		}
	}
	for _, id := range w.idsHistory {
		add(id)
	}
	return found
}

// gather adds a block's text to every selection still open around it, and
// counts it under the metric the file counts it under.
func (w *walker) gather(txt string, line int, metric string) {
	for _, a := range w.aggs {
		if a.line < 0 {
			a.line = line
		}
		a.text.WriteString(txt)
		a.text.WriteString("\n\n")
	}
	w.count(metric)
}

// count records an element in every open selection.
func (w *walker) count(metric string) {
	for _, a := range w.aggs {
		if a.metrics == nil {
			a.metrics = map[string]int{}
		}
		a.metrics[metric]++
	}
}

// closeSelection returns the selection whose element has just closed, once
// the walker has flushed the element's own text, or nil.
//
// The aggregate leaves the open set here rather than when the end tag was
// read: the flush between the two is what gathers a paragraph's or heading's
// own text, and a selection on one gathered nothing when it was popped first.
func (w *walker) closeSelection() *aggregate {
	a := w.closed
	w.closed = nil
	if a != nil {
		for i := len(w.aggs) - 1; i >= 0; i-- {
			if w.aggs[i] == a {
				w.aggs = append(w.aggs[:i], w.aggs[i+1:]...)
				break
			}
		}
	}
	return a
}

// unclose closes the innermost open container with the given tag.
//
// A selection closes with its element. It is held until the element's text
// has been flushed, since that text is the element's own.
func (w *walker) unclose(tag string) {
	for i := len(w.enclosing) - 1; i >= 0; i-- {
		if w.enclosing[i].tag == tag {
			if a := w.enclosing[i].agg; a != nil {
				w.closed = a
			}
			w.enclosing = append(w.enclosing[:i], w.enclosing[i+1:]...)
			return
		}
	}
}

// inClass reports whether a block container still open carries one of the
// given classes.
func (w *walker) inClass(classes []string) bool {
	for _, o := range w.enclosing {
		if checkClasses(o.cls, classes) {
			return true
		}
	}
	return false
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
		if len(b.Runs) == 0 {
			b.Runs = w.locateWrapped(text)
		}
	}

	return b
}

// locateWrapped places a block whose lines are in the source but whose line
// breaks are followed there by a list item's indentation or a blockquote's
// `>`, which the text does not carry. Each line becomes a run, so a match
// across the break is placed by its bytes. Without this the block is found
// by searching for its text, and a copy of the same text written without
// the indentation -- a paragraph after the list -- takes the alert. See #1203.
func (w *walker) locateWrapped(text string) []nlp.Run {
	if !strings.Contains(text, "\n") || isMask(text) {
		return nil
	}
	ctx := w.getCtx()
	if w.cursor > len(ctx) {
		return nil
	}

	runs, end := wrappedRuns(text, ctx, w.cursor)
	if runs == nil {
		return nil
	}
	w.cursor = end
	return runs
}

// wrappedRuns finds text in ctx at or after from, allowing each of its line
// breaks to be followed in ctx by indentation or `>` markers, and returns the
// runs placing each line, plus the index just past the match.
func wrappedRuns(text, ctx string, from int) ([]nlp.Run, int) {
	lines := strings.Split(text, "\n")
	for start := from; start <= len(ctx); {
		i := strings.Index(ctx[start:], lines[0])
		if i < 0 {
			return nil, -1
		}
		at := start + i
		if runs, end, ok := matchWrapped(lines, ctx, at); ok {
			return runs, end
		}
		start = at + 1
	}
	return nil, -1
}

// matchWrapped matches lines against ctx from at, each after the first
// preceded by a newline and any indentation or quote markers.
func matchWrapped(lines []string, ctx string, at int) ([]nlp.Run, int, bool) {
	var runs []nlp.Run
	src, txt := at, 0
	for i, line := range lines {
		if i > 0 {
			if src >= len(ctx) || ctx[src] != '\n' {
				return nil, 0, false
			}
			src++
			txt++ // the text's own newline
			for src < len(ctx) && (ctx[src] == ' ' || ctx[src] == '\t' || ctx[src] == '>') {
				src++
			}
		}
		if !strings.HasPrefix(ctx[src:], line) {
			return nil, 0, false
		}
		if line != "" {
			runs = append(runs, nlp.Run{At: txt, Src: src, N: len(line)})
		}
		src += len(line)
		txt += len(line)
	}
	return runs, src, true
}

// isMask reports whether text is nothing but mask characters, as the text
// of a skipped inline element is. Such a block is nowhere in the source,
// and searching for it lands on whatever the source has masked the same
// way -- an info string -- dragging the cursor past real text.
func isMask(text string) bool {
	return text != "" && strings.Trim(text, "*@ \t\n") == ""
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
	if text == "" || isMask(text) {
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

	// The search runs from the last comment, not from the block cursor: a
	// converter may emit a block ahead of a comment written before it, as
	// rst2html does with a promoted document title.
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
		w.mapWords(at, raw, ctx)
		return
	}

	src := w.srcCursor + i
	w.srcCursor = src + len(raw)
	w.spans = append(w.spans, nlp.Run{At: at, Src: src, N: len(raw)})
}

// mapWords maps a run that isn't in the source as written one word at a
// time, in order. A converter can drop markup from inside a run -- Sphinx's
// `:ref:` keeps only its title -- and the words are still there to be found;
// unmapped, a match in the run was placed by searching from the top of the
// file, on whatever came first.
func (w *walker) mapWords(at int, raw, ctx string) {
	// The words sit close together in the source: the markup between two of
	// them is short, so a word far ahead is someone else's.
	const gap = 256

	cursor := w.srcCursor
	var runs []nlp.Run
	for _, word := range wordSpans(raw) {
		limit := min(len(ctx), cursor+gap+word[1]-word[0])
		i := strings.Index(ctx[cursor:limit], raw[word[0]:word[1]])
		if i < 0 {
			// Rewritten too, as an escaped `\*` is: it stays unmapped, and
			// the words after it are still found.
			continue
		}
		src := cursor + i
		runs = append(runs, nlp.Run{At: at + word[0], Src: src, N: word[1] - word[0]})
		cursor = src + word[1] - word[0]
	}

	w.srcCursor = cursor
	w.spans = append(w.spans, runs...)
}

// wordSpans returns the start and end of each whitespace-separated word in s.
func wordSpans(s string) [][2]int {
	var out [][2]int
	start := -1
	for i, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				out = append(out, [2]int{start, i})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(s)})
	}
	return out
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

// walk trims only ASCII whitespace: a non-breaking or narrow space beside
// inline markup is text the rules should see, not a separator (#1197).
func (w *walker) walk() (html.TokenType, html.Token, string) {
	tokt := w.z.Next()
	tok := w.z.Token()
	return tokt, tok, html.UnescapeString(strings.Trim(tok.Data, " \t\n\r\f\v"))
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
				forms := []string{a.Val}
				if a.Key == "href" {
					if decoded, err := url.PathUnescape(a.Val); err == nil && decoded != a.Val {
						forms = append(forms, decoded)
					}
				}
				w.pending = append(w.pending, forms)
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
// The search is for the first occurrence in the masked context, from the
// top: it is what keeps a block's line right when the cursors that place
// blocks have run ahead. The index serves it without the scan.
//
// The line this returns is a fallback, and core.assignLoc has a standing NOTE
// saying so. It matters less than it did: an alert in a block holding inline
// markup is now placed by the runs the block was read from rather than by
// searching from this line, which is what moved the rst alert in
// `testdata/e2e/frontmatter.yaml` out of a literal block and onto the prose it
// belongs to. What still comes through here is a block no run could place.
func (w *walker) advance(text string) int {
	if isMask(text) {
		return -1
	}
	last := text
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		last = text[i+1:]
	}

	pos := w.first(last)
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
			pos = w.first(longest)
		}
	}

	if pos >= 0 {
		if l := w.lineAt(pos); l > w.idx {
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
	maskAt(ctx, idx, sub, char)
	return true
}

// maskAt overwrites the occurrence of sub at idx, byte for byte, leaving
// newlines and multibyte runes in place.
func maskAt(ctx []byte, idx int, sub string, char rune) {
	mask := byte(char) //nolint:gosec // every mask is ASCII
	for _, r := range sub {
		if r != '\n' && utf8.RuneLen(r) == 1 {
			ctx[idx] = mask
		}
		idx += utf8.RuneLen(r)
	}
}
