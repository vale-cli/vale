package core

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

// A Cell is one cell of a Jupyter notebook: what kind it is, the language
// of its code, and its source placed in the file.
type Cell struct {
	// Type is `markdown`, `code`, or `raw`.
	Type string

	// Lang is the extension of the code's language, such as `.py`, and
	// empty for a language Vale has no grammar for.
	Lang string

	Value ScopedValue
}

// kernelExts maps a kernel's language name to the extension Vale lints it
// as. A name that is not here has no grammar, so its cells are skipped.
var kernelExts = map[string]string{
	"python":     ".py",
	"python2":    ".py",
	"python3":    ".py",
	"ipython":    ".py",
	"r":          ".r",
	"julia":      ".jl",
	"javascript": ".js",
	"typescript": ".ts",
	"rust":       ".rs",
	"go":         ".go",
	"c":          ".cpp",
	"c++":        ".cpp",
	"cpp":        ".cpp",
	"java":       ".java",
	"ruby":       ".rb",
	"lua":        ".lua",
	"php":        ".php",
	"haskell":    ".hs",
	"elixir":     ".ex",
	"clojure":    ".clj",
	"perl":       ".r",
	"csharp":     ".cs",
	"c#":         ".cs",
	"scala":      ".c",
	"powershell": ".ps1",
}

// ParseNotebook reads a `.ipynb` into its cells, in order.
//
// The current layout (nbformat 4) and the one before it (nbformat 3, with
// its worksheets and heading cells) are both read. A cell's source may be
// one string or a list of lines; either way the cell is one value, placed
// by each of its scalars.
func ParseNotebook(f *File) ([]Cell, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(f.Content), &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("a notebook is a JSON object")
	}
	doc := root.Content[0]
	srcLines := strings.Split(f.Content, "\n")

	meta := mapValue(doc, "metadata")
	lang := kernelLang(meta)

	var nodes []*yaml.Node
	if cells := mapValue(doc, "cells"); cells != nil {
		nodes = cells.Content
	} else if sheets := mapValue(doc, "worksheets"); sheets != nil {
		for _, sheet := range sheets.Content {
			if sheetCells := mapValue(sheet, "cells"); sheetCells != nil {
				nodes = append(nodes, sheetCells.Content...)
			}
		}
	} else {
		return nil, errors.New("a notebook has `cells` or `worksheets`")
	}

	found := make([]Cell, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind != yaml.MappingNode {
			continue
		}
		cell := Cell{Type: scalarValue(mapValue(node, "cell_type"))}

		source := mapValue(node, "source")
		if cell.Type == "code" && source == nil {
			source = mapValue(node, "input") // nbformat 3
		}
		if source == nil {
			continue
		}

		switch cell.Type {
		case "heading":
			// nbformat 3 keeps a heading's level apart from its text; the
			// text is placed after the marks it would be written with.
			level := scalarValue(mapValue(node, "level"))
			if level == "" {
				level = "1"
			}
			marks := strings.Repeat("#", max(1, min(6, int(level[0]-'0')))) + " "
			cell.Type = "markdown"
			cell.Value = sourceValue(source, srcLines, marks)
		case "code":
			cell.Lang = cellLang(node, lang)
			cell.Value = sourceValue(source, srcLines, "")
			if magic, ok := cellMagic(cell.Value.Text); ok && (magic == "markdown" || magic == "md") {
				// The cell is prose; the magic line is blanked, not cut,
				// so the rest keeps its place.
				cell.Type = "markdown"
				cell.Value.Text = blankFirstLine(cell.Value.Text)
			}
		case "markdown", "raw":
			cell.Value = sourceValue(source, srcLines, "")
		default:
			continue
		}
		found = append(found, cell)
	}

	return found, nil
}

// mapValue returns the value under key in a mapping node, or nil.
func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// scalarValue returns a scalar node's text, or "" for anything else.
func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

// kernelLang returns the extension of the notebook's kernel language.
func kernelLang(meta *yaml.Node) string {
	name := scalarValue(mapValue(mapValue(meta, "kernelspec"), "language"))
	if name == "" {
		name = scalarValue(mapValue(mapValue(meta, "language_info"), "name"))
	}
	if name == "" {
		name = scalarValue(mapValue(meta, "language")) // nbformat 3
	}
	return kernelExts[strings.ToLower(name)]
}

// cellLang returns the extension of a code cell's language: the cell's own,
// where an editor set one, and the kernel's otherwise.
func cellLang(cell *yaml.Node, kernel string) string {
	name := scalarValue(mapValue(cell, "language")) // nbformat 3
	if name == "" {
		name = scalarValue(mapValue(mapValue(mapValue(cell, "metadata"), "vscode"), "languageId"))
	}
	if ext, ok := kernelExts[strings.ToLower(name)]; ok {
		return ext
	}
	return kernel
}

// cellMagic returns the name of the cell magic a code cell starts with.
func cellMagic(text string) (string, bool) {
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	if !strings.HasPrefix(first, "%%") {
		return "", false
	}
	name, _, _ := strings.Cut(first[2:], " ")
	return name, true
}

// blankFirstLine replaces a text's first line with spaces of the same width.
func blankFirstLine(text string) string {
	first, rest, ok := strings.Cut(text, "\n")
	blank := strings.Repeat(" ", nlp.StrLen(first))
	if !ok {
		return blank
	}
	return blank + "\n" + rest
}

// sourceValue joins a cell's source, one string or a list of them, into one
// value placed by each of its scalars. A prefix is text the file does not
// hold, so the value's parts start after it.
func sourceValue(source *yaml.Node, srcLines []string, prefix string) ScopedValue {
	var nodes []*yaml.Node
	switch {
	case source.Kind == yaml.ScalarNode:
		nodes = []*yaml.Node{source}
	case source.Kind == yaml.SequenceNode:
		nodes = source.Content
	}

	sv := ScopedValue{Joined: source.Kind == yaml.SequenceNode}
	var text strings.Builder
	text.WriteString(prefix)

	offset := nlp.StrLen(prefix)
	for _, node := range nodes {
		if node.Kind != yaml.ScalarNode {
			continue
		}
		sp := scalarAt(node, srcLines)
		if len(sv.Parts) == 0 {
			sv.Line, sv.Column = sp.Line, sp.Column
		}
		sv.Parts = append(sv.Parts, newPart(srcLines, sp, offset))
		text.WriteString(node.Value)
		offset += nlp.StrLen(node.Value)
	}

	sv.Text = text.String()
	return sv
}

// String describes a cell for test output.
func (c Cell) String() string {
	return fmt.Sprintf("%s%s@%d:%d %q", c.Type, c.Lang, c.Value.Line, c.Value.Column, c.Value.Text)
}
