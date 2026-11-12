//go:build vale_nocode

// Package tsw runs tree-sitter grammars compiled to WebAssembly. This is the
// build without it: the vale_nocode tag leaves the runtime out, Load reports
// ErrNotBuilt, and nothing below is ever reached.
package tsw

// Built reports whether this build carries the runtime.
const Built = false

// A Language is one compiled grammar module.
type Language struct{ name string }

// Load reports that this build has no runtime for the module.
func Load(_ string, _ []byte) (*Language, error) { return nil, ErrNotBuilt }

// Name labels the grammar in errors.
func (l *Language) Name() string { return l.name }

// Parse reports that this build has no runtime.
func (l *Language) Parse(_ []byte) (*Tree, error) { return nil, ErrNotBuilt }

// A Tree is a parsed source.
type Tree struct{}

// RootNode is the tree's root.
func (t *Tree) RootNode() *Node { return nil }

// Close releases the tree.
func (t *Tree) Close() {}

// Query compiles expr for the tree's grammar.
func (t *Tree) Query(_ string) (*Query, error) { return nil, ErrNotBuilt }

// Matches runs q over the tree.
func (t *Tree) Matches(_ *Query, _ []byte) []Match { return nil }

// A Query is a compiled pattern.
type Query struct{}

// CaptureNameForID is the name a capture was given in the query.
func (q *Query) CaptureNameForID(_ uint32) string { return "" }

// A Match is one occurrence of a query's pattern.
type Match struct {
	PatternIndex uint32
	Captures     []Capture
}

// A Capture is one captured node.
type Capture struct {
	Index uint32
	Node  *Node
}

// A Point is a position in the source, zero-based.
type Point struct {
	Row, Column uint32
}

// A Node is a node of a tree.
type Node struct{}

// Type is the node's kind in the grammar.
func (n *Node) Type() string { return "" }

// StartByte is where the node begins in the source.
func (n *Node) StartByte() uint32 { return 0 }

// EndByte is where the node ends in the source, exclusive.
func (n *Node) EndByte() uint32 { return 0 }

// StartPoint is the node's starting row and column.
func (n *Node) StartPoint() Point { return Point{} }

// Content is the node's text within src.
func (n *Node) Content(_ []byte) string { return "" }

// NamedChildCount is how many named children the node has.
func (n *Node) NamedChildCount() uint32 { return 0 }

// NamedChild is the node's i-th named child, or nil.
func (n *Node) NamedChild(_ int) *Node { return nil }

// NextNamedSibling is the named node after this one, or nil.
func (n *Node) NextNamedSibling() *Node { return nil }

// ChildByFieldName is the child the grammar names field, or nil.
func (n *Node) ChildByFieldName(_ string) *Node { return nil }

// String is the node as an S-expression.
func (n *Node) String() string { return "" }
