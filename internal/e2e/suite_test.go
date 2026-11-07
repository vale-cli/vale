package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// A suite is one testdata/e2e/*.yaml file: a group of cases that share a
// working directory.
type suite struct {
	// Name labels the suite in test output; it may contain a `/`.
	Name string `yaml:"name"`

	// Dir is the cases' working directory, relative to testdata/. A trailing
	// `/*` means each case runs in a directory named after it.
	Dir string `yaml:"dir"`

	// Files every case in the suite starts from, keyed by name. A suite that
	// declares them doesn't need a Dir: each case runs in a scratch directory
	// built from these plus its own.
	Files map[string]string `yaml:"files"`

	Cases []testCase `yaml:"cases"`

	Path string `yaml:"-"` // the file this was read from
}

type testCase struct {
	// Name is unique within the suite. It may contain a `/` to group related
	// cases under a common prefix.
	Name string `yaml:"name"`

	// About is a note for the reader -- an issue number, or what the case is
	// really pinning down.
	About string `yaml:"about"`

	// Dir overrides the suite's working directory. A relative path is resolved
	// against it; `../` escapes it.
	Dir string `yaml:"dir"`

	// Files the case needs, keyed by name; they're written over the suite's.
	// Declaring any means the case runs in a scratch directory of its own
	// rather than in a checked-in fixture.
	Files map[string]string `yaml:"files"`

	// Args are passed to Vale after the shared flags. Write them as one string
	// and they're split on spaces, honoring quotes; write a list to pass words
	// through verbatim.
	Args words `yaml:"args"`

	// Requires lists the external converters Vale shells out to for this
	// case's formats. The case is skipped where they aren't installed, so the
	// suite runs on any machine -- see CONTRIBUTING for the full toolchain.
	Requires tools `yaml:"requires"`

	// Stdin names a file in the working directory to pipe in.
	Stdin string `yaml:"stdin"`

	// Sync runs `vale sync` before the case.
	Sync bool `yaml:"sync"`

	// Env is set for the case's commands, the sync included. A relative path
	// in a value is read from the working directory.
	Env map[string]string `yaml:"env"`

	// Exit is the expected exit status.
	Exit int `yaml:"exit"`

	// Want is the exact output expected, and Contains an excerpt of it. Want is
	// a pointer so that an empty block still asserts "no output at all".
	Want     *string `yaml:"want"`
	Contains string  `yaml:"contains"`

	// Absent lists strings the output must not contain.
	Absent []string `yaml:"absent"`
}

// workdir returns the absolute directory a case runs in.
func (s *suite) workdir(tc testCase) string {
	if s.inline(tc) {
		// Two levels under testdata/, matching a checked-in fixture, so that a
		// relative `StylesPath` in an inline config resolves the same way.
		return filepath.Join(testdata, "tmp", slug(s.Name)+"-"+slug(tc.Name))
	}

	base, star := strings.CutSuffix(s.Dir, "/*")

	dir := tc.Dir
	if dir == "" && star {
		dir = tc.Name
	}

	return filepath.Join(testdata, filepath.FromSlash(base), filepath.FromSlash(dir))
}

// inline reports whether the case brings its own files rather than running in
// a checked-in fixture.
func (s *suite) inline(tc testCase) bool {
	return len(s.Files) > 0 || len(tc.Files) > 0
}

// build writes a case's files into a scratch directory of its own.
func (s *suite) build(tc testCase) (string, error) {
	dir := s.workdir(tc)

	if err := os.RemoveAll(dir); err != nil {
		return "", err
	} else if err = os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", err
	}

	for _, set := range []map[string]string{s.Files, tc.Files} {
		for name, body := range set {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
				return "", err
			} else if err = os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec
				return "", err
			}
		}
	}

	return dir, nil
}

// slug makes a name safe for a directory.
func slug(s string) string {
	return strings.NewReplacer("/", "-", " ", "-").Replace(s)
}

// loadSuites reads every *.yaml in dir, in filename order.
func loadSuites(dir string) ([]*suite, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	} else if len(paths) == 0 {
		return nil, fmt.Errorf("no suites found in %s", dir)
	}
	sort.Strings(paths)

	seen := map[string]string{}
	suites := make([]*suite, 0, len(paths))

	for _, path := range paths {
		b, rErr := os.ReadFile(path)
		if rErr != nil {
			return nil, rErr
		}

		s := &suite{Path: path}
		if err = yaml.Unmarshal(b, s); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		if err = s.validate(seen); err != nil {
			return nil, err
		}

		suites = append(suites, s)
	}

	return suites, nil
}

// validate rejects the mistakes that would otherwise make a case silently
// assert nothing.
func (s *suite) validate(seen map[string]string) error {
	where := filepath.Base(s.Path)

	if s.Name == "" {
		return fmt.Errorf("%s: missing `name`", where)
	} else if len(s.Cases) == 0 {
		return fmt.Errorf("%s: no cases", where)
	}

	names := map[string]bool{}
	for _, tc := range s.Cases {
		full := s.Name + "/" + tc.Name

		switch {
		case tc.Name == "":
			return fmt.Errorf("%s: a case is missing `name`", where)
		case names[tc.Name]:
			return fmt.Errorf("%s: duplicate case %q", where, tc.Name)
		case len(tc.Args) == 0 && !tc.Sync:
			return fmt.Errorf("%s: %q has no `args`", where, tc.Name)
		case tc.Want == nil && tc.Contains == "" && len(tc.Absent) == 0:
			return fmt.Errorf("%s: %q asserts nothing about the output", where, tc.Name)
		case tc.Want != nil && tc.Contains != "":
			return fmt.Errorf("%s: %q sets both `want` and `contains`", where, tc.Name)
		case tc.Dir != "" && s.inline(tc):
			return fmt.Errorf("%s: %q sets `dir` but builds its own files", where, tc.Name)
		}

		// A suite name that prefixes a case's full path would make `-run`
		// ambiguous and force Go to disambiguate with a `#01` suffix.
		if other, dup := seen[full]; dup {
			return fmt.Errorf("%s: %q collides with %s", where, full, other)
		}
		seen[full] = where
		names[tc.Name] = true
	}

	if other, dup := seen[s.Name]; dup {
		return fmt.Errorf("%s: suite %q collides with a case in %s", where, s.Name, other)
	}
	seen[s.Name] = where

	return nil
}

// tools is a list of external programs, written either as one comma-separated
// string or as a list.
type tools []string

func (x *tools) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.SequenceNode {
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*x = list
		return nil
	}

	var line string
	if err := node.Decode(&line); err != nil {
		return err
	}
	for _, name := range strings.Split(line, ",") {
		if name = strings.TrimSpace(name); name != "" {
			*x = append(*x, name)
		}
	}

	return nil
}

// missing returns the required programs that aren't installed.
//
// Vale accepts several spellings of some of them, so a case is only skipped
// when none of the alternatives resolve.
func (x tools) missing() []string {
	alternatives := map[string][]string{
		"rst2html":    {"rst2html", "rst2html.py", "rst2html-3", "rst2html-3.py"},
		"dita":        {"dita", "dita.bat"},
		"xsltproc":    {"xsltproc", "xsltproc.exe"},
		"asciidoctor": {"asciidoctor", "asciidoctor.bat"},
	}

	var absent []string
	for _, name := range x {
		names, ok := alternatives[name]
		if !ok {
			names = []string{name}
		}
		if !slices.ContainsFunc(names, func(n string) bool {
			_, err := exec.LookPath(n)
			return err == nil
		}) {
			absent = append(absent, name)
		}
	}

	return absent
}

// words is a list of command-line arguments that reads either as a single
// shell-like string or as an explicit list.
type words []string

func (w *words) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.SequenceNode {
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*w = list
		return nil
	}

	var line string
	if err := node.Decode(&line); err != nil {
		return err
	}
	*w = splitArgs(line)

	return nil
}

// splitArgs splits on spaces, honoring single and double quotes.
func splitArgs(s string) []string {
	var (
		out            []string
		cur            strings.Builder
		quote          rune
		quoted, inWord bool
	)

	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, quoted, inWord = r, true, true
		case r == ' ':
			if inWord || quoted {
				out = append(out, cur.String())
				cur.Reset()
			}
			quoted, inWord = false, false
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}

	if inWord || quoted {
		out = append(out, cur.String())
	}

	return out
}
