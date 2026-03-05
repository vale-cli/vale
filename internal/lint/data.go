package lint

import (
	"fmt"
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/glob"
)

func (l *Linter) lintData(f *core.File) error {
	for syntax, view := range l.Manager.Config.Views {
		sec, err := glob.Compile(syntax)
		if err != nil {
			return err
		} else if sec.Match(f.Path) {
			found, berr := view.Apply(f)
			if berr != nil {
				return core.NewE201FromTarget(
					berr.Error(),
					fmt.Sprintf("View = %s", view),
					l.Manager.Config.RootINI,
				)
			}
			return l.lintScopedValues(f, found)
		}
	}
	return nil
}

func (l *Linter) lintScopedValues(f *core.File, values []core.ScopedValues) error {
	var err error
	// We want to set up our processing servers as if we were dealing with
	// a directory since we likely have many fragments to convert.
	l.HasDir = true

	wholeFile := f.Content
	last := 0

	for _, match := range values {
		l.SetMetaScope(match.Scope)

		seen := make(map[string]int)
		for _, v := range match.Values {
			i, line := findLineBySubstring(wholeFile, v, seen)
			if i < 0 {
				return core.NewE100(f.Path, fmt.Errorf("'%s' not found", v))
			}
			seen[line] = i

			if strings.Contains(line, "\\n") {
				f.SetText(strings.ReplaceAll(v, "\n", " "))
			} else {
				f.SetText(v)
			}
			f.SetNormedExt(match.Format)

			switch match.Format {
			case "md":
				err = l.lintMarkdown(f)
			case "rst":
				err = l.lintRST(f)
			case "html":
				err = l.lintHTML(f)
			case "org":
				err = l.lintOrg(f)
			case "adoc":
				err = l.lintADoc(f)
			default:
				err = l.lintLines(f)
			}

			size := len(f.Alerts)
			if size != last {
				padding := strings.Index(line, v)
				if strings.Count(v, "\n") > 0 {
					firstLine := strings.SplitN(v, "\n", 2)[0]
					padding = strings.Index(line, firstLine)
					if padding < 0 {
						// block scalar case - use indentation of matched line
						i-- // adjust for 1-based line numbers
						padding = strings.Index(line, strings.TrimSpace(line))
					}
				}
				f.Alerts = adjustPos(f.Alerts, last, i, padding, v, line)
			}
			last = size
		}
	}

	return err
}
