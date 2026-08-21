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
	Delims *regexp.Regexp
	Parser *sitter.Language
	// Prefix matches a block comment's per-line decoration -- the ` *` that
	// starts each line of a JSDoc or Javadoc block.
	//
	// This is not the same thing as Cutset. Decoration is noise of a known
	// width that has to come off for the body to be valid markup; indentation
	// is meaningful and only its common part comes off. Conflating them is why
	// a cutset of " *" cannot work: `*` is both the decoration and Markdown's
	// list and emphasis marker, so a cutset wide enough to remove the noise
	// also eats a list.
	//
	// The match is blanked rather than deleted, which keeps every column where
	// it was and leaves the dedent below to remove the whitespace it becomes.
	Prefix  *regexp.Regexp
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
	case ".ex":
		return Elixir(), nil
	case ".cpp":
		return Cpp(), nil
	case ".c":
		return C(), nil
	case ".js", ".jsx":
		return JavaScript(), nil
	case ".hs":
		return Haskell(), nil
	case ".jl":
		return Julia(), nil
	case ".java":
		return Java(), nil
	case ".lua":
		return Lua(), nil
	case ".php":
		return PHP(), nil
	case ".ts":
		return TypeScript(), nil
	case ".tsx":
		return Tsx(), nil
	case ".proto":
		return Protobuf(), nil
	case ".qml":
		return QML(), nil
	case ".r":
		return R(), nil
	case ".yml":
		return YAML(), nil
	case ".css":
		return CSS(), nil
	default:
		return nil, fmt.Errorf("unsupported extension: '%s'", ext)
	}
}
