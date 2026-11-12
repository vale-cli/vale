//go:build !vale_nocode

package tsw

// handle widens a node handle for a call; handles are never negative here.
func handle(h int32) uint64 { return uint64(h) } //nolint:gosec // see i32

// A Point is a position in the source, zero-based.
type Point struct {
	Row, Column uint32
}

// A Node is a handle into the tree's node table, good until the tree is
// closed.
type Node struct {
	tree *Tree
	h    int32
}

func (n *Node) node(h int32) *Node {
	if h < 0 {
		return nil
	}
	return &Node{tree: n.tree, h: h}
}

// Type is the node's kind in the grammar: "comment", "function_declaration".
func (n *Node) Type() string {
	in := n.tree.inst
	return in.str(in.call(in.nodeType, handle(n.h)))
}

// StartByte is where the node begins in the source.
func (n *Node) StartByte() uint32 {
	return u32(n.tree.inst.call(n.tree.inst.nodeStart, handle(n.h)))
}

// EndByte is where the node ends in the source, exclusive.
func (n *Node) EndByte() uint32 {
	return u32(n.tree.inst.call(n.tree.inst.nodeEnd, handle(n.h)))
}

// StartPoint is the node's starting row and column.
func (n *Node) StartPoint() Point {
	in := n.tree.inst
	return Point{
		Row:    u32(in.call(in.nodeRow, handle(n.h))),
		Column: u32(in.call(in.nodeCol, handle(n.h))),
	}
}

// Content is the node's text within src, which must be what was parsed.
func (n *Node) Content(src []byte) string {
	return string(src[n.StartByte():n.EndByte()])
}

// NamedChildCount is how many named children the node has.
func (n *Node) NamedChildCount() uint32 {
	return u32(n.tree.inst.call(n.tree.inst.namedChildCount, handle(n.h)))
}

// NamedChild is the node's i-th named child, or nil.
func (n *Node) NamedChild(i int) *Node {
	return n.node(i32(n.tree.inst.call(n.tree.inst.namedChild, handle(n.h), uint64(i)))) //nolint:gosec // i is a count
}

// NextNamedSibling is the named node after this one, or nil.
func (n *Node) NextNamedSibling() *Node {
	return n.node(i32(n.tree.inst.call(n.tree.inst.nextNamedSibling, handle(n.h))))
}

// ChildByFieldName is the child the grammar names field, or nil.
func (n *Node) ChildByFieldName(field string) *Node {
	in := n.tree.inst
	ptr := in.write([]byte(field))
	h := i32(in.call(in.childByField, handle(n.h), uint64(ptr), uint64(len(field))))
	in.call(in.release, uint64(ptr))
	return n.node(h)
}

// String is the node as an S-expression, for tests and debugging.
func (n *Node) String() string {
	in := n.tree.inst
	ptr := in.call(in.nodeSexp, handle(n.h))
	s := in.str(ptr)
	in.call(in.release, ptr)
	return s
}
