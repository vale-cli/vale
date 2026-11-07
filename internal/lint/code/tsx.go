package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Tsx() *Language {
	return &Language{
		Delims:    regexp.MustCompile(`//|/\*|\*/`),
		Prefix:    cStylePrefix,
		Grammar:   "tsx",
		Queries:   []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding:   cStyle,
		Directive: jsDirective,
		Doc:       JSDoc,
	}
}
