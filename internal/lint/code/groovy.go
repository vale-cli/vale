package code

import (
	"regexp"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/smacker/go-tree-sitter/groovy"
)

func Groovy() *Language {
	return &Language{
		Delims: regexp.MustCompile(`//|/\*\*?|\*/`),
		Prefix: cStylePrefix,
		Parser: groovy.GetLanguage(),
		// The grammar has one `comment` node for both `//` and `/* */`
		// comments; Groovydoc (`/** */`) is its own node. A shebang is a
		// `shebang` node, so it never arrives here as a comment.
		Queries: []core.Scope{
			{Name: "", Expr: "(comment) @comment", Type: ""},
			{Name: "", Expr: "(groovy_doc) @comment", Type: ""},
		},
		Padding: cStyle,
	}
}
