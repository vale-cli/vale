// Code linting: parse source with tree-sitter, extract the regions a
// language's queries capture (comments by default, or a `tree-sitter` View's
// scopes), and lint each. A grammar is a WebAssembly module installed in the
// StylesPath and run in pure Go, so every build reads code the same way.
package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code"
	"github.com/vale-cli/vale/v3/internal/lint/code/tsw"
)

// grammarLoader finds a grammar's module for the configuration: under
// config/grammars on the StylesPath, where `vale sync` installs it, else in
// the directory VALE_GRAMMARS_PATH names. Read through the configuration's
// filesystem, so the browser build finds what its host added.
func grammarLoader(cfg *core.Config) func(name string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		file := name + ".wasm"
		if path := core.FindConfigAsset(cfg, file, core.GrammarDir); path != "" {
			return cfg.FS.ReadFile(path)
		}
		if dir := os.Getenv("VALE_GRAMMARS_PATH"); dir != "" {
			if path := filepath.Join(dir, file); cfg.FS.Exists(path) {
				return cfg.FS.ReadFile(path)
			}
		}
		return nil, &code.NotInstalledError{Name: name}
	}
}

// missingGrammars holds each grammar already warned about, so a run says
// it once however many files it reads.
var missingGrammars sync.Map

// grammarMissing reports whether err is a grammar that isn't installed, or
// a build that has no grammar runtime at all, warning the first time. The
// file is still read, by its comment delimiters, so a project that never
// asked for grammars lints as it did.
func grammarMissing(err error) bool {
	if errors.Is(err, tsw.ErrNotBuilt) {
		if _, seen := missingGrammars.LoadOrStore("", true); !seen {
			core.Warn(err.Error() + "; comments are read by their delimiters")
		}
		return true
	}
	var missing *code.NotInstalledError
	if !errors.As(err, &missing) {
		return false
	}
	if _, seen := missingGrammars.LoadOrStore(missing.Name, true); !seen {
		core.Warn(missing.Error() + "; until then, comments are read by their delimiters")
	}
	return true
}

// languageFor returns the tree-sitter grammar for a file. When the file's own
// extension has none, the format it is read as supplies it: `NormedExt` holds
// what `[formats]` or `--ext` resolved the file to, so `es = js` parses a
// `.es` file as JavaScript.
func (l *Linter) languageFor(f *core.File) (*code.Language, error) {
	lang, err := code.GetLanguageFromExt(f.RealExt)
	if err == nil {
		return lang, nil
	}
	if mapped, mappedErr := code.GetLanguageFromExt(f.NormedExt); mappedErr == nil {
		return mapped, nil
	}
	return nil, err
}

func (l *Linter) lintCode(f *core.File) error {
	lang, err := l.languageFor(f)
	if err != nil {
		// No tree-sitter grammar available for this file type.
		return l.lintCodeOld(f)
	}

	if found := updateQueries(f, l.Manager.Config); len(found) > 0 {
		lang.Queries = found
	}

	comments, err := code.GetComments([]byte(f.Content), lang)
	if err != nil {
		if grammarMissing(err) {
			return l.lintCodeOld(f)
		}
		return err
	}
	wholeFile := f.Content

	converted := l.rstBatch(f, comments, "")

	last := len(f.Alerts) // the file may hold alerts from cells before this one
	for i, comment := range comments {
		f.SetMetaScope(comment.Scope)
		if l.skipsComment(comment.Scope) {
			continue
		}
		err = l.lintComment(f, comment, comment.Format, converted[i])
		if err != nil {
			return err
		}
		f.Alerts = dropMasked(f.Alerts, last, comment)

		size := len(f.Alerts)
		if size != last {
			f.Alerts = adjustAlerts(f.Alerts, last, comment)
		}
		last = size
	}

	f.SetText(wholeFile)
	if f.Summary.Len() > 0 {
		// The comments read as markup added up to a summary; see lintSizedScopes.
		return l.lintSummary(f)
	}
	return nil
}

// rstBatch converts every comment written in reStructuredText in one
// Docutils call, and returns each one's HTML by index. fallback is the
// format a comment has when its query named none. Nothing is returned when
// fewer than two comments qualify or the batch fails, and each comment is
// then converted on its own.
func (l *Linter) rstBatch(f *core.File, comments []code.Comment, fallback string) map[int]string {
	var (
		docs []string
		idx  []int
	)

	// Each comment is prepared as the fragment it will be linted as; see
	// lintComment.
	wholeFile, kind, ext := f.Content, f.Format, f.NormedExt
	f.Format = "fragment"
	f.SetNormedExt("rst")
	defer func() {
		f.Format, f.NormedExt = kind, ext
		f.SetText(wholeFile)
	}()

	for i, c := range comments {
		format := c.Format
		if format == "" {
			format = fallback
		}
		if format != "rst" || l.skipsComment(c.Scope) {
			continue
		}

		f.SetText(c.Text)
		s, err := l.prepareRST(f)
		if err != nil {
			return nil
		}
		docs = append(docs, s)
		idx = append(idx, i)
	}

	if len(docs) < 2 {
		return nil
	}
	parts, err := l.convertRSTBatch(docs)
	if err != nil {
		return nil
	}

	out := make(map[int]string, len(idx))
	for k, i := range idx {
		out[i] = parts[k]
	}
	return out
}

// dropMasked removes the alerts, from last on, that the comment's
// documentation convention masks: the symbol a tag names, the body of an
// example. The alerts are still placed within the comment's text.
func dropMasked(alerts []core.Alert, last int, comment code.Comment) []core.Alert {
	if len(comment.Masks) == 0 {
		return alerts
	}
	kept := alerts[:last]
	for _, a := range alerts[last:] {
		if !comment.Masked(a.Line, a.Span[0], a.Span[1]) {
			kept = append(kept, a)
		}
	}
	return kept
}

// lintComment lints one comment as the markup named -- `md`, `rst`, `html`,
// `org`, `adoc`, or `qdoc` -- or as plain lines when none is.
//
// A View's scope names the markup its comments are written in, so a fenced
// block in a Rust `///` comment is code and a `<pre>` in a Javadoc block is
// not prose. The file is a fragment of that markup while the comment is
// read, and itself again afterwards.
//
// html is the comment already converted, when it was part of a batch (see
// rstBatch), and empty otherwise.
func (l *Linter) lintComment(f *core.File, comment code.Comment, format, html string) error {
	if format == "" {
		f.SetText(maskURLs(comment.Text))
		return l.lintLines(f)
	}

	kind, ext := f.Format, f.NormedExt
	f.Format = "fragment"
	f.SetNormedExt(format)
	f.SetText(comment.Text)

	var err error
	switch f.NormedExt {
	case ".md":
		err = l.lintMarkdown(f)
	case ".rst":
		if html != "" {
			err = l.lintRSTHTML(f, html)
		} else {
			err = l.lintRST(f)
		}
	case ".html":
		err = l.lintHTML(f)
	case ".adoc":
		err = l.lintADoc(f)
	case ".org":
		err = l.lintOrg(f)
	case ".qdoc":
		err = l.lintQDocFragment(f)
	default:
		err = fmt.Errorf("unsupported markup format '%s'", format)
	}

	f.Format, f.NormedExt = kind, ext
	return err
}

// lintCodeOld lints source code by analyzing its comments.
//
// Deprecated: we now use tree-sitter to parse code and collect comments.

// lintDataComments lints the comments of a data file that has a grammar --
// YAML or TOML -- and does nothing for one that has no comments to read.
func (l *Linter) lintDataComments(f *core.File) error {
	if _, err := code.GetLanguageFromExt(f.RealExt); err == nil {
		return l.lintCode(f)
	}
	return nil
}
