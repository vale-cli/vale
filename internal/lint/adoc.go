package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/system"
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
	// Section numbers and a table of contents are generated artifacts: they
	// prefix headings with "N." or duplicate heading text in a <ul class="toc">.
	// Both break heading detection and position lookup, so we force them off --
	// they aren't authored prose. CLI attributes override in-document ones.
	//
	// See https://github.com/errata-ai/vale/issues/1101.
	"-a",
	"sectnums!",
	"-a",
	"toc!",
	// Caption labels are generated the same way: "Table 1." prefixes a block
	// title with text that appears nowhere in the source, so a match against
	// it lands on an unrelated line.
	//
	// See https://github.com/vale-cli/vale/issues/1152.
	"-a",
	"table-caption!",
	"-a",
	"figure-caption!",
	"-a",
	"example-caption!",
	"-a",
	"listing-caption!",
}

// The `asciidoctor` on PATH is usually a shim that boots RubyGems before it
// reaches Asciidoctor, and RubyGems is most of Ruby's startup: `ruby -e ""`
// takes 70ms where `ruby --disable-gems -e ""` takes none. Vale pays that once
// per file, so on a corpus of a few hundred AsciiDoc pages it is the single
// largest cost in the run.
//
// Asciidoctor itself loads in a few milliseconds once Ruby is up, so invoking
// Ruby directly with the library on its load path does the same work for less
// than half the time -- 120ms to 50ms on a 15 KB file.
//
// Where that library lives is not discoverable the way the executable is: a
// Homebrew cellar, a gem install, rbenv and Bundler all put it somewhere
// different, and `gem which` answers on none of them reliably. So Ruby is asked
// once, and the answer is verified by converting a document before it is
// trusted. Anything unexpected leaves adocDirect nil and the executable is used
// as before.
// adocConcurrency is how many files Vale lints at once, and so how many
// Asciidoctor processes the pool keeps warm.
//
// This was a flat 5. On a machine with more cores than that it left them idle,
// and on a machine with fewer it oversubscribed. External formats are the case
// that cares: each file is a process, so the number in flight is the number of
// interpreters running.
var adocConcurrency = max(2, runtime.NumCPU()-2)

var (
	adocOnce   sync.Once
	adocDirect []string // ruby --disable-gems -I <lib> -e <server>, or nil

)

// adocConverter renders a cross-reference the author gave no text as an
// empty link. Asciidoctor otherwise invents text for it: the target's title
// when it resolves, and `[refid]` or the file path when it doesn't, as every
// reference into another chapter of a book does when a chapter is linted on
// its own. None of that is in the source, so none of it is prose to lint.
//
//nolint:dupword // Ruby closes each block with end
const adocConverter = `class ValeHtml5 < (Asciidoctor::Converter.for "html5")
  register_for "html5"
  def convert_inline_anchor node
    return %(<a href="#{node.target}"></a>) if node.type == :xref && node.text.nil?
    super
  end
end
`

const adocServer = `Encoding.default_external = Encoding::UTF_8
Encoding.default_internal = Encoding::UTF_8
$stdin.binmode
$stdout.binmode

require "asciidoctor"
` + adocConverter + `

attrs = {}
ARGV.each { |a| k, _, v = a.partition("="); attrs[k] = v }

while (header = $stdin.gets)
  n = header.to_i
  break if n < 0
  doc = n.zero? ? "" : $stdin.read(n)
  break if doc.nil?

  begin
    out = Asciidoctor.convert(doc.force_encoding("UTF-8"),
      standalone: false, safe: :secure, attributes: attrs).to_s
    $stdout.write("ok #{out.bytesize}\n", out)
  rescue => e
    msg = e.message.to_s
    $stdout.write("err #{msg.bytesize}\n", msg)
  end
  $stdout.flush
end`

// adocLoadPath finds Asciidoctor's library, starting from its executable.
//
// Asking Ruby does not work: the `asciidoctor` on PATH is often a shim that
// sets GEM_HOME to a private prefix, and the Ruby on PATH cannot load the gem
// without it -- on macOS the system Ruby answers with a LoadError. The library
// does sit near the executable in every layout that ships one, so the search
// starts there and walks up.
func adocLoadPath(exe string) string {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}

	// bin/asciidoctor -> the prefix holding it, and its parent for layouts
	// that put the gem in a sibling directory.
	dir := filepath.Dir(filepath.Dir(resolved))
	for i := 0; i < 3 && dir != "/" && dir != "."; i++ {
		for _, pattern := range []string{
			filepath.Join(dir, "lib", "asciidoctor.rb"),
			filepath.Join(dir, "gems", "asciidoctor-*", "lib", "asciidoctor.rb"),
			filepath.Join(dir, "libexec", "gems", "asciidoctor-*", "lib", "asciidoctor.rb"),
		} {
			matches, _ := filepath.Glob(pattern)
			if len(matches) > 0 {
				return filepath.Dir(matches[0])
			}
		}
		dir = filepath.Dir(dir)
	}

	return ""
}

// adocFastPath returns the argv prefix for invoking Asciidoctor without
// RubyGems, or nil when that could not be established.
func adocFastPath() []string {
	adocOnce.Do(func() {
		ruby := system.Which([]string{"ruby"})
		if ruby == "" {
			return
		}

		lib := adocLoadPath(system.Which([]string{"asciidoctor"}))
		if lib == "" {
			return
		}

		candidate := []string{ruby, "--disable-gems", "-I" + lib, "-e", adocServer}

		// Trust it only after a document has made the round trip. The probe
		// carries a non-ASCII character on purpose: a mismatched default
		// encoding is what broke the first version of this, and an ASCII-only
		// probe would have passed anyway.
		probe, err := startExtProc(candidate, nil)
		if err != nil {
			return
		}
		defer probe.close()

		got, err := probe.convert("= T\n\nna\u00efve body\n")
		if err != nil || !strings.Contains(got, "na\u00efve body") {
			return
		}

		adocDirect = candidate
	})

	return adocDirect
}

func (l *Linter) lintADoc(f *core.File) error {
	exe := system.Which([]string{"asciidoctor"})
	if exe == "" {
		return core.NewE100(f.Path, errors.New("asciidoctor not found; it's needed to read AsciiDoc"))
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}
	s = adocSanitizer.Replace(s)

	html, err := l.callAdoc(s, exe, l.Manager.Config.Asciidoctor)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	return l.lintADocHTML(f, html)
}

// lintADocHTML lints a document from Asciidoctor's HTML for it. Source
// blocks and line comments are masked in the source first, so a match in
// the HTML can't be placed on them.
func (l *Linter) lintADocHTML(f *core.File, html string) error {
	if err := l.lintMetadata(f); err != nil {
		return err
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
		span := strings.Repeat("*", nlp.StrLen(strings.TrimPrefix(m, "// ")))
		return "// " + span
	})

	f.Content = body
	return l.lintHTMLTokens(f, []byte(html), 0)
}

func (l *Linter) callAdoc(text, exe string, attrs map[string]string) (string, error) {
	if direct := adocFastPath(); direct != nil {
		converted := append(append([]string{}, adocDirectAttrs...), directAttributes(attrs)...)

		l.adocOnce.Do(func() {
			// One process per file Vale has in flight; more would idle and
			// fewer would make the concurrent walk queue behind them.
			pool, err := newProcPool(direct, converted, l.poolSize())
			if err == nil {
				l.adoc = pool
			}
		})

		if l.adoc != nil {
			return l.adoc.convert(text, direct, converted)
		}
	}

	args := adocArgs
	if conv := adocConverterFile(adocIsJS(exe)); conv != "" {
		args = append(args, "-r", conv)
	}

	args = append(args, parseAttributes(attrs)...)
	args = append(args, []string{"--safe-mode", "secure", "-"}...)

	return system.ExecuteWithInput(exe, text, args...)
}

var (
	adocConvOnce sync.Once
	adocConvPath string
)

// adocConverterFile writes the converter where the `asciidoctor` executable
// can require it, once, and returns its path; "" when it couldn't be written,
// in which case the executable runs without it.
//
// Asciidoctor.js requires a JavaScript module, so it gets the converter the
// browser build uses, registered the way its CLI expects.
func adocConverterFile(js bool) string {
	adocConvOnce.Do(func() {
		name, src := "vale-asciidoctor.rb", adocConverter
		if js {
			name, src = "vale-asciidoctor.js", adocConverterModule
		}
		path := filepath.Join(os.TempDir(), name)
		if err := os.WriteFile(path, []byte(src), 0o600); err == nil {
			adocConvPath = path
		}
	})
	return adocConvPath
}

// adocConverterModule is adocConverterJS as a module for `asciidoctor -r`.
//
// Asciidoctor.js's CLI loads the module from wherever it is, so the module
// can't name @asciidoctor/core by a relative path; it resolves the copy the
// CLI itself runs, starting from the CLI's script.
const adocConverterModule = `const path = require("path");
const core = require(require.resolve("@asciidoctor/core", { paths: [path.dirname(process.argv[1])] }));
const asciidoctor = core();
` + adocConverterJS + `asciidoctor.ConverterFactory.register(new ValeHtml5Converter(asciidoctor), ["html5"]);
`

var adocKinds sync.Map // exe -> bool, true for Asciidoctor.js

// adocIsJS reports whether the asciidoctor at exe is Asciidoctor.js rather
// than the Ruby gem, asking it once per run.
func adocIsJS(exe string) bool {
	if v, ok := adocKinds.Load(exe); ok {
		return v.(bool)
	}
	out, err := system.ExecuteWithInput(exe, "", "--version")
	js := err == nil && isAsciidoctorJS(out)
	adocKinds.Store(exe, js)
	return js
}

// isAsciidoctorJS reads `asciidoctor --version`: the JavaScript port names
// itself, as "Asciidoctor.js 3.0.4 (Asciidoctor 2.0.20)".
func isAsciidoctorJS(version string) bool {
	return strings.Contains(version, "Asciidoctor.js")
}

// adocDirectAttrs mirrors adocArgs for the API, which takes a hash rather than
// repeated -a flags. `-s` becomes `standalone: false` in the script.
var adocDirectAttrs = []string{
	"notitle!=",
	"attribute-missing=drop",
	"sectnums!=",
	"toc!=",
	"table-caption!=",
	"figure-caption!=",
	"example-caption!=",
	"listing-caption!=",
}

// directAttributes renders user attributes the way the script parses them.
func directAttributes(attrs map[string]string) []string {
	var args []string

	for k, v := range attrs {
		entry := fmt.Sprintf("%s=%s", k, v)
		if v == "YES" {
			entry = k + "="
		} else if v == "NO" {
			entry = k + "!="
		}
		args = append(args, entry)
	}

	return args
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
