package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func C() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*|\*/`),
		Prefix:  cStylePrefix,
		Grammar: "c",
		Queries: []core.Scope{
			{Name: "", Expr: "(comment) @comment", Type: ""},
		},
		Padding:   cStyle,
		Directive: cDirective,
	}
}
