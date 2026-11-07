//go:build js

// The wasm build excludes the handlers that need an external tool (xsltproc
// for XML, dita-ot for DITA). The formats are still classified and
// dispatched; here they degrade to a no-op, so a document in one of them
// produces no alerts rather than failing to compile.
package lint

import "github.com/vale-cli/vale/v3/internal/core"

func (l *Linter) lintXML(_ *core.File) error { return nil }

func (l *Linter) lintDITA(_ *core.File) error { return nil }

func (l *Linter) prepareDITA(_ []string) {}
