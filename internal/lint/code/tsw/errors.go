package tsw

import "errors"

// ErrNotBuilt is what a build made with the vale_nocode tag reports in place
// of a grammar: it carries no runtime, so comments are read by their
// delimiters instead.
var ErrNotBuilt = errors.New("grammar support is not in this build")
