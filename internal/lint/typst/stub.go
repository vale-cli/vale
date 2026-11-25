//go:build vale_nocode

// Package typst reads Typst with an embedded typst2vast. This is the build
// without it: the vale_nocode tag leaves the WebAssembly runtime out.
package typst

import "errors"

// Built reports whether this build carries the reader.
const Built = false

// Convert reports that this build has no reader.
func Convert(_ string) (string, error) {
	return "", errors.New("typst: this build has no reader")
}
