//go:build !vale_noexpr

package check

import "github.com/expr-lang/expr"

// runFilter evaluates a compiled `--filter` expression over the rules.
func runFilter(code string, env FilterEnv) ([]any, error) {
	program, err := expr.Compile(code, expr.Env(env))
	if err != nil {
		return nil, err
	}
	output, err := expr.Run(program, env)
	if err != nil {
		return nil, err
	}
	return output.([]any), nil
}
