package lint

import (
	"strings"
	"unicode/utf8"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code"
)

func findLine(s string, line int) string {
	lines := strings.Split(s, "\n")
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}

// adjustAlerts moves alerts reported against a comment's Text back to their
// place in the file. The comment recorded what came off the front of each of
// its lines, so a column in Text is that plus a column in Source.
//
// A Source line is the file's line from the comment's own delimiter -- the
// first line, and every line of a run of line comments -- or, inside a block,
// the whole file line. The first kind sits comment.Offset in from the margin;
// the second already includes it.
func adjustAlerts(alerts []core.Alert, last int, comment code.Comment) []core.Alert {
	block := strings.HasSuffix(comment.Scope, ".block")
	for i := range alerts {
		if i < last {
			continue
		}
		line := alerts[i].Line
		source := findLine(comment.Source, line)

		padding := 0
		if n, ok := comment.StripAt(line); ok {
			padding = utf8.RuneCountInString(source[:min(n, len(source))])
		}
		if line == 1 || !block {
			padding += comment.Offset
		}

		alerts[i].Line += comment.Line - 1
		alerts[i].Span = []int{alerts[i].Span[0] + padding, alerts[i].Span[1] + padding}
	}
	return alerts
}

func (l *Linter) lintFragments(f *core.File) error {
	lang, err := l.languageFor(f)
	if err != nil {
		return err
	}

	if found := updateQueries(f, l.Manager.Config); len(found) > 0 {
		lang.Queries = found
	}

	comments, err := code.GetComments([]byte(f.Content), lang)
	if err != nil {
		if grammarMissing(err) {
			return nil
		}
		return err
	}

	wholeFile := f.Content
	converted := l.rstBatch(f, comments, strings.TrimPrefix(f.NormedExt, "."))

	last := 0
	for i, comment := range comments {
		// QDoc reads `/*! ... */` only; a `//` line comment or a plain
		// `/* ... */` block is code, not documentation.
		if f.NormedExt == ".qdoc" && !strings.HasPrefix(comment.Source, "/*!") {
			continue
		}

		f.SetMetaScope(comment.Scope)
		if l.skipsComment(comment.Scope) {
			continue
		}

		// The file's own mapping, unless the query named a markup of its own.
		format := comment.Format
		if format == "" {
			format = strings.TrimPrefix(f.NormedExt, ".")
		}
		err = l.lintComment(f, comment, format, converted[i])
		if err != nil {
			return err
		}
		f.Alerts = dropMasked(f.Alerts, last, comment)

		size := len(f.Alerts)
		if size != last {
			f.Alerts = adjustAlerts(f.Alerts, last, comment)
		}
		last = size
	}

	// Each comment was linted in the file's place; put the file back, so the
	// `raw` scope that runs next reads the source and not the last comment.
	f.RestoreText(wholeFile)
	if err != nil {
		return err
	}
	return l.lintSummary(f)
}
