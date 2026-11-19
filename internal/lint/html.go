package lint

import (
	"fmt"
	"regexp"
	"strings"

	rx "github.com/vale-cli/vale/v3/internal/regex"

	"github.com/vale-cli/vale/v3/internal/core"
)

var reFrontMatter = regexp.MustCompile(
	`^(?s)(?:---|\+\+\+)\n(.+?)\n(?:---|\+\+\+)`)

var heading = regexp.MustCompile(`^h\d$`)

func (l *Linter) lintHTML(f *core.File) error {
	if l.Manager.Config.Flags.Built != "" {
		return l.lintTxtToHTML(f)
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	return l.lintHTMLTokens(f, []byte(s), 0)
}

type extensionConfig struct {
	// Normed is the format the file is read as, which selects the parser and
	// the delimiters below. Real is its extension on disk, and RealPath its
	// path.
	Normed, Real, RealPath string
	// Held is the conditional sections whose `if` held for the file.
	Held map[string]bool
}

// match reports whether a config section applies to this file.
//
// Sections are keyed on the file as it is on disk -- `[*.qmd]`, never `[*.md]`
// by way of `[formats] qmd = md` -- which is how `BasedOnStyles` matches, and
// the two have to agree: patterns that ignored text no rule was going to reach
// would be pointless, and the reverse silently lints what the section says to
// skip.
//
// The path is matched as well as the extension. Without it a section keyed on
// one -- `[docs/*.md]` -- could never match, and its patterns were read,
// compiled and then silently never applied. See #839.
func (e extensionConfig) match(c *core.Config, sec string) bool {
	return c.SectionApplies(sec, e.Held, e.Real, e.RealPath)
}

// blockDelimiters wrap a BlockIgnores match in the format's block code
// delimiter. HTML has no source-level one, so a match is wrapped in the
// element the others are converted to.
var blockDelimiters = map[string]string{
	".adoc": "\n----\n$1\n----\n",
	".dita": "<codeblock>$1</codeblock>",
	".html": "<pre>$1</pre>",
	".md":   "\n```\n$1\n```\n",
	".mdx":  "\n```\n$1\n```\n",
	".myst": "\n```\n$1\n```\n",
	".mdoc": "\n```\n$1\n```\n",
	".qdoc": "\n\\code\n$1\n\\endcode\n",
	".qmd":  "\n```\n$1\n```\n",
	".rst":  "\n::\n\n%s\n",
	".typ":  "\n```\n$1\n```\n",
	".org":  orgExample,
}

func applyBlockPatterns(c *core.Config, exts extensionConfig, content string) (string, error) {
	block, ok := blockDelimiters[exts.Normed]
	if !ok {
		return content, fmt.Errorf("ignore patterns are not supported in '%s' files", exts.Normed)
	}

	// TODO: Should we assume this?
	s := reFrontMatter.ReplaceAllString(content, block)

	for syntax, regexes := range c.BlockIgnores {
		if exts.match(c, syntax) {
			for _, r := range regexes {
				pat, errc := rx.Compile(r)
				if errc != nil { //nolint:gocritic
					return s, core.NewE201FromTarget(
						errc.Error(),
						r,
						c.Flags.Path,
					)
				} else if strings.HasSuffix(exts.Normed, ".rst") {
					// HACK: We need to add padding for the literal block.
					for _, c := range pat.FindAllStringSubmatch(s, -1) {
						sec := fmt.Sprintf(block, core.Indent(c[0], "    "))
						s = strings.Replace(s, c[0], sec, 1)
					}
				} else {
					var err error
					s, err = pat.Replace(s, block, 0, -1)
					if err != nil {
						return s, core.NewE201FromTarget(
							err.Error(),
							r,
							c.Flags.Path,
						)
					}
				}
			}
		}
	}
	return s, nil
}

var inlineDelimiters = map[string]string{
	".adoc": "`$1`",
	".dita": "<codeph>$1</codeph>",
	".html": "<code>$1</code>",
	".md":   "`$1`",
	".mdx":  "`$1`",
	".myst": "`$1`",
	".mdoc": "`$1`",
	".qdoc": `\c {$1}`,
	".qmd":  "`$1`",
	".rst":  "``$1``",
	".typ":  "`$1`",
	".org":  "=$1=",
}

func applyInlinePatterns(c *core.Config, exts extensionConfig, content string) (string, error) {
	inline, ok := inlineDelimiters[exts.Normed]
	if !ok {
		return content, fmt.Errorf("ignore patterns are not supported in '%s' files", exts.Normed)
	}

	for syntax, regexes := range c.TokenIgnores {
		if exts.match(c, syntax) {
			for _, r := range regexes {
				pat, errc := rx.Compile(r)
				if errc != nil {
					return content, core.NewE201FromTarget(
						errc.Error(),
						r,
						c.Flags.Path,
					)
				}
				var err error
				content, err = pat.Replace(content, inline, 0, -1)
				if err != nil {
					return content, core.NewE201FromTarget(
						err.Error(),
						r,
						c.Flags.Path,
					)
				}
			}
		}
	}
	return content, nil
}

// applyCommentPatterns replaces any custom comment delimiters with HTML comment
// tags based on the user configuration. This makes it possible to apply
// comment-based controls using custom comment delimiters.
func applyCommentPatterns(c *core.Config, exts extensionConfig, content string) (string, error) {
	for syntax, delims := range c.CommentDelimiters {
		if exts.match(c, syntax) {
			// This field was not assigned, so do nothing.
			if delims[0] == "" && delims[1] == "" {
				return content, nil
			}
			// Return an error if only one delimiter is configured
			if (delims[0] == "" && delims[1] != "") || (delims[0] != "" && delims[1] == "") {
				return content, fmt.Errorf("CommentDelimiters must be empty or have two values")
			}

			content = strings.ReplaceAll(content, delims[0], "<!--")
			content = strings.ReplaceAll(content, delims[1], "-->")
		}
	}
	return content, nil
}

func applyPatterns(c *core.Config, exts extensionConfig, content string) (string, error) {
	s, err := applyBlockPatterns(c, exts, content)
	if err != nil {
		return s, err
	}

	s, err = applyInlinePatterns(c, exts, s)
	if err != nil {
		return s, err
	}

	s, err = applyCommentPatterns(c, exts, s)
	if err != nil {
		return s, err
	}

	return s, nil
}

func (l *Linter) lintTxtToHTML(f *core.File) error {
	html, err := l.Manager.Config.FS.ReadFile(l.Manager.Config.Flags.Built)
	if err != nil {
		return core.NewE100(f.Path, err)
	}
	return l.lintHTMLTokens(f, html, 0)
}
