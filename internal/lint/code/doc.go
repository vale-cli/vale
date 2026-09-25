package code

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
)

// A documentation comment has a convention of its own on top of the
// language's comment syntax: a tag that names a parameter, a bracket that
// links to another symbol, a block that holds example code. None of it is
// prose. Each convention here returns the byte ranges of a comment's text
// that its convention marks as code, and an alert inside one is dropped.
//
// Masking rather than editing the text keeps the markup as written: a space
// put where a tag was reads as an indented code block in Markdown, and takes
// the description after it out of the document.

// Mask is a byte range of a comment's text that holds no prose.
type Mask [2]int

// ranges returns the byte range of each match of re in s.
func ranges(re *regexp.Regexp, s string) []Mask {
	var out []Mask
	for _, loc := range re.FindAllStringIndex(s, -1) {
		out = append(out, Mask{loc[0], loc[1]})
	}
	return out
}

// groupRanges returns the byte range of the first group of each match: the
// bracket itself, and not the character after it that settled whether the
// bracket was a link at all.
func groupRanges(re *regexp.Regexp, s string) []Mask {
	var out []Mask
	for _, loc := range re.FindAllStringSubmatchIndex(s, -1) {
		if loc[2] >= 0 {
			out = append(out, Mask{loc[2], loc[3]})
		}
	}
	return out
}

// A bracketed symbol is a link when nothing turns it into ordinary Markdown:
// a `(` or `[` after it makes a link with its own target, and a `:` a link
// definition.
const linkEnd = `([^(\[:]|$)`

var (
	goLink   = regexp.MustCompile(`(\[\*?[A-Za-z_][\w.]*\])` + linkEnd)
	rustLink = regexp.MustCompile("(\\[`[^`\\n]+`\\]|\\[[A-Za-z_][\\w:]*\\])" + linkEnd)
	kdocLink = regexp.MustCompile(`(\[[A-Za-z_][\w.]*\])` + linkEnd)
	kdocRef  = regexp.MustCompile(`\[[^\]\n]+\](\[[A-Za-z_][\w.]*\])`)

	kdocNamedTag = regexp.MustCompile(`(?m)^[ \t]*@(?:param|property|throws|exception|see|sample|suppress)[ \t]+\S+`)
	kdocBareTag  = regexp.MustCompile(`(?m)^[ \t]*@(?:return|constructor|receiver|author|since)\b`)

	javadocInline   = regexp.MustCompile(`\{@(?:code|link|linkplain|literal|value|docRoot|inheritDoc|index|summary|systemProperty|snippet|return)\b[^}]*\}`)
	javadocNamedTag = regexp.MustCompile(`(?m)^[ \t]*@(?:param|throws|exception|see|since|author|version|serial|serialField|serialData|provides|uses|hidden)[ \t]+\S+`)
	javadocBareTag  = regexp.MustCompile(`(?m)^[ \t]*@(?:return|deprecated|apiNote|implNote|implSpec)\b`)

	jsdocInline   = regexp.MustCompile(`\{@(?:link|linkcode|linkplain|tutorial)\b[^}]*\}`)
	jsdocNamedTag = regexp.MustCompile(`(?m)^[ \t]*@(?:param|arg|argument|prop|property|typedef|callback|event|member|var|name|alias|memberof|augments|extends|mixes|borrows|function|func|method|class|constructor|namespace|module|enum|external|host|kind|listens|fires|emits|this|requires|template|implements|satisfies|see)\b(?:[ \t]*\{[^}\n]*\})?(?:[ \t]+\[?[\w.$#~/-]+(?:=[^\]\n]*)?\]?)?`)
	jsdocTypedTag = regexp.MustCompile(`(?m)^[ \t]*@(?:returns?|throws|exception|type|yields?|default|defaultvalue)\b(?:[ \t]*\{[^}\n]*\})?`)
	jsdocBareTag  = regexp.MustCompile(`(?m)^[ \t]*@[A-Za-z]+\b`)
	jsdocTagLine  = regexp.MustCompile(`^[ \t]*@[A-Za-z]+\b`)
	jsdocExample  = regexp.MustCompile(`^[ \t]*@example\b`)

	pydocNamedField = regexp.MustCompile(`(?m)^[ \t]*:(?:param|parameter|arg|argument|key|keyword|type|raises|raise|except|exception|var|ivar|cvar|vartype|meta)[ \t]+[^:\n]+:`)
	pydocBareField  = regexp.MustCompile(`(?m)^[ \t]*:(?:returns?|rtype|yields?|ytype)[ \t]*:`)
)

// GoDoc masks a Go doc comment's links: `[Name]` and `[pkg.Name]`.
func GoDoc(s string) []Mask {
	return groupRanges(goLink, s)
}

// RustDoc masks a rustdoc comment's intra-doc links.
func RustDoc(s string) []Mask {
	return groupRanges(rustLink, s)
}

// KDoc masks a KDoc comment's links and the symbols its block tags name.
func KDoc(s string) []Mask {
	var out []Mask
	out = append(out, groupRanges(kdocRef, s)...)
	out = append(out, groupRanges(kdocLink, s)...)
	out = append(out, ranges(kdocNamedTag, s)...)
	return append(out, ranges(kdocBareTag, s)...)
}

// Javadoc masks a Javadoc comment's inline tags and the symbols its block
// tags name.
func Javadoc(s string) []Mask {
	var out []Mask
	out = append(out, ranges(javadocInline, s)...)
	out = append(out, ranges(javadocNamedTag, s)...)
	return append(out, ranges(javadocBareTag, s)...)
}

// JSDoc masks a JSDoc comment's inline tags, its block tags with their types
// and names, and the whole of an `@example` block.
func JSDoc(s string) []Mask {
	var out []Mask
	out = append(out, exampleRanges(s)...)
	out = append(out, ranges(jsdocInline, s)...)
	out = append(out, ranges(jsdocNamedTag, s)...)
	out = append(out, ranges(jsdocTypedTag, s)...)
	return append(out, ranges(jsdocBareTag, s)...)
}

// exampleRanges masks each `@example` block: the tag's line and every line
// after it up to the next tag.
func exampleRanges(s string) []Mask {
	var out []Mask

	at, inExample := 0, false
	for _, line := range strings.SplitAfter(s, "\n") {
		if jsdocExample.MatchString(line) {
			inExample = true
		} else if jsdocTagLine.MatchString(line) {
			inExample = false
		}
		if inExample {
			out = append(out, Mask{at, at + len(line)})
		}
		at += len(line)
	}

	return out
}

// PyDoc masks the field names of a docstring's reStructuredText field list:
// `:param name:` and `:raises Type:` up to the description.
func PyDoc(s string) []Mask {
	var out []Mask
	out = append(out, ranges(pydocNamedField, s)...)
	return append(out, ranges(pydocBareField, s)...)
}

// leadingMask masks name at the start of text, where a Go doc comment puts
// the name of what it documents, and reports whether it was there.
func leadingMask(text, name string) (Mask, bool) {
	if name == "" || !strings.HasPrefix(text, name) {
		return Mask{}, false
	}
	if rest := text[len(name):]; rest != "" {
		if r, _ := utf8.DecodeRuneInString(rest); unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return Mask{}, false
		}
	}
	return Mask{0, len(name)}, true
}

// goDocName is the name a Go doc comment is expected to open with: that of
// the declaration it precedes, or `Package name` for a package clause.
func goDocName(n *sitter.Node, src []byte) string {
	next := n.NextNamedSibling()
	if next == nil {
		return ""
	}

	switch next.Type() {
	case "package_clause":
		if id := next.NamedChild(0); id != nil {
			return "Package " + id.Content(src)
		}
	case "function_declaration", "method_declaration", "type_spec", "var_spec",
		"const_spec", "field_declaration", "method_spec", "method_elem":
		if name := next.ChildByFieldName("name"); name != nil {
			return name.Content(src)
		}
	case "type_declaration", "var_declaration", "const_declaration":
		for i := 0; i < int(next.NamedChildCount()); i++ {
			if name := next.NamedChild(i).ChildByFieldName("name"); name != nil {
				return name.Content(src)
			}
		}
	}

	return ""
}

// Masked reports whether the position -- a 1-based line of the comment's
// text and a 1-based column in runes, as an alert carries them -- falls in
// one of the comment's masks.
func (c Comment) Masked(line, col int) bool {
	if len(c.Masks) == 0 {
		return false
	}

	// The byte offset of the position.
	at, ln := 0, 1
	for ln < line {
		nl := strings.IndexByte(c.Text[at:], '\n')
		if nl < 0 {
			return false
		}
		at += nl + 1
		ln++
	}
	for i := 1; i < col && at < len(c.Text); i++ {
		_, n := utf8.DecodeRuneInString(c.Text[at:])
		at += n
	}

	for _, m := range c.Masks {
		if at >= m[0] && at < m[1] {
			return true
		}
	}
	return false
}

// Directives are comments addressed to a tool rather than a reader: a build
// constraint, a linter suppression, a formatter switch. They hold no prose
// and are dropped before anything is read.
var (
	goDirective     = regexp.MustCompile(`^//(?:go:|line |export |extern |nolint\b|lint:ignore|[a-z0-9]+:[a-z0-9])`)
	pyDirective     = regexp.MustCompile(`^#[ \t]*(?:noqa\b|type:|pylint:|pragma\b|fmt:[ \t]*(?:on|off|skip)|isort:|mypy:|pyright:|ruff:|flake8:|nosec\b|pyre-|coverage:|-\*-)`)
	jsDirective     = regexp.MustCompile(`^//[ \t]*(?:eslint|@ts-|prettier-ignore|biome-ignore|istanbul\b|c8\b|v8\b|@flow\b|@jsx|jshint|jslint)|^/\*[ \t]*(?:eslint|globals?\b|jshint|jslint|istanbul\b|c8\b|@flow\b|@jsx|webpackChunkName|[@#]__PURE__)`)
	javaDirective   = regexp.MustCompile(`^//[ \t]*(?:NOPMD|NOSONAR|CHECKSTYLE|noinspection|@formatter:|\$NON-NLS)`)
	kotlinDirective = regexp.MustCompile(`^//[ \t]*(?:ktlint|NOSONAR|noinspection|@formatter:)`)
	cDirective      = regexp.MustCompile(`^//[ \t]*(?:NOLINT|clang-format|cppcheck-suppress)|^/\*[ \t]*(?:NOLINT|clang-format)`)
	csharpDirective = regexp.MustCompile(`^//[ \t]*(?:ReSharper (?:disable|restore)\b|NOSONAR)`)
	rubyDirective   = regexp.MustCompile(`^#[ \t]*(?:rubocop:|frozen_string_literal:|encoding:|typed:|sorbet:|-\*-)`)
)
