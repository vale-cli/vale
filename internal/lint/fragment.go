package lint

import (
	"fmt"
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/lint/code"
)

func findLine(s string, line int) string {
	lines := strings.Split(s, "\n")
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}

func leadingSpaces(line string, offset int) int {
	spaces := 0
	for _, r := range line {
		if r == ' ' {
			spaces++
		} else {
			break
		}
	}
	return spaces - offset
}

func adjustAlerts(alerts []core.Alert, last int, comment code.Comment, lang *code.Language) []core.Alert {
	for i := range alerts {
		if i >= last {
			line := findLine(comment.Source, alerts[i].Line)

			padding := lang.Padding(line)
			if strings.HasPrefix(line, " ") {
				padding += leadingSpaces(line, comment.Offset)
			}

			alerts[i].Line += comment.Line - 1
			alerts[i].Span = []int{
				alerts[i].Span[0] + comment.Offset + padding,
				alerts[i].Span[1] + comment.Offset + padding,
			}
		}
	}
	return alerts
}

func (l *Linter) lintFragments(f *core.File) error {
	// We want to set up our processing servers as if we were dealing with
	// a directory since we likely have many fragments to convert.
	l.HasDir = true

	lang, err := code.GetLanguageFromExt(f.RealExt)
	if err != nil {
		return err
	}

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

	last := 0
	for _, comment := range comments {
		l.SetMetaScope(comment.Scope)
		f.SetText(comment.Text)

		switch f.NormedExt {
		case ".md":
			err = l.lintMarkdown(f)
		case ".rst":
			err = l.lintRST(f)
		case ".adoc":
			err = l.lintADoc(f)
		case ".org":
			err = l.lintOrg(f)
		default:
			return fmt.Errorf("unsupported markup format '%s'", f.NormedExt)
		}

		size := len(f.Alerts)
		if size != last {
			f.Alerts = adjustAlerts(f.Alerts, last, comment, lang)
		}
		last = size
	}

	return err
}
