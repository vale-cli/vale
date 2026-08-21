package code

import (
	"regexp"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/smacker/go-tree-sitter/elixir"
)

// Elixir extracts `#` comments and the prose held by the `@moduledoc`,
// `@doc`, `@typedoc` and `@shortdoc` attributes.
//
// The attributes are the point. Elixir has no documentation comment syntax:
// its API documentation lives in module attributes holding a string or a
// heredoc, and that is what `mix docs` publishes and what a reader of the
// module reads first. A comment-only pass sees the asides and none of the
// documentation.
//
// They are given a `doc` meta scope -- `text.comment.doc.line` and
// `text.comment.doc.block` -- so that published documentation can be held to
// a different standard than an implementation note, or excluded on its own.
//
// The queries capture `quoted_content`, the body of the string, rather than
// the string itself. That leaves the delimiters out of the extracted text
// without a `Delims` pattern having to take them off, which matters for the
// single-quoted form: stripping its `"` with a regex would also strip any
// quote written inside the prose. `@doc false` and `@doc since: "1.0"` carry
// no prose, and neither matches a query that requires a string or sigil.
func Elixir() *Language {
	return &Language{
		Delims: regexp.MustCompile(`#`),
		Parser: elixir.GetLanguage(),
		Queries: []core.Scope{
			{Name: "", Expr: `(comment) @comment`, Type: ""},
			// `@` applied to a call whose target names a documentation
			// attribute and whose argument is a string or sigil.
			//
			// The attribute name is tested through `@_attr`, a
			// predicate-only capture, so that the prose can be captured on
			// its own.
			{Name: "doc", Expr: `((unary_operator
  operand: (call
    target: (identifier) @_attr
    (arguments [
      (string (quoted_content) @comment)
      (sigil (quoted_content) @comment)
    ])))
 (#match? @_attr "^(module|type|short)?doc$"))`, Type: ""},
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"#"})
		},
	}
}
