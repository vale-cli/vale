package core

import (
	"path/filepath"
	"regexp"
	"strings"
)

// CommentsByNormedExt determines what parts of a file we should lint -- e.g.,
// we only want to lint // or /* comments in a C++ file. Multiple formats are
// mapped to a single extension (e.g., .java -> .c) because many languages use
// the same comment delimiters.
//
// Deprecated: When possible, we now use tree-sitter grammars to determine the
// comment delimiters for a given file. See the `lint/code` package for more
// information.
//
// TODO: This should be removed once we have tree-sitter grammars for all
// languages.
var CommentsByNormedExt = map[string]map[string]string{
	".c": {
		"inline":     `(?:^|\s)(?:(//.+)|(/\*.+\*/))`,
		"blockStart": `(/\*.*)`,
		"blockEnd":   `(.*\*/)`,
	},
	".clj": {
		"inline":     `(;+.+)`,
		"blockStart": `$^`,
		"blockEnd":   `$^`,
	},
	".r": {
		"inline":     `(#.+)`,
		"blockStart": `$^`,
		"blockEnd":   `$^`,
	},
	".ps1": {
		"inline":     `(#.+)`,
		"blockStart": `(<#.*)`,
		"blockEnd":   `(.*#>)`,
	},
	".php": {
		"inline":     `(//.+)|(/\*.+\*/)|(#.+)`,
		"blockStart": `(/\*.*)`,
		"blockEnd":   `(.*\*/)`,
	},
	".lua": {
		"inline":     `(-- .+)`,
		"blockStart": `(-{2,3}\[\[.*)`,
		"blockEnd":   `(.*\]\])`,
	},
	".hs": {
		"inline":     `(-- .+)`,
		"blockStart": `(\{-.*)`,
		"blockEnd":   `(.*-\})`,
	},
}

// FormatByExtension associates a file extension with its "normed" extension
// and its format (markup, code or text).
var FormatByExtension = map[string][]string{
	`\.(?:[rc]?py[3w]?|[Ss][Cc]onstruct)$`:     {".py", "code"},
	`\.(?:adoc|asciidoc|asc)$`:                 {".adoc", "markup"},
	`\.(?:clj|cljs|cljc|cljd)$`:                {".clj", "code"},
	`\.(?:cpp|cc|c|cp|cxx|c\+\+|h|hpp|h\+\+)$`: {".cpp", "code"},
	`\.(?:css)$`:                      {".css", "code"},
	`\.(?:cs|csx)$`:                   {".c", "code"},
	`\.(?:dita)$`:                     {".dita", "markup"},
	`\.(?:go)$`:                       {".go", "code"},
	`\.(?:hs)$`:                       {".hs", "code"},
	`\.(?:html|htm|shtml|xhtml)$`:     {".html", "markup"},
	`\.(?:java|bsh)$`:                 {".java", "code"},
	`\.(?:jl)$`:                       {".jl", "code"},
	`\.(?:js|jsx)$`:                   {".js", "code"},
	`\.(?:lua)$`:                      {".lua", "code"},
	`\.(?:md|mdown|markdown|markdn)$`: {".md", "markup"},
	`\.(?:mdx)$`:                      {".mdx", "markup"},
	`\.(?:org)$`:                      {".org", "markup"},
	`\.(?:php)$`:                      {".php", "code"},
	`\.(?:pl|pm|pod)$`:                {".r", "code"},
	`\.(?:proto)$`:                    {".proto", "code"},
	`\.(?:ps1|psm1|psd1)$`:            {".ps1", "code"},
	`\.(?:rb|Gemfile|Rakefile|Brewfile|gemspec)$`: {".rb", "code"},
	`\.(?:rs)$`:        {".rs", "code"},
	`\.(?:rst|rest)$`:  {".rst", "markup"},
	`\.(?:r|R)$`:       {".r", "code"},
	`\.(?:sass|less)$`: {".c", "code"},
	`\.(?:scala|sbt)$`: {".c", "code"},
	`\.(?:swift)$`:     {".c", "code"},
	`\.(?:ts|tsx)$`:    {".ts", "code"},
	`\.(?:txt)$`:       {".txt", "text"},
	`\.(?:xml|xsd)$`:   {".xml", "markup"},
	`\.(?:yaml|yml)$`:  {".yml", "data"},
	`\.(?:json)$`:      {".json", "data"},
	`\.(?:toml)$`:      {".toml", "data"},
}

// FormatFromExt takes a file extension and returns its [normExt, format]
// list, if supported.
func FormatFromExt(path string, mapping map[string]string) (string, string) {
	base := strings.Trim(filepath.Ext(path), ".")
	kind := getFormat("." + base)

	if format, found := mapping[base]; found {
		if kind == "code" && getFormat("."+format) == "markup" {
			// NOTE: This is a special case of embedded markup within code.
			return "." + format, "fragment"
		} else if kind == "data" && getFormat("."+format) == "markup" {
			// NOTE: This is a special case of embedded markup within data.
			//
			// Unlike code, data formats are *always* linted as a fragment with
			// the default format as plain text.
			return "." + format, "data"
		}
		base = format
	}

	base = "." + base
	for r, f := range FormatByExtension {
		m, _ := regexp.MatchString(r, base)
		if m {
			return f[0], f[1]
		}
	}

	return "unknown", "unknown"
}

func getFormat(ext string) string {
	for r, f := range FormatByExtension {
		m, _ := regexp.MatchString(r, ext)
		if m {
			return f[1]
		}
	}
	return ""
}

func GetNormedExt(ext string) string {
	for r, f := range FormatByExtension {
		m, _ := regexp.MatchString(r, ext)
		if m {
			return f[0]
		}
	}
	return ""
}
