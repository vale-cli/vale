package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

// Haskell is parsed with the Elm grammar: the two languages share their
// comment syntax (`--`, `{- -}`), and comments are extras that survive error
// recovery, which is all comment extraction needs.
func Haskell() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`\{-\|?|-\}|--`),
		Grammar: "elm",
		Queries: []core.Scope{
			{Name: "", Expr: "(line_comment) @comment", Type: ""},
			{Name: "", Expr: "(block_comment) @comment", Type: ""},
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"--", "{-"})
		},
	}
}
