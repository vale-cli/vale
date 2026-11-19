package lint

import (
	"regexp"
	"runtime"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// NOTE: Asciidoctor converts "'" to "’".
//
// See #206.
var adocSanitizer = strings.NewReplacer(
	"‘", "&apos;",
	"’", "&apos;",
	"“", "&#8220;",
	"”", "&#8221;",
	"&#8217;", "&apos;",
	"&rsquo;", "&apos;")

// Convert listing blocks of the form `[source,.+]` to `[source]`
var reSource = regexp.MustCompile(`\[source,.+\]`)
var reComment = regexp.MustCompile(`// .+`)

// adocArgs is the Asciidoctor command line a host's own converter must be
// given for its HTML to be what Vale produces; see ValeConverterOptions.
// adocAttrs is the same as the attributes Vale converts with.
var adocArgs = []string{
	"-s",
	"-a",
	"notitle!",
	"-a",
	"attribute-missing=drop",
	// Section numbers and a table of contents are generated artifacts: they
	// prefix headings with "N." or duplicate heading text in a <ul class="toc">.
	// Both break heading detection and position lookup, so we force them off --
	// they aren't authored prose. CLI attributes override in-document ones.
	//
	// See https://github.com/errata-ai/vale/issues/1101.
	"-a",
	"sectnums!",
	"-a",
	"toc!",
	// Caption labels are generated the same way: "Table 1." prefixes a block
	// title with text that appears nowhere in the source, so a match against
	// it lands on an unrelated line.
	//
	// See https://github.com/vale-cli/vale/issues/1152.
	"-a",
	"table-caption!",
	"-a",
	"figure-caption!",
	"-a",
	"example-caption!",
	"-a",
	"listing-caption!",
}

// poolConcurrency is how many files Vale lints at once, and so how many
// helper processes a pool keeps warm. On a machine with more cores than a
// flat number it would leave them idle, and on one with fewer it would
// oversubscribe.
var poolConcurrency = max(2, runtime.NumCPU()-2)

func (l *Linter) lintADoc(f *core.File) error {
	s, err := l.Transform(f)
	if err != nil {
		return err
	}
	s = adocSanitizer.Replace(s)

	html, err := convertADoc(s, l.Manager.Config.Asciidoctor)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	return l.lintADocHTML(f, html)
}

// lintADocHTML lints a document from Asciidoctor's HTML for it. Source
// blocks and line comments are masked in the source first, so a match in
// the HTML can't be placed on them.
func (l *Linter) lintADocHTML(f *core.File, html string) error {
	if err := l.lintMetadata(f); err != nil {
		return err
	}

	html = adocSanitizer.Replace(html)
	body := reSource.ReplaceAllStringFunc(f.Content, func(m string) string {
		offset := 0
		if strings.HasSuffix(m, ",]") {
			offset = 1
			m = strings.Replace(m, ",]", "]", 1)
		}
		// NOTE: This is required to avoid finding matches in block attributes.
		//
		// See https://github.com/errata-ai/vale/issues/296.
		parts := strings.Split(m, ",")
		size := nlp.StrLen(parts[len(parts)-1])

		span := strings.Repeat("*", size-2+offset)
		return "[source, " + span + "]"
	})

	body = reComment.ReplaceAllStringFunc(body, func(m string) string {
		// NOTE: This is required to avoid finding matches in line comments.
		//
		// See https://github.com/errata-ai/vale/issues/414.
		//
		// TODO: Multiple line comments are not handled correctly.
		//
		// https://docs.asciidoctor.org/asciidoc/latest/comments/
		span := strings.Repeat("*", nlp.StrLen(strings.TrimPrefix(m, "// ")))
		return "// " + span
	})

	f.Content = body
	return l.lintHTMLTokens(f, []byte(html), 0)
}

// adocCLIAttributes renders the configuration's attributes as `-a` values
// for a host's own Asciidoctor.
func adocCLIAttributes(attrs map[string]string) []string {
	var out []string
	for k, v := range attrs {
		switch v {
		case "YES":
			out = append(out, k)
		case "NO":
			out = append(out, k+"!")
		default:
			out = append(out, k+"="+v)
		}
	}
	return out
}
