//go:build vale_noviews

// The build without dasel: a view's `dasel` engine and TOML data files report
// ErrNoViews instead of running.
package core

import "errors"

// ErrNoViews reports a view this build cannot evaluate.
var ErrNoViews = errors.New("this build has no dasel engine")

func selectValues(_ DaselValue, _ string) ([]any, error) { return nil, ErrNoViews }

func unmarshalTOML(_ []byte, _ any) error { return ErrNoViews }
