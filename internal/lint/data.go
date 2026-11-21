package lint

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// hasView reports whether a `textfsm` or `dasel` view applies to the file.
// Either reads the whole file itself, so the file's own format does not
// matter; a tree-sitter view is reached through the code path instead.
func (l *Linter) hasView(f *core.File) bool {
	_, view := l.Manager.Config.ViewFor(f.Held, f.Path)
	return view != nil && view.Engine != "tree-sitter"
}

// ruleProse names the fields of a Vale rule that hold prose.
var ruleProse = []string{"message", "description"}

// extendsKey opens a Vale rule: a top-level `extends`.
var extendsKey = regexp.MustCompile(`(?m)^extends:[ \t]*\S`)

// isRuleFile reports whether f is a Vale rule, which a YAML file with a
// top-level `extends` is.
func isRuleFile(f *core.File) bool {
	return f.NormedExt == ".yml" && extendsKey.MatchString(f.Content)
}

// lintRule lints a Vale rule's message and description, and nothing else:
// its tokens, swaps, and exceptions are patterns, and often wrong on purpose.
func (l *Linter) lintRule(f *core.File) error {
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(f.Content), &fields); err != nil {
		return nil //nolint:nilerr // not a rule Vale could load either; nothing to lint
	}

	view := core.View{Engine: "dasel"}
	for _, key := range ruleProse {
		if _, ok := fields[key]; ok {
			view.Scopes = append(view.Scopes, core.Scope{Expr: key})
		}
	}
	if len(view.Scopes) == 0 {
		return nil
	}

	found, err := view.Apply(f)
	if err != nil {
		return core.NewE100(f.Path, err)
	}
	return l.lintScopedValues(f, found)
}

// hasCodeView reports whether a tree-sitter view applies to the file.
func (l *Linter) hasCodeView(f *core.File) bool {
	_, view := l.Manager.Config.ViewFor(f.Held, f.Path)
	return view != nil && view.Engine == "tree-sitter"
}

func (l *Linter) lintData(f *core.File) error {
	syntax, view := l.Manager.Config.ViewFor(f.Held, f.Path)
	if view == nil || view.Engine == "tree-sitter" {
		return nil
	}

	// The View says which values are prose. The file's comments are prose
	// whatever it says, and are read as a source file's.
	if cerr := l.lintDataComments(f); cerr != nil {
		return cerr
	}

	found, berr := view.Apply(f)
	if berr != nil {
		return core.NewE201FromTarget(
			fmt.Sprintf("%s: %s", f.Path, berr),
			fmt.Sprintf("[%s] View", syntax),
			l.Manager.Config.RootINI,
		)
	}
	return l.lintScopedValues(f, found)
}

func (l *Linter) lintScopedValues(f *core.File, values []core.ScopedValues) error {
	var err error
	wholeFile := f.Content
	srcLines := strings.Split(wholeFile, "\n")
	// The file's comments were placed already; only the values' alerts move.
	last := len(f.Alerts)

	f.Scoped = make(map[string][]string, len(values))
	for _, match := range values {
		for _, sv := range match.Values {
			f.Scoped[match.Scope] = append(f.Scoped[match.Scope], sv.Text)
		}
	}

	for _, match := range values {
		f.SetMetaScope(match.Scope)
		// Once per scope, not per value: the lookup walks the format table.
		asCode := isCode(match.Format)

		seen := make(map[string]int)
		for _, sv := range match.Values {
			v := sv.Text

			line := ""
			i := sv.Line
			padding := sv.Column - 1
			fromParse := i > 0 && sv.Column > 0

			if fromParse {
				if i-1 < len(srcLines) {
					line = srcLines[i-1]
				}
			} else {
				var found int
				found, line = findLineBySubstring(wholeFile, v, seen)
				if found < 0 {
					return core.NewE100(f.Path, fmt.Errorf("'%s' not found", v))
				}
				i = found
				seen[line] = i
				padding = strings.Index(line, v)
				if strings.Count(v, "\n") > 0 {
					firstLine := strings.SplitN(v, "\n", 2)[0]
					padding = strings.Index(line, firstLine)
					if padding < 0 {
						// block scalar case - use indentation of matched line
						i--
						padding = strings.Index(line, strings.TrimSpace(line))
					}
				}
			}

			if strings.Contains(line, "\\n") && !sv.Joined {
				f.SetText(strings.ReplaceAll(v, "\n", " "))
			} else {
				f.SetText(v)
			}
			if sv.Joined {
				// One element per source line: an alert's line within the
				// value is its offset from the first, and there is no
				// escaped newline to widen its column by.
				line = ""
			}
			f.SetNormedExt(match.Format)

			switch {
			case v == "":
				// An empty value has nothing to parse, and parsing it yields no
				// block at all; a rule that requires text needs one to run on.
				err = l.lintLines(f)
			case match.Format == "md":
				err = l.lintMarkdown(f)
			case match.Format == "rst":
				err = l.lintRST(f)
			case match.Format == "html":
				err = l.lintHTML(f)
			case match.Format == "org":
				err = l.lintOrg(f)
			case match.Format == "adoc":
				err = l.lintADoc(f)
			case asCode:
				err = l.lintAsCode(f, "."+match.Format)
			default:
				err = l.lintLines(f)
			}
			if err != nil {
				return err
			}

			size := len(f.Alerts)
			switch {
			case size == last:
				continue
			case len(sv.Parts) > 0:
				f.Alerts = placeAlerts(f.Alerts, last, sv, f.Content)
			default:
				f.Alerts = adjustPos(f.Alerts, last, i, padding, v, line)
			}
			last = size
		}
	}

	// The values were linted in the file's place; put the file back, so the
	// `raw` scope that runs next reads the document and not the last value.
	f.RestoreText(wholeFile)
	return err
}

// placeAlerts moves the alerts from last on, reported against the value's
// text, to where the value's parts sit in the source.
func placeAlerts(alerts []core.Alert, last int, sv core.ScopedValue, text string) []core.Alert {
	lines := strings.Split(text, "\n")
	for i := last; i < len(alerts); i++ {
		a := &alerts[i]

		// The alert's rune offset in the value.
		off := 0
		for k := 0; k < a.Line-1 && k < len(lines); k++ {
			off += nlp.StrLen(lines[k]) + 1
		}
		off += max(a.Span[0]-1, 0)

		line, col := sv.Locate(off)
		endLine, end := sv.Locate(off + max(a.Span[1]-a.Span[0], 0))
		if endLine != line || end < col {
			end = col + a.Span[1] - a.Span[0]
		}
		a.Line, a.Span = line, []int{col, end}
	}
	return alerts
}

// isCode reports whether a scope's `type` names a programming language.
func isCode(format string) bool {
	_, kind := core.FormatFromExt("x."+format, nil)
	return kind == "code"
}

// lintAsCode lints the file's text as code in the given language, so that
// its comments are the prose.
func (l *Linter) lintAsCode(f *core.File, ext string) error {
	realExt, normedExt := f.RealExt, f.NormedExt
	f.RealExt, f.NormedExt = ext, ext

	err := l.lintCode(f)

	f.RealExt, f.NormedExt = realExt, normedExt
	return err
}
