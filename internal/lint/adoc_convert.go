//go:build !vale_noconvert

package lint

import (
	"strings"

	"github.com/jdkato/adocgo"
)

// convertADoc converts one document the way `asciidoctor -s --safe-mode
// secure` would with adocArgs, and the configuration's `[asciidoctor]`
// attributes after them. A cross-reference the author gave no text renders
// as an empty link: Asciidoctor otherwise invents text for it, the target's
// title or `[refid]`, and none of that is prose to lint.
func convertADoc(text string, attrs map[string]string) (string, error) {
	html, err := adocgo.Convert([]byte(text), adocgo.Options{
		Attributes: adocOptions(attrs),
		Safe:       adocgo.SafeSecure,
		NoXrefText: true,
	})
	if err != nil {
		return "", err
	}
	return string(html), nil
}

// adocOptions is Vale's attributes with the configuration's over them, as
// the API takes them: a `YES` sets the attribute and a `NO` unsets it. The
// configuration's word is last, as the later `-a` was on the command line,
// so a set and an unset of the same name never both reach the converter.
func adocOptions(attrs map[string]string) map[string]string {
	out := map[string]string{}
	for i := 0; i+1 < len(adocArgs); i++ {
		if adocArgs[i] != "-a" {
			continue
		}
		k, v, _ := strings.Cut(adocArgs[i+1], "=")
		out[k] = v
		i++
	}
	for k, v := range attrs {
		delete(out, k)
		delete(out, k+"!")
		switch v {
		case "YES":
			out[k] = ""
		case "NO":
			out[k+"!"] = ""
		default:
			out[k] = v
		}
	}
	return out
}
