//go:build !vale_nocode

package code

import (
	"strings"
	"testing"
)

// covers reports whether some mask holds exactly the first occurrence of
// part in text.
func covers(masks []Mask, text, part string) bool {
	at := strings.Index(text, part)
	if at < 0 {
		return false
	}
	for _, m := range masks {
		if m[0] <= at && at+len(part) <= m[1] {
			return true
		}
	}
	return false
}

func checkMasks(t *testing.T, masks []Mask, text string, masked, prose []string) {
	t.Helper()
	for _, part := range masked {
		if !covers(masks, text, part) {
			t.Errorf("%q should be masked in %q; masks %v", part, text, masks)
		}
	}
	for _, part := range prose {
		if covers(masks, text, part) {
			t.Errorf("%q should be prose in %q; masks %v", part, text, masks)
		}
	}
}

func TestJavadocMasks(t *testing.T) {
	text := "Reads the record.\n\n<p>Call {@code foo(ctx)} before.\n@param name the identifier\n@throws IOError when it fails\n@return the payload\n"
	checkMasks(t, Javadoc(text), text,
		[]string{"{@code foo(ctx)}", "@param name", "@throws IOError", "@return"},
		[]string{"the identifier", "when it fails", "the payload", "Reads"})
}

func TestJSDocMasks(t *testing.T) {
	text := "Reads the record.\n\n@example\nconst value = read(x);\n\n@param {string} name - The identifier.\n@param {object} [opts] - Options.\n@returns {Promise<object>} The payload.\n@see {@link other}\n@deprecated use read2.\n"
	checkMasks(t, JSDoc(text), text,
		[]string{"@example", "const value = read(x);", "@param {string} name", "@param {object} [opts]",
			"@returns {Promise<object>}", "{@link other}", "@deprecated"},
		[]string{"The identifier", "Options", "The payload", "use read2", "Reads"})
}

func TestKDocMasks(t *testing.T) {
	text := "See [foo] and [a.b] and [text][ref] and [link](url) for the details.\n@param name the identifier\n@return the payload\n"
	checkMasks(t, KDoc(text), text,
		[]string{"[foo]", "[a.b]", "[ref]", "@param name", "@return"},
		[]string{"[text]", "[link]", "the details", "the identifier", "the payload"})
}

func TestRustDocMasks(t *testing.T) {
	text := "See [`Foo`] and [bar::baz] and [x](y) and [def]: url for the details.\n"
	checkMasks(t, RustDoc(text), text,
		[]string{"[`Foo`]", "[bar::baz]"},
		[]string{"[x]", "[def]", "the details"})
}

func TestGoDocMasks(t *testing.T) {
	text := "See [Name] and [pkg.Name] and [*T] for the details; [x]: is a definition.\n"
	checkMasks(t, GoDoc(text), text,
		[]string{"[Name]", "[pkg.Name]", "[*T]"},
		[]string{"[x]", "the details"})
}

func TestPyDocMasks(t *testing.T) {
	text := "Reads the record.\n\n:param name: the identifier\n:raises ValueError: when it fails\n:returns: the payload\n:rtype: dict\n"
	checkMasks(t, PyDoc(text), text,
		[]string{":param name:", ":raises ValueError:", ":returns:", ":rtype:"},
		[]string{"the identifier", "when it fails", "the payload", "Reads"})
}

func TestMaskedByLineAndSpan(t *testing.T) {
	// The mask covers "cdé" on the second line, in bytes 3..7.
	c := Comment{Text: "ab\ncdé f", Masks: []Mask{{3, 7}}}
	for _, tc := range []struct {
		line, from, to int
		want           bool
	}{
		{1, 1, 2, false}, {1, 2, 3, false}, {3, 1, 2, false},
		{2, 1, 2, true}, {2, 3, 4, true}, {2, 4, 5, false}, {2, 5, 6, false},
		// A match inside the mask is dropped; one that runs past it is prose.
		{2, 1, 4, true}, {2, 1, 5, false}, {2, 2, 6, false},
	} {
		if got := c.Masked(tc.line, tc.from, tc.to); got != tc.want {
			t.Errorf("Masked(%d, %d, %d) = %v, want %v", tc.line, tc.from, tc.to, got, tc.want)
		}
	}
}

func TestGoCommentsMaskTheDeclaredName(t *testing.T) {
	src := "//go:build linux\n\n// Package foo does x.\npackage foo\n\n// Bar reads.\n// More.\nfunc Bar() {}\n\n// Unrelated text.\n\ntype T struct {\n\t// Name is the name.\n\tName string\n}\n"
	comments, err := GetComments([]byte(src), Go())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range comments {
		lead := ""
		for _, m := range c.Masks {
			if m[0] == 0 {
				lead = c.Text[:m[1]]
			}
		}
		got = append(got, lead)
	}
	want := []string{"Package foo", "Bar", "", "Name"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("leading masks = %q, want %q", got, want)
	}
}

func TestDirectivesAreDropped(t *testing.T) {
	src := "# noqa: E501\n# type: ignore\n# A real comment.\ndef f():  # pylint: disable=foo\n    return 1  # fmt: skip\n"
	comments, err := GetComments([]byte(src), Python())
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Text != "A real comment." {
		t.Errorf("got %d comments %+v, want the one real comment", len(comments), comments)
	}
}

func TestTOMLComments(t *testing.T) {
	src := "# A top-level comment.\ntitle = \"not # a comment\" # A trailing one.\n\n[section]\n# Another.\nkey = 1\n"
	comments, err := GetComments([]byte(src), TOML())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range comments {
		got = append(got, c.Text)
	}
	want := "A top-level comment.|A trailing one.|Another."
	if strings.Join(got, "|") != want {
		t.Errorf("comments = %q, want %q", got, want)
	}
}
