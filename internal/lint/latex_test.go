package lint

import (
	"strings"
	"testing"
)

// TestTeXHTML pins what the LaTeX reader makes of each construct: prose is
// kept, commands become the elements they mean, and setup, keys and math are
// markup with nothing to lint.
func TestTeXHTML(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   []string
		absent []string
	}{
		{
			"the preamble is setup, its title aside",
			"\\documentclass{article}\n\\usepackage{setup}\n\\title{The Title}\n" +
				"\\author{A. Person}\n\\begin{document}\nProse here.\n\\end{document}\nAfter.\n",
			[]string{"<h1>The Title</h1>", "<p>Prose here.\n</p>"},
			[]string{"article", "setup", "Person", "After"},
		},
		{
			"a file with no document environment is all body",
			"\\section{Methods}\n\nProse here.\n",
			[]string{"<h2>Methods</h2>", "<p>Prose here.</p>"},
			nil,
		},
		{
			"sections are headings",
			"\\chapter{One}\n\\section*{Two}\n\\subsection[short]{Three}\n\\paragraph{Four} Text.\n",
			[]string{"<h1>One</h1>", "<h2>Two</h2>", "<h3>Three</h3>", "<h5>Four</h5>", "<p>Text.</p>"},
			[]string{"short"},
		},
		{
			"a blank line ends a paragraph",
			"One\ntwo.\n\nThree.\n",
			[]string{"<p>One\ntwo.</p>", "<p>Three.</p>"},
			nil,
		},
		{
			"a comment is dropped with its line break",
			"One % hidden\n  two.\n% hidden\n\nThree.\n",
			[]string{"<p>One two.\n</p>", "<p>Three.</p>"},
			[]string{"hidden"},
		},
		{
			"a comment to Vale is kept",
			"One.\n\n% vale off\n\nTwo.\n",
			[]string{"<!-- vale off-->"},
			nil,
		},
		{
			"styled text keeps its element",
			"\\emph{a} \\textit{b} \\textbf{c} \\underline{d} \\textsuperscript{e}\n",
			[]string{"<em>a</em>", "<em>b</em>", "<strong>c</strong>", "<u>d</u>", "<sup>e</sup>"},
			nil,
		},
		{
			"math is code",
			"With $x^2$ and \\(y\\) and \\[ z \\] and $$w$$.\n\n\\begin{align*}\n a &= b\n\\end{align*}\n",
			[]string{"<code>x^2</code>", "<code>y</code>", "<code> z </code>", "<code>w</code>", "<code>\n a &amp;= b\n</code>"},
			nil,
		},
		{
			"a dollar sign nothing closes is text",
			"It costs $5 today.\n\nAnd \\$10 tomorrow.\n",
			[]string{"<p>It costs $5 today.</p>", "And $10 tomorrow."},
			[]string{"<code>"},
		},
		{
			"keys are code",
			"See \\cite[p.~3]{key1,key2} and \\citep{key3} and \\ref{fig:a} and \\autoref{b}.\\label{sec}\n",
			[]string{"<code>key1,key2</code>", "<code>key3</code>", "<code>fig:a</code>", "<code>b</code>"},
			[]string{"p. 3", "sec"},
		},
		{
			"verbatim text is code",
			"Use \\verb|a_b| and \\texttt{c} and \\url{http://d}.\n\n\\begin{verbatim}\nhidden\n\\end{verbatim}\n\n\\begin{lstlisting}[language=C]\nhidden\n\\end{lstlisting}\n",
			[]string{"<code>a_b</code>", "<code>c</code>", "<code>http://d</code>", "<pre><code></code></pre>"},
			[]string{"hidden", "language"},
		},
		{
			"a footnote is a block after its paragraph",
			"One\\footnote{A note.} two.\n",
			[]string{"<p>One two.</p>\n<div class=\"footnote\"><p>A note.</p></div>"},
			nil,
		},
		{
			"lists hold items",
			"\\begin{itemize}\n\\item One\n\\item[x] Two\n\\end{itemize}\n\\begin{enumerate}\n\\item Three\n\\end{enumerate}\n",
			[]string{"<ul>", "<li><p>One\n</p>\n</li>", "<li><p>Two\n</p>\n</li>\n</ul>", "<ol>", "</ol>"},
			[]string{"x"},
		},
		{
			"a description's label is a term",
			"\\begin{description}\n\\item[Term] Meaning.\n\\end{description}\n",
			[]string{"<dl>", "<dt>Term</dt>", "<dd><p>Meaning.\n</p>\n</dd>\n</dl>"},
			nil,
		},
		{
			"a table's cells are prose",
			"\\begin{tabular}{l|r}\n\\hline\nOne & Two \\\\\n\\multicolumn{2}{c}{Three} \\\\\n\\end{tabular}\n",
			[]string{"<table>", "<td><p>One </p>\n</td><td><p>Two </p>\n</td></tr>", "Three", "</table>"},
			[]string{"l|r", "hline"},
		},
		{
			"a figure holds its caption",
			"\\begin{figure}[htbp]\n\\centering\n\\includegraphics[width=3cm]{file.png}\n\\caption[Short]{The caption.}\n\\end{figure}\n",
			[]string{"<figure>", "<figcaption>The caption.</figcaption>", "</figure>"},
			[]string{"htbp", "file", "3cm", "Short"},
		},
		{
			"an unknown environment is a classed block",
			"\\begin{theorem}[Name]\nThe body.\n\\end{theorem}\n\\begin{abstract}\nIn short.\n\\end{abstract}\n",
			[]string{`<div class="theorem">`, "<p>The body.\n</p>", `<div class="abstract">`},
			[]string{"Name"},
		},
		{
			"a quotation is a blockquote",
			"\\begin{quote}\nSaid.\n\\end{quote}\n",
			[]string{"<blockquote>\n<p>Said.\n</p>\n</blockquote>"},
			nil,
		},
		{
			"an unknown command is markup and its argument prose",
			"A \\unknown[opt]{kept} word, \\other{2pt} and \\rule{1cm}{2pt}.\n",
			[]string{"A kept word,  and ."},
			[]string{"opt", "2pt", "1cm"},
		},
		{
			"a link keeps its text",
			"A \\href{http://x.y}{linked} word and \\textcolor{blue}{colored} text.\n",
			[]string{`<a href="http://x.y">linked</a>`, "and colored text."},
			[]string{"blue"},
		},
		{
			"escapes, ties, quotes and dashes are the text they print",
			"50\\% of R\\&D~costs ``more'' or `less'---pages 1--2, \\LaTeX{} too\\ldots\n",
			[]string{"50% of R&amp;D costs “more” or ‘less'—pages 1–2, LaTeX too…"},
			nil,
		},
		{
			"accents are the letters they print",
			"caf\\'e, na\\\"{\\i}ve, \\c{c}a, G\\\"odel\n",
			[]string{"café, naïve, ça, Gödel"},
			nil,
		},
		{
			"an environment left open does not swallow the document",
			"\\begin{itemize}\n\\item One\n\nAfter $x and more.\n",
			[]string{"<li><p>One</p>", "<p>After $x and more.</p>", "</ul>"},
			nil,
		},
		{
			"definitions are markup",
			"\\newcommand{\\foo}[1]{body #1}\n\\def\\bar#1{body}\n\\setlength{\\parindent}{0pt}\nProse.\n",
			[]string{"Prose."},
			[]string{"body", "0pt"},
		},
	}

	for _, tc := range cases {
		got := texToHTML(tc.in)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: want %q in:\n%s", tc.name, want, got)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(got, absent) {
				t.Errorf("%s: want no %q in:\n%s", tc.name, absent, got)
			}
		}
	}
}

// TestTeXNeverPanics reads sources cut off at every byte: a document being
// typed is rarely well-formed.
func TestTeXNeverPanics(_ *testing.T) {
	src := "\\begin{document}\\section[a]{b} c \\emph{d} $e$ \\verb|f| \\'e \\\\[1pt]\n" +
		"\\begin{tabular}{l} g & h \\\\ \\end{tabular} \\cite[i]{j} \\footnote{k} % l\n" +
		"\\begin{equation} m \\end{equation} \\href{n}{o} \\[ p \\] \\item[q] ``r''\n"
	for i := 0; i <= len(src); i++ {
		texToHTML(src[:i])
		texToHTML(src[i:])
	}
}
