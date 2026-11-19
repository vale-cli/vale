package lint

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	grh "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/vale-cli/vale/v3/internal/core"
)

// Markdoc is CommonMark plus tags -- `{% name attr="value" %}` through
// `{% /name %}` -- and, in the same delimiters, annotations, variables, and
// functions, all of which a Markdown parser reads as prose. markdocExtension
// parses them into shapes the walker already understands. See #1213.
//
// A tag's body is Markdown by specification, so a paired tag becomes
// `<div class="<name>">` (a span, when inline) and its content is linted
// with the tag's name as a class scope (`text.class.callout`). A tag's
// string attributes are its data rather than its prose: each becomes a
// `data` element classed with the attribute's name, so a rule that asks for
// `meta.class.title` reaches it and every other rule passes over it. The
// rest -- annotations, variables, functions, closing tags, and the other
// kinds of value -- renders as nothing at all.

// Markdoc configuration: Markdown, plus the Markdoc constructs.
var goldMarkdoc = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		mathExtension{},
		markdocExtension{},
	),
	goldmark.WithRendererOptions(
		grh.WithUnsafe(),
	),
)

type markdocExtension struct{}

func (markdocExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithBlockParsers(
		// Ahead of the paragraph parser (1000), which would otherwise read a
		// tag line as prose; no Markdown block opens with '{'.
		util.Prioritized(&markdocBlockParser{}, 990),
	))
	m.Parser().AddOptions(parser.WithInlineParsers(
		// '{' has no built-in owner, so the priority is uncontested.
		util.Prioritized(&markdocInlineParser{}, 100),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(markdocRenderer{}, 1),
	))
}

// A markdocTag is one `{% ... %}` as read from the source.
type markdocTag struct {
	// name is the tag's; empty for an annotation, a variable, or a function.
	name string
	// close is a `{% /name %}`, and self a `{% name /%}`.
	close, self bool
	// attrs holds the string-valued attributes, in order.
	attrs [][2]string
	// end is the offset just past `%}`.
	end int
}

// scanMarkdocTag reads the tag opening at src[at:], which begins with `{%`,
// up to the first `%}` outside a string. It reports false when none closes
// it, in which case the text is prose.
func scanMarkdocTag(src []byte, at int) (markdocTag, bool) {
	for i := at + 2; i < len(src); i++ {
		switch src[i] {
		case '"':
			i, _ = skipMarkdocString(src, i)
			i--
		case '%':
			if i+1 < len(src) && src[i+1] == '}' {
				tag := parseMarkdocTag(src[at+2 : i])
				tag.end = i + 2
				return tag, true
			}
		}
	}
	return markdocTag{}, false
}

// markdocBlockTag reads the tag opening at src[at:] when it stands alone:
// nothing but whitespace follows its `%}` on that line. A tag followed by
// text is inline, and the paragraph parser's to read.
func markdocBlockTag(src []byte, at int) (markdocTag, bool) {
	if at+1 >= len(src) || src[at] != '{' || src[at+1] != '%' {
		return markdocTag{}, false
	}
	tag, ok := scanMarkdocTag(src, at)
	if !ok {
		return markdocTag{}, false
	}
	rest := src[tag.end:]
	if i := bytes.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	if !util.IsBlank(rest) {
		return markdocTag{}, false
	}
	return tag, true
}

// parseMarkdocTag reads what sits between a tag's delimiters.
func parseMarkdocTag(s []byte) markdocTag {
	var tag markdocTag

	s = bytes.TrimSpace(s)
	if len(s) > 0 && s[0] == '/' {
		tag.close = true
		tag.name = string(markdocIdent(s[1:]))
		return tag
	}
	if len(s) > 0 && s[len(s)-1] == '/' {
		tag.self = true
		s = bytes.TrimSpace(s[:len(s)-1])
	}

	// A tag is named; an annotation opens with an attribute, a variable
	// with `$`, and a function is a name followed by its arguments.
	name := markdocIdent(s)
	rest := s[len(name):]
	if len(name) == 0 || (len(rest) > 0 && (rest[0] == '=' || rest[0] == '(')) {
		return tag
	}
	tag.name = string(name)
	tag.attrs = markdocAttrs(rest)
	return tag
}

// markdocIdent returns the identifier s opens with: a letter, then letters,
// digits, hyphens, and underscores.
func markdocIdent(s []byte) []byte {
	i := 0
	for i < len(s) {
		c := s[i]
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || !((c >= '0' && c <= '9') || c == '-' || c == '_')) {
			break
		}
		i++
	}
	return s[:i]
}

func markdocSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// markdocAttrs returns the string-valued `name="value"` attributes in s. The
// other items -- shorthands, a primary value, and values of every other
// type -- are read only to be passed over.
func markdocAttrs(s []byte) [][2]string {
	var attrs [][2]string
	for i := 0; i < len(s); {
		if markdocSpace(s[i]) {
			i++
			continue
		}
		name := markdocIdent(s[i:])
		if n := len(name); n > 0 && i+n < len(s) && s[i+n] == '=' {
			val, next := markdocValue(s, i+n+1)
			if val != nil {
				attrs = append(attrs, [2]string{string(name), string(val)})
			}
			i = next
			continue
		}
		_, i = markdocValue(s, i)
	}
	return attrs
}

// markdocValue reads the value at s[at:], returning its text when it is a
// string and the offset just past it either way.
func markdocValue(s []byte, at int) ([]byte, int) {
	if at >= len(s) {
		return nil, at
	}
	switch s[at] {
	case '"':
		end, closed := skipMarkdocString(s, at)
		if !closed {
			return s[at+1 : end], end
		}
		return s[at+1 : end-1], end
	case '[', '{':
		return nil, skipMarkdocBalanced(s, at)
	}

	// A number, boolean, null, variable, function call, or shorthand runs
	// to the next space, with its brackets balanced and strings whole.
	i := at
	for i < len(s) && !markdocSpace(s[i]) {
		switch s[i] {
		case '(', '[', '{':
			i = skipMarkdocBalanced(s, i)
		case '"':
			i, _ = skipMarkdocString(s, i)
		default:
			i++
		}
	}
	return nil, i
}

// skipMarkdocString returns the offset just past the string opening at
// s[at], honoring backslash escapes, and whether a closing quote was found.
func skipMarkdocString(s []byte, at int) (int, bool) {
	for i := at + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		}
	}
	return len(s), false
}

// skipMarkdocBalanced returns the offset just past the bracket matching the
// one at s[at], with the strings inside read whole.
func skipMarkdocBalanced(s []byte, at int) int {
	depth := 0
	for i := at; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '"':
			i, _ = skipMarkdocString(s, i)
			i--
		}
	}
	return len(s)
}

// A markdocBlock is a tag standing on a line of its own: a paired tag, whose
// children are the Markdown up to its closing tag, or markup -- a closing
// tag, a self-closing tag, an annotation, or an interpolation -- that
// renders as nothing.
type markdocBlock struct {
	ast.BaseBlock

	tag markdocTag

	// nested counts the open tags of this tag's name inside it, so the
	// closing tag that ends it is its own and not a child's.
	nested int
}

var kindMarkdocBlock = ast.NewNodeKind("MarkdocBlock")

func (n *markdocBlock) Kind() ast.NodeKind { return kindMarkdocBlock }
func (n *markdocBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// paired reports whether the tag opens a body.
func (n *markdocBlock) paired() bool {
	return n.tag.name != "" && !n.tag.close && !n.tag.self
}

type markdocBlockParser struct{}

func (*markdocBlockParser) Trigger() []byte {
	return []byte{'{'}
}

func (*markdocBlockParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || pos >= len(line) {
		return nil, parser.NoChildren
	}

	tag, ok := markdocBlockTag(reader.Source(), segment.Start+pos)
	if !ok {
		return nil, parser.NoChildren
	}

	reader.Advance(segment.Len() - 1)
	n := &markdocBlock{tag: tag}
	if !n.paired() {
		return n, parser.NoChildren
	}
	return n, parser.HasChildren
}

func (*markdocBlockParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	n, ok := node.(*markdocBlock)
	if !ok {
		return parser.Close
	}
	line, segment := reader.PeekLine()

	// The opener's own lines: attributes may span several.
	if segment.Start < n.tag.end {
		reader.Advance(segment.Len() - 1)
		if n.paired() {
			return parser.Continue | parser.HasChildren
		}
		return parser.Continue | parser.NoChildren
	}
	if !n.paired() {
		return parser.Close
	}

	// A tag of this tag's name on a line of its own: the closing tag that
	// ends it, or a nested one whose closing tag must not.
	if w, pos := util.IndentWidth(line, reader.LineOffset()); w < 4 {
		tag, found := markdocBlockTag(reader.Source(), segment.Start+pos)
		if found && tag.name == n.tag.name {
			switch {
			case tag.close && n.nested == 0:
				reader.Advance(segment.Len() - 1)
				return parser.Close
			case tag.close:
				n.nested--
			case !tag.self:
				n.nested++
			}
		}
	}
	return parser.Continue | parser.HasChildren
}

func (*markdocBlockParser) Close(ast.Node, text.Reader, parser.Context) {}

func (*markdocBlockParser) CanInterruptParagraph() bool { return true }
func (*markdocBlockParser) CanAcceptIndentedLine() bool { return false }

// A markdocInline is a tag within a line of text: a paired tag's opener or
// closer, read as a span's, or markup that renders as nothing.
type markdocInline struct {
	ast.BaseInline

	tag markdocTag
}

var kindMarkdocInline = ast.NewNodeKind("MarkdocInline")

func (n *markdocInline) Kind() ast.NodeKind { return kindMarkdocInline }
func (n *markdocInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type markdocInlineParser struct{}

func (*markdocInlineParser) Trigger() []byte {
	return []byte{'{'}
}

func (*markdocInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 2 || line[1] != '%' {
		return nil
	}
	// Within the line: a tag whose attributes wrap inside a paragraph is
	// left as the prose it reads as.
	tag, ok := scanMarkdocTag(line, 0)
	if !ok {
		return nil
	}
	block.Advance(tag.end)
	return &markdocInline{tag: tag}
}

type markdocRenderer struct{}

func (markdocRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMarkdocBlock, renderMarkdocBlock)
	reg.Register(kindMarkdocInline, renderMarkdocInline)
}

// writeMarkdocAttrs writes a tag's string attributes as `data` elements,
// each classed with the attribute's name.
func writeMarkdocAttrs(w util.BufWriter, tag markdocTag) {
	for _, attr := range tag.attrs {
		_, _ = w.WriteString(`<data class="`)
		_, _ = w.Write(util.EscapeHTML([]byte(attr[0])))
		_, _ = w.WriteString(`">`)
		_, _ = w.Write(util.EscapeHTML([]byte(attr[1])))
		_, _ = w.WriteString("</data>")
	}
}

func renderMarkdocBlock(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n, ok := node.(*markdocBlock)
	if !ok || n.tag.name == "" || n.tag.close {
		return ast.WalkSkipChildren, nil
	}

	if entering {
		_, _ = w.WriteString(`<div class="`)
		_, _ = w.Write(util.EscapeHTML([]byte(n.tag.name)))
		_, _ = w.WriteString(`">`)
		writeMarkdocAttrs(w, n.tag)
		if n.tag.self {
			_, _ = w.WriteString("</div>\n")
			return ast.WalkSkipChildren, nil
		}
		_, _ = w.WriteString("\n")
	} else {
		_, _ = w.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}

func renderMarkdocInline(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n, ok := node.(*markdocInline)
	if !ok || !entering || n.tag.name == "" {
		return ast.WalkSkipChildren, nil
	}

	if n.tag.close {
		_, _ = w.WriteString("</span>")
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(`<span class="`)
	_, _ = w.Write(util.EscapeHTML([]byte(n.tag.name)))
	_, _ = w.WriteString(`">`)
	writeMarkdocAttrs(w, n.tag)
	if n.tag.self {
		_, _ = w.WriteString("</span>")
	}
	return ast.WalkSkipChildren, nil
}

// lintMarkdoc lints Markdoc: Markdown, parsed with the Markdoc constructs.
func (l *Linter) lintMarkdoc(f *core.File) error {
	return l.lintMarkdownWith(f, goldMarkdoc)
}
