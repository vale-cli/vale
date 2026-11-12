package lint

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

func updateQueries(f *core.File, cfg *core.Config) []core.Scope {
	var found []core.Scope

	for syntax, view := range cfg.Views {
		if view.Engine != "tree-sitter" {
			// A data View's selectors are not queries; it reads the file
			// itself, and may hand a cell of it here as code.
			continue
		}
		if cfg.SectionApplies(syntax, f.Held, f.Path) {
			found = view.Scopes
		}
	}

	return found
}

// skipsComment reports whether `IgnoredScopes` excludes a comment of this
// scope -- `text.comment.block` for a Python docstring, say.
//
// Both paths that read comments consult this. Which one runs depends on
// whether a markup format is mapped onto the file, and asking for Markdown in
// your comments must not also cost you the ability to exclude one. See #858.
func (l *Linter) skipsComment(scope string) bool {
	ignored := l.Manager.Config.IgnoredScopes
	return core.StringInSlice("comment", ignored) ||
		core.StringInSlice(scope, ignored)
}

// maskURLs blanks the URLs in a comment: a URL is never prose, and in
// markup the converter keeps one out of the text already.
// urlRE matches a URL in a comment, up to the punctuation that would
// close a sentence or a bracket around it.
var urlRE = regexp.MustCompile(`\b(?:https?|ftp)://[^\s<>"'` + "`" + `)\]]*[^\s<>"'` + "`" + `)\].,;:!?]`)

func maskURLs(s string) string {
	return urlRE.ReplaceAllStringFunc(s, nlp.BlankRunes)
}

// lintCommentBlock lints a comment as prose: the comment itself, plus its
// sentences and paragraphs, so a rule scoped to either reaches a comment as
// it does a text file.
func (l *Linter) lintCommentBlock(f *core.File, blk nlp.Block, lines, pad int) error {
	placed := blk.Offset >= 0

	blks, err := f.NLP.Compute(&blk, true)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	for _, b := range blks {
		if !placed {
			// A comment found by its delimiters is located by search, in
			// file lines; an offset would place its parts in the comment.
			b.Offset = -1
		}
		if err = l.lintBlock(f, b, lines, pad, true); err != nil {
			return err
		}
	}
	return nil
}

func (l *Linter) lintCodeOld(f *core.File) error {
	var line, match, txt string
	var lnLength, padding int
	var block bytes.Buffer

	lines := 0
	comments := core.CommentsByNormedExt[f.NormedExt]
	if len(comments) == 0 {
		return nil
	}

	scanner := bufio.NewScanner(strings.NewReader(f.Content))
	ignored := l.Manager.Config.IgnoredScopes

	skipAll := core.StringInSlice("comment", ignored)
	skipInline := core.StringInSlice("comment.line", ignored)
	skipBlock := core.StringInSlice("comment.block", ignored)

	scope := "%s" + f.RealExt
	inline := regexp.MustCompile(comments["inline"])
	blockStart := regexp.MustCompile(comments["blockStart"])
	blockEnd := regexp.MustCompile(comments["blockEnd"])
	ignore := false
	inBlock := false

	scanner.Split(core.SplitLines)
	for scanner.Scan() {
		line = core.Sanitize(scanner.Text() + "\n")
		lnLength = len(line)
		lines++
		if inBlock {
			// We're in a block comment.
			if match = blockEnd.FindString(line); len(match) > 0 {
				// We've found the end of the block.
				block.WriteString(line)
				txt = block.String()

				txt = maskURLs(txt)
				b := nlp.NewBlock(
					txt, txt, fmt.Sprintf(scope, "text.comment.block"))
				if !(skipAll || skipBlock) {
					if err := l.lintCommentBlock(f, b, lines+1, 0); err != nil {
						return err
					}
				}

				block.Reset()
				inBlock = false
			} else {
				block.WriteString(line)
			}
		} else if match = inline.FindString(line); len(match) > 0 {
			// We've found an inline comment. We need padding here in order to
			// calculate the column span because, for example, a line like
			// 'print("foo") # ...' will be condensed to '# ...'.
			padding = lnLength - len(match)

			match = maskURLs(match)
			b := nlp.NewBlock(
				match, match, fmt.Sprintf(scope, "text.comment.line"))
			if !(skipAll || skipInline) {
				if err := l.lintCommentBlock(f, b, lines, padding-1); err != nil {
					return err
				}
			}
		} else if match = blockStart.FindString(line); len(match) > 0 && !ignore {
			// We've found the start of a block comment.
			block.WriteString(line)
			inBlock = true
		} else if match = blockEnd.FindString(line); len(match) > 0 {
			ignore = !ignore
		}
	}
	return nil
}
