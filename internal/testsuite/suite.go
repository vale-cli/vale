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
	"sort"
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

	// Rule isolates the case to a single rule, named by a path relative to the
	// test file. Nothing else is loaded -- not the rest of the style, not the
	// project's configuration -- so the case says exactly what the rule does
	// and nothing about what a run would surface.
	//
	// Without it, the case is linted by the configuration a `vale` run in this
	// directory would use, which is what tells you whether the rule reaches
	// real documents at all.
	Rule string `yaml:"rule"`

	// Want is the exact output expected, Contains an excerpt of it, and Absent
	// what must not appear. Want is a pointer so that an empty block asserts
	// "no alerts at all" rather than going unset.
	Want     *string  `yaml:"want"`
	Contains string   `yaml:"contains"`
	Absent   []string `yaml:"absent"`

	// Path is the file this case was read from.
	Path string `yaml:"-"`
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
	if c.Want == nil && c.Contains == "" && len(c.Absent) == 0 {
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

	if core.IsTestFile(filepath.Base(path)) {
		var cases []Case
		if err = yaml.Unmarshal(b, &cases); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		return finish(cases, path, "")
	}

	var rule struct {
		Extends string `yaml:"extends"`
		Tests   []Case `yaml:"tests"`
	}
	if uErr := yaml.Unmarshal(b, &rule); uErr != nil {
		// Not even a mapping -- a stray sequence, or YAML in name only. That
		// is a fact about the file, not a failure of the run.
		return nil, nil //nolint:nilerr // foreign YAML is skipped, not failed
	}
	if rule.Extends == "" || len(rule.Tests) == 0 {
		return nil, nil
	}

	// An in-source case tests the rule it lives in: `rule` defaults to the
	// file itself, which also makes it isolated -- the doctest reading.
	return finish(rule.Tests, path, filepath.Base(path))
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
