package lint

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	grh "github.com/yuin/goldmark/renderer/html"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// Markdown configuration.
var goldMd = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
	),
	goldmark.WithRendererOptions(
		grh.WithUnsafe(),
	),
)

// Convert extended info strings -- e.g., ```callout{'title': 'NOTE'} -- that
// might confuse Blackfriday into normal "```".
var reExInfo = regexp.MustCompile("`{3,}" + `.+`)

var reLinkRef = regexp.MustCompile(`\]\[(?:[^]\n]+)\]`)
var reLinkDef = regexp.MustCompile(`\[(?:[^]\n]+)\]:`)

var reNumericList = regexp.MustCompile(`(?m)^\d+\.`)

func (l Linter) lintMarkdown(f *core.File) error {
	var buf bytes.Buffer

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	if err = goldMd.Convert([]byte(s), &buf); err != nil {
		return core.NewE100(f.Path, err)
	}

	f.Content = prepMarkdown(f.Content)
	return l.lintHTMLTokens(f, buf.Bytes(), 0)
}

func prepMarkdown(content string) string {
	// NOTE: This is required to avoid finding matches inside info strings. For
	// example, if we're looking for 'json' we many incorrectly report the
	// location as being in an infostring like '```json'.
	//
	// See https://github.com/errata-ai/vale/v2/issues/248.
	body := reExInfo.ReplaceAllStringFunc(content, func(m string) string {
		parts := strings.Split(m, "`")

		// This ensures that we respect the number of opening backticks, which
		// could be more than 3.
		//
		// See https://github.com/errata-ai/vale/v2/issues/271.
		tags := strings.Repeat("`", len(parts)-1)
		span := strings.Repeat("*", nlp.StrLen(parts[len(parts)-1]))

		return tags + span
	})

	// NOTE: This is required to avoid finding matches inside link references.
	body = reLinkRef.ReplaceAllStringFunc(body, func(m string) string {
		return "][" + strings.Repeat("*", nlp.StrLen(m)-3) + "]"
	})
	body = reLinkDef.ReplaceAllStringFunc(body, func(m string) string {
		return "[" + strings.Repeat("*", nlp.StrLen(m)-3) + "]:"
	})

	// NOTE: This is required to avoid finding matches inside ordered lists.
	body = reNumericList.ReplaceAllStringFunc(body, func(m string) string {
		return strings.Repeat("*", nlp.StrLen(m))
	})

	return body
}
