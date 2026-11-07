package code

import (
	"regexp"

	"github.com/vale-cli/vale/v3/internal/core"
)

// QML is parsed with the JavaScript grammar: QML's object syntax isn't valid
// JavaScript, but comments are extras and survive error recovery, which is all
// comment extraction needs.
func QML() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*[!*]?|\*/`),
		Prefix:  cStylePrefix,
		Grammar: "javascript",
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: cStyle,
	}
}
