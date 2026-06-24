package lint

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	mathjax "github.com/litao91/goldmark-mathjax"
)

// mathExtension parses `$$…$$` display-math blocks and renders them as `<pre>`,
// which vale skips -- so equations aren't spell-checked as prose (#878).
//
// We deliberately enable *only* the block parser, not goldmark-mathjax's inline
// `$…$` parser: that one treats `$5 and $10` style currency as math, which
// would silently exclude real prose from linting.
type mathExtension struct{}

func (mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithBlockParsers(
		util.Prioritized(mathjax.NewMathJaxBlockParser(), 701),
	))
	// Priority must be lower than goldmark-mathjax's own renderers (501/502)
	// to win, since renderers are registered in reverse-priority order with
	// later registrations overwriting earlier ones.
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(mathRenderer{}, 1),
	))
}

// mathRenderer renders math nodes as `<pre>` rather than the extension's
// default `<span class="math">`, so vale's walker (which skips `pre`) excludes
// them from linting.
type mathRenderer struct{}

func (mathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(mathjax.KindMathBlock, renderMathBlock)
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
