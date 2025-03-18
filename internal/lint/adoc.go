package lint

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
	"github.com/errata-ai/vale/v3/internal/system"
)

// NOTE: Asciidoctor converts "'" to "’".
//
// See #206.
var adocSanitizer = strings.NewReplacer(
	"\u2018", "&apos;",
	"\u2019", "&apos;",
	"\u201C", "&#8220;",
	"\u201D", "&#8221;",
	"&#8217;", "&apos;",
	"&rsquo;", "&apos;")

// Convert listing blocks of the form `[source,.+]` to `[source]`
var reSource = regexp.MustCompile(`\[source,.+\]`)
var reComment = regexp.MustCompile(`// .+`)

var adocArgs = []string{
	"-s",
	"-a",
	"notitle!",
	"-a",
	"attribute-missing=drop",
}

func (l *Linter) lintADoc(f *core.File) error {
	var html string
	var err error

	exe := system.Which([]string{"asciidoctor"})
	if exe == "" {
		return core.NewE100("lintAdoc", errors.New("asciidoctor not found"))
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}
	s = adocSanitizer.Replace(s)

	html, err = callAdoc(s, exe, l.Manager.Config.Asciidoctor)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	html = adocSanitizer.Replace(html)
	body := reSource.ReplaceAllStringFunc(f.Content, func(m string) string {
		offset := 0
		if strings.HasSuffix(m, ",]") {
			offset = 1
			m = strings.Replace(m, ",]", "]", 1)
		}
		// NOTE: This is required to avoid finding matches in block attributes.
		//
		// See https://github.com/errata-ai/vale/issues/296.
		parts := strings.Split(m, ",")
		size := nlp.StrLen(parts[len(parts)-1])

		span := strings.Repeat("*", size-2+offset)
		return "[source, " + span + "]"
	})

	body = reComment.ReplaceAllStringFunc(body, func(m string) string {
		// NOTE: This is required to avoid finding matches in line comments.
		//
		// See https://github.com/errata-ai/vale/issues/414.
		//
		// TODO: Multiple line comments are not handled correctly.
		//
		// https://docs.asciidoctor.org/asciidoc/latest/comments/
		parts := strings.Split(m, "//")
		span := strings.Repeat("*", nlp.StrLen(parts[1])-1)
		return "// " + span
	})

	f.Content = body
	return l.lintHTMLTokens(f, []byte(html), 0)
}

func callAdoc(text, exe string, attrs map[string]string) (string, error) {
	args := adocArgs

	args = append(args, parseAttributes(attrs)...)
	args = append(args, []string{"--safe-mode", "secure", "-"}...)

	return system.ExecuteWithInput(exe, text, args...)
}

func parseAttributes(attrs map[string]string) []string {
	var args []string

	for k, v := range attrs {
		entry := fmt.Sprintf("%s=%s", k, v)
		if v == "YES" {
			entry = k
		} else if v == "NO" {
			entry = k + "!"
		}
		args = append(args, []string{"-a", entry}...)
	}

	return args
}
