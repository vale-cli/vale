package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/adrg/frontmatter"

	"github.com/vale-cli/vale/v3/internal/glob"
	"github.com/vale-cli/vale/v3/internal/system"
)

// condSep splits a section header into its glob and its condition, the way a
// list comprehension's `if` filters its source: `[*.md if .Meta.draft]`.
const condSep = " if "

// SplitSection returns a section header's glob and condition, which is ""
// for a plain section.
func SplitSection(label string) (string, string) {
	if i := strings.Index(label, condSep); i >= 0 {
		return strings.TrimSpace(label[:i]), strings.TrimSpace(label[i+len(condSep):])
	}
	return label, ""
}

// FileFields are what a section's condition reads about a file, in the shape
// `--filter` reads a rule: capitalized, with a leading dot.
type FileFields struct {
	Path string // relative to the root .vale.ini, with forward slashes
	Dir  string
	Name string
	Ext  string
	Meta map[string]any // the front matter, keyed as written
}

// A Condition is a section header's compiled `if` expression.
type Condition struct {
	Source string
	holds  func(FileFields) (bool, error)
	meta   bool // whether the expression reads front matter
}

// NewCondition compiles a section's condition.
func NewCondition(src string) (*Condition, error) {
	holds, err := compileCondition(src)
	if err != nil {
		return nil, err
	}
	return &Condition{Source: src, holds: holds, meta: strings.Contains(src, ".Meta")}, nil
}

// Holds reports whether the condition is true for the file at path.
func (c *Condition) Holds(f FileFields) (bool, error) {
	return c.holds(f)
}

// NewFileFields describes the file at path, holding content. Front matter is
// parsed only when meta is set, since most conditions never read it.
func NewFileFields(path, content string, meta bool, cfg *Config) FileFields {
	f := FileFields{
		Path: filepath.ToSlash(relToRoot(path, cfg)),
		Name: filepath.Base(path),
		Ext:  filepath.Ext(path),
		Meta: map[string]any{},
	}
	f.Dir = filepath.ToSlash(filepath.Dir(f.Path))

	if meta {
		_, err := frontmatter.Parse(strings.NewReader(content), &f.Meta)
		if err != nil && !errors.Is(err, frontmatter.ErrNotFound) {
			f.Meta = map[string]any{}
		}
	}
	return f
}

// heldConditions evaluates the conditions of the sections whose glob matches
// path, and returns the ones that held.
func heldConditions(path, content string, cfg *Config) (map[string]bool, error) {
	held := map[string]bool{}

	var matched []string
	meta := false
	for sec, cond := range cfg.SecToCond {
		if pat, found := cfg.SecToPat[sec]; found && globMatches(pat, path) {
			matched = append(matched, sec)
			meta = meta || cond.meta
		}
	}
	if len(matched) == 0 {
		return held, nil
	}

	fields := NewFileFields(path, content, meta, cfg)
	for _, sec := range matched {
		ok, err := cfg.SecToCond[sec].Holds(fields)
		if err != nil {
			return nil, fmt.Errorf("[%s]: %w", sec, err)
		}
		held[sec] = ok
	}
	return held, nil
}

// SectionApplies reports whether section sec applies to a file: its glob
// matches one of names, and its condition, if it has one, held for the file.
func (c *Config) SectionApplies(sec string, held map[string]bool, names ...string) bool {
	if cond := c.SecToCond[sec]; cond != nil && !held[sec] {
		return false
	}
	pat, found := c.SecToPat[sec]
	if !found {
		// A global key is stored under "*", which no section registers.
		g, _ := SplitSection(sec)
		compiled, err := glob.Compile(g)
		if err != nil {
			return false
		}
		pat = compiled
	}
	for _, n := range names {
		if n != "" && globMatches(pat, n) {
			return true
		}
	}
	return false
}

// globMatches reports whether pat matches name as given or with forward
// slashes, so `[docs/*.md]` matches a path written with the OS separator.
func globMatches(pat glob.Glob, name string) bool {
	return pat.Match(name) || pat.Match(filepath.ToSlash(name))
}

// relToRoot makes path relative to the project root: the directory the
// search found the .vale.ini at, or the file's own directory.
func relToRoot(path string, cfg *Config) string {
	root := cfg.RootDir
	if root == "" && cfg.RootINI != "" {
		root = filepath.Dir(cfg.RootINI)
	}
	if root == "" {
		return path
	}
	rel, _ := system.RelInside(root, path)
	return rel
}
