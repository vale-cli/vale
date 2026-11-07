package lint

import (
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/system"
)

// The executable path loads the same converter the server does: a
// cross-reference without authored text renders as an empty link.
func TestAdocConverterFile(t *testing.T) {
	exe := system.Which([]string{"asciidoctor"})
	if exe == "" {
		t.Skip("asciidoctor not installed")
	}
	conv := adocConverterFile(adocIsJS(exe))
	if conv == "" {
		t.Fatal("no converter file was written")
	}

	args := append([]string{}, adocArgs...)
	args = append(args, "-r", conv, "--safe-mode", "secure", "-")
	html, err := system.ExecuteWithInput(exe,
		"= T\n\nSee <<zorbblat>> and xref:other.adoc#x[] and xref:x[kept].\n", args...)
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

// Asciidoctor.js names itself in its version line, and gets the converter
// as a module its CLI can require; the Ruby gem gets the Ruby one. See
// #1224.
func TestIsAsciidoctorJS(t *testing.T) {
	for out, want := range map[string]bool{
		"Asciidoctor.js 3.0.4 (Asciidoctor 2.0.20) [https://asciidoctor.org]\nRuntime Environment (node v22.22.3 on darwin)": true,
		"Asciidoctor 2.0.18 [https://asciidoctor.org]\nRuntime Environment (ruby 3.2.2 [arm64-darwin23])":                    false,
		"": false,
	} {
		if got := isAsciidoctorJS(out); got != want {
			t.Errorf("isAsciidoctorJS(%q) = %t, want %t", out, got, want)
		}
	}

	for _, want := range []string{
		`require.resolve("@asciidoctor/core"`,
		"class ValeHtml5Converter",
		`asciidoctor.ConverterFactory.register(new ValeHtml5Converter(asciidoctor), ["html5"]);`,
	} {
		if !strings.Contains(adocConverterModule, want) {
			t.Errorf("the converter module lacks %q", want)
		}
	}
}
