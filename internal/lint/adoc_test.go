package lint

import (
	"strings"
	"testing"
)

// A cross-reference without authored text renders as an empty link, whether
// or not it resolves; authored text is kept. See #1224 for the host-side
// converter that does the same.
func TestADocXrefKeepsOnlyAuthoredText(t *testing.T) {
	html, err := convertADoc("= T\n\nSee <<zorbblat>> and xref:other.adoc#x[] and xref:x[kept].\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The path stays in the href; it's the link text that must be gone.
	for _, generated := range []string{">[zorbblat]</a>", ">other.html</a>"} {
		if strings.Contains(html, generated) {
			t.Errorf("generated text %q survived:\n%s", generated, html)
		}
	}
	if !strings.Contains(html, ">kept</a>") {
		t.Errorf("authored text was dropped:\n%s", html)
	}
}

// Vale's own attributes hold until the configuration's `[asciidoctor]`
// section speaks, read the way the CLI read `-a`: YES sets, NO unsets, and
// the configuration's word is last.
func TestADocOptions(t *testing.T) {
	got := adocOptions(map[string]string{"experimental": "YES", "sectnums": "YES", "icons": "font", "toc": "NO"})
	for k, want := range map[string]string{
		"notitle!":          "",
		"attribute-missing": "drop",
		"experimental":      "",
		"sectnums":          "",
		"icons":             "font",
		"toc!":              "",
	} {
		if v, ok := got[k]; !ok || v != want {
			t.Errorf("attribute %q = %q, %t; want %q", k, v, ok, want)
		}
	}
	if _, ok := got["sectnums!"]; ok {
		t.Error("the configuration's sectnums = YES left Vale's sectnums! in place")
	}

	doc := "= T\n\n== One\n\nText.\n"
	html, err := convertADoc(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "1. One") {
		t.Errorf("section numbers are off by default:\n%s", html)
	}
	html, err = convertADoc(doc, map[string]string{"sectnums": "YES"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "1. One") {
		t.Errorf("the configuration's sectnums = YES should turn them on:\n%s", html)
	}
}
