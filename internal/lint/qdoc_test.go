package lint

import (
	"strings"
	"testing"
)

// TestQdocHTML pins what the QDoc converter makes of each construct: prose
// is kept word-for-word, commands become the elements they mean, and topic
// or meta commands are markup with nothing to lint. See #784.
func TestQdocHTML(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   []string
		absent []string
	}{
		{
			"comment delimiters are markup",
			"/*!\n    Prose here.\n*/\n",
			[]string{"<p>    Prose here.</p>"},
			[]string{"/*!", "*/"},
		},
		{
			"title and sections are headings",
			"\\title The Page Title\n\n\\section1 First Level\n\n\\section2 Second Level\n",
			[]string{"<h1>The Page Title</h1>", "<h2>First Level</h2>", "<h3>Second Level</h3>"},
			nil,
		},
		{
			"topic commands are markup",
			"\\page overview.html\n\\class QWidget\n\\fn void QWidget::show()\n\nProse here.\n",
			[]string{"<p>Prose here.</p>"},
			[]string{"overview", "QWidget", "show"},
		},
		{
			"a wrapped signature is markup to its closing parenthesis",
			"\\fn void Foo::arcTo(qreal x, qreal y, qreal width, qreal\n    height, qreal startAngle)\n    Sets the arc.\n",
			[]string{"<p>    Sets the arc.</p>"},
			[]string{"qreal", "startAngle", "height"},
		},
		{
			"a signature missing its parenthesis ends at a blank line",
			"\\fn void Foo::bar(qreal x\n\nProse here.\n",
			[]string{"<p>Prose here.</p>"},
			[]string{"qreal"},
		},
		{
			"brief and note are classed paragraphs",
			"\\brief One brief sentence.\n\n\\note A note sentence.\n",
			[]string{
				`<div class="brief">`, "One brief sentence.",
				`<div class="note">`, "A note sentence.",
			},
			nil,
		},
		{
			"code runs to endcode and is skipped",
			"\\code\n    int x = 0;\n\\endcode\n\nProse here.\n",
			[]string{"<pre><code>", "<p>Prose here.</p>"},
			[]string{"int x"},
		},
		{
			"omit drops its content",
			"Before.\n\n\\omit\nHidden prose.\n\\endomit\n\nAfter.\n",
			[]string{"<p>Before.</p>", "<p>After.</p>"},
			[]string{"Hidden"},
		},
		{
			"c and a are code spans",
			"Use the \\c QWidget class with the \\a parent argument.\n",
			[]string{"<code>QWidget</code>", "<code>parent</code>"},
			[]string{"\\c", "\\a"},
		},
		{
			"b and e keep their text",
			"See \\b {bold words} and \\e {emphasized words}.\n",
			[]string{"<strong>bold words</strong>", "<em>emphasized words</em>"},
			[]string{"\\b", "\\e", "{"},
		},
		{
			"a bare link keeps its target as text",
			"See \\l QWidget for details.\n",
			[]string{`<a href="#">QWidget</a>`},
			[]string{"\\l"},
		},
		{
			"a two-part link keeps its text, not its target",
			"See \\l {target-page.html} {the linked text} for details.\n",
			[]string{`<a href="#">the linked text</a>`},
			[]string{"target-page"},
		},
		{
			"lists become lists",
			"\\list\n    \\li First item text.\n    \\li Second item text.\n\\endlist\n",
			[]string{"<ul>", "<li>First item text.", "<li>Second item text.", "</ul>"},
			[]string{"\\li"},
		},
		{
			"tables have header and body cells",
			"\\table\n    \\header\n        \\li Name\n        \\li Meaning\n    \\row\n        \\li \\c Value\n        \\li Cell prose.\n\\endtable\n",
			[]string{"<table>", "<th>Name", "<th>Meaning", "<td><code>Value</code>", "<td>Cell prose."},
			[]string{"\\li"},
		},
		{
			"a value's constant is code, its description prose",
			"\\value AlignLeft Aligns with the left edge.\n",
			[]string{"<code>AlignLeft</code>", "Aligns with the left edge."},
			[]string{"\\value"},
		},
		{
			"an image's caption is prose, its file name metadata",
			"\\image diagram.png A caption sentence.\n",
			[]string{`<data class="image">diagram.png</data>`,
				"<figcaption>A caption sentence.</figcaption>"},
			nil,
		},
		{
			"an anchor's name is metadata",
			"\\target some-anchor\n\\keyword {Another Anchor}\n\nProse here.\n",
			[]string{`<data class="anchor">some-anchor</data>`,
				`<data class="anchor">Another Anchor</data>`, "<p>Prose here.</p>"},
			[]string{"\\target", "\\keyword"},
		},
		{
			"a snippet marker is markup, and does not break its paragraph",
			"/*!\n    Prose one.\n//! [intro]\n    Prose two.\n*/\n",
			[]string{"Prose one.", "Prose two."},
			[]string{"intro", "//!"},
		},
		{
			"prose about markup stays prose",
			"The file must be included using a <script> tag.\n\nProse after it.\n",
			[]string{"using a &lt;script&gt; tag.", "<p>Prose after it.</p>"},
			nil,
		},
		{
			"an HTML comment survives escaping",
			"A <!-- vale off --> toggle and a <script> tag.\n",
			[]string{"<!-- vale off -->", "&lt;script&gt;"},
			[]string{"&lt;!--"},
		},
		{
			"sa lines are markup",
			"Prose here.\n\n\\sa QWidget, QObject\n",
			[]string{"<p>Prose here.</p>"},
			[]string{"QWidget"},
		},
		{
			"an unknown inline command is masked, its text kept",
			"The \\unknowncmd rest of the sentence.\n",
			[]string{"The  rest of the sentence."},
			[]string{"unknowncmd"},
		},
		{
			"a command may share the opening delimiter's line",
			"/*! \\page overview.html\n    \\title The Page Title\n*/\n",
			[]string{"<h1>The Page Title</h1>"},
			[]string{"/*!", "*/", "overview"},
		},
		{
			"prose may share the opening delimiter's line",
			"/*! One brief sentence. */\n",
			[]string{"One brief sentence."},
			[]string{"/*!", "*/"},
		},
		{
			"adjacent comment blocks are separate paragraphs",
			"/*!\n    First block.\n*/\n\n/*!\n    Second block.\n*/\n",
			[]string{"<p>    First block.</p>", "<p>    Second block.</p>"},
			nil,
		},
		{
			"build-system and type commands are markup",
			"\\qmlenum Mode\n\\qmlsingletontype Theme\n\\typealias Alias\n" +
				"\\nativetype QString\n\\cmakepackage Qt6\n\\cmakecomponent Widgets\n" +
				"\\cmaketargetitem Target\n\\qtcmakepackage Core\n" +
				"\\qtcmaketargetitem Item\n\\qtvariable widgets\n" +
				"\\tableofcontents auto\n\\dontdocument (Hidden)\n\nProse here.\n",
			[]string{"<p>Prose here.</p>"},
			[]string{"Mode", "Theme", "Alias", "QString", "Qt6", "Widgets",
				"Target", "Core", "Item", "widgets", "auto", "Hidden"},
		},
		{
			"a comparison block's command lines are markup",
			"\\compareswith equality QAnyStringView\n\\endcompareswith\n\nProse here.\n",
			[]string{"<p>Prose here.</p>"},
			[]string{"QAnyStringView"},
		},
		{
			"a conditional's expression is markup, its branches prose",
			"\\if defined(onlinedocs)\nOnline prose.\n\\else\nOffline prose.\n\\endif\n",
			[]string{"Online prose.", "Offline prose."},
			[]string{"onlinedocs"},
		},
		{
			"an escaped backslash is text, not a command",
			"Write \\\\c to show the command.\n",
			[]string{`Write \c to show the command.`},
			[]string{"<code>"},
		},
		{
			"a unicode code point is markup",
			"A bullet \\unicode 0x2022 sits here.\n",
			[]string{"A bullet", "sits here."},
			[]string{"0x2022"},
		},
		{
			"a span's class scopes its text, which is prose",
			"See \\span {class=\"vrheader\"} {the spanned words} here.\n",
			[]string{`<span class="vrheader">the spanned words</span>`, "here."},
			nil,
		},
		{
			"a div's class scopes the prose it holds",
			"\\div {class=\"note\"}\nProse inside.\n\\enddiv\n",
			[]string{`<div class="note">`, "Prose inside.", "</div>"},
			[]string{"class=\"class"},
		},
		{
			"details keeps its summary as prose",
			"\\details {A summary sentence.}\nBody prose.\n\\enddetails\n",
			[]string{`<div class="details">`, `<p class="summary">A summary sentence.</p>`,
				"Body prose.", "</div>"},
			nil,
		},
		{
			"a subtitle is a heading",
			"\\title The Title\n\\subtitle The Subtitle\n",
			[]string{"<h1>The Title</h1>", `<h2 class="subtitle">The Subtitle</h2>`},
			nil,
		},
		{
			"a deprecation's version is markup, its advice prose",
			"\\deprecated [6.5] Use the replacement instead.\n",
			[]string{"Use the replacement instead."},
			[]string{"6.5"},
		},
		{
			"a bare deprecation says nothing",
			"\\deprecated\n\nProse here.\n",
			[]string{"<p>Prose here.</p>"},
			[]string{"deprecated"},
		},
		{
			"a cell span is markup, the cell's text prose",
			"\\table\n    \\row\n        \\li {2,1} Spanned cell prose.\n\\endtable\n",
			[]string{"<td>Spanned cell prose."},
			[]string{"2,1"},
		},
		{
			"deprecated inline aliases keep their text",
			"See \\bold {bold words} and \\i {italic words}.\n",
			[]string{"<strong>bold words</strong>", "<em>italic words</em>"},
			[]string{"\\bold", "\\i"},
		},
		{
			"a no-argument command keeps the words after it",
			"Qt \\tm is a trademark, and \\br a line break.\n",
			[]string{"is a trademark", "a line break."},
			[]string{"\\tm", "\\br"},
		},
		{
			"modifier and metadata commands are markup",
			"\\abstract\n\\readonly\n\\required\n\\preliminary\n\\qmlabstract\n" +
				"\\qmldefault\n\\qmlenumeratorsfrom Mode\n\\omitvalue Hidden\n" +
				"\\modulestate {Technical Preview}\n\\notranslate\n\\compares strong\n" +
				"\\default true\n\\inheaderfile QtCore\n\\attribution Upstream\n" +
				"\\toc\n\\tocentry Entry\n\nProse here.\n",
			[]string{"<p>Prose here.</p>"},
			[]string{"Mode", "Hidden", "Technical", "strong", "true", "QtCore",
				"Upstream", "Entry"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := qdocFragmentToHTML(c.in)

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

// TestQdocVerbatimClose pins how a non-prose block ends. The code variants all
// close on \endcode, and none of them may run past the comment that holds it.
func TestQdocVerbatimClose(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"badcode closes on endcode", "\\badcode\n    int x = FIXME;\n\\endcode\n\nProse here.\n"},
		{"oldcode runs through newcode", "\\oldcode\n    FIXME old;\n\\newcode\n    FIXME new;\n\\endcode\n\nProse here.\n"},
		{"a symmetrical end is accepted too", "\\badcode\n    int x = FIXME;\n\\endbadcode\n\nProse here.\n"},
		{"an unterminated block ends with the comment",
			"/*!\n\\code\n    int x = FIXME;\n*/\n\n/*!\nProse here.\n*/\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := qdocFragmentToHTML(c.in)
			if !strings.Contains(html, "<p>Prose here.</p>") {
				t.Errorf("prose did not survive the block:\n%s", html)
			}
			if strings.Contains(html, "FIXME") {
				t.Errorf("code leaked into prose:\n%s", html)
			}
		})
	}
}

// TestQdocWholeFile pins that a `.qdoc` file is a file of `/*! ... */`
// comments: a licence header, a `//! [name]` snippet body, and anything else
// between them is code, however much it reads like prose.
func TestQdocWholeFile(t *testing.T) {
	const src = `// Copyright (C) 2016 The Qt Company Ltd.
// SPDX-License-Identifier: LicenseRef-Qt-Commercial

//! [15]
md build_shared
cd build_shared
//! [15]

/*!
    \title A Real Heading
    Real prose here.
*/

Trailing text outside any comment.
`

	html := qdocToHTML(src)
	for _, want := range []string{"<h1>A Real Heading</h1>", "Real prose here."} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	for _, absent := range []string{"Copyright", "SPDX", "build_shared", "Trailing"} {
		if strings.Contains(html, absent) {
			t.Errorf("content outside a comment was kept (%q):\n%s", absent, html)
		}
	}
}
