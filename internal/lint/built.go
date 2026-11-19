package lint

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/vale-cli/vale/v3/internal/core"
)

// lintBuilt lints file from the HTML a host's converter produced for it.
// Only a format Vale itself reads through a converter has a built form.
func (l *Linter) lintBuilt(file *core.File, html []byte) error {
	switch file.NormedExt {
	case ".adoc":
		return l.lintADocHTML(file, string(html))
	case ".rst":
		return l.lintRSTHTML(file, string(html))
	case ".typ":
		return l.lintTypstHTML(file, string(html))
	case ".xml":
		return l.lintHTMLTokens(file, html, 0)
	case ".dita":
		return l.lintDITAHTML(file, html)
	default:
		return core.NewE100(file.Path, fmt.Errorf(
			"'%s' is not read through a converter, so it has no built form", file.NormedExt))
	}
}

// lintDITAHTML lints the toolkit's HTML for file, without its head.
func (l *Linter) lintDITAHTML(file *core.File, data []byte) error {
	head1 := bytes.Index(data, []byte("<head>"))
	head2 := bytes.Index(data, []byte("</head>"))

	if head1 >= 0 && head2 >= 0 {
		data = append(data[:head1], data[head2:]...)
	}

	return l.lintHTMLTokens(file, data, 0)
}

// ConverterOptions returns what a host must give the converter for format so
// that its HTML is what Vale would have produced: Asciidoctor's attributes,
// Docutils' flags, or xsltproc's parameters. The values are those Vale passes
// itself, plus, for AsciiDoc, the configuration's own `[asciidoctor]` section.
func ConverterOptions(cfg *core.Config, format string) (map[string]any, error) {
	switch format {
	case "adoc", ".adoc":
		attrs := append([]string{}, adocAttributes(adocArgs)...)
		attrs = append(attrs, adocCLIAttributes(cfg.Asciidoctor)...)
		return map[string]any{
			"attributes": attrs,
			"standalone": false,
			"safe":       "secure",
			"converter":  adocConverterJS,
		}, nil
	case "rst", ".rst":
		return map[string]any{"args": append([]string{}, rstArgs...)}, nil
	case "xml", ".xml":
		return map[string]any{"params": xsltParams()}, nil
	default:
		return nil, errors.New("no converter options for '" + format + "'")
	}
}

// adocAttributes returns the attributes in an argv of `-a name` pairs.
func adocAttributes(args []string) []string {
	var out []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-a" {
			out = append(out, args[i+1])
			i++
		}
	}
	return out
}

// adocConverterJS is adocConverter for Asciidoctor.js: a cross-reference
// the author gave no text renders as an empty link. The host registers it
// before converting.
const adocConverterJS = `class ValeHtml5Converter {
  constructor(asciidoctor) {
    this.base = asciidoctor.Html5Converter.create();
  }
  convert(node, transform) {
    const name = transform || node.getNodeName();
    if (name === "inline_anchor" && node.getType() === "xref" && !node.getText()) {
      return ` + "`<a href=\"${node.getTarget()}\"></a>`" + `;
    }
    return this.base.convert(node, transform);
  }
}
`

// xsltArgs are the arguments Vale gives xsltproc. A stylesheet is project
// configuration that runs as a program, so it is contained the way the
// AsciiDoc converter's safe mode contains a document: it can't fetch over the
// network, write a file, or create a directory. Its parameters turn off
// extensions and the generated table of contents, whose text is in no source
// line.
var xsltArgs = []string{
	"--nonet",
	"--nowrite",
	"--nomkdir",
	"--stringparam",
	"use.extensions",
	"0",
	"--stringparam",
	"generate.toc",
	"nop",
}

// xsltParams returns xsltArgs as the name-value pairs a host's XSLT
// processor takes.
func xsltParams() map[string]string {
	out := map[string]string{}
	for i := 0; i+2 < len(xsltArgs); i++ {
		if xsltArgs[i] == "--stringparam" {
			out[xsltArgs[i+1]] = xsltArgs[i+2]
			i += 2
		}
	}
	return out
}
