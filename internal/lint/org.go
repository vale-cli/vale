//go:build !vale_noconvert

package lint

import (
	"html"
	"regexp"
	"strings"

	"github.com/niklasfasching/go-org/org"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

var orgConverter = org.New()

var reOrgAttribute = regexp.MustCompile(`(#(?:\+| )[^\s]+:.+)`)

// orgProseKeys are the keywords whose value is prose: the document's title,
// its author, and what it says about itself. Every other keyword holds a
// setting, and is hidden from the converter as verbatim text.
var orgProseKeys = map[string]bool{
	"TITLE": true, "SUBTITLE": true, "AUTHOR": true, "DESCRIPTION": true,
}

// reOrgTags matches the tags at the end of a headline, `:work:urgent:`.
var reOrgTags = regexp.MustCompile(`(?m)^\*+[ \t]+.*?([ \t]+:[\w@#%:]+:)[ \t]*$`)

// orgKeywordKey is the key of a keyword line as reOrgAttribute matched it.
func orgKeywordKey(line string) string {
	key, _, _ := strings.Cut(line, ":")
	return strings.ToUpper(strings.TrimLeft(key, "#+ "))
}

var reOrgProps = regexp.MustCompile(`(:PROPERTIES:\n.+\n:END:)`)
var reOrgSrc = regexp.MustCompile(`(?i)#\+BEGIN_SRC .+`)

type ExtendedHTMLWriter struct {
	*org.HTMLWriter
}

func (w *ExtendedHTMLWriter) WriteComment(n org.Comment) {
	w.HTMLWriter.WriteString("<!-- ")
	w.HTMLWriter.WriteString(n.Content)
	w.HTMLWriter.WriteString(" -->\n")
}

// WriteKeyword writes a prose keyword where it is: the title as a heading,
// the author or description as a paragraph. go-org writes the title at the
// top of the document instead, which is not where the source has it, so that
// is switched off in lintOrg.
func (w *ExtendedHTMLWriter) WriteKeyword(k org.Keyword) {
	key := strings.ToUpper(k.Key)
	if !orgProseKeys[key] {
		w.HTMLWriter.WriteKeyword(k)
		return
	}

	value := w.inline(k.Value)
	if key == "TITLE" || key == "SUBTITLE" {
		w.HTMLWriter.WriteString(`<h1 class="title">` + value + "</h1>\n")
	} else {
		w.HTMLWriter.WriteString(`<p class="` + strings.ToLower(key) + `">` + value + "</p>\n")
	}
}

// inline renders a keyword's value, which may carry markup of its own.
func (w *ExtendedHTMLWriter) inline(value string) string {
	d := orgConverter.Parse(strings.NewReader(value), "")
	if d.Error == nil && len(d.Nodes) == 1 {
		if p, ok := d.Nodes[0].(org.Paragraph); ok {
			return w.HTMLWriter.WriteNodesAsString(p.Children...)
		}
	}
	return html.EscapeString(value)
}

// WriteFootnoteDefinition writes a footnote's body where the source has it.
// go-org collects the definitions and writes them at the end of the
// document, and an alert in one was then reported wherever the text next
// happened to match.
func (w *ExtendedHTMLWriter) WriteFootnoteDefinition(f org.FootnoteDefinition) {
	w.HTMLWriter.WriteString(`<div class="footnote-definition">` + "\n")
	org.WriteNodes(w.HTMLWriter, f.Children...)
	w.HTMLWriter.WriteString("</div>\n")
}

// WriteFootnoteLink writes an inline footnote's text and nothing for a
// reference: the number go-org writes is not in the source.
func (w *ExtendedHTMLWriter) WriteFootnoteLink(l org.FootnoteLink) {
	if l.Definition == nil || !l.Definition.Inline {
		return
	}
	for _, n := range l.Definition.Children {
		if p, ok := n.(org.Paragraph); ok {
			org.WriteNodes(w.HTMLWriter, p.Children...)
		} else {
			org.WriteNodes(w.HTMLWriter, n)
		}
	}
}

func (l *Linter) lintOrg(f *core.File) error {
	// A writer per file: `org.HTMLWriter` accumulates its output in an embedded
	// `strings.Builder` that nothing resets, so a shared one hands each file
	// every earlier file's HTML as well. See #1129.
	writer := org.NewHTMLWriter()
	writer.ExtendingWriter = &ExtendedHTMLWriter{writer}

	old := f.Content

	s := reOrgAttribute.ReplaceAllStringFunc(f.Content, func(m string) string {
		if orgProseKeys[orgKeywordKey(m)] {
			return m
		}
		return "\n=" + m + "=\n"
	})
	s = reOrgProps.ReplaceAllString(s, orgExample)

	// A headline's tags are labels, not part of its text.
	s = reOrgTags.ReplaceAllStringFunc(s, func(m string) string {
		loc := reOrgTags.FindStringSubmatchIndex(m)
		return m[:loc[2]] + strings.Repeat(" ", loc[3]-loc[2]) + m[loc[3]:]
	})

	f.Content = s

	err := l.lintMetadata(f)
	if err != nil {
		return err
	}

	s, err = l.Transform(f)
	if err != nil {
		return err
	}
	f.Content = old

	// We don't want to find matches in `begin_src` lines.
	body := reOrgSrc.ReplaceAllStringFunc(f.Content, func(m string) string {
		return strings.Repeat("*", nlp.StrLen(m))
	})

	doc := orgConverter.Parse(strings.NewReader(s), f.Path)
	// We don't want to introduce any *new* content into our HTML,
	// so we clear the outline.
	doc.Outline.Children = nil

	// The title and the footnotes are written where the source has them (see
	// ExtendedHTMLWriter), not at the top and the end. The first setting of
	// an option wins, so the file's own cannot turn these back on.
	doc.BufferSettings["OPTIONS"] = "title:nil f:nil " + doc.BufferSettings["OPTIONS"]

	html, err := doc.Write(writer)
	if err != nil {
		return err
	}

	f.Content = body
	return l.lintHTMLTokens(f, []byte(html), 0)
}
