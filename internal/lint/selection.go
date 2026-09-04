package lint

import (
	"bytes"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// markAttr names the elements a selector matched, for the walker.
//
// The rendered HTML is the one channel between the tree the selectors run on
// and the token stream the walker reads, and an attribute is what survives
// it. This one is Vale's own, so an author's classes stay theirs.
const markAttr = "data-vale-doc"

// markSelections prepares a document for rules scoped with `doc(...)`.
//
// The walker reads a token stream and never holds the tree a selector needs,
// so the tree is built here, ahead of it: each heading is wrapped with what
// follows it into a `section`, every element a selector matches is marked
// with that selector's id, and the result is rendered back to the stream the
// walker reads. The mark then reaches each block inside the element as an
// `in.<id>` part of its scope, which is what the selector's rule matches on.
//
// This runs only when some rule declares a selector, so a style without one
// reads the document exactly as before.
func markSelections(raw []byte, sels []check.Selection) ([]byte, error) {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return raw, err
	}

	wrapSections(doc)
	for _, s := range sels {
		for _, n := range cascadia.QueryAll(doc, s.Sel) {
			mark(n, s.ID)
		}
	}

	var buf bytes.Buffer
	if err = html.Render(&buf, doc); err != nil {
		return raw, err
	}
	return buf.Bytes(), nil
}

// wrapSections encloses each heading and the siblings that follow it, up to
// the next heading of the same or a higher level, in a `section` element.
//
// Sections nest: an h3 after an h2 sits inside the h2's section. The wrapping
// is done among siblings, so a heading inside a list item or a blockquote
// sections its neighbors there and nothing outside.
func wrapSections(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			wrapSections(c)
		}
	}

	var open []*html.Node
	var levels []int

	c := n.FirstChild
	for c != nil {
		next := c.NextSibling

		if level := headingLevel(c); level > 0 {
			for len(open) > 0 && levels[len(levels)-1] >= level {
				open, levels = open[:len(open)-1], levels[:len(levels)-1]
			}

			sec := &html.Node{Type: html.ElementNode, Data: "section", DataAtom: atom.Section}
			if len(open) > 0 {
				open[len(open)-1].AppendChild(sec)
			} else {
				n.InsertBefore(sec, c)
			}

			n.RemoveChild(c)
			sec.AppendChild(c)

			open, levels = append(open, sec), append(levels, level)
		} else if len(open) > 0 {
			n.RemoveChild(c)
			open[len(open)-1].AppendChild(c)
		}

		c = next
	}
}

// headingLevel returns 1 through 6 for a heading element, and 0 otherwise.
func headingLevel(n *html.Node) int {
	if n.Type != html.ElementNode || len(n.Data) != 2 || n.Data[0] != 'h' {
		return 0
	}
	if level := int(n.Data[1] - '0'); level >= 1 && level <= 6 {
		return level
	}
	return 0
}

func mark(n *html.Node, id string) {
	for i, a := range n.Attr {
		if a.Key == markAttr {
			n.Attr[i].Val = a.Val + " " + id
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: markAttr, Val: id})
}

// lintAbsent lints an empty block for each selection the document never
// opened, so a rule can report that the element it names is missing.
//
// The block sits on line one, which is the only place a missing section can
// be reported. Every check but a count sees nothing and says nothing.
func (l *Linter) lintAbsent(f *core.File, sels []check.Selection, seen map[string]bool) error {
	for _, s := range sels {
		if seen[s.ID] {
			continue
		}
		blk := nlp.NewLinedBlock(f.Content, "", "doc."+s.ID+f.MetaScope+f.RealExt, 0)
		if err := l.lintBlock(f, blk, len(f.Lines), 0, true); err != nil {
			return err
		}
	}
	return nil
}

// lintSelection lints a matched element as one block, for a rule whose scope
// is a `doc(...)` term alone.
//
// The block's scope is `doc` plus the id of every selector that matched the
// element, so an element two selectors both name is linted once. Its context
// is the file, as the summary's is: a match is placed by searching the file
// for it.
func (l *Linter) lintSelection(f *core.File, agg *aggregate) error {
	text := strings.TrimSpace(agg.text.String())
	if text == "" {
		return nil
	}

	scope := "doc." + strings.Join(agg.ids, ".") + f.MetaScope + f.RealExt
	blk := nlp.NewLinedBlock(f.Content, text, scope, agg.line)
	blk.Metrics = agg.metrics

	return l.lintBlock(f, blk, len(f.Lines), 0, true)
}
