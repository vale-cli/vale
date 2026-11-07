package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Python() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`#|"""|'''`),
		Grammar: "python",
		Queries: []core.Scope{
			{Name: "", Expr: `(comment)+ @comment`, Type: ""},
			// Function docstrings
			{Name: "", Expr: `((function_definition
  body: (block . (expression_statement (string) @docstring)))
 (#offset! @docstring 0 3 0 -3))`, Type: ""},
			// Class docstrings
			{Name: "", Expr: `((class_definition
  body: (block . (expression_statement (string) @docstring)))
 (#offset! @docstring 0 3 0 -3))`, Type: ""},
			// Module docstrings
			{Name: "", Expr: `((module . (expression_statement (string) @docstring))
 (#offset! @docstring 0 3 0 -3))`, Type: ""},
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"#", `"""`, "'''"})
		},
		Directive: pyDirective,
		Doc:       PyDoc,
	}
}
