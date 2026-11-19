//go:build vale_noconvert

// The build without the reStructuredText, AsciiDoc, and Org converters. A
// document in one of them is linted from HTML the host supplies (see
// lintBuilt) or not at all.
package lint

import (
	"errors"

	"github.com/vale-cli/vale/v3/internal/core"
)

// ErrNoConverter reports a format this build cannot convert to HTML.
var ErrNoConverter = errors.New("this build has no converter for the format")

func (l *Linter) convertRST(_ string) (string, error) { return "", ErrNoConverter }

func convertADoc(_ string, _ map[string]string) (string, error) { return "", ErrNoConverter }

func (l *Linter) lintOrg(f *core.File) error { return core.NewE100(f.Path, ErrNoConverter) }
