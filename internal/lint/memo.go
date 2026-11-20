package lint

import (
	"hash/maphash"
	"slices"
	"sync"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// A blockMemo remembers what the block-only rules found, from one lint to the
// next.
//
// A host that lints as its user types sends the same document again with one
// paragraph changed. A rule in memoKinds reads nothing but its block, so what
// it found in an unchanged block is what it would find again.
//
// It holds two generations: a lint reads both and writes the current one, so
// an entry lives as long as the block it belongs to keeps appearing. An entry
// is a block's, holding each rule's findings by the rule's place in the
// block's rule list, which a linter keeps fixed: one lookup per block rather
// than one per rule.
type blockMemo struct {
	mu        sync.Mutex
	seed      maphash.Seed
	now, last map[memoKey]*memoEntry

	// runs counts the rules run, rather than remembered, since the last
	// rotate.
	runs int
}

// A memoKey names one block of one file.
type memoKey struct {
	hash uint64
	text int
}

// A memoEntry holds what each of a block's rules found, by its place in the
// block's rule list.
type memoEntry struct {
	found [][]core.Alert
	have  []bool
}

func newBlockMemo() *blockMemo {
	return &blockMemo{seed: maphash.MakeSeed(), now: map[memoKey]*memoEntry{}}
}

// rotate starts a lint: what the last one used is kept, the rest is dropped.
func (m *blockMemo) rotate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last, m.now = m.now, map[memoKey]*memoEntry{}
	m.runs = 0
}

// key names a block: what a rule's Run reads from it, and the file whose
// configuration it runs under. Where the block sits in the document is left
// out; see get.
func (m *blockMemo) key(f *core.File, blk nlp.Block) memoKey {
	var h maphash.Hash
	h.SetSeed(m.seed)
	for _, s := range []string{f.Path, blk.Scope, blk.Parent, blk.Text} {
		h.WriteString(s)
		h.WriteByte(0)
	}
	for _, in := range blk.Inline {
		h.WriteString(in.Scope)
		maphash.WriteComparable(&h, [2]int{in.Begin, in.End})
	}
	return memoKey{hash: h.Sum64(), text: len(blk.Text)}
}

// entry returns the block's entry for a rule list of n, carrying it over from
// the last lint if it was there.
func (m *blockMemo) entry(k memoKey, n int) *memoEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.now[k]
	if e == nil {
		e = m.last[k]
	}
	if e == nil || len(e.have) != n {
		e = &memoEntry{found: make([][]core.Alert, n), have: make([]bool, n)}
	}
	m.now[k] = e
	return e
}

// get returns what rule i found in a block holding the same text, placed in
// blk: an alert the rule anchored is anchored again, where the block is now.
func (e *memoEntry) get(i int, blk nlp.Block) ([]core.Alert, bool) {
	if !e.have[i] {
		return nil, false
	}
	out := cloneAlerts(e.found[i])
	for j := range out {
		check.Reanchor(&out[j], blk)
	}
	return out, true
}

// put remembers what rule i found, unless an alert was placed by some means
// that can't be repeated for a block that has moved.
func (m *blockMemo) put(e *memoEntry, i int, alerts []core.Alert) {
	m.mu.Lock()
	m.runs++
	m.mu.Unlock()
	for j := range alerts {
		if alerts[j].HasByteOffsets && len(alerts[j].BlockSpan) != 2 {
			return
		}
	}
	e.found[i] = cloneAlerts(alerts)
	e.have[i] = true
}

// cloneAlerts copies alerts and the slices placing one rewrites.
func cloneAlerts(alerts []core.Alert) []core.Alert {
	out := slices.Clone(alerts)
	for i := range out {
		out[i].Span = slices.Clone(out[i].Span)
		out[i].Offset = slices.Clone(out[i].Offset)
		out[i].BlockSpan = slices.Clone(out[i].BlockSpan)
	}
	return out
}
