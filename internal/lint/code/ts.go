package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func TypeScript() *Language {
	return &Language{
		Delims:    regexp.MustCompile(`//|/\*|\*/`),
		Prefix:    cStylePrefix,
		Grammar:   "typescript",
		Queries:   []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding:   cStyle,
		Directive: jsDirective,
		Doc:       JSDoc,
	}
}
