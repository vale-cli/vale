package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

func Protobuf() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*|\*/`),
		Grammar: "proto",
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: cStyle,
	}
}
