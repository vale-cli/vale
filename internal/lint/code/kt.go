package code

import (
	"regexp"

	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/vale-cli/vale/v3/internal/core"
)

// Kotlin extracts `//` line comments and `/* */` block comments, including
// KDoc (`/** */`), which the grammar parses as an ordinary block comment.
func Kotlin() *Language {
	return &Language{
		Delims: regexp.MustCompile(`//|/\*\*?|\*/`),
		Prefix: cStylePrefix,
		Parser: kotlin.GetLanguage(),
		Queries: []core.Scope{
			{Name: "", Expr: "(line_comment)+ @comment", Type: ""},
			{Name: "", Expr: "(multiline_comment) @comment", Type: ""},
		},
		// `/**` is listed so that a one-line KDoc comment's padding matches
		// what Delims takes off; cStyle alone stops at `/*`.
		Padding: func(s string) int {
			return computePadding(s, []string{"//", "/*", "/**"})
		},
	}
}
