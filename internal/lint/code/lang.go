package code

import (
	"fmt"
	"regexp"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/vale-cli/vale/v3/internal/core"
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
	// Directive matches a comment addressed to a tool -- a build constraint,
	// a linter suppression -- which is dropped rather than read. It is
	// matched against the comment as written, delimiter included.
	Directive *regexp.Regexp
	// Doc masks what the language's documentation convention marks as code:
	// the tags of Javadoc, the links of rustdoc. See doc.go.
	Doc func(string) []Mask
	// DocName is the name a documentation comment is expected to open with,
	// read from the node it precedes; nil for a language with no such
	// convention.
	DocName func(*sitter.Node, []byte) string
}

// GetLanguageFromExt returns a Language based on the given file extension.
//
// The grammar is shared across calls: each call builds a fresh Language,
// whose queries a View may replace, but the grammar underneath is the same
// one, and a compiled query is cached against it (see compiledQuery). A
// fresh grammar handle per call made that cache miss every time.
func GetLanguageFromExt(ext string) (*Language, error) {
	lang, err := newLanguage(ext)
	if err != nil {
		return nil, err
	}
	lang.Parser = sharedGrammar(core.GetNormedExt(ext), lang.Parser)
	return lang, nil
}

// grammars holds the first grammar handle seen for each extension.
var grammars sync.Map

func sharedGrammar(ext string, fresh *sitter.Language) *sitter.Language {
	if g, ok := grammars.Load(ext); ok {
		return g.(*sitter.Language) //nolint:errcheck // only *sitter.Language is stored
	}
	g, _ := grammars.LoadOrStore(ext, fresh)
	return g.(*sitter.Language) //nolint:errcheck // only *sitter.Language is stored
}

func newLanguage(ext string) (*Language, error) {
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
	case ".cs":
		return CSharp(), nil
	case ".js", ".jsx":
		return JavaScript(), nil
	case ".hs":
		return Haskell(), nil
	case ".jl":
		return Julia(), nil
	case ".java":
		return Java(), nil
	case ".kt":
		return Kotlin(), nil
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
	case ".yml", ".yaml":
		return YAML(), nil
	case ".toml":
		return TOML(), nil
	case ".css":
		return CSS(), nil
	default:
		return nil, fmt.Errorf("unsupported extension: '%s'", ext)
	}
}
