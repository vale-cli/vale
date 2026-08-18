package lint

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	mathjax "github.com/litao91/goldmark-mathjax"
)

// mathExtension parses `$$…$$` display math and `$…$` inline math, rendering
// them as elements vale skips -- so equations aren't spell-checked as prose
// (#878, #839).
//
// Neither parser is goldmark-mathjax's. Its inline parser takes any two `$` on
// a line as delimiters, so `$5 and $10` reads as math and the prose between
// them is silently dropped (see mathInlineParser). Its block parser only ever
// closes on a line that is nothing but `$$`, so `$$x=1$$`, a content line that
// ends in `$$`, and a `$$ … $$ {#eq-foo}` label all leave the block open -- and
// an unclosed block consumes the rest of the file (see mathBlockParser, #1148).
type mathExtension struct{}

func (mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithBlockParsers(
		util.Prioritized(mathBlockParser{}, 701),
	))
	m.Parser().AddOptions(parser.WithInlineParsers(
		// '$' has no built-in owner.
		util.Prioritized(mathInlineParser{}, 500),
	))
	// Priority must be lower than goldmark-mathjax's own renderers (501/502)
	// to win, since renderers are registered in reverse-priority order with
	// later registrations overwriting earlier ones.
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(mathRenderer{}, 1),
	))
}

// mathBlockParser reads `$$…$$` display math into a mathjax.MathBlock, the node
// the renderer already knows how to skip. Unlike goldmark-mathjax's parser it
// recognizes every place Pandoc closes the block, not just a bare `$$` line:
// the opener may also close (`$$x=1$$`), a content line may end in `$$`
// (`\end{aligned}$$`), and a Pandoc/Quarto label may trail the close
// (`$$ … $$ {#eq-foo}`). Missing those left the block open, and an open block
// swallows the rest of the document unlinted (#1148).
type mathBlockParser struct{}

// mathBlockData carries the opener's indent, so content lines dedent to match,
// and whether the opener already closed the block on its own line.
type mathBlockData struct {
	indent   int
	complete bool
}

var mathBlockInfoKey = parser.NewContextKey()

func (mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (mathBlockParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || line[pos] != '$' {
		return nil, parser.NoChildren
	}
	i := pos
	for i < len(line) && line[i] == '$' {
		i++
	}
	if i-pos < 2 {
		return nil, parser.NoChildren
	}

	node := mathjax.NewMathBlock()
	data := &mathBlockData{indent: pos}

	// A close later on the opening line makes this single-line display math:
	// `$$x=1$$` or `$$ … $$ {#eq-foo}`. Keep the whole line as content so none
	// of it -- delimiters, equation, or label -- is linted as prose.
	if mathCloseKind(line[i:]) != mathNoClose {
		node.Lines().Append(text.NewSegment(segment.Start+pos, segment.Stop))
		data.complete = true
	}

	pc.Set(mathBlockInfoKey, data)
	return node, parser.NoChildren
}

func (mathBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	data := pc.Get(mathBlockInfoKey).(*mathBlockData) //nolint:errcheck // set in Open
	if data.complete {
		// The opener closed the block; this line belongs to what follows.
		return parser.Close
	}

	line, segment := reader.PeekLine()
	w, pos := util.IndentWidth(line, 0)
	if w < 4 {
		switch mathCloseKind(line[pos:]) {
		case mathBareClose:
			// A line of just `$$`: markup, not content -- drop it and close,
			// as goldmark-mathjax did.
			reader.Advance(segment.Stop - segment.Start - segment.Padding)
			return parser.Close
		case mathContentClose:
			// `$$` ends a content line (`\end{aligned}$$`) or trails a label
			// (`$$ {#eq-foo}`). Keep the whole line as content, then close.
			node.Lines().Append(text.NewSegment(segment.Start+pos, segment.Stop))
			reader.Advance(segment.Stop - segment.Start - segment.Padding)
			return parser.Close
		case mathNoClose:
		}
	}

	pos, padding := util.DedentPosition(line, 0, data.indent)
	seg := text.NewSegmentPadding(segment.Start+pos, segment.Stop, padding)
	node.Lines().Append(seg)
	reader.AdvanceAndSetPadding(segment.Stop-segment.Start-pos-1, padding)
	return parser.Continue | parser.NoChildren
}

func (mathBlockParser) Close(_ ast.Node, _ text.Reader, pc parser.Context) {
	pc.Set(mathBlockInfoKey, nil)
}

func (mathBlockParser) CanInterruptParagraph() bool { return true }
func (mathBlockParser) CanAcceptIndentedLine() bool { return false }

// mathClose is how a display-math line ends: not a close, a bare `$$` line, or
// a `$$` that closes a line carrying other content (equation text or a label).
type mathClose int

const (
	mathNoClose mathClose = iota
	mathBareClose
	mathContentClose
)

// mathCloseKind reports whether s -- the tail of a line, with its trailing
// newline still attached -- closes display math. A bare close is a line of
// nothing but `$` delimiters; a content close ends in `$$` after an optional
// trailing Pandoc/Quarto `{…}` label, but carries other text as well.
func mathCloseKind(s []byte) mathClose {
	trimmed := bytes.TrimRight(s, " \t\r\n")
	body := trimMathLabel(trimmed)
	if len(body) < 2 || !bytes.HasSuffix(body, []byte("$$")) {
		return mathNoClose
	}
	if bytes.Equal(body, trimmed) && isAllDollars(body) {
		return mathBareClose
	}
	return mathContentClose
}

// trimMathLabel drops a single trailing `{…}` attribute list, the form Pandoc
// and Quarto use to label an equation (`$$ … $$ {#eq-foo}`).
func trimMathLabel(s []byte) []byte {
	if len(s) == 0 || s[len(s)-1] != '}' {
		return s
	}
	if i := bytes.LastIndexByte(s, '{'); i >= 0 {
		return bytes.TrimRight(s[:i], " \t")
	}
	return s
}

// isAllDollars reports whether s is two or more `$` and nothing else.
func isAllDollars(s []byte) bool {
	if len(s) < 2 {
		return false
	}
	for _, c := range s {
		if c != '$' {
			return false
		}
	}
	return true
}

// kindInlineMath identifies a `$…$` span.
var kindInlineMath = ast.NewNodeKind("ValeInlineMath")

// inlineMath is a `$…$` span. Its children are the raw source segments it
// covers, which may be more than one: Pandoc lets inline math wrap across
// lines within a paragraph.
//
// The segments include the `$` delimiters. The walker masks a skipped element
// with one character per character of its text, so a mask that dropped them
// would be two short of the source and every alert later in the paragraph
// would be placed two columns early.
type inlineMath struct {
	ast.BaseInline
}

func (*inlineMath) Kind() ast.NodeKind { return kindInlineMath }

func (*inlineMath) IsRaw() bool { return true }

func (n *inlineMath) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// mathInlineParser reads `$…$` inline math under Pandoc's delimiter rules: the
// opening `$` must be followed by a non-space character, and the closing `$`
// must be preceded by one and not followed by a digit.
//
// Those rules are what separate math from money. In `$5 and $10` the second
// `$` has a space to its left, so it can't close -- the sentence stays prose.
// A rule-free reader pairs the two and hides `5 and 10` from every check.
type mathInlineParser struct{}

func (mathInlineParser) Trigger() []byte { return []byte{'$'} }

// closesMath reports whether line[i] is a `$` that closes a span: one that
// isn't escaped, has a non-space character to its left, and isn't followed by
// a digit.
func closesMath(line []byte, i int) bool {
	switch {
	case line[i] != '$' || (i > 0 && line[i-1] == '\\'):
		return false
	case i == 0 || util.IsSpace(line[i-1]):
		return false
	case i+1 < len(line) && util.IsNumeric(line[i+1]):
		return false
	default:
		return true
	}
}

func (mathInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	head, opening := block.PeekLine()

	// `$$` opens display math, which the block parser owns.
	if len(head) < 2 || head[1] == '$' || util.IsSpace(head[1]) {
		return nil
	}

	startLine, startPos := block.Position()
	block.Advance(1)

	// Where the span begins in the source: the opening `$`, which the loop
	// below has already read past.
	start := opening.Start

	node := &inlineMath{}
	for {
		line, segment := block.PeekLine()
		if line == nil {
			// The paragraph ended with the span still open, so there was no
			// math here: leave the `$` to be read as text.
			block.SetPosition(startLine, startPos)
			return nil
		}
		for i := range line {
			if !closesMath(line, i) {
				continue
			}
			// Through the closing `$`.
			node.AppendChild(node, ast.NewRawTextSegment(
				text.NewSegment(start, segment.Start+i+1)))
			block.Advance(i + 1)
			return node
		}
		node.AppendChild(node, ast.NewRawTextSegment(
			text.NewSegment(start, segment.Stop)))
		start = segment.Stop
		block.AdvanceLine()
	}
}

// mathRenderer renders math nodes as `pre` and `code` rather than the
// extension's default `<span class="math">`, so vale's walker -- which skips
// both -- excludes them from linting.
type mathRenderer struct{}

func (mathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(mathjax.KindMathBlock, renderMathBlock)
	reg.Register(kindInlineMath, renderInlineMath)
}

func renderMathBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n, ok := node.(*mathjax.MathBlock)
	if !ok {
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = w.WriteString("<pre>")
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			_, _ = w.Write(line.Value(source))
		}
	} else {
		_, _ = w.WriteString("</pre>\n")
	}
	return ast.WalkContinue, nil
}

// renderInlineMath writes the span as `code`. The body is escaped so that a
// `<` in an equation can't open a tag: the walker unescapes it again, so what
// reaches vale is the source text at its source length.
func renderInlineMath(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</code>")
		return ast.WalkContinue, nil
	}

	_, _ = w.WriteString("<code>")
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		t, ok := c.(*ast.Text)
		if !ok {
			continue
		}
		_, _ = w.Write(util.EscapeHTML(t.Segment.Value(source)))
	}

	return ast.WalkSkipChildren, nil
}
