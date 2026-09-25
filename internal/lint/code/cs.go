package code

import (
	"regexp"

	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/vale-cli/vale/v3/internal/core"
)

// CSharp extracts `//` line comments, `///` XML documentation comments and
// `/* */` block comments, all of which the grammar parses as `comment`.
func CSharp() *Language {
	return &Language{
		Delims: regexp.MustCompile(`///?|/\*|\*/`),
		Prefix: cStylePrefix,
		Parser: csharp.GetLanguage(),
		Queries: []core.Scope{
			{Name: "", Expr: "(comment) @comment", Type: ""},
		},
		// `///` is listed so that a documentation comment's padding matches
		// what Delims takes off; cStyle alone stops at `//`.
		Padding: func(s string) int {
			return computePadding(s, []string{"//", "///", "/*"})
		},
		Directive: csharpDirective,
	}
}
