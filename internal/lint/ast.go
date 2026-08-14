package lint

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// skipTags are tags that we don't want to lint.
var skipTags = []string{"script", "style", "pre", "figure", "noscript", "iframe"}

// skipClasses are classes that we don't want to lint:
//   - `problematic` is added by rst2html to processing errors which, in our
//     case, could be things like file-insertion URLs.
//   - `pre` is added by rst2html to code spans.
var skipClasses = []string{"problematic", "pre", "code"}
var inlineTags = []string{
	"b", "big", "i", "small", "abbr", "acronym", "cite", "dfn", "em", "kbd",
	"strong", "a", "br", "img", "span", "sub", "sup", "code", "tt", "del"}

// voidTags never carry content, so they are never "open" as containers.
var voidTags = []string{
	"area", "base", "br", "col", "embed", "hr", "img", "input", "link",
	"meta", "source", "track", "wbr"}

// inlineToScope names the inline elements that can be scoped on their own.
//
// These are siblings of `text`, not children of it: a scope of `text` is
// matched by containment, so `text.link` would be matched by every ordinary
// rule and the whole style would run a second time over each link. That
// doubling is why the previous attempt at this was removed. As `link`, only a
// rule that asks for one does any work.
var inlineToScope = map[string]string{
	"a":      "link",
	"strong": "strong",
	"b":      "strong",
	"em":     "emphasis",
	"i":      "emphasis",
	"code":   "code",
	"tt":     "code",
}

var tagToScope = map[string]string{
	"th":         "text.table.header",
	"td":         "text.table.cell",
	"caption":    "text.table.caption",
	"li":         "text.list",
	"blockquote": "text.blockquote",
	"figcaption": "text.figure.caption",
}

func (l *Linter) lintHTMLTokens(f *core.File, raw []byte, offset int) error { //nolint:unparam
	var class, parentClass, attr string
	var inBlock, inline, skip, skipClass bool
	// closedInline records that the previous token closed an inline element.
	// Only then does the next text node's leading whitespace faithfully
	// reflect the source, since `walk` trims it away before we see it.
	var closedInline bool

	buf := bytes.NewBufferString("")

	// The user has specified a custom list of tags/classes to ignore.
	if len(l.Manager.Config.SkippedScopes) > 0 {
		skipTags = l.Manager.Config.SkippedScopes
	}
	if len(l.Manager.Config.IgnoredClasses) > 0 {
		skipClasses = append(skipClasses, l.Manager.Config.IgnoredClasses...)
	}

	skipped := []string{"tt", "code", "kbd"}
	if len(l.Manager.Config.IgnoredScopes) > 0 {
		skipped = l.Manager.Config.IgnoredScopes
	}

	// Inline elements the style actually asks about; nothing is captured for
	// the rest, so a style that scopes none of them pays nothing at all.
	wanted := map[string]string{}
	for tag, scope := range inlineToScope {
		if l.Manager.HasScope(scope) {
			wanted[tag] = scope
		}
	}
	var open []inlineCapture

	walker := newWalker(f, raw, offset)
	for {
		tokt, tok, txt := walker.walk()

		walker.addCls(txt, tokt == html.StartTagToken)
		closed := walker.canClose()

		class = getAttribute(tok, "class")
		skipClass = checkClasses(class, skipClasses)

		if tokt == html.StartTagToken && !core.StringInSlice(txt, inlineTags) &&
			!core.StringInSlice(txt, voidTags) {
			walker.enclose(txt, class)
		} else if tokt == html.EndTagToken && !core.StringInSlice(txt, inlineTags) {
			walker.unclose(txt)
		}

		blockSkip := skipClass && !core.StringInSlice(txt, inlineTags)
		if tokt == html.ErrorToken { //nolint:gocritic
			break
		} else if tokt == html.StartTagToken && (core.StringInSlice(txt, skipTags) || blockSkip) {
			walker.setCls(txt, blockSkip)
			inBlock = true
			// A skipped *inline* element (e.g. `code` in SkippedScopes) still
			// separates the words around it, so mark the following text inline
			// to keep its leading space -- otherwise `in <code/> for` collapses
			// to `infor`. See #1052.
			inline = core.StringInSlice(txt, inlineTags)
			f.Metrics[txt]++
		} else if inBlock && (core.StringInSlice(txt, skipTags) || closed) {
			inBlock = false
			if closed {
				walker.close()
			}
		} else if tokt == html.StartTagToken {
			// Text buffered before a block-level child belongs to the
			// enclosing element: a tight list item's own text, when a nested
			// list opens inside it. Lint it now, under the parent's scope;
			// left in the buffer it fuses with the child's first item, an
			// end-anchored rule sees the child's ending, and the parent item
			// never reaches `list`-scoped rules on its own. See #791.
			if !core.StringInSlice(txt, inlineTags) && !core.StringInSlice(txt, voidTags) &&
				strings.TrimSpace(buf.String()) != "" {
				if err := l.lintScope(f, walker, buf.String()); err != nil {
					return err
				}
				walker.reset()
				buf.Reset()
			}
			if (txt == "em" || txt == "b") && walker.lastTag() == "code" {
				// FIXME: See https://github.com/errata-ai/vale/issues/421
				txt = "code"
			}
			inline = core.StringInSlice(txt, inlineTags)
			skip = core.StringInSlice(txt, skipped)
			closedInline = false
			if scope, ok := wanted[txt]; ok {
				// A skipped element's text is masked out of the block, so its
				// capture has to read the text as it arrived instead.
				open = append(open, inlineCapture{
					tag: txt, scope: scope, masked: core.StringInSlice(txt, skipped)})
			}
			walker.addTag(txt, class)
		} else if tokt == html.EndTagToken && core.StringInSlice(txt, inlineTags) {
			walker.activeTag = ""
			closedInline = true
			if n := len(open); n > 0 && open[n-1].tag == txt {
				done := open[n-1]
				open = open[:n-1]
				if body := strings.TrimSpace(done.text); body != "" {
					walker.inline = append(walker.inline, inlineCapture{
						tag: done.tag, scope: done.scope,
						masked: done.masked, text: body})
				}
			}
		} else if tokt == html.CommentToken {
			// A comment, like a closed inline element, is a source-faithful
			// boundary: trust the next text's own whitespace (#882). Inside a
			// still-open inline element the opening tag remains the boundary,
			// so leave the state alone and keep its padding (#1052).
			if !inline {
				closedInline = true
			}
			f.UpdateComments(txt)
			walker.update(txt, tokt)
		} else if tokt == html.TextToken {
			skip = skip || shouldBeSkipped(walker.tagHistory, f.NormedExt)
			// NOTE: We used to create a "temporary" context here to support
			// nested scopes, such as 'link', that are linted twice: once as
			// their own scope and once as part of the overall paragraph.
			//
			// (See issue #105 for more info.)
			//
			// We no longer support this because the performance (+memory)
			// overhead is too high. Instead, we recommend that users use
			// `scope: raw` and target the actual markup they want to lint.
			//
			// Styles should still be able to support rules that require this
			// (such as disallowing 'here' links) by using format-specific
			// scopes (e.g., `text.md`).
			walker.append(txt)
			// Text inside an active `vale off` region is excluded from the
			// block entirely. The whole paragraph is linted at once (on the
			// closing tag), by which point the `off` toggle has flipped back,
			// so gating shouldRun isn't enough to suppress inline `vale off`
			// ... `vale on` spans -- e.g. `*Note*: <!-- vale off -->TODO<!--
			// vale on -->`. See #1001.
			if !inBlock && !f.Comments["off"] && txt != "" {
				skipClass = checkClasses(parentClass, skipClasses)
				if walker.isNestedList() && !inline {
					txt = " " + txt
				}
				// Once an inline element closes, the next token's leading
				// whitespace is exactly what the source had, so trust it
				// rather than inferring one: `<code>X</code>s` must not gain a
				// space (#1111) and `<strong>x</strong> :` must not lose one
				// (#1119).
				spaced := startsWithSpace(tok.Data)
				// Kept before `clean`, which empties the text of a skipped
				// element: inline code never reaches the block, so a capture of
				// it has nothing else to read.
				raw := txt
				txt, skip = clean(txt, attr, skip || skipClass, inline, spaced, closedInline)
				closedInline = false
				// `clean` prefixes inline content with a space so it doesn't
				// fuse with the preceding text. When that content directly
				// follows a tight boundary -- an opening bracket (a link in
				// parentheses, `([HNSW](...))`, #1056) or a dash
				// (`Triggers—**Article**`, #1029) -- the space is spurious and
				// produces false positives like ` —` / `( ACRONYM)`.
				if strings.HasPrefix(txt, " ") && endsWithTightBoundary(buf) {
					txt = txt[1:]
				}
				// Record where this run came from before it loses its identity
				// in the block. It places inline fragments, and it places a
				// match in a block that inline markup has made unfindable in
				// the source -- `has <b>has</b>` arrives as `has has`, which is
				// nowhere in the file. See #502.
				//
				// Only a run that survived extraction unchanged can be mapped;
				// `clean` may have prefixed a space, which belongs to the block
				// and not to the source.
				if body := strings.TrimLeft(txt, " "); body == raw {
					walker.mapRun(buf.Len()+(len(txt)-len(body)), raw)
				}
				buf.WriteString(txt)
				// Feed the same text to any inline element still open, so a
				// captured fragment matches what the block itself holds --
				// except where the block holds nothing, and the text as it was
				// read is all there is.
				for j := range open {
					if open[j].masked {
						open[j].text += raw
					} else {
						open[j].text += txt
					}
				}
			}
		}

		if tokt == html.EndTagToken && !core.StringInSlice(txt, inlineTags) {
			content := buf.String()
			if strings.TrimSpace(content) != "" {
				err := l.lintScope(f, walker, content)
				if err != nil {
					return err
				}
			}
			walker.reset()
			buf.Reset()
		}

		attr = getAttribute(tok, "href")
		parentClass = getAttribute(tok, "class")

		if err := l.lintTags(f, walker, tok); err != nil {
			return err
		}

		walker.replaceToks(tok)
	}

	return l.lintSizedScopes(f)
}

func (l *Linter) lintScope(f *core.File, state *walker, txt string) error {
	// A `data` element holds a document's machine-readable content -- an
	// anchor name, a file name, an identifier -- which is in the document
	// without being prose.
	//
	// Like `link` and `code`, `meta` is a sibling of `text` rather than a
	// child of it, so an ordinary rule passes over it and only a rule that
	// asks for `meta` does any work. That is the whole point: the name is
	// reachable for a rule about naming, and invisible to a style about
	// writing. It is not segmented either -- an identifier has no sentences.
	if core.StringInSlice("data", state.tagHistory) {
		f.Metrics["meta"]++

		b := state.block(txt, withClasses("meta", state)+metaScope(f)+f.RealExt, 0)
		return l.lintBlock(f, b, state.lines, 0, false)
	}

	for _, tag := range state.tagHistory {
		scope, match := tagToScope[tag]
		if (match && !core.StringInSlice(tag, inlineTags)) || heading.MatchString(tag) {
			if scope == "text.blockquote" || scope == "text.list" {
				f.Summary.WriteString(txt + "\n\n")
			}

			if !match {
				scope = "text.heading." + tag
			}
			f.Metrics[strings.TrimPrefix(scope, "text.")]++

			shift := len(txt)
			txt = strings.TrimLeft(txt, " ")
			shift -= len(txt)

			b := state.block(txt, withClasses(scope, state)+f.MetaScope+f.RealExt, shift)

			// Prose, not just a block: a list item or a heading is made of
			// sentences the same way a paragraph is, and only this path segments
			// them. Sending these straight to lintBlock left `sentence.*` blocks
			// existing for paragraphs alone, so any rule scoped to a sentence --
			// which is every `sequence` rule, and so every rule that can reach
			// part-of-speech data -- silently skipped the list items, headings
			// and table cells where much of a document's prose lives.
			//
			// The same text in a `.txt` file already went through here, which is
			// why renaming a file changed what matched. See #1124.
			//
			// This costs nothing when it is not needed: segmenting, splitting and
			// tagging are each switched on only if some rule asks for that scope,
			// and with all three off this reduces to the lintBlock call it
			// replaced.
			//
			// Prose, but not paragraphs: a rule scoped to `paragraph` must not
			// reach a heading or a table cell. See #1132.
			if err := l.lintProse(f, b, state.lines, false); err != nil {
				return err
			}
			return l.lintInline(f, state, b, state.lines, shift)
		}
	}

	f.Summary.WriteString(txt + "\n\n")

	// Counted here, and not from the summary: quotes and list items share the
	// summary so that readability metrics see all of a document's prose, but
	// `paragraphs` means what the `paragraph` scope reaches -- this branch.
	f.Metrics["paragraphs"]++

	b := state.block(txt, withClasses("text", state)+f.MetaScope+f.RealExt, 0)
	if err := l.lintProse(f, b, state.lines, true); err != nil {
		return err
	}
	return l.lintInline(f, state, b, state.lines, 0)
}

// lintInline lints the inline elements captured inside blk -- link text, bold
// and so on -- as scopes of their own.
//
// Their position comes from the block rather than from the walker's cursor,
// which the block has already consumed: an inline fragment sits inside the text
// that was just placed, so its offset is the block's plus its index within it.
func (l *Linter) lintInline(f *core.File, state *walker, blk nlp.Block, lines, shift int) error {
	// Captures arrive in document order, so each search starts where the last
	// one ended. Two links with the same text are two alerts; searching from
	// the start each time would place both on the first and lose one.
	//
	// Two cursors, because the two kinds are found in different strings: an
	// ordinary fragment is part of the block and is translated to the source
	// through the runs recorded as it was read, while a masked one -- inline
	// code, written out of the block -- was never in the block to translate.
	within, source := 0, 0
	for _, cap := range state.inline {
		var at int
		if cap.masked {
			at, source = seek(blk.Context, cap.text, source)
		} else {
			at, within = seek(blk.Text, cap.text, within)
			if at >= 0 {
				at = state.sourceOffset(at + shift)
			}
		}

		b := nlp.NewLinedBlock(
			blk.Context, cap.text, cap.scope+f.MetaScope+f.RealExt, blk.Line)
		b.Offset = at

		// Without an offset the match has to be located by searching, which is
		// what `lookup` asks for.
		if err := l.lintBlock(f, b, lines, 0, at < 0); err != nil {
			return err
		}
	}

	return nil
}

// seek finds text in s at or after from, returning where it begins and where
// the next search should start. A miss leaves the cursor where it was, so one
// fragment that cannot be placed does not displace the rest.
func seek(s, text string, from int) (int, int) {
	if from < 0 || from > len(s) {
		return -1, from
	}
	i := strings.Index(s[from:], text)
	if i < 0 {
		return -1, from
	}
	at := from + i
	return at, at + len(text)
}

// withClasses adds the classes enclosing a block to its scope, so that markup
// carrying no distinct tag can still be selected. An AsciiDoc block title is a
// `<div class="title">`, indistinguishable from body text by tag alone, and
// becomes `text.class.title` here. See #642.
// metaScope is the context a fragment carries, with the `text` it opens with
// taken off.
//
// MetaScope is appended to every block's scope, and a comment's is
// `text.comment.line`. A selector matches on the set of sections a scope has,
// so appending that to `meta` puts `text` back and an ordinary rule matches
// the content `meta` exists to hold apart -- the same QDoc comment lints
// differently in a `.qdoc` file, where there is no MetaScope, than in the C++
// source it was written in. See #784.
func metaScope(f *core.File) string {
	return strings.TrimPrefix(f.MetaScope, ".text")
}

func withClasses(scope string, state *walker) string {
	classes := state.classes()
	if len(classes) == 0 {
		return scope
	}
	return scope + ".class." + strings.Join(classes, ".class.")
}

func (l *Linter) lintSizedScopes(f *core.File) error {
	f.ResetComments()

	// Run all rules with `scope: summary`
	//
	// TODO: is this the most efficient place to assign tagging?
	summary := nlp.NewLinedBlock(f.Content, f.Summary.String(),
		"summary"+f.RealExt, 0)

	for _, blk := range []nlp.Block{summary} {
		err := l.lintBlock(f, blk, len(f.Lines), 0, true)
		if err != nil {
			return err
		}
	}

	return nil
}

func (l *Linter) lintTags(f *core.File, state *walker, tok html.Token) error {
	ignored := core.StringInSlice("alt", l.Manager.Config.SkippedScopes)
	if tok.Data == "img" {
		for _, a := range tok.Attr {
			if a.Key == "alt" && !ignored {
				err := l.lintBlock(
					f,
					state.block(a.Val, "text.attr."+a.Key, 0), state.lines, 0, false)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkClasses(attr string, ignore []string) bool {
	for _, class := range strings.Split(attr, " ") {
		if core.StringInSlice(class, ignore) {
			return true
		}
	}
	return false
}

// HACK: We need to look for inserted `spans` within `tt` tags.
//
// See https://github.com/errata-ai/vale/v2/issues/140.
func shouldBeSkipped(tagHistory []string, ext string) bool {
	if ext == ".rst" {
		n := len(tagHistory)
		for i := n - 1; i >= 0; i-- {
			if tagHistory[i] == "span" {
				continue
			}
			return tagHistory[i] == "tt" && i+1 != n
		}
	}
	return false
}

// endsWithTightBoundary reports whether the buffer ends with a character that
// binds tightly to what follows -- an opening bracket or a dash -- so inline
// content placed after it shouldn't be padded with a leading space. Handles
// "([HNSW](...))" (#1056) and "Triggers—**Article**" (#1029). We decode the
// last rune because dashes are multi-byte.
func endsWithTightBoundary(buf *bytes.Buffer) bool {
	r, _ := utf8.DecodeLastRune(buf.Bytes())
	switch r {
	case '(', '[', '{', '—', '–':
		return true
	default:
		return false
	}
}

// startsWithSpace reports whether s begins with whitespace. It's applied to
// raw token data -- before `walk` trims it -- to recover whether the source
// actually separated a text node from the markup preceding it.
func startsWithSpace(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

func clean(txt, attr string, skip, inline, spaced, closedInline bool) (string, bool) {
	// Closing brackets are included so that inline content immediately
	// followed by one (e.g., a link inside parentheses) doesn't get a spurious
	// space inserted before it -- "(HNSW)" rather than "(HNSW )" (#1056). Dashes
	// are included so text like "—This" right after a link isn't padded to
	// " —This" (#1029).
	punct := []string{".", "?", "!", ",", ":", ";", ")", "]", "}", "—", "–"}
	first, _ := utf8.DecodeRuneInString(txt)
	starter := core.StringInSlice(string(first), punct) && !skip

	if skip || attr == txt {
		txt, _ = core.Substitute(txt, txt, '*')
		skip = false
	}

	// Text that follows a closed inline element is separated exactly as the
	// source separated it. Elsewhere -- text *inside* an inline element -- the
	// opening tag is the boundary and carries no whitespace of its own, so we
	// fall back to padding inline content; otherwise `in <code/> for`
	// collapses to `infor` (#1052).
	if (closedInline && spaced) || (!closedInline && inline && !starter) {
		txt = " " + txt
	}

	return txt, skip
}

func getAttribute(tok html.Token, key string) string {
	for _, attr := range tok.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}
