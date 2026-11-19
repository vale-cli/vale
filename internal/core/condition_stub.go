//go:build vale_noexpr

// The build without the expression engine: a conditional section fails to
// compile, so a configuration that has one is rejected.
package core

import "errors"

// ErrNoExpr reports an expression this build cannot evaluate.
var ErrNoExpr = errors.New("this build has no expression engine")

func compileCondition(_ string) (func(FileFields) (bool, error), error) {
	return nil, ErrNoExpr
}
