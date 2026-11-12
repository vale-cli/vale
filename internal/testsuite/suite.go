// Package testsuite runs the test cases a configuration keeps beside its
// rules.
//
// A rule is a small program, and nobody maintains a significant Vale
// configuration without some way to ask "does this still fire where I think it
// does?". The cases are YAML, they live next to what they test, and they are
// run by `vale test`. See #1122.
package testsuite

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vale-cli/vale/v3/internal/core"
)

// A Case is one document and what linting it must produce.
type Case struct {
	// Name identifies the case in the report. It is required, and unique
	// within its file.
	Name string `yaml:"name"`

	// About is a note for the reader -- an issue number, or what the case is
	// really pinning down. Never asserted on.
	About string `yaml:"about"`

	// Input is the document to lint.
	Input string `yaml:"input"`

	// Format is the extension Input is read as, with or without the dot.
	// Defaults to `md`.
	Format string `yaml:"format"`

	// View names a View, found under a search path's `config/views`, to
	// read Input through. An isolated case loads nothing from the project's
	// configuration, and that includes the View that gives a rule its scope:
	// a rule scoped to `subject` sees no subject without one. With it, the
	// case runs the way the project's section would run it, and the cases
	// of a View's rules can live beside them.
	View string `yaml:"view"`

	// Lang is the language Input is written in, as `Lang` names it, for an
	// isolated case: the rule runs with that language's segmenter and
	// tagger, read from config/models on the search path. A project case
	// takes its language from the project's sections instead.
	Lang string `yaml:"lang"`

	// Rule isolates the case to a single rule, named by a path relative to the
	// test file. Nothing else is loaded -- not the rest of the style, not the
	// project's configuration -- so the case says exactly what the rule does
	// and nothing about what a run would surface.
	//
	// Without it, the case is linted by the configuration a `vale` run in this
	// directory would use, which is what tells you whether the rule reaches
	// real documents at all.
	Rule string `yaml:"rule"`

	// Want is the exact output expected, Contains excerpts of it, and Absent
	// what must not appear. Want is a pointer so that an empty block asserts
	// "no alerts at all" rather than going unset.
	Want     *string `yaml:"want"`
	Contains Strings `yaml:"contains"`
	Absent   Strings `yaml:"absent"`

	// Path is the file this case was read from.
	Path string `yaml:"-"`
}

// Strings is one string or a list of them, so `contains: T.Rule` and a
// list of excerpts read the same way.
type Strings []string

// UnmarshalYAML accepts a scalar as a one-item list.
func (s *Strings) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*s = Strings{node.Value}
		return nil
	}
	var list []string
	if err := node.Decode(&list); err != nil {
		return err
	}
	*s = list
	return nil
}

// Ext is the extension Input is linted as, dot included.
func (c Case) Ext() string {
	format := c.Format
	if format == "" {
		format = "md"
	}
	return "." + strings.TrimPrefix(format, ".")
}

// validate reports a case that cannot be run, rather than one that fails.
func (c Case) validate(seen map[string]bool) error {
	where := fmt.Sprintf("%s: %q", filepath.Base(c.Path), c.Name)

	if c.Name == "" {
		return fmt.Errorf("%s: every case needs a name", filepath.Base(c.Path))
	} else if seen[c.Name] {
		return fmt.Errorf("%s: duplicate name", where)
	}
	seen[c.Name] = true

	// A case that asserts nothing passes whatever the rule does, which is
	// worse than having no case at all: it reads as coverage.
	if c.Want == nil && len(c.Contains) == 0 && len(c.Absent) == 0 {
		return fmt.Errorf("%s: needs one of `want`, `contains`, or `absent`", where)
	}

	return nil
}

// Load reads the cases in one file.
//
// A `.test.yml` holds a sequence of cases and is validated strictly -- it
// exists for no other reason. Any other YAML file is considered only if it is
// a rule carrying its own cases: a mapping with `extends` and a `tests`
// sequence. Everything else -- workflows, configs, data files -- is silently
// not ours, so `vale test` can walk a whole repository without tripping on
// YAML it has no business reading.
func Load(path string) ([]Case, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, b)
}

// Parse reads the cases in one file's contents, with Load's rules, for a
// caller that holds the file already.
func Parse(path string, b []byte) ([]Case, error) {
	var err error

	if core.IsTestFile(filepath.Base(path)) {
		var cases []Case
		if err = yaml.Unmarshal(b, &cases); err != nil {
			return nil, yamlError(path, err)
		}
		return finish(cases, path, "")
	}

	var rule struct {
		Extends string `yaml:"extends"`
		Tests   []Case `yaml:"tests"`
	}
	if uErr := yaml.Unmarshal(b, &rule); uErr != nil {
		// A rule carrying cases that doesn't parse is a mistake in it -- an
		// unquoted `input: Note: this`, say -- and skipping it would drop
		// its cases from the run unannounced. Anything else that doesn't
		// parse isn't ours: `.eslintrc.yml` has an `extends` too.
		if ruleKey.Match(b) && testsKey.Match(b) {
			return nil, yamlError(path, uErr)
		}
		return nil, nil
	}
	if rule.Extends == "" || len(rule.Tests) == 0 {
		return nil, nil
	}

	// An in-source case tests the rule it lives in: `rule` defaults to the
	// file itself, which also makes it isolated -- the doctest reading.
	return finish(rule.Tests, path, filepath.Base(path))
}

var (
	ruleKey  = regexp.MustCompile(`(?m)^extends:`)
	testsKey = regexp.MustCompile(`(?m)^tests:`)
	yamlLine = regexp.MustCompile(`^yaml: line (\d+): `)
)

// yamlError places a YAML error at the line the parser names.
func yamlError(path string, err error) error {
	m := yamlLine.FindStringSubmatch(err.Error())
	if m == nil {
		return core.NewE201FromPosition(err.Error(), path, 1)
	}
	line, _ := strconv.Atoi(m[1])
	return core.NewE201FromPosition(strings.TrimPrefix(err.Error(), m[0]), path, line)
}

func finish(cases []Case, path, defaultRule string) ([]Case, error) {
	seen := map[string]bool{}
	for i := range cases {
		cases[i].Path = path
		if cases[i].Rule == "" {
			cases[i].Rule = defaultRule
		}
		if err := cases[i].validate(seen); err != nil {
			return nil, err
		}
	}
	return cases, nil
}

// Find resolves what to run.
//
// A file is taken as given, a directory is searched, and no argument at all
// searches the working directory -- which finds a style's cases wherever the
// StylesPath happens to be.
func Find(paths []string) ([]string, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var found []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		if !info.IsDir() {
			found = append(found, path)
			continue
		}

		if err = walk(path, &found); err != nil {
			return nil, err
		}
	}

	sort.Strings(found)
	return found, nil
}

func walk(root string, found *[]string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()
		if d.IsDir() {
			// A dot directory is somebody else's -- `.git`, `.venv`. The root
			// itself is exempt, since `vale test .vale/tests` names one.
			if path != root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if core.IsTestFile(name) || strings.HasSuffix(name, ".yml") ||
			strings.HasSuffix(name, ".yaml") {
			*found = append(*found, path)
		}

		return nil
	})
}
