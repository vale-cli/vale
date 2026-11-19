package check

import (
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// anchor converts an alert's block-relative rune span into an absolute byte
// span within the document.
//
// Vale otherwise locates an alert by searching the document for the text it
// matched, and masking that text so the next alert from the same rule finds a
// later occurrence. That copies the whole context per alert, which is the bulk
// of Vale's allocation on a document with many findings -- and it mislocates a
// match that appears more than once or that spans irregular whitespace.
//
// An anchored alert needs none of that: it already says exactly where it is.
//
// Anchoring is skipped unless the block's position is known *and* verifiable.
// Blocks carved out of markup hold text that has been stripped of its markup,
// so their offsets do not address the original document; checking that the
// text really sits where the offset claims is what keeps those out.
func anchor(a *core.Alert, blk nlp.Block) {
	if len(a.Span) != 2 {
		return
	}
	a.BlockSpan = []int{a.Span[0], a.Span[1]}

	lo, hi, ok := blk.ByteSpan(a.Span[0], a.Span[1])
	if !ok {
		return
	}

	if blk.Offset < 0 {
		// A block that inline markup has rewritten -- `has <b>has</b>` read as
		// `has has` -- is nowhere in the document to be found, but each of its
		// runs was placed as it was read, and that is enough to say where the
		// match is. The span reaches from the first byte to the last, markup
		// between them included, because that is its extent in the file. See
		// #502.
		if lo == hi {
			// An empty match has no last byte: it sits before the byte at lo,
			// or after the final one when it is at the end.
			at := blk.SourceOffset(lo)
			if at < 0 && lo > 0 {
				if p := blk.SourceOffset(lo - 1); p >= 0 {
					at = p + 1
				}
			}
			if at < 0 {
				return
			}
			a.Span = []int{at, at}
			a.HasByteOffsets = true
			return
		}

		from, to := blk.SourceOffset(lo), blk.SourceOffset(hi-1)
		if from < 0 || to < from {
			return
		}
		a.Span = []int{from, to + 1}
		a.HasByteOffsets = true
		return
	}

	// The offset is only ever set after a successful search for this text, so
	// it is already known good. It cannot be re-checked here: extraction masks
	// text in the context buffer as it goes, so by now the bytes at that
	// position may have been overwritten -- length-preservingly, which is what
	// keeps the offset itself valid.
	if blk.Offset+len(blk.Text) > len(blk.Context) {
		return
	}

	a.Span = []int{blk.Offset + lo, blk.Offset + hi}
	a.HasByteOffsets = true
}

// Reanchor places an alert that anchor has seen before in blk, which holds
// the same text at what may be a new position.
func Reanchor(a *core.Alert, blk nlp.Block) {
	if len(a.BlockSpan) != 2 {
		return
	}
	a.Span = []int{a.BlockSpan[0], a.BlockSpan[1]}
	a.HasByteOffsets = false
	anchor(a, blk)
}

// runeSpanToBytes converts a rune-indexed span into byte offsets by walking
// s. A block converts its own spans without the walk; see nlp.Block.ByteSpan.
func runeSpanToBytes(s string, from, to int) (int, int, bool) {
	return nlp.RuneSpanToBytes(s, from, to)
}
