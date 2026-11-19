//go:build !vale_noexpr

package core

import (
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
)

// condEnv wraps the file the way a filter wraps the rules, so `.Path` means
// this file's path.
type condEnv struct {
	Files []FileFields
}

// compileCondition compiles src into a test of one file.
func compileCondition(src string) (func(FileFields) (bool, error), error) {
	program, err := expr.Compile(fmt.Sprintf("any(Files, {%s})", src),
		expr.Env(condEnv{}), expr.AsBool(), expr.Patch(missingIsFalse{}))
	if err != nil {
		return nil, err
	}
	return func(f FileFields) (bool, error) {
		out, runErr := expr.Run(program, condEnv{Files: []FileFields{f}})
		if runErr != nil {
			return false, runErr
		}
		return out.(bool), nil
	}, nil
}

// missingIsFalse reads a field used as a condition as false when it's
// missing: `.Meta.draft` on a file with no `draft` is nil, which `or`, `not`,
// and the expression as a whole would otherwise reject.
type missingIsFalse struct{}

func (missingIsFalse) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.BinaryNode:
		if n.Operator == "and" || n.Operator == "or" || n.Operator == "&&" || n.Operator == "||" {
			n.Left, n.Right = orFalse(n.Left), orFalse(n.Right)
		}
	case *ast.UnaryNode:
		if n.Operator == "not" || n.Operator == "!" {
			n.Node = orFalse(n.Node)
		}
	case *ast.PredicateNode:
		n.Node = orFalse(n.Node)
	}
}

func orFalse(n ast.Node) ast.Node {
	if _, ok := n.(*ast.MemberNode); ok {
		return &ast.BinaryNode{Operator: "??", Left: n, Right: &ast.BoolNode{Value: false}}
	}
	return n
}
