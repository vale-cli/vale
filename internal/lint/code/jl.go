package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Julia() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`#|#=|=#`),
		Grammar: "julia",
		Queries: []core.Scope{
			{Name: "", Expr: "(line_comment)+ @comment", Type: ""},
			{Name: "", Expr: "(block_comment)+ @comment", Type: ""},
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"#", `#=`, `=#`})
		},
	}
}
