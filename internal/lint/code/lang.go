package code

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code/tsw"
)

type padding func(string) int

// Language represents a supported programming language.
//
// NOTE: What about haskell, less, perl, php, powershell, r, sass, swift?
type Language struct {
	Delims *regexp.Regexp
	// Grammar names the tree-sitter grammar the language is parsed with; see
	// Grammar.
	Grammar string
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
	DocName func(*tsw.Node, []byte) string
}

// GetLanguageFromExt returns a Language based on the given file extension.
func GetLanguageFromExt(ext string) (*Language, error) {
	lang, _, err := newLanguage(ext)
	return lang, err
}

// Names lists every grammar Vale reads, as grammars/build.sh names them.
func Names() []string {
	return []string{
		"bash", "c", "cpp", "csharp", "css", "elixir", "elm", "go", "java",
		"javascript", "julia", "kotlin", "lua", "php", "proto", "python",
		"ruby", "rust", "toml", "tsx", "typescript", "yaml",
	}
}

// Extensions lists every file extension a grammar reads, as languageFor
// knows them.
func Extensions() []string {
	return []string{
		".go", ".rs", ".py", ".rb", ".ex", ".cpp", ".c", ".cs", ".js", ".jsx",
		".hs", ".jl", ".java", ".kt", ".lua", ".php", ".ts", ".tsx", ".proto",
		".qml", ".r", ".yml", ".yaml", ".toml", ".css",
	}
}

// A NotInstalledError is a grammar the configuration has no module for.
type NotInstalledError struct {
	Name string
}

func (e *NotInstalledError) Error() string {
	return fmt.Sprintf("grammar %q is not installed; add 'Grammars = %s' to .vale.ini and run 'vale sync'", e.Name, e.Name)
}

// Loader finds the module for a grammar by name: the bytes of
// `<name>.wasm`, as grammars/build.sh writes it. The linter sets it from the
// configuration, which knows where grammars are installed.
var Loader = func(name string) ([]byte, error) {
	return nil, &NotInstalledError{Name: name}
}

// grammars holds each grammar's load: compiling a module is the costly
// step, and every file of a language shares the result.
var grammars sync.Map // name -> *grammarLoad

// A grammarLoad compiles a grammar once, however many files ask for it at
// the same time. A second compile of the same module in the shared runtime
// can hand a caller a module whose entry points aren't ready yet, which
// panics in the runtime.
type grammarLoad struct {
	once sync.Once
	lang *tsw.Language
	err  error
}

// Grammar returns the compiled grammar of the given name, loading it on
// first use.
func Grammar(name string) (*tsw.Language, error) {
	if !tsw.Built {
		return nil, tsw.ErrNotBuilt
	}
	v, _ := grammars.LoadOrStore(name, &grammarLoad{})
	l := v.(*grammarLoad) //nolint:errcheck // only *grammarLoad is stored
	l.once.Do(func() {
		wasm, err := Loader(name)
		if err == nil {
			l.lang, err = tsw.Load(name, wasm)
		}
		l.err = err
	})
	if l.err != nil {
		// Try again next time: a long run may see the grammar installed.
		grammars.CompareAndDelete(name, l)
	}
	return l.lang, l.err
}

// newLanguage returns the language for ext and the extension it was found
// under: ext itself when a grammar is specific to it, as the TSX grammar is
// to `.tsx`, else the normed extension it shares with others.
func newLanguage(ext string) (*Language, string, error) {
	ext = strings.ToLower(ext)
	if lang := languageFor(ext); lang != nil {
		return lang, ext, nil
	}
	if normed := core.GetNormedExt(ext); normed != "" {
		if lang := languageFor(normed); lang != nil {
			return lang, normed, nil
		}
	}
	return nil, "", fmt.Errorf("unsupported extension: '%s'", ext)
}

// languageFor returns the language whose grammar reads ext, or nil.
func languageFor(ext string) *Language {
	switch ext {
	case ".go":
		return Go()
	case ".rs":
		return Rust()
	case ".py":
		return Python()
	case ".rb":
		return Ruby()
	case ".ex":
		return Elixir()
	case ".cpp":
		return Cpp()
	case ".c":
		return C()
	case ".cs":
		return CSharp()
	case ".js", ".jsx":
		return JavaScript()
	case ".hs":
		return Haskell()
	case ".jl":
		return Julia()
	case ".java":
		return Java()
	case ".kt":
		return Kotlin()
	case ".lua":
		return Lua()
	case ".php":
		return PHP()
	case ".ts":
		return TypeScript()
	case ".tsx":
		return Tsx()
	case ".proto":
		return Protobuf()
	case ".qml":
		return QML()
	case ".r":
		return R()
	case ".yml", ".yaml":
		return YAML()
	case ".toml":
		return TOML()
	case ".css":
		return CSS()
	default:
		return nil
	}
}
