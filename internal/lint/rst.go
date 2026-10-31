package lint

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// reStructuredText configuration.
//
// reCodeBlock is used to convert Sphinx-style code directives to the regular
// `::` for rst2html, including the use of runtime options (e.g., :caption:).
var reCodeBlock = regexp.MustCompile(`\.\. (?:raw|code(?:-block)?):: *(?:[\w-]+)?(?:\s+:\w+:.*)*`)

// A `contents` directive repeats the document's headings, so it is read as
// code. See https://github.com/errata-ai/vale/v2/issues/119.
var reSphinx = regexp.MustCompile(`.. contents::`)
var rstArgs = []string{
	"--quiet",
	"--halt=5",
	"--report=5",
	"--link-stylesheet",
	"--no-file-insertion",
	"--no-toc-backlinks",
	"--no-footnote-backlinks",
	"--no-section-numbering",
}

// rstEnv puts the spawned Python in UTF-8 mode. Docutils reads stdin through
// Python's text layer, which on Windows decodes with the console's code page,
// cp1252, and no Docutils flag reaches below that layer: a document's UTF-8
// went in as mojibake and came out the same. The pooled interpreter is not
// affected, since it reads and writes bytes.
var rstEnv = []string{"PYTHONUTF8=1", "PYTHONIOENCODING=utf-8"}

// Converting a document with Docutils takes a few milliseconds; starting
// Python and importing Docutils takes seventy. Vale paid the latter once per
// file, so on a corpus of reStructuredText nearly all of the time went to
// starting the same interpreter over and over.
//
// rstServer keeps one interpreter up and converts documents as they arrive. It
// parses the flags through Docutils' own command-line handling rather than
// mapping them to settings by hand, so the pooled settings cannot drift from
// what a per-file rst2html invocation would use.
//
// Docutils knows its own directives and roles, and a Sphinx project's files
// use Sphinx's and its extensions' as well; Docutils drops the body of every
// directive it does not know. So before parsing, the server fills the gap:
// the body of an unknown directive is prose, except for a named few holding
// code, data, or generated content, and the text of an unknown role is code,
// except for the reference and interface roles, whose text is prose. A
// project's `[sphinx]` section adds to both lists; the first argument
// carries it as JSON, and the Docutils flags follow. See #294.
const rstServer = `import json, re, sys
from docutils import nodes
from docutils.core import Publisher, publish_string
from docutils.parsers.rst import Directive, directives, roles
from docutils.statemachine import StringList

CONFIG = json.loads(sys.argv[1] or "{}")

class AnyOptions(dict):
    def __contains__(self, key):
        return True
    def __getitem__(self, key):
        return directives.unchanged
    def get(self, key, default=None):
        return directives.unchanged

class Prose(Directive):
    optional_arguments = 1
    final_argument_whitespace = True
    has_content = True
    option_spec = AnyOptions()
    def run(self):
        # The first line of the argument block is the argument -- a version, a
        # signature, a title. Any lines indented under it are prose, as in
        # "versionadded", whose text sits there rather than in the content.
        node = nodes.container()
        if self.arguments:
            rest = self.arguments[0].split("\n")[1:]
            if rest:
                src = self.state.document.current_source
                block = StringList(rest, items=[(src, self.lineno + i) for i in range(len(rest))])
                self.state.nested_parse(block, self.lineno, node)
        if self.content:
            self.state.nested_parse(self.content, self.content_offset, node)
        return [node]

class Skip(Prose):
    def run(self):
        # A literal block rather than nothing: the text is then masked out of
        # the source, so a match elsewhere is not placed on a copy in here.
        text = "\n".join(list(self.arguments) + list(self.content))
        return [nodes.literal_block(text, text)]

CODE = {
    "toctree", "literalinclude", "highlight", "index", "tabularcolumns",
    "math", "graphviz", "graph", "digraph", "inheritance-diagram",
    "productionlist", "doctest", "testcode", "testoutput", "testsetup",
    "testcleanup", "currentmodule", "module", "default-domain",
    "codeauthor", "sectionauthor", "cssclass", "rst-class",
} | set(CONFIG.get("code") or [])

_directive = directives.directive
def directive(name, language_module, document):
    fn, messages = _directive(name, language_module, document)
    if fn is not None:
        return fn, messages
    norm = name.lower()
    if norm in CODE or norm.startswith("auto"):
        return Skip, []
    return Prose, []
directives.directive = directive

PROSE = {
    "ref", "doc", "any", "numref", "term", "guilabel", "menuselection",
    "abbr", "dfn",
} | set(CONFIG.get("prose") or [])
TARGETED = {"ref", "doc", "any", "numref"}
_title = re.compile(r"^(.*\S)\s*<[^<>]+>$", re.S)

def prose_role(name, rawtext, text, lineno, inliner, options={}, content=[]):
    m = _title.match(text)
    if m:
        text = m.group(1)
    elif name.lower().split(":")[-1] in TARGETED:
        return [nodes.literal(rawtext, text)], []
    # Plain text, not an inline element: the words are part of the sentence.
    return [nodes.Text(text)], []

def code_role(name, rawtext, text, lineno, inliner, options={}, content=[]):
    return [nodes.literal(rawtext, text)], []

_role = roles.role
def role(name, language_module, lineno, reporter):
    fn, messages = _role(name, language_module, lineno, reporter)
    if fn is not None:
        return fn, messages
    base = name.lower().split(":")[-1]
    return (prose_role if base in PROSE else code_role), []
roles.role = role

_pub = Publisher()
_pub.set_components("standalone", "restructuredtext", "html4css1")
_pub.process_command_line(argv=sys.argv[2:])
SETTINGS = _pub.settings

buf = sys.stdin.buffer
out = sys.stdout.buffer

while True:
    header = buf.readline()
    if not header:
        break
    n = int(header)
    if n < 0:
        break
    doc = buf.read(n) if n else b""
    try:
        b = publish_string(
            source=doc.decode("utf-8"),
            source_path="<stdin>",
            writer_name="html4css1",
            settings=SETTINGS,
        )
        out.write(b"ok %d\n" % len(b))
        out.write(b)
    except Exception as e:
        m = str(e).encode("utf-8", "replace")
        out.write(b"err %d\n" % len(m))
        out.write(m)
    out.flush()`

var (
	rstOnce   sync.Once
	rstDirect []string // <python> -c <server>, or nil
)

// rePython matches the names Python is installed under -- `python`, `python3`,
// `python3.12`, `pypy3` -- and nothing else.
var rePython = regexp.MustCompile(`(?i)^(?:python|pypy)[0-9.]*(?:\.exe)?$`)

// rstInterpreter finds the Python that can import Docutils.
//
// It is not necessarily the `python3` on PATH: rst2html is installed with a
// shebang naming the interpreter of the environment Docutils was installed
// into, and on a machine with several Pythons the one on PATH often cannot
// import it. So the script is asked which interpreter it runs on.
//
// The answer is only used when it names Python. rst2html is not always the
// script it looks like: a version manager can put a shell wrapper on PATH under
// that name, and the wrapper's shebang names its own shell. Passing the server
// source to a shell runs its first line, `import sys`, as a command -- which is
// a real program on a machine with ImageMagick. See #1137.
func rstInterpreter(exe string) string {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}

	file, err := os.Open(resolved)
	if err != nil {
		return ""
	}
	defer file.Close()

	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "#!") {
		return ""
	}

	fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "#!"))
	if len(fields) == 0 {
		return ""
	}

	// `#!/usr/bin/env python3` names the interpreter in the second field.
	python := fields[0]
	if filepath.Base(python) == "env" {
		if len(fields) < 2 {
			return ""
		}
		python = fields[1]
	}

	if !rePython.MatchString(filepath.Base(python)) {
		return ""
	} else if !strings.ContainsAny(python, `/\`) {
		// A bare name -- what follows `env` -- is resolved on PATH. A shebang
		// writes a path the POSIX way whatever the host does, so `filepath` is
		// not the judge of whether this is one: on Windows `IsAbs` wants a
		// volume, and read that way every `#!/usr/bin/python3` in the world is
		// a relative path that resolves to nothing.
		return system.Which([]string{python})
	}

	return python
}

// hasShebang reports whether exe begins with `#!`, naming an interpreter.
func hasShebang(exe string) bool {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return false
	}
	file, err := os.Open(resolved)
	if err != nil {
		return false
	}
	defer file.Close()

	head := make([]byte, 2)
	n, _ := io.ReadFull(file, head)
	return n == 2 && string(head) == "#!"
}

// pythonWith finds a Python on PATH that can import module, or "".
func pythonWith(module string) string {
	for _, name := range []string{"python3", "python", "py"} {
		if p := system.Which([]string{name}); p != "" &&
			exec.Command(p, "-c", "import "+module).Run() == nil {
			return p
		}
	}
	return ""
}

// rstFastPath returns the argv prefix for converting through a long-lived
// interpreter, or nil when that could not be established.
func rstFastPath(exe string) []string {
	rstOnce.Do(func() {
		rstDirect = rstProbe(exe)
	})

	return rstDirect
}

// rstProbe establishes the argv for a long-lived interpreter, or nil.
//
// Only the interpreter the script itself names will do. Falling back to a
// Python on PATH sounds harmless -- the probe below still has to pass -- but
// it is a different Docutils, and the pool it warms then converts documents
// that a spawned rst2html would have converted differently. On Windows it
// picked up an interpreter where the two disagree about output encoding, and
// the same document came back as `naïve` one way and `naÃ¯ve` the other.
//
// The one exception is a script with no shebang at all: on Windows, pip
// installs rst2html as a launcher executable, which names nothing to read,
// and the spawned fallback cannot register the directives a Sphinx project
// needs. There, a Python on PATH that imports Docutils is tried, and the
// round trip below, with its non-ASCII character, guards the encoding.
func rstProbe(exe string) []string {
	python := rstInterpreter(exe)
	if python == "" && !hasShebang(exe) {
		python = pythonWith("docutils")
	}
	if python == "" {
		return nil
	}

	candidate := []string{python, "-c", rstServer}

	// Trust it only after a document has made the round trip. The probe
	// carries a non-ASCII character on purpose: a mismatched default
	// encoding is what broke the first version of the AsciiDoc pool, and
	// an ASCII-only probe would have passed anyway.
	probe, err := startExtProc(candidate, rstAttrs("{}"))
	if err != nil {
		return nil
	}
	defer probe.close()

	got, err := probe.convert("naïve body\n")
	if err != nil || !strings.Contains(got, "naïve body") {
		return nil
	}

	return candidate
}

func (l *Linter) lintRST(f *core.File) error {
	rst2html := rstExe()
	if rst2html == "" {
		return core.NewE100(f.Path, errors.New("rst2html not found; it's needed to read reStructuredText"))
	}

	s, err := l.prepareRST(f)
	if err != nil {
		return err
	}

	html, err := l.callRst(s, rst2html)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	return l.lintRSTHTML(f, html)
}

// rstExe is the rst2html on PATH, or empty.
func rstExe() string {
	return system.Which([]string{
		"rst2html", "rst2html.py", "rst2html-3", "rst2html-3.py"})
}

// prepareRST is the document as Docutils is given it.
func (l *Linter) prepareRST(f *core.File) (string, error) {
	s, err := l.Transform(f)
	if err != nil {
		return "", err
	}

	s = reSphinx.ReplaceAllString(s, ".. code::")
	s = reCodeBlock.ReplaceAllString(s, "::")
	return s, nil
}

// lintRSTHTML lints a document from its converted form.
func (l *Linter) lintRSTHTML(f *core.File, html string) error {
	if err := l.lintMetadata(f); err != nil {
		return err
	}
	return l.lintHTMLTokens(f, []byte(markSubstitutions(html)), 0)
}

// A substitution Docutils couldn't resolve, one a Sphinx project defines in
// its `rst_epilog`, comes through as written, since --report=5 drops the
// `problematic` markup it would get otherwise.
var reSubstitution = regexp.MustCompile(`\|[^\s|<>](?:[^|<>]*[^\s|<>])?\|(?:__?)?`)

// Literal text keeps its pipes as they are.
var reRSTLiteral = regexp.MustCompile(`(?s)<pre\b.*?</pre>|<tt\b.*?</tt>|<code\b.*?</code>`)

// markSubstitutions wraps each unresolved substitution in the `problematic`
// span Docutils would have given it, so it is skipped rather than read as
// prose.
func markSubstitutions(html string) string {
	if !strings.Contains(html, "|") {
		return html
	}

	var b strings.Builder
	last := 0
	for _, lit := range reRSTLiteral.FindAllStringIndex(html, -1) {
		b.WriteString(reSubstitution.ReplaceAllString(html[last:lit[0]], `<span class="problematic">$0</span>`))
		b.WriteString(html[lit[0]:lit[1]])
		last = lit[1]
	}
	b.WriteString(reSubstitution.ReplaceAllString(html[last:], `<span class="problematic">$0</span>`))
	return b.String()
}

// A batch is several documents converted together, joined by a literal
// block holding a token no document has and split apart again on it.
//
// Docutils parses at a steady couple of milliseconds per kilobyte, so a file
// of a thousand docstrings costs the same whether they are sent one at a
// time or all at once -- and one at a time, they went to one process in
// turn. Sent as chunks, they go to every process the pool has, which on a
// run over a directory is one per core.
const (
	rstSplitMark  = "vale-split-9d1f7c"
	rstChunkBytes = 32 * 1024
)

var (
	rstSplit   = "\n\n::\n\n   " + rstSplitMark + "\n\n"
	reRSTSplit = regexp.MustCompile(`<pre class="literal-block">\s*` + rstSplitMark + `\s*</pre>\n?`)
)

// convertRSTBatch converts the documents, in chunks and concurrently, and
// returns each one's HTML in order.
func (l *Linter) convertRSTBatch(docs []string) ([]string, error) {
	rst2html := rstExe()
	if rst2html == "" {
		return nil, errors.New("rst2html not found")
	}

	// Chunks of consecutive documents, each up to rstChunkBytes.
	var chunks [][]string
	size := 0
	for _, doc := range docs {
		if n := len(chunks); n == 0 || size+len(doc) > rstChunkBytes {
			chunks = append(chunks, nil)
			size = 0
		}
		chunks[len(chunks)-1] = append(chunks[len(chunks)-1], doc)
		size += len(doc)
	}

	converted := make([][]string, len(chunks))
	errs := make([]error, len(chunks))

	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(i int, chunk []string) {
			defer wg.Done()
			html, err := l.callRst(strings.Join(chunk, rstSplit), rst2html)
			if err != nil {
				errs[i] = err
				return
			}
			parts := reRSTSplit.Split(html, -1)
			if len(parts) != len(chunk) {
				errs[i] = fmt.Errorf("rst batch: %d documents, %d parts", len(chunk), len(parts))
				return
			}
			converted[i] = parts
		}(i, chunk)
	}
	wg.Wait()

	var out []string
	for i := range chunks {
		if errs[i] != nil {
			return nil, errs[i]
		}
		out = append(out, converted[i]...)
	}
	return out, nil
}

// callRst converts one document, over a pooled interpreter when Docutils can
// be reached directly.
func (l *Linter) callRst(text, exe string) (string, error) {
	if direct := rstFastPath(exe); direct != nil {
		attrs := rstAttrs(l.sphinxConfig())
		l.rstOnce.Do(func() {
			pool, err := newProcPool(direct, attrs, l.poolSize())
			if err == nil {
				l.rst = pool
			}
		})

		if l.rst != nil {
			html, err := l.rst.convert(text, direct, attrs)
			if err != nil {
				return "", err
			}
			return rstBody(html), nil
		}
	}

	html, err := rstSpawn(exe, text)
	if err != nil {
		return "", err
	}

	return rstBody(html), nil
}

// rstSpawn converts one document with a fresh rst2html process.
func rstSpawn(exe, text string) (string, error) {
	return system.ExecuteWithInputEnv(exe, text, rstEnv, rstArgs...)
}

// rstAttrs is the server's argument list: the `[sphinx]` section as JSON,
// then the Docutils flags.
func rstAttrs(config string) []string {
	return append([]string{config}, rstArgs...)
}

// sphinxConfig is the `[sphinx]` section as the server reads it.
func (l *Linter) sphinxConfig() string {
	cfg, _ := json.Marshal(map[string][]string{
		"code":  l.Manager.Config.SphinxNames("CodeDirectives"),
		"prose": l.Manager.Config.SphinxNames("ProseRoles"),
	})
	return string(cfg)
}

// rstBody takes the document body out of a full rst2html page.
func rstBody(html string) string {
	html = strings.ReplaceAll(html, "\r", "")

	bodyStart := strings.Index(html, "<body>\n")
	if bodyStart < 0 {
		bodyStart = -7
	}
	bodyEnd := strings.Index(html, "\n</body>")
	if bodyEnd < 0 || bodyEnd >= len(html) {
		bodyEnd = len(html) - 1
		if bodyEnd < 0 {
			bodyEnd = 0
		}
	}

	return html[bodyStart+7 : bodyEnd]
}
