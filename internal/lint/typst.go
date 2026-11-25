package lint

import (
	"errors"
	"strings"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/typst"
	"github.com/vale-cli/vale/v3/internal/system"
)

// Typst is its own grammar, so Vale reads it with its own parser:
// typst2vast, which parses documents with typst-syntax -- the compiler's
// parser, error-tolerant and evaluation-free -- and emits the HTML the
// walker reads. Nothing is compiled or executed: a document that doesn't
// build still lints, no package is ever fetched, and no show rule can make
// the linted text differ from the source. See #651.
//
// typst2vast is embedded as a WebAssembly module (internal/lint/typst), so
// there is nothing to install. A build without the runtime runs the
// installed program instead.

// typst2vast converts one document per process unless asked otherwise;
// `--batch` keeps the process up and takes documents over the same framing
// the other converter pools use. Support is probed rather than assumed, so
// an older install falls back to one process per file.
var (
	typstOnce   sync.Once
	typstDirect []string
)

func typstFastPath() []string {
	typstOnce.Do(func() {
		exe := system.Which([]string{"typst2vast"})
		if exe == "" {
			return
		}

		candidate := []string{exe, "--batch"}

		probe, err := startExtProc(candidate, nil)
		if err != nil {
			return
		}
		defer probe.close()

		// Non-ASCII on purpose: encoding is what broke the equivalent path
		// for AsciiDoc, and an ASCII-only probe would not have caught it.
		got, err := probe.convert("= naïve heading\n")
		if err != nil || !strings.Contains(got, "naïve heading") {
			return
		}

		typstDirect = candidate
	})

	return typstDirect
}

// lintTypst lints Typst: its markup layer, parsed without evaluation.
func (l *Linter) lintTypst(f *core.File) error {
	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	html, err := l.convertTypst(s)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	return l.lintTypstHTML(f, html)
}

// convertTypst converts one document with the embedded reader, or with the
// installed typst2vast in a build that has none.
func (l *Linter) convertTypst(text string) (string, error) {
	if typst.Built {
		return typst.Convert(text)
	}

	exe := system.Which([]string{"typst2vast"})
	if exe == "" {
		return "", errors.New("typst2vast not found; it's needed to read Typst")
	}
	return l.callTypst(text, exe)
}

// lintTypstHTML lints a document from typst2vast's HTML for it.
func (l *Linter) lintTypstHTML(f *core.File, html string) error {
	if err := l.lintMetadata(f); err != nil {
		return err
	}
	return l.lintHTMLTokens(f, []byte(html), 0)
}

// callTypst converts one document, over a pooled process when the installed
// typst2vast supports it.
func (l *Linter) callTypst(text, exe string) (string, error) {
	if direct := typstFastPath(); direct != nil {
		l.typstPoolOnce.Do(func() {
			pool, err := newProcPool(direct, nil, l.poolSize())
			if err == nil {
				l.typst = pool
			}
		})

		if l.typst != nil {
			return l.typst.convert(text, direct, nil)
		}
	}

	return system.ExecuteWithInput(exe, text)
}
