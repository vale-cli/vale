package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Go() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*|\*/`),
		Grammar: "go",
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: cStyle,
		// A doc comment opens with the name it documents and links to
		// others in brackets; a `//go:` line is a directive.
		Directive: goDirective,
		Doc:       GoDoc,
		DocName:   goDocName,
	}
}
