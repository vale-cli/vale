package lint

import (
	"bytes"
	"strings"
	"testing"
)

// TestMarkdocHTML pins what the Markdoc extension makes of each construct:
// a paired tag becomes a classed div (or span) the walker lints and scopes,
// its string attributes become classed data elements under `meta`, and
// annotations, variables, functions, and closing tags render as nothing.
// See #1213.
func TestMarkdocHTML(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   []string
		absent []string
	}{
		{
			"a paired tag is a classed div",
			"{% callout type=\"check\" %}\nCallout prose here.\n{% /callout %}\n\nAfter.\n",
			[]string{`<div class="callout"><data class="type">check</data>`, "<p>Callout prose here.</p>", "</div>", "<p>After.</p>"},
			[]string{"{%", "%}"},
		},
		{
			"string attributes are data; the other kinds are not",
			"{% card title=\"Lorem Ipsum\" size=\"large\" index=0 open=true tags=[\"a\", \"b\"] meta={id: \"x\"} color=$color %}\nBody.\n{% /card %}\n",
			[]string{`<data class="title">Lorem Ipsum</data><data class="size">large</data>`, "<p>Body.</p>"},
			[]string{"index", "true", `"a"`, `"x"`, "$color", "<data class=\"tags\">", "<data class=\"meta\">"},
		},
		{
			"attributes span lines",
			"{% card\n   title=\"Spread out\"\n   size=\"large\" %}\nBody.\n{% /card %}\n",
			[]string{`<div class="card"><data class="title">Spread out</data><data class="size">large</data>`, "<p>Body.</p>", "</div>"},
			[]string{"<p>title", "size=", "<p>{%"},
		},
		{
			"a self-closing tag has no body",
			"Before.\n\n{% partial file=\"header.md\" /%}\n\nAfter.\n",
			[]string{"<p>Before.</p>", `<div class="partial"><data class="file">header.md</data></div>`, "<p>After.</p>"},
			[]string{"<p>{%", "/%}"},
		},
		{
			"tags nest, by name",
			"{% callout %}\nOuter.\n\n{% callout %}\nInner.\n{% /callout %}\n\nStill outer.\n{% /callout %}\n\nOutside.\n",
			[]string{"<p>Inner.</p>\n</div>\n<p>Still outer.</p>\n</div>\n<p>Outside.</p>"},
			[]string{"{%"},
		},
		{
			"if and else are tags",
			"{% if equals($a, 1) %}\nOne.\n{% else /%}\nOther.\n{% /if %}\n",
			[]string{`<div class="if">`, "<p>One.</p>", `<div class="else"></div>`, "<p>Other.</p>", "</div>"},
			[]string{"equals", "{%"},
		},
		{
			"a stray closing tag is markup",
			"Text.\n\n{% /callout %}\n\nMore.\n",
			[]string{"<p>Text.</p>", "<p>More.</p>"},
			[]string{"callout", "{%"},
		},
		{
			"variables, functions, and annotations are nothing",
			"# {% $markdoc.frontmatter.title %} {% #overview %}\n\nIt is {% uppercase($name) %} and {% $version %}.\n\n- Function {% width=\"25%\" %}\n",
			[]string{"<h1> </h1>", "<p>It is  and .</p>", "<li>Function </li>"},
			[]string{"markdoc", "overview", "uppercase", "version", "25%"},
		},
		{
			"an inline tag is a span",
			"Press {% kbd %}Enter{% /kbd %} to {% link href=\"/docs\" %}continue{% /link %}.\n",
			[]string{`Press <span class="kbd">Enter</span> to <span class="link"><data class="href">/docs</data>continue</span>.`},
			[]string{"{%"},
		},
		{
			"a tag pair alone on one line is inline",
			"{% highlight %}Inline tag{% /highlight %}\n",
			[]string{`<p><span class="highlight">Inline tag</span></p>`},
			[]string{"<div"},
		},
		{
			"a tag line followed by text is a paragraph",
			"{% callout %}Same line.\n{% /callout %}\n",
			[]string{`<span class="callout">Same line.`},
			[]string{"<div"},
		},
		{
			"a table is a tag holding lists",
			"{% table %}\n* Heading 1\n* Heading 2\n---\n* Cell 1 {% colspan=2 %}\n* Cell 2\n{% /table %}\n",
			[]string{`<div class="table">`, "<li>Heading 1</li>", "<hr>", "<li>Cell 1 </li>", "<li>Cell 2</li>"},
			[]string{"colspan", "{%"},
		},
		{
			"a string holds the closing delimiter",
			"{% callout title=\"100%} done\" %}\nBody.\n{% /callout %}\n",
			[]string{`<data class="title">100%} done</data>`, "<p>Body.</p>"},
			[]string{"<p>{%", "done\" %}"},
		},
		{
			"an escaped quote stays in its string",
			"{% callout title=\"Say \\\"hi\\\" now\" %}\nBody.\n{% /callout %}\n",
			[]string{`<data class="title">Say \&quot;hi\&quot; now</data>`, "<p>Body.</p>"},
			[]string{"<p>{%"},
		},
		{
			"an unclosed delimiter is prose",
			"A {% sign in text.\n",
			[]string{"<p>A {% sign in text.</p>"},
			nil,
		},
		{
			"a fence is untouched",
			"```md\n{% callout %}\nShown as code.\n{% /callout %}\n```\n",
			[]string{"<pre><code class=\"language-md\">{% callout %}", "{% /callout %}"},
			[]string{"<div"},
		},
		{
			"a tag in a list item",
			"- Item.\n\n  {% callout %}\n  Nested prose.\n  {% /callout %}\n\n- Next.\n",
			[]string{`<div class="callout">`, "<p>Nested prose.</p>", "</div>", "<li>\n<p>Next.</p>"},
			[]string{"{%"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := goldMarkdoc.Convert([]byte(c.in), &buf); err != nil {
				t.Fatal(err)
			}
			html := buf.String()

			for _, want := range c.want {
				if !strings.Contains(html, want) {
					t.Errorf("missing %q in:\n%s", want, html)
				}
			}
			for _, absent := range c.absent {
				if strings.Contains(html, absent) {
					t.Errorf("unwanted %q in:\n%s", absent, html)
				}
			}
		})
	}
}
