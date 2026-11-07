//go:build tools

// The grammars are compiled from the C sources the go-tree-sitter bindings
// carry; this file keeps those modules in go.mod for build.sh to find,
// without linking the CGo bindings into Vale.
package grammars

import (
	_ "github.com/jdkato/go-tree-sitter-julia/julia"
	_ "github.com/smacker/go-tree-sitter"
)
