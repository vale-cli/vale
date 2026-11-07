package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Java() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*|\*/`),
		Prefix:  cStylePrefix,
		Grammar: "java",
		Queries: []core.Scope{
			{Name: "", Expr: "(line_comment)+ @comment", Type: ""},
			{Name: "", Expr: "(block_comment)+ @comment", Type: ""},
		},
		Padding:   cStyle,
		Directive: javaDirective,
		Doc:       Javadoc,
	}
}
