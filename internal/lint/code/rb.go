package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Ruby() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`#|=begin|=end`),
		Grammar: "ruby",
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: func(s string) int {
			return computePadding(s, []string{"#", `=begin`, `=end`})
		},
		Directive: rubyDirective,
	}
}
