package lint

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/glob"
	"github.com/errata-ai/vale/v3/internal/lint/code"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

func updateQueries(f *core.File, views map[string]*core.View) ([]core.Scope, error) {
	var found []core.Scope

	for syntax, view := range views {
		sec, err := glob.Compile(syntax)
		if err != nil {
			return nil, err
		} else if sec.Match(f.Path) {
			found = view.Scopes
		}
	}

	return found, nil
}

func (l *Linter) lintCode(f *core.File) error {
	lang, err := code.GetLanguageFromExt(f.RealExt)
	if err != nil {
		// No tree-sitter grammar available for this file type.
		return l.lintCodeOld(f)
	}
	ignored := l.Manager.Config.IgnoredScopes

	found, err := updateQueries(f, l.Manager.Config.Views)
	if err != nil {
		return err
	} else if len(found) > 0 {
		lang.Queries = found
	}

	comments, err := code.GetComments([]byte(f.Content), lang)
	if err != nil {
		return err
	}
	wholeFile := f.Content

	last := 0
	for _, comment := range comments {
		l.SetMetaScope(comment.Scope)
		if core.StringInSlice("comment", ignored) {
			continue
		} else if core.StringInSlice(comment.Scope, ignored) {
			continue
		}
		f.SetText(comment.Text)

		err = l.lintLines(f)
		if err != nil {
			return err
		}

		size := len(f.Alerts)
		if size != last {
			f.Alerts = adjustAlerts(f.Alerts, last, comment, lang)
		}
		last = size
	}

	f.SetText(wholeFile)
	return nil
}

// lintCodeOld lints source code by analyzing its comments.
//
// Deprecated: we now use tree-sitter to parse code and collect comments.
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

				b := nlp.NewBlock(
					txt, txt, fmt.Sprintf(scope, "text.comment.block"))
				if !(skipAll || skipBlock) {
					if err := l.lintBlock(f, b, lines+1, 0, true); err != nil {
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

			b := nlp.NewBlock(
				match, match, fmt.Sprintf(scope, "text.comment.line"))
			if !(skipAll || skipInline) {
				if err := l.lintBlock(f, b, lines, padding-1, true); err != nil {
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
