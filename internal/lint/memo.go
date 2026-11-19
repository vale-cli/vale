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
// paragraph changed. A rule in concurrentKinds reads nothing but its block, so
// what it found in an unchanged block is what it would find again.
//
// It holds two generations: a lint reads both and writes the current one, so
// an entry lives as long as the block it belongs to keeps appearing.
type blockMemo struct {
	mu        sync.Mutex
	seed      maphash.Seed
	now, last map[memoKey][]core.Alert
}

// A memoKey names one rule's run over one block of one file.
type memoKey struct {
	rule string
	hash uint64
	text int
}

func newBlockMemo() *blockMemo {
	return &blockMemo{seed: maphash.MakeSeed(), now: map[memoKey][]core.Alert{}}
}

// rotate starts a lint: what the last one used is kept, the rest is dropped.
func (m *blockMemo) rotate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last, m.now = m.now, map[memoKey][]core.Alert{}
}

// key is the part of a memoKey a block's rules share: what a rule's Run reads
// from the block, and the file whose configuration it runs under. Where the
// block sits in the document is left out; see get.
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

// get returns what a rule found in a block holding the same text, placed in
// blk: an alert the rule anchored is anchored again, where the block is now.
func (m *blockMemo) get(k memoKey, blk nlp.Block) ([]core.Alert, bool) {
	m.mu.Lock()
	alerts, ok := m.now[k]
	if !ok {
		if alerts, ok = m.last[k]; ok {
			m.now[k] = alerts
		}
	}
	m.mu.Unlock()
	if !ok {
		return nil, false
	}

	out := cloneAlerts(alerts)
	for i := range out {
		check.Reanchor(&out[i], blk)
	}
	return out, true
}

// put remembers what a rule found, unless an alert was placed by some means
// that can't be repeated for a block that has moved.
func (m *blockMemo) put(k memoKey, alerts []core.Alert) {
	for i := range alerts {
		if alerts[i].HasByteOffsets && len(alerts[i].BlockSpan) != 2 {
			return
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now[k] = cloneAlerts(alerts)
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
