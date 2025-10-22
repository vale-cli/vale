package code

import (
	"regexp"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/smacker/go-tree-sitter/java"
)

func Java() *Language {
	return &Language{
		Delims: regexp.MustCompile(`//|/\*|\*/`),
		Parser: java.GetLanguage(),
		Queries: []core.Scope{
			{Name: "", Expr: "(line_comment)+ @comment", Type: ""},
			{Name: "", Expr: "(block_comment)+ @comment", Type: ""},
		},
		Padding: cStyle,
	}
}
