package lint

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/vale-cli/vale/v3/internal/core"
)

// LaTeX is read without running TeX: texToHTML scans the source and writes
// the HTML the walker reads. Nothing is expanded or included, so a document
// that doesn't compile still lints and every file is read on its own.
//
// Prose is kept in place: sections are headings, text between blank lines is
// a paragraph, and captions, footnotes, list items and table cells are
// blocks of their own. Math, citations, references and verbatim text are
// code. A command the reader doesn't know is markup and its braced
// arguments are prose.

// texSigs says what a command's arguments are, in order: `o` an optional
// argument, dropped; `m` a mandatory one, dropped; `p` prose; `k` code.
var texSigs = map[string]string{
	"documentclass": "om", "usepackage": "om", "RequirePackage": "om",
	"input": "m", "include": "m", "includeonly": "m", "subfile": "m",
	"bibliography": "m", "bibliographystyle": "m", "addbibresource": "om",
	"printbibliography": "o", "label": "m", "includegraphics": "om",
	"graphicspath": "m", "vspace": "m", "hspace": "m", "setlength": "mm",
	"addtolength": "mm", "setcounter": "mm", "addtocounter": "mm",
	"newcounter": "mo", "stepcounter": "m", "refstepcounter": "m",
	"pagestyle": "m", "thispagestyle": "m", "pagenumbering": "m",
	"color": "om", "definecolor": "mmm", "pagecolor": "m", "hypersetup": "m",
	"geometry": "m", "newcommand": "moom", "renewcommand": "moom",
	"providecommand": "moom", "DeclareMathOperator": "mm",
	"DeclareRobustCommand": "moom", "newtheorem": "momo", "theoremstyle": "m",
	"newenvironment": "moomm", "renewenvironment": "moomm", "let": "mm",
	"bibitem": "om", "index": "m", "glossary": "m", "linespread": "m",
	"fontsize": "mm", "rule": "omm", "raisebox": "mp", "parbox": "ooomp",
	"makebox": "oop", "framebox": "oop", "scalebox": "mp", "resizebox": "mmp",
	"rotatebox": "omp", "textcolor": "omp", "colorbox": "omp",
	"fcolorbox": "ommp", "multicolumn": "mmp", "multirow": "mmp",
	"hyperref": "op", "hyperlink": "mp", "hypertarget": "mp",
	"footnotemark": "o", "author": "m", "date": "m", "affiliation": "m",
	"email": "m", "institute": "m", "address": "m", "cline": "m",
	"url": "k", "path": "k", "nolinkurl": "k", "ensuremath": "k",
	"si": "k", "SI": "kk", "num": "k", "qty": "kk", "unit": "k",
	"gls": "ok", "Gls": "ok", "glspl": "ok", "Glspl": "ok", "acrshort": "ok",
	"acrlong": "ok", "acrfull": "ok", "ac": "ok", "texttt": "k",
	"item": "", "centering": "", "noindent": "", "newpage": "", "clearpage": "",
	"captionsetup": "om", "lstset": "m", "usetikzlibrary": "m",
	"tikzset": "m", "pgfplotsset": "m", "sisetup": "m", "newacronym": "ommm",
	"newglossaryentry": "mm", "makeatletter": "", "makeatother": "",
	"lstinputlisting": "om", "inputminted": "omm", "includepdf": "om",
	"enlargethispage": "m", "phantom": "m", "hphantom": "m", "vphantom": "m",
}

// texWraps names the commands whose argument is prose inside an element.
var texWraps = map[string]string{
	"emph": "em", "textit": "em", "textsl": "em", "textbf": "strong",
	"underline": "u", "uline": "u", "sout": "s", "st": "s",
	"textsuperscript": "sup", "textsubscript": "sub",
}

// texHeadings maps the sectioning commands to heading levels.
var texHeadings = map[string]string{
	"title": "h1", "part": "h1", "chapter": "h1", "section": "h2",
	"subsection": "h3", "subsubsection": "h4", "paragraph": "h5",
	"subparagraph": "h6", "frametitle": "h2", "framesubtitle": "h3",
}

// texNotes names the commands whose argument is a block of its own, written
// after the paragraph it sits in.
var texNotes = map[string]string{
	"footnote": "footnote", "footnotetext": "footnote", "thanks": "footnote",
	"marginpar": "marginpar", "marginnote": "marginpar", "todo": "todo",
}

// texSymbols is the text a command stands for.
var texSymbols = map[string]string{
	"LaTeX": "LaTeX", "TeX": "TeX", "LaTeXe": "LaTeX2e", "BibTeX": "BibTeX",
	"ldots": "…", "dots": "…", "textellipsis": "…", "textbackslash": `\`,
	"textasciitilde": "~", "textasciicircum": "^", "textbar": "|",
	"textless": "<", "textgreater": ">", "textendash": "–", "textemdash": "—",
	"textquoteleft": "‘", "textquoteright": "’", "textquotedblleft": "“",
	"textquotedblright": "”", "textregistered": "®", "texttrademark": "™",
	"textcopyright": "©", "copyright": "©", "S": "§", "P": "¶", "pounds": "£",
	"euro": "€", "textdegree": "°", "ss": "ß", "ae": "æ", "AE": "Æ",
	"oe": "œ", "OE": "Œ", "o": "ø", "O": "Ø", "aa": "å", "AA": "Å",
	"l": "ł", "L": "Ł", "i": "ı", "j": "ȷ", "quad": " ", "qquad": " ",
	"space": " ", "enspace": " ", "thinspace": " ", "newline": " ",
	"linebreak": " ", "hfill": " ", "textvisiblespace": "␣",
	"slash": "/", "textunderscore": "_", "textdollar": "$",
	"textpercent": "%", "textampersand": "&", "texthash": "#",
}

// texChars is the text an escaped character stands for.
var texChars = map[byte]string{
	'%': "%", '&': "&", '$': "$", '#': "#", '_': "_", '{': "{", '}': "}",
	' ': " ", ',': " ", ';': " ", ':': " ", '\n': " ",
	'!': "", '/': "", '-': "", '@': "",
}

// texAccents maps an accent command to the mark it combines with a letter.
var texAccents = map[string]string{
	"'": "\u0301", "`": "\u0300", "^": "\u0302", `"`: "\u0308",
	"~": "\u0303", "=": "\u0304", ".": "\u0307", "c": "\u0327",
	"v": "\u030C", "H": "\u030B", "u": "\u0306", "r": "\u030A",
	"k": "\u0328", "d": "\u0323", "b": "\u0331",
}

// texRaw names the environments whose body is not prose and is dropped;
// the ones marked true are blocks of code, the rest leave nothing.
var texRaw = map[string]bool{
	"verbatim": true, "Verbatim": true, "BVerbatim": true, "LVerbatim": true,
	"lstlisting": true, "minted": true, "alltt": true, "algorithmic": true,
	"pycode": true, "luacode": true,
	"comment": false, "filecontents": false, "tikzpicture": false,
	"pgfpicture": false, "tikzcd": false, "forest": false,
	"thebibliography": false, "axis": false,
}

// texMath names the environments that are math.
var texMath = map[string]struct{}{
	"equation": {}, "align": {}, "alignat": {}, "gather": {}, "multline": {},
	"flalign": {}, "eqnarray": {}, "displaymath": {}, "math": {},
	"IEEEeqnarray": {}, "dmath": {}, "subequations": {}, "array": {},
}

// texEnvSigs is texSigs for the arguments that follow `\begin{name}`.
var texEnvSigs = map[string]string{
	"tabular": "om", "tabularx": "mm", "tabulary": "mm", "longtable": "om",
	"supertabular": "m", "minipage": "om", "wrapfigure": "omm",
	"subfigure": "om", "subtable": "om", "multicols": "m", "adjustbox": "m",
	"spacing": "m", "list": "mm", "tcolorbox": "o",
}

// texLists and texTables name the list and table environments.
var texLists = map[string]string{
	"itemize": "ul", "enumerate": "ol", "description": "dl",
	"compactitem": "ul", "compactenum": "ol", "inparaenum": "ol",
}

var texTables = map[string]struct{}{
	"tabular": {}, "tabularx": {}, "tabulary": {}, "longtable": {},
	"supertabular": {},
}

// texBlocks names the environments that have an element of their own; any
// other is a div carrying its name as a class.
var texBlocks = map[string]string{
	"quote": "blockquote", "quotation": "blockquote", "verse": "blockquote",
	"figure": "figure", "table": "figure",
}

var (
	// `\begin{document}` on a line no comment opens.
	texDocument = regexp.MustCompile(`(?m)^[^%\n]*?(\\begin\{document\})`)
	// A paragraph break: a line with nothing on it.
	texBlank = regexp.MustCompile(`\n[ \t]*\n`)
	// The glue a TeX primitive takes without braces: `\vskip 1em plus 2pt`.
	texGlue = regexp.MustCompile(`^\s*=?\s*-?[\d.]*\s*(?:pt|em|ex|cm|mm|in|bp|pc|sp|\\[a-zA-Z]+)(?:\s+(?:plus|minus)\s+[\d.]+[a-z]+)*`)
	// An argument that is a length or a number and nothing else.
	texLength = regexp.MustCompile(`^\s*[-+]?[\d.]*\s*(?:pt|em|ex|cm|mm|in|bp|pc|sp|\\(?:text|line|column|paper)(?:width|height)|\\baselineskip|\\fill)?\s*$`)
	texName   = regexp.MustCompile(`[^A-Za-z0-9_-]`)
)

var texEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace

// texEnv is one open environment.
type texEnv struct {
	name string
	kind string // "list", "table" or "block"
	tag  string
	item bool // a list's item is open
}

// texConv converts one LaTeX document.
type texConv struct {
	src string
	i   int
	out *strings.Builder

	para   bool // a paragraph is open
	inline int  // inside a heading, caption or note: text opens no paragraph
	mute   int  // inside the preamble: nothing is written
	done   bool // `\end{document}` was read

	stack []texEnv
	notes []string // footnotes waiting for their paragraph to close
}

func (c *texConv) w(s string) {
	if c.mute == 0 {
		c.out.WriteString(s)
	}
}

func (c *texConv) open() {
	if c.inline == 0 && !c.para {
		c.w("<p>")
		c.para = true
	}
}

// text writes prose, opening a paragraph for it if none is open.
func (c *texConv) text(s string) {
	if c.inline == 0 && !c.para {
		if strings.TrimSpace(s) == "" {
			return
		}
		c.open()
		s = strings.TrimLeft(s, " \t\n")
	}
	c.w(texEsc(s))
}

// code writes text no prose rule should read.
func (c *texConv) code(s string) {
	c.open()
	c.w("<code>" + texEsc(s) + "</code>")
}

// closePara ends the open paragraph and writes the notes it held.
func (c *texConv) closePara() {
	if c.para {
		c.w("</p>\n")
		c.para = false
	}
	if c.inline > 0 {
		return
	}
	for _, n := range c.notes {
		c.w(n)
	}
	c.notes = nil
}

// match returns the index of what closes the group or bracket at `at`, or
// `end`. A bracket closes outside any group.
func (c *texConv) match(at, end int, open, shut byte) int {
	depth := 0
	for j := at; j < end; j++ {
		switch ch := c.src[j]; {
		case ch == '\\':
			j++
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if open == '{' && depth == 0 {
				return j
			}
		case ch == shut && depth == 0:
			return j
		}
	}
	return end
}

// gap skips the space a command's argument may follow, a line break included
// but never a blank line.
func (c *texConv) gap(end int) int {
	j := c.i
	breaks := 0
	for j < end && (c.src[j] == ' ' || c.src[j] == '\t' || c.src[j] == '\n') {
		if c.src[j] == '\n' {
			breaks++
		}
		j++
	}
	if breaks > 1 {
		return c.i
	}
	return j
}

// opt reads an optional argument, reporting its bounds.
func (c *texConv) opt(end int) (int, int, bool) {
	if c.i >= end || c.src[c.i] != '[' {
		return 0, 0, false
	}
	shut := c.match(c.i, end, '[', ']')
	a := c.i + 1
	c.i = min(shut+1, end)
	return a, shut, true
}

// arg reads a mandatory argument: a group, or the single token TeX takes
// when there is none.
func (c *texConv) arg(end int) (int, int) {
	j := c.gap(end)
	if j >= end {
		return c.i, c.i
	}
	if c.src[j] == '{' {
		shut := c.match(j, end, '{', '}')
		c.i = min(shut+1, end)
		return j + 1, shut
	}
	if c.src[j] == '\\' {
		k := j + 1
		for k < end && isTeXLetter(c.src[k]) {
			k++
		}
		if k == j+1 && k < end {
			k++
		}
		c.i = k
		return j, k
	}
	c.i = j + 1
	return j, j + 1
}

// span converts the source between a and b where it stands.
func (c *texConv) span(a, b int) {
	rest := c.i
	c.i = a
	c.run(b)
	if !c.done {
		c.i = rest
	}
}

// capture converts the source between a and b as inline text, apart from
// the document.
func (c *texConv) capture(a, b int) string {
	out, para := c.out, c.para
	c.out, c.para = &strings.Builder{}, false
	c.inline++
	c.span(a, b)
	c.inline--
	s := c.out.String()
	c.out, c.para = out, para
	return s
}

func isTeXTable(name string) bool {
	_, ok := texTables[name]
	return ok
}

func isTeXLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// run converts the source up to `end`.
func (c *texConv) run(end int) { //nolint:gocyclo // one case per character class
	for c.i < end && !c.done {
		ch := c.src[c.i]
		switch ch {
		case '\\':
			c.command(end)
		case '%':
			c.comment(end)
		case '{':
			shut := c.match(c.i, end, '{', '}')
			c.span(c.i+1, shut)
			if !c.done {
				c.i = min(shut+1, end)
			}
		case '}':
			c.i++
		case '$':
			c.dollar(end)
		case '&':
			c.i++
			if c.inTable() {
				c.closePara()
				c.w("</td><td>")
			}
		case '~':
			c.i++
			c.text(" ")
		case '\n':
			c.newline(end)
		case '`', '\'', '-':
			c.ligature(end)
		default:
			j := c.i + 1
			for j < end && !strings.ContainsRune("\\%{}$&~\n`'-", rune(c.src[j])) {
				j++
			}
			c.text(c.src[c.i:j])
			c.i = j
		}
	}
}

// newline reads a line break: a space, or a paragraph break when the next
// line is blank.
func (c *texConv) newline(end int) {
	j := c.i + 1
	for j < end && (c.src[j] == ' ' || c.src[j] == '\t') {
		j++
	}
	if j < end && c.src[j] != '\n' {
		c.text("\n")
		c.i++
		return
	}
	for j < end && strings.ContainsRune(" \t\n", rune(c.src[j])) {
		j++
	}
	c.i = j
	if c.inline == 0 {
		c.closePara()
	} else {
		c.text(" ")
	}
}

// ligature reads the quotes and dashes TeX builds from plain characters.
func (c *texConv) ligature(end int) {
	rest := c.src[c.i:end]
	for _, l := range [][2]string{
		{"---", "—"}, {"--", "–"}, {"``", "“"}, {"''", "”"}, {"`", "‘"},
	} {
		if strings.HasPrefix(rest, l[0]) {
			c.text(l[1])
			c.i += len(l[0])
			return
		}
	}
	c.text(rest[:1])
	c.i++
}

// comment drops a `%` comment and, as TeX does, the line break after it. A
// comment addressed to Vale is kept, as the HTML comment the walker reads.
func (c *texConv) comment(end int) {
	stop := strings.IndexByte(c.src[c.i:end], '\n')
	if stop < 0 {
		stop = end
	} else {
		stop += c.i
	}
	body := c.src[c.i+1 : stop]

	c.i = min(stop+1, end)
	for c.i < end && (c.src[c.i] == ' ' || c.src[c.i] == '\t') {
		c.i++
	}

	if strings.HasPrefix(strings.TrimSpace(body), "vale ") {
		c.w("<!--" + strings.ReplaceAll(body, "--", "") + "-->")
	}

	// The line after the comment is the one TeX reads next: a blank one
	// still ends the paragraph.
	if c.i < end && c.src[c.i] == '\n' {
		c.i--
		c.newline(end)
	}
}

// dollar reads `$...$` or `$$...$$`. A `$` nothing closes before the
// paragraph ends is text.
func (c *texConv) dollar(end int) {
	mark := "$"
	if strings.HasPrefix(c.src[c.i:end], "$$") {
		mark = "$$"
	}
	body := c.i + len(mark)

	limit := end
	if loc := texBlank.FindStringIndex(c.src[body:end]); loc != nil && mark == "$" {
		limit = body + loc[0]
	}
	for j := body; j < limit; j++ {
		if c.src[j] == '\\' {
			j++
		} else if strings.HasPrefix(c.src[j:limit], mark) {
			c.code(c.src[body:j])
			c.i = j + len(mark)
			return
		}
	}

	c.text(mark)
	c.i = body
}

// until writes the source up to `mark` as code and moves past it.
func (c *texConv) until(mark string, end int) {
	stop := strings.Index(c.src[c.i:end], mark)
	if stop < 0 {
		return
	}
	c.code(c.src[c.i : c.i+stop])
	c.i += stop + len(mark)
}

func (c *texConv) inTable() bool {
	return len(c.stack) > 0 && c.stack[len(c.stack)-1].kind == "table"
}

// sig reads a command's arguments as its signature says.
func (c *texConv) sig(sig string, end int) {
	for _, kind := range sig {
		switch kind {
		case 'o':
			c.opt(end)
		case 'm':
			c.arg(end)
		case 'k':
			a, b := c.arg(end)
			c.code(c.src[a:b])
		case 'p':
			a, b := c.arg(end)
			c.span(a, b)
		}
	}
}

// command reads the command at the cursor.
func (c *texConv) command(end int) { //nolint:gocyclo // one case per command family
	c.i++
	if c.i >= end {
		return
	}

	ch := c.src[c.i]
	if !isTeXLetter(ch) {
		c.i++
		switch ch {
		case '\\':
			if c.i < end && c.src[c.i] == '*' {
				c.i++
			}
			c.opt(end)
			if c.inTable() {
				c.closePara()
				c.w("</td></tr>\n<tr><td>")
			} else {
				c.text(" ")
			}
		case '(':
			c.until(`\)`, end)
		case '[':
			c.until(`\]`, end)
		default:
			if mark := texAccents[string(ch)]; mark != "" {
				c.accent(mark, end)
			} else {
				c.text(texChars[ch])
			}
		}
		return
	}

	j := c.i
	for j < end && isTeXLetter(c.src[j]) {
		j++
	}
	name := c.src[c.i:j]
	c.i = j
	if c.i < end && c.src[c.i] == '*' {
		c.i++
	}

	if c.known(name, end) {
		return
	}

	lower := strings.ToLower(name)
	switch {
	case name == "begin":
		a, b := c.arg(end)
		c.begin(strings.TrimSuffix(c.src[a:b], "*"), end)
	case name == "end":
		a, b := c.arg(end)
		c.end(strings.TrimSuffix(c.src[a:b], "*"))
	case name == "item":
		c.item(end)
	case name == "par":
		c.closePara()
	case name == "caption":
		c.closePara()
		c.opt(end)
		a, b := c.arg(end)
		c.w("<figcaption>" + c.capture(a, b) + "</figcaption>\n")
		c.closePara()
	case name == "href":
		a, b := c.arg(end)
		ta, tb := c.arg(end)
		c.open()
		c.w(`<a href="` + texEsc(c.src[a:b]) + `">`)
		c.span(ta, tb)
		c.w("</a>")
	case name == "enquote":
		a, b := c.arg(end)
		c.text("“")
		c.span(a, b)
		c.text("”")
	case name == "verb", name == "lstinline", name == "mintinline":
		c.verb(name, end)
	case name == "def":
		// `\def\name#1{body}`: everything through the body is markup.
		if at := strings.IndexByte(c.src[c.i:end], '{'); at >= 0 {
			c.i += at
			c.arg(end)
		}
	case name == "vskip", name == "hskip", name == "kern":
		c.i += len(texGlue.FindString(c.src[c.i:end]))
	case name == "cmidrule":
		if c.i < end && c.src[c.i] == '(' {
			c.i = min(c.match(c.i, end, '(', ')')+1, end)
		}
		c.arg(end)
	case strings.Contains(lower, "cite"):
		// The natbib and biblatex families: notes are prose-like, the keys
		// are not, and together they stand where the citation prints.
		c.opt(end)
		c.opt(end)
		c.sig("k", end)
	case strings.HasSuffix(lower, "ref"):
		c.sig("k", end)
	default:
		if sig, ok := texSigs[name]; ok {
			c.sig(sig, end)
			return
		}
		// An unknown command is markup. Its options say nothing, nor does
		// an argument that is only a length; any other is read as the
		// group it is.
		c.opt(end)
		for c.i < end && c.src[c.i] == '{' {
			shut := c.match(c.i, end, '{', '}')
			if !texLength.MatchString(c.src[c.i+1 : shut]) {
				break
			}
			c.i = min(shut+1, end)
		}
	}
}

// known reads a command one of the tables names, reporting whether one did.
func (c *texConv) known(name string, end int) bool {
	if tag, ok := texHeadings[name]; ok {
		c.heading(name, tag, end)
		return true
	}
	if tag, ok := texWraps[name]; ok {
		a, b := c.arg(end)
		c.open()
		c.w("<" + tag + ">")
		c.span(a, b)
		c.w("</" + tag + ">")
		return true
	}
	if class, ok := texNotes[name]; ok {
		c.opt(end)
		a, b := c.arg(end)
		c.notes = append(c.notes,
			`<div class="`+class+`"><p>`+c.capture(a, b)+"</p></div>\n")
		if !c.para {
			c.closePara()
		}
		return true
	}
	if s, ok := texSymbols[name]; ok {
		c.text(s)
		return true
	}
	if mark, ok := texAccents[name]; ok && c.i < end && !isTeXLetter(c.src[c.i]) {
		c.accent(mark, end)
		return true
	}
	return false
}

// accent writes the letter that follows with `mark` over it.
func (c *texConv) accent(mark string, end int) {
	a, b := c.arg(end)
	letter := strings.Trim(c.src[a:b], "{} ")
	switch letter {
	case `\i`:
		letter = "i"
	case `\j`:
		letter = "j"
	}
	if letter == "" || strings.HasPrefix(letter, `\`) {
		return
	}
	c.text(norm.NFC.String(letter[:1] + mark + letter[1:]))
}

// verb reads inline verbatim text: `\verb|...|`, and the listings and minted
// commands, which take the same form or a group.
func (c *texConv) verb(name string, end int) {
	if name != "verb" {
		c.opt(end)
	}
	if name == "mintinline" {
		c.arg(end)
	}
	if c.i >= end {
		return
	}
	if c.src[c.i] == '{' && name != "verb" {
		a, b := c.arg(end)
		c.code(c.src[a:b])
		return
	}
	mark := c.src[c.i : c.i+1]
	c.i++
	c.until(mark, end)
}

func (c *texConv) heading(name, tag string, end int) {
	// The title is the one line of the preamble that is prose.
	mute := c.mute
	if name == "title" {
		c.mute = 0
	}

	c.closePara()
	c.opt(end)
	a, b := c.arg(end)
	c.w("<" + tag + ">" + c.capture(a, b) + "</" + tag + ">\n")
	c.closePara()

	c.mute = mute
}

// item opens a list item, closing the one before it.
func (c *texConv) item(end int) {
	c.closePara()
	a, b, labeled := c.opt(end)

	n := len(c.stack) - 1
	for n >= 0 && c.stack[n].kind != "list" {
		n--
	}
	if n < 0 {
		return
	}
	list := &c.stack[n]

	if list.tag != "dl" {
		if list.item {
			c.w("</li>\n")
		}
		c.w("<li>")
	} else {
		if list.item {
			c.w("</dd>\n")
		}
		if labeled {
			c.w("<dt>" + c.capture(a, b) + "</dt>\n")
		}
		c.w("<dd>")
	}
	list.item = true
}

// begin opens an environment.
func (c *texConv) begin(name string, end int) {
	if code, raw := texRaw[name]; raw {
		c.closePara()
		c.skipEnv(name, end)
		if code {
			c.w("<pre><code></code></pre>\n")
		}
		return
	}
	if _, math := texMath[name]; math {
		at := c.i
		c.skipEnv(name, end)
		stop := strings.LastIndex(c.src[at:c.i], `\end{`)
		if stop < 0 {
			stop = c.i - at
		}
		c.code(c.src[at : at+stop])
		return
	}

	c.closePara()
	if name == "document" {
		return
	}
	c.opt(end)
	c.sig(texEnvSigs[name], end)

	env := texEnv{name: name, kind: "block", tag: "div"}
	switch {
	case texLists[name] != "":
		env.kind, env.tag = "list", texLists[name]
		c.w("<" + env.tag + ">\n")
	case isTeXTable(name):
		env.kind, env.tag = "table", "table"
		c.w("<table>\n<tr><td>")
	case texBlocks[name] != "":
		env.tag = texBlocks[name]
		c.w("<" + env.tag + ">\n")
	default:
		c.w(`<div class="` + texName.ReplaceAllString(name, "") + "\">\n")
	}
	c.stack = append(c.stack, env)
}

// skipEnv moves past the body of the environment just opened.
func (c *texConv) skipEnv(name string, end int) {
	for _, shut := range []string{`\end{` + name + `}`, `\end{` + name + `*}`} {
		if at := strings.Index(c.src[c.i:end], shut); at >= 0 {
			c.i += at + len(shut)
			return
		}
	}
	c.i = end
}

// end closes an environment, and any left open inside it.
func (c *texConv) end(name string) {
	if name == "document" {
		c.closePara()
		c.done = true
		return
	}

	n := len(c.stack) - 1
	for n >= 0 && c.stack[n].name != name {
		n--
	}
	if n >= 0 {
		c.unwind(n)
	}
}

// unwind closes every environment open from the nth up.
func (c *texConv) unwind(n int) {
	c.closePara()
	for top := len(c.stack) - 1; top >= n; top-- {
		env := c.stack[top]
		switch {
		case env.kind == "table":
			c.w("</td></tr>\n</table>\n")
		case env.tag == "dl" && env.item:
			c.w("</dd>\n</dl>\n")
		case env.kind == "list" && env.item:
			c.w("</li>\n</" + env.tag + ">\n")
		default:
			c.w("</" + env.tag + ">\n")
		}
	}
	c.stack = c.stack[:n]
}

// texToHTML converts a LaTeX source to the HTML the walker reads.
func texToHTML(src string) string {
	c := &texConv{src: src, out: &strings.Builder{}}

	// The preamble is setup, not prose: only its title is read.
	if loc := texDocument.FindStringSubmatchIndex(src); loc != nil {
		c.mute = 1
		c.run(loc[2])
		c.mute, c.para, c.stack, c.notes = 0, false, nil, nil
		c.i = loc[3]
	}

	c.run(len(src))
	c.unwind(0)

	return c.out.String()
}

// lintLaTeX lints a LaTeX source.
func (l *Linter) lintLaTeX(f *core.File) error {
	if err := l.lintMetadata(f); err != nil {
		return err
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	return l.lintHTMLTokens(f, []byte(texToHTML(s)), 0)
}
