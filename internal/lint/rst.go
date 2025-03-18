package lint

import (
	"errors"
	"regexp"
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/system"
)

// reStructuredText configuration.
//
// reCodeBlock is used to convert Sphinx-style code directives to the regular
// `::` for rst2html, including the use of runtime options (e.g., :caption:).
var reCodeBlock = regexp.MustCompile(`\.\. (?:raw|code(?:-block)?):: *(?:[\w-]+)?(?:\s+:\w+:.*)*`)

// We replace custom directives with `.. code::`.
//
// See https://github.com/errata-ai/vale/v2/issues/119.
var reSphinx = regexp.MustCompile(`.. (?:glossary|contents)::`)
var rstArgs = []string{
	"--quiet",
	"--halt=5",
	"--report=5",
	"--link-stylesheet",
	"--no-file-insertion",
	"--no-toc-backlinks",
	"--no-footnote-backlinks",
	"--no-section-numbering",
}

func (l *Linter) lintRST(f *core.File) error {
	var html string

	rst2html := system.Which([]string{
		"rst2html", "rst2html.py", "rst2html-3", "rst2html-3.py"})
	python := system.Which([]string{
		"python", "py", "python.exe", "python3", "python3.exe", "py3"})

	if rst2html == "" || python == "" {
		return core.NewE100("lintRST", errors.New("rst2html not found"))
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	s = reSphinx.ReplaceAllString(s, ".. code::")
	s = reCodeBlock.ReplaceAllString(s, "::")

	html, err = callRst(s, rst2html, python)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	return l.lintHTMLTokens(f, []byte(html), 0)
}

func callRst(text, lib, _ string) (string, error) {
	html, err := system.ExecuteWithInput(lib, text, rstArgs...)
	if err != nil {
		return "", err
	}
	html = strings.ReplaceAll(html, "\r", "")

	bodyStart := strings.Index(html, "<body>\n")
	if bodyStart < 0 {
		bodyStart = -7
	}
	bodyEnd := strings.Index(html, "\n</body>")
	if bodyEnd < 0 || bodyEnd >= len(html) {
		bodyEnd = len(html) - 1
		if bodyEnd < 0 {
			bodyEnd = 0
		}
	}

	return html[bodyStart+7 : bodyEnd], nil
}
