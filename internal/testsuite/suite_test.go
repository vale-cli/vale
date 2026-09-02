package testsuite

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestExtDefaultsToMarkdown(t *testing.T) {
	for _, tt := range []struct{ format, want string }{
		{"", ".md"},
		{"rst", ".rst"},
		{".rst", ".rst"},
	} {
		if got := (Case{Format: tt.format}).Ext(); got != tt.want {
			t.Errorf("Ext(%q) = %q; want %q", tt.format, got, tt.want)
		}
	}
}

// A case that asserts nothing passes whatever the rule does, which reads as
// coverage while testing nothing. It is a broken file, not a failing case.
func TestLoadRejectsUnusableCases(t *testing.T) {
	tests := []struct {
		name, body, wants string
	}{
		{
			name:  "no assertion",
			body:  "- name: a case\n  input: Some prose.\n",
			wants: "needs one of",
		},
		{
			name:  "no name",
			body:  "- input: Some prose.\n  contains: X\n",
			wants: "needs a name",
		},
		{
			name: "duplicate name",
			body: "- name: same\n  contains: X\n" +
				"- name: same\n  contains: Y\n",
			wants: "duplicate name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(t.TempDir(), "x.test.yml"), tt.body)

			_, err := Load(path)
			if err == nil {
				t.Fatal("no error")
			} else if !strings.Contains(err.Error(), tt.wants) {
				t.Errorf("error = %q; want it to mention %q", err, tt.wants)
			}
		})
	}
}

// An empty `want` says "no alerts at all", which is a different assertion from
// not writing one. Only a pointer can tell the two apart.
func TestLoadKeepsAnEmptyWant(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "x.test.yml"),
		"- name: silent\n  input: Fine prose.\n  want: \"\"\n")

	cases, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cases[0].Want == nil {
		t.Fatal("an empty want was read as unset")
	}
	if *cases[0].Want != "" {
		t.Errorf("want = %q; want empty", *cases[0].Want)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()

	rule := writeFile(t, filepath.Join(root, "styles", "S", "R.yml"), "extends: existence\n")
	beside := writeFile(t, filepath.Join(root, "styles", "S", "R.test.yml"), "[]\n")
	grouped := writeFile(t, filepath.Join(root, "tests", "config.test.yml"), "[]\n")

	// `.yml` is the spelling to document, but a configuration written the
	// other way is still somebody's configuration.
	spelled := writeFile(t, filepath.Join(root, "tests", "other.test.yaml"), "[]\n")

	writeFile(t, filepath.Join(root, ".git", "hidden.test.yml"), "[]\n")

	found, err := Find([]string{root})
	if err != nil {
		t.Fatal(err)
	}

	// A rule is a candidate now too -- it may carry in-source cases, and
	// Load, not Find, is what decides. Only the dot directory stays out.
	for _, want := range []string{beside, grouped, spelled, rule} {
		if !slices.Contains(found, want) {
			t.Errorf("did not find %s", want)
		}
	}
	if len(found) != 4 {
		t.Errorf("found %v; a dot directory should have been skipped", found)
	}

	// A file names itself, whatever it is called.
	one, err := Find([]string{rule})
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0] != rule {
		t.Errorf("Find(%q) = %v", rule, one)
	}
}

func TestCompare(t *testing.T) {
	want := func(s string) *string { return &s }

	tests := []struct {
		name   string
		c      Case
		got    string
		passes bool
	}{
		{"exact match", Case{Want: want("1:4:A.B:msg")}, "1:4:A.B:msg\n", true},
		{"exact mismatch", Case{Want: want("1:4:A.B:msg")}, "1:5:A.B:msg\n", false},
		{"empty want, silent", Case{Want: want("")}, "", true},
		{"empty want, noisy", Case{Want: want("")}, "1:4:A.B:msg\n", false},
		{"contains", Case{Contains: "A.B"}, "1:4:A.B:msg\n", true},
		{"contains, missing", Case{Contains: "A.C"}, "1:4:A.B:msg\n", false},
		{"absent", Case{Absent: []string{"A.C"}}, "1:4:A.B:msg\n", true},
		{"absent, present", Case{Absent: []string{"A.B"}}, "1:4:A.B:msg\n", false},
		{
			"every assertion has to hold",
			Case{Contains: "A.B", Absent: []string{"A.C"}},
			"1:4:A.B:msg\n1:9:A.C:msg\n",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := compare(tt.c, tt.got)
			if passed := reason == ""; passed != tt.passes {
				t.Errorf("compare() = %q; passed = %v, want %v", reason, passed, tt.passes)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	flatten := func(diff []Line) string {
		var b strings.Builder
		for _, line := range diff {
			fmt.Fprintf(&b, "%c %s\n", line.Op, line.Text)
		}
		return b.String()
	}

	tests := []struct {
		name, want, got, rendered string
	}{
		{
			name: "a line changed",
			want: "a\nb\nc", got: "a\nx\nc",
			rendered: "  a\n- b\n+ x\n  c\n",
		},
		{
			name: "nothing on either side",
			want: "", got: "",
			rendered: "",
		},
		{
			// The runner's output ends in a newline and a `want:` block does
			// not. Split naively, that difference is a line of its own.
			name: "a trailing newline is not a line",
			want: "a", got: "a\n",
			rendered: "  a\n",
		},
		{
			name: "silence against an alert",
			want: "", got: "1:4:A.B:msg",
			rendered: "+ 1:4:A.B:msg\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flatten(Diff(tt.want, tt.got)); got != tt.rendered {
				t.Errorf("Diff() =\n%q\nwant\n%q", got, tt.rendered)
			}
		})
	}
}

// A rule file with a `tests:` sequence is its own test file -- the doctest
// reading -- and each case defaults to isolating the rule it lives in.
func TestLoadInSourceCases(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "Ellipses.yml"), `extends: existence
message: "Ellipsis."
tokens: ['\.\.\.']
tests:
  - name: fires
    input: "Wait... done."
    contains: Ellipsis
  - name: clean
    input: "Wait. Done."
    want: ""
`)

	cases, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases; want 2", len(cases))
	}
	for _, c := range cases {
		if c.Rule != "Ellipses.yml" {
			t.Errorf("case %q: rule = %q; want the file itself", c.Name, c.Rule)
		}
		if c.Path != path {
			t.Errorf("case %q: path = %q; want %q", c.Name, c.Path, path)
		}
	}
}

// YAML that is neither a case file nor a rule with cases is silently not
// ours: `vale test` walks whole repositories, and a workflow file or a rule
// without a `tests` key must not read as an error or as coverage.
func TestLoadSkipsForeignYAML(t *testing.T) {
	dir := t.TempDir()

	for name, body := range map[string]string{
		"workflow.yml": "on: [push]\njobs:\n  build:\n    runs-on: ubuntu-latest\n",
		"rule.yml":     "extends: existence\nmessage: 'x'\ntokens: ['y']\n",
		"list.yml":     "- one\n- two\n",
	} {
		path := writeFile(t, filepath.Join(dir, name), body)
		cases, err := Load(path)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
		if len(cases) != 0 {
			t.Errorf("%s: got %d cases; want none", name, len(cases))
		}
	}
}

// In-source cases get the same strictness as a sidecar: one that asserts
// nothing is a broken file, not quiet coverage.
func TestLoadInSourceRejectsUnusableCases(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "Rule.yml"), `extends: existence
message: "x"
tokens: ['y']
tests:
  - name: no assertion
    input: "Some prose."
`)

	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "needs one of") {
		t.Fatalf("got %v; want the no-assertion error", err)
	}
}

// The name an isolated rule reports must match the one a project run gives
// it, or a case's `want` changes depending on how it was invoked. Under a
// search path that name includes any subdirectories; outside one, the parent
// directory is the style, as before.
func TestIsolatedName(t *testing.T) {
	sp := t.TempDir()
	nested := filepath.Join(sp, "Std", "dates", "TimeFormat.yml")
	flat := filepath.Join(sp, "Std", "OxfordComma.yml")
	loose := filepath.Join(t.TempDir(), "House", "Length.yml")

	cases := []struct {
		path   string
		search []string
		want   string
	}{
		{nested, []string{sp}, "Std.dates.TimeFormat"},
		{flat, []string{sp}, "Std.OxfordComma"},
		{nested, nil, "dates.TimeFormat"},
		{loose, []string{sp}, "House.Length"},
	}

	for _, tt := range cases {
		if got := isolatedName(tt.path, tt.search); got != tt.want {
			t.Errorf("isolatedName(%q, %v) = %q; want %q", tt.path, tt.search, got, tt.want)
		}
	}
}
