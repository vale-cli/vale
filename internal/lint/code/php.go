package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func PHP() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*\*?|\*/|#`),
		Prefix:  cStylePrefix,
		Grammar: "php",
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: func(s string) int {
			return computePadding(s, []string{"//", "/*", "#"})
		},
	}
}
