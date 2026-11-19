package lint

import (
	"strings"
	"testing"
)

func TestMarkSubstitutions(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`<p>Open the |rsp-at| today.</p>`, `<p>Open the <span class="problematic">|rsp-at|</span> today.</p>`},
		{`<p>Follow |rsp|_ there.</p>`, `<p>Follow <span class="problematic">|rsp|_</span> there.</p>`},
		{`<p>Keep |a b| too.</p>`, `<p>Keep <span class="problematic">|a b|</span> too.</p>`},
		{`<p>Not | a pipe | pair.</p>`, `<p>Not | a pipe | pair.</p>`},
		{`<p>Run <tt class="docutils literal">a|b|c</tt> as is.</p>`, `<p>Run <tt class="docutils literal">a|b|c</tt> as is.</p>`},
		{"<pre>x |y| z</pre><p>|w|</p>", `<pre>x |y| z</pre><p><span class="problematic">|w|</span></p>`},
	} {
		if got := markSubstitutions(tc.in); got != tc.want {
			t.Errorf("markSubstitutions(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
		}
	}
}

// Sphinx's directives and roles are read as prose or code the way the
// `[sphinx]` section says, on top of the built-in lists: an unknown
// directive's body is prose unless it holds code, and an unknown role's text
// is code unless it names a reference or an interface element.
func TestRSTSphinxFallbacks(t *testing.T) {
	l, err := initLinter()
	if err != nil {
		t.Fatal(err)
	}
	l.Manager.Config.Sphinx = map[string]string{
		"CodeDirectives": "mermaid",
		"ProseRoles":     "kbd",
	}

	html, err := l.convertRST(strings.Join([]string{
		".. versionadded:: 1.2",
		"   Added the thing.",
		"",
		".. mermaid::",
		"",
		"   graph LR",
		"",
		".. autoclass:: pkg.Thing",
		"",
		"See :ref:`The guide <guide>` and :ref:`guide`, press :kbd:`Enter`, call :py:func:`open`.",
		"",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Added the thing.",
		`<pre class="literal-block">`,
		"See The guide and <tt class=\"docutils literal\">guide</tt>, press Enter, call <tt class=\"docutils literal\">open</tt>.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	for _, absent := range []string{"system-message", "graph LR</p>", "pkg.Thing</p>"} {
		if strings.Contains(html, absent) {
			t.Errorf("found %q in:\n%s", absent, html)
		}
	}
}
