package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Rust() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`/{2,3}!?|/\*[*!]?|\*/`),
		Prefix:  cStylePrefix,
		Grammar: "rust",
		Queries: []core.Scope{
			{Name: "", Expr: `(line_comment)+ @comment`, Type: ""},
			// A block comment nests in Rust; the node is the outermost one.
			{Name: "", Expr: `(block_comment) @comment`, Type: ""},
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"//", "//!", "///", "/*", "/**", "/*!"})
		},
		Doc: RustDoc,
	}
}
