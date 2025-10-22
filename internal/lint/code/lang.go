package code

import (
	"fmt"
	"regexp"

	"github.com/errata-ai/vale/v3/internal/core"
	sitter "github.com/smacker/go-tree-sitter"
)

type padding func(string) int

// Language represents a supported programming language.
//
// NOTE: What about haskell, less, perl, php, powershell, r, sass, swift?
type Language struct {
	Delims  *regexp.Regexp
	Parser  *sitter.Language
	Queries []core.Scope
	Cutset  string
	Padding padding
}

// GetLanguageFromExt returns a Language based on the given file extension.
func GetLanguageFromExt(ext string) (*Language, error) {
	switch core.GetNormedExt(ext) {
	case ".go":
		return Go(), nil
	case ".rs":
		return Rust(), nil
	case ".py":
		return Python(), nil
	case ".rb":
		return Ruby(), nil
	case ".cpp":
		return Cpp(), nil
	case ".c":
		return C(), nil
	case ".js", ".jsx":
		return JavaScript(), nil
	case ".jl":
		return Julia(), nil
	case ".java":
		return Java(), nil
	case ".ts":
		return TypeScript(), nil
	case ".tsx":
		return Tsx(), nil
	case ".proto":
		return Protobuf(), nil
	case ".yml":
		return YAML(), nil
	case ".css":
		return CSS(), nil
	default:
		return nil, fmt.Errorf("unsupported extension: '%s'", ext)
	}
}
