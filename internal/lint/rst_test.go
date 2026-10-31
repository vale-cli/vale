package lint

import "testing"

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
