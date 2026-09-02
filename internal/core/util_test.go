package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/system"
)

func TestFormatFromExt(t *testing.T) {
	extToFormat := map[string][]string{
		".py":    {".py", "code"},
		".cxx":   {".cpp", "code"},
		".mdown": {".md", "markup"},
		".Rmd":   {".md", "markup"},
		".rmd":   {".md", "markup"},
		".R":     {".r", "code"},
		".qml":   {".qml", "code"},
	}
	m := map[string]string{}
	for ext, format := range extToFormat {
		normExt, f := FormatFromExt(ext, m)
		if format[0] != normExt {
			t.Errorf("expected = %v, got = %v", format[0], normExt)
		}
		if format[1] != f {
			t.Errorf("expected = %v, got = %v", format[1], f)
		}
	}

	mapped := map[string]string{"cpp": "qdoc", "qml": "qdoc"}
	for _, ext := range []string{".cpp", ".qml"} {
		normExt, f := FormatFromExt(ext, mapped)
		if normExt != ".qdoc" || f != "fragment" {
			t.Errorf("expected = [.qdoc fragment], got = [%v %v]", normExt, f)
		}
	}
}

// TextToContext is the production caller of nlp.TextToTokens (reached from
// the `tag` CLI command, cmd/vale/command.go's runTag) -- it used to panic
// when a configured remote endpoint's /tag request failed, crashing the
// whole vale process instead of letting runTag's already-existing `if err
// != nil { return err }` handling report it normally, the same way
// Info.Compute's fix reused lintProse's own pre-existing error handling.
// This confirms TextToContext now returns a clean error instead.
func TestTextToContextReturnsErrorOnTagEndpointFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"Tokens":[]}`))
	}))
	defer server.Close()

	var out []nlp.TaggedWord
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("TextToContext panicked instead of returning an error: %v", p)
			}
		}()
		out, err = TextToContext("some text", &nlp.Info{Lang: "id", Endpoint: server.URL})
	}()

	if err == nil {
		t.Fatalf("TextToContext returned a nil error for a failed /tag request, want a non-nil error")
	}
	if out != nil {
		t.Errorf("context = %v, want nil alongside the error", out)
	}
}

func TestPrepText(t *testing.T) {
	rawToPrepped := map[string]string{
		"foo\r\nbar":     "foo\nbar",
		"foo\r\n\r\nbar": "foo\n\nbar",
	}
	for raw, prepped := range rawToPrepped {
		if prepped != Sanitize(raw) {
			t.Errorf("expected = %v, got = %v", prepped, Sanitize(raw))
		}
	}
}

func TestFindShifts(t *testing.T) {
	if got := findShifts("no entities here\n"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	got := findShifts("a&rsquo;b\nplain\nx &rsquo; y&rsquo;\n")
	want := map[int][]int{1: {2}, 3: {3, 6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestMapAlertsToSource(t *testing.T) {
	f := File{
		sanShifts: map[int][]int{1: {15}, 2: {6}},
		Alerts: []Alert{
			// The rewrite sits inside the match: the span widens.
			{Line: 1, Span: []int{13, 16}},
			// The rewrite precedes the match: the span shifts.
			{Line: 2, Span: []int{11, 14}},
			// A line with no rewrites is untouched.
			{Line: 3, Span: []int{2, 5}},
		},
	}
	f.MapAlertsToSource()

	want := [][]int{{13, 22}, {17, 20}, {2, 5}}
	for i, a := range f.Alerts {
		if !reflect.DeepEqual(a.Span, want[i]) {
			t.Errorf("alert %d: expected %v, got %v", i, want[i], a.Span)
		}
	}
}

func TestPhrase(t *testing.T) {
	rawToPrepped := map[string]bool{
		"test suite":               true,
		"test[ ]?suite":            false,
		"Google":                   true,
		"write-good":               true,
		"https://vale.sh/explorer": false,
		"Google.zip":               false,
	}
	for input, output := range rawToPrepped {
		result := IsPhrase(input)
		if result != output {
			t.Errorf("expected = %v, got = %v", output, result)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Log("os.UserHomeDir failed, will not proceed with tests")
		return
	}
	stylesPathInput := filepath.FromSlash("~/.vale")
	expectedOutput := filepath.Join(homedir, ".vale")
	result := system.NormalizePath(stylesPathInput)
	if result != expectedOutput {
		t.Errorf("expected = %v, got = %v", expectedOutput, result)
	}
	stylesPathInput, err = os.MkdirTemp("", "vale_test")
	if err != nil {
		t.Log("os.MkdirTemp failed, will not proceed with tests")
		return
	}
	expectedOutput = stylesPathInput
	result = system.NormalizePath(stylesPathInput)
	if result != expectedOutput {
		t.Errorf("expected = %v, got = %v", expectedOutput, result)
	}
	stylesPathInput, err = os.MkdirTemp("", "vale~test")
	if err != nil {
		t.Log("os.MkdirTemp failed in second case, will not proceed with tests")
		return
	}
	expectedOutput = stylesPathInput
	result = system.NormalizePath(stylesPathInput)
	if result != expectedOutput {
		t.Errorf("expected = %v, got = %v", expectedOutput, result)
	}
}

func TestShouldIgnoreDirectory(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "empty directory name",
			path:     "",
			expected: false,
		},
		// Direct directory names
		{
			name:     "direct node_modules",
			path:     "node_modules",
			expected: true,
		},
		{
			name:     "direct .git",
			path:     ".git",
			expected: true,
		},
		// Nested paths with ignored directories
		{
			name:     "nested node_modules",
			path:     "plugins/foo/node_modules",
			expected: true,
		},
		{
			name:     "nested .git in worktree",
			path:     "worktree-a/.git",
			expected: true,
		},
		{
			name:     "deeply nested node_modules",
			path:     "project/src/components/node_modules",
			expected: true,
		},
		{
			name:     "node_modules in path with backslashes",
			path:     filepath.Join("project", "src", "node_modules"),
			expected: true,
		},
		// Non-ignored directories
		{
			name:     "regular directory",
			path:     "src",
			expected: false,
		},
		{
			name:     "nested regular directory",
			path:     "plugins/foo",
			expected: false,
		},
		{
			name:     "directory containing node_modules in name",
			path:     "my_node_modules_backup",
			expected: false,
		},
		{
			name:     "directory containing .git in name",
			path:     "my.github",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldIgnoreDirectory(tt.path)
			if result != tt.expected {
				t.Errorf("ShouldIgnoreDirectory(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestStyleName(t *testing.T) {
	tests := map[string]string{
		"proselint.Typography": "proselint",
		"proselint":            "proselint",
		"Vale.Spelling":        "Vale",
		// A consistency check reports under a third part; the style is still
		// the first one.
		"demo.Consistency.Smart": "demo",
		"":                       "",
	}

	for in, want := range tests {
		if got := StyleName(in); got != want {
			t.Errorf("StyleName(%q) = %q, want %q", in, got, want)
		}
	}
}

// CheckName is what makes a subdirectory part of a rule's identity: the tree
// under the style root joins the name, and the file's base keeps its
// historical first-dot reading.
func TestCheckName(t *testing.T) {
	cases := []struct {
		root, path, want string
	}{
		{"styles/Std", "styles/Std/OxfordComma.yml", "Std.OxfordComma"},
		{"styles/Std", "styles/Std/dates/TimeFormat.yml", "Std.dates.TimeFormat"},
		{"styles/Std", "styles/Std/a/b/Deep.yml", "Std.a.b.Deep"},
		{"styles/Std", "styles/Std/Terms.custom.yml", "Std.Terms"},
	}

	if _, err := CheckName("styles/Std", "styles/Std/Weird[max].yml"); err == nil {
		t.Error("a bracketed rule name must be rejected")
	}

	for _, tt := range cases {
		got, err := CheckName(tt.root, tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("CheckName(%q, %q) = %q; want %q", tt.root, tt.path, got, tt.want)
		}
	}
}
