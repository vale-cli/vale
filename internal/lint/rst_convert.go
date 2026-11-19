//go:build !vale_noconvert

package lint

import (
	"strings"

	"github.com/jdkato/rstgo"
)

// rstOptions is how Vale reads reStructuredText: the flags in rstArgs, plus
// the Sphinx fallbacks for the configuration's `[sphinx]` section.
func (l *Linter) rstOptions() rstgo.Options {
	code := map[string]bool{}
	for name := range sphinxCode {
		code[name] = true
	}
	for _, name := range l.Manager.Config.SphinxNames("CodeDirectives") {
		code[strings.ToLower(name)] = true
	}
	prose := map[string]bool{}
	for name := range sphinxProse {
		prose[name] = true
	}
	for _, name := range l.Manager.Config.SphinxNames("ProseRoles") {
		prose[strings.ToLower(name)] = true
	}

	return rstgo.Options{
		ReportLevel:         5,
		HaltLevel:           5,
		LinkStylesheet:      true,
		NoFileInsertion:     true,
		TocBacklinks:        "none",
		NoFootnoteBacklinks: true,
		NoSectionNumbering:  true,
		UnknownDirective: func(name string) rstgo.DirectiveFallback {
			norm := strings.ToLower(name)
			if code[norm] || strings.HasPrefix(norm, "auto") {
				return rstgo.DirectiveLiteral
			}
			return rstgo.DirectiveProse
		},
		UnknownRole: func(name string) rstgo.RoleFallback {
			base := strings.ToLower(name)
			base = base[strings.LastIndexByte(base, ':')+1:]
			switch {
			case sphinxTargeted[base]:
				return rstgo.RoleReference
			case prose[base]:
				return rstgo.RoleText
			}
			return rstgo.RoleLiteral
		},
	}
}

// convertRST converts one document to the body of its HTML.
func (l *Linter) convertRST(text string) (string, error) {
	html, err := rstgo.Convert([]byte(text), l.rstOptions())
	if err != nil {
		return "", err
	}
	return rstBody(string(html)), nil
}
