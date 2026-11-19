package lint

import (
	"regexp"
	"strings"

	"github.com/jdkato/rstgo"

	"github.com/vale-cli/vale/v3/internal/core"
)

// reStructuredText configuration.
//
// reCodeBlock is used to convert Sphinx-style code directives to the regular
// `::` for Docutils, including the use of runtime options (e.g., :caption:).
var reCodeBlock = regexp.MustCompile(`\.\. (?:raw|code(?:-block)?):: *(?:[\w-]+)?(?:\s+:\w+:.*)*`)

// A `contents` directive repeats the document's headings, so it is read as
// code. See https://github.com/errata-ai/vale/v2/issues/119.
var reSphinx = regexp.MustCompile(`.. contents::`)

// rstArgs is the Docutils command line a host's own rst2html must be given
// for its HTML to be what Vale produces; see ValeConverterOptions.
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

// Docutils knows its own directives and roles, and a Sphinx project's files
// use Sphinx's and its extensions' as well; Docutils drops the body of every
// directive it does not know. So the body of an unknown directive is prose,
// except for a named few holding code, data, or generated content, and the
// text of an unknown role is code, except for the reference and interface
// roles, whose text is prose. A project's `[sphinx]` section adds to both
// lists. See #294.
var sphinxCode = map[string]bool{
	"toctree": true, "literalinclude": true, "highlight": true, "index": true,
	"tabularcolumns": true, "math": true, "graphviz": true, "graph": true,
	"digraph": true, "inheritance-diagram": true, "productionlist": true,
	"doctest": true, "testcode": true, "testoutput": true, "testsetup": true,
	"testcleanup": true, "currentmodule": true, "module": true,
	"default-domain": true, "codeauthor": true, "sectionauthor": true,
	"cssclass": true, "rst-class": true,
}

var sphinxProse = map[string]bool{
	"ref": true, "doc": true, "any": true, "numref": true, "term": true,
	"guilabel": true, "menuselection": true, "abbr": true, "dfn": true,
}

// A reference role's bare text names a target, not words.
var sphinxTargeted = map[string]bool{"ref": true, "doc": true, "any": true, "numref": true}

// rstOptions is how Vale reads reStructuredText: the flags in rstArgs, plus
// the Sphinx fallbacks for the configuration's `[sphinx]` section.
func (l *Linter) rstOptions() rstgo.Options {
	code := map[string]bool{}
	for name := range sphinxCode {
		code[name] = true
	}
	for _, name := range l.Manager.Config.SphinxNames("CodeDirectives") {
		code[strings.ToLower(name)] = true
	}
	prose := map[string]bool{}
	for name := range sphinxProse {
		prose[name] = true
	}
	for _, name := range l.Manager.Config.SphinxNames("ProseRoles") {
		prose[strings.ToLower(name)] = true
	}

	return rstgo.Options{
		ReportLevel:         5,
		HaltLevel:           5,
		LinkStylesheet:      true,
		NoFileInsertion:     true,
		TocBacklinks:        "none",
		NoFootnoteBacklinks: true,
		NoSectionNumbering:  true,
		UnknownDirective: func(name string) rstgo.DirectiveFallback {
			norm := strings.ToLower(name)
			if code[norm] || strings.HasPrefix(norm, "auto") {
				return rstgo.DirectiveLiteral
			}
			return rstgo.DirectiveProse
		},
		UnknownRole: func(name string) rstgo.RoleFallback {
			base := strings.ToLower(name)
			base = base[strings.LastIndexByte(base, ':')+1:]
			switch {
			case sphinxTargeted[base]:
				return rstgo.RoleReference
			case prose[base]:
				return rstgo.RoleText
			}
			return rstgo.RoleLiteral
		},
	}
}

func (l *Linter) lintRST(f *core.File) error {
	s, err := l.prepareRST(f)
	if err != nil {
		return err
	}

	html, err := l.convertRST(s)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	return l.lintRSTHTML(f, html)
}

// prepareRST is the document as Docutils is given it.
func (l *Linter) prepareRST(f *core.File) (string, error) {
	s, err := l.Transform(f)
	if err != nil {
		return "", err
	}

	s = reSphinx.ReplaceAllString(s, ".. code::")
	s = reCodeBlock.ReplaceAllString(s, "::")
	return s, nil
}

// lintRSTHTML lints a document from its converted form.
func (l *Linter) lintRSTHTML(f *core.File, html string) error {
	if err := l.lintMetadata(f); err != nil {
		return err
	}
	return l.lintHTMLTokens(f, []byte(markSubstitutions(html)), 0)
}

// A substitution Docutils couldn't resolve, one a Sphinx project defines in
// its `rst_epilog`, comes through as written, since --report=5 drops the
// `problematic` markup it would get otherwise.
var reSubstitution = regexp.MustCompile(`\|[^\s|<>](?:[^|<>]*[^\s|<>])?\|(?:__?)?`)

// Literal text keeps its pipes as they are.
var reRSTLiteral = regexp.MustCompile(`(?s)<pre\b.*?</pre>|<tt\b.*?</tt>|<code\b.*?</code>`)

// markSubstitutions wraps each unresolved substitution in the `problematic`
// span Docutils would have given it, so it is skipped rather than read as
// prose.
func markSubstitutions(html string) string {
	if !strings.Contains(html, "|") {
		return html
	}

	var b strings.Builder
	last := 0
	for _, lit := range reRSTLiteral.FindAllStringIndex(html, -1) {
		b.WriteString(reSubstitution.ReplaceAllString(html[last:lit[0]], `<span class="problematic">$0</span>`))
		b.WriteString(html[lit[0]:lit[1]])
		last = lit[1]
	}
	b.WriteString(reSubstitution.ReplaceAllString(html[last:], `<span class="problematic">$0</span>`))
	return b.String()
}

// convertRSTBatch converts the documents and returns each one's HTML in
// order.
func (l *Linter) convertRSTBatch(docs []string) ([]string, error) {
	out := make([]string, len(docs))
	for i, doc := range docs {
		html, err := l.convertRST(doc)
		if err != nil {
			return nil, err
		}
		out[i] = html
	}
	return out, nil
}

// convertRST converts one document to the body of its HTML.
func (l *Linter) convertRST(text string) (string, error) {
	html, err := rstgo.Convert([]byte(text), l.rstOptions())
	if err != nil {
		return "", err
	}
	return rstBody(string(html)), nil
}

// rstBody takes the document body out of a full rst2html page.
func rstBody(html string) string {
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

	return html[bodyStart+7 : bodyEnd]
}
