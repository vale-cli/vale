package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Lua() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`--\[=*\[|\]=*\]|--`),
		Grammar: "lua",
		Queries: []core.Scope{
			{Name: "", Expr: "(comment) @comment", Type: ""},
			// `---` doc comments (including the `---[[` toggle idiom, which
			// is a line comment, not a block opener).
			{Name: "", Expr: "(emmy_documentation) @comment", Type: ""},
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"--", "--[["})
		},
	}
}
