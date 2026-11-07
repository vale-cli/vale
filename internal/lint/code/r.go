package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

// R (and Perl, which shares the normed extension) is parsed with the Bash
// grammar: all three languages use `#` line comments, and comments are extras
// that survive error recovery, which is all comment extraction needs.
func R() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`#'|#`),
		Grammar: "bash",
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: func(s string) int {
			return computePadding(s, []string{"#", "#'"})
		},
	}
}
