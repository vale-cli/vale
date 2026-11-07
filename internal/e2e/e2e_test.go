// Package e2e runs Vale end to end: it builds the binary, invokes it the way a
// user would, and compares its output against the expected output declared
// alongside each case.
//
// The cases live in testdata/e2e/*.yaml, one file per suite. Run a whole suite
// or a single case with the usual `-run` filter:
//
//	go test ./internal/e2e -run 'TestScenarios/config'
//	go test ./internal/e2e -run 'TestScenarios/lint/css'
//
// After an intentional change to Vale's output, rewrite the `want:` blocks in
// place and review the diff:
//
//	go test ./internal/e2e -update
package e2e

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the `want:` blocks in testdata/e2e/*.yaml")

// sharedFlags are applied to every case.
//
// They pin the output format and keep results stable across platforms and
// developer machines: `--normalize` and `--relative` make paths comparable,
// `--sort` makes ordering deterministic, and `--no-global` keeps the caller's
// own configuration out of the run.
var sharedFlags = []string{"--output=line", "--sort", "--normalize", "--relative", "--no-global"}

var (
	valeBin  string // the binary under test
	testdata string // the repository's testdata/ directory
)

func TestMain(m *testing.M) {
	flag.Parse()

	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// run builds the binary under test and hands off to the suite.
//
// It exists so that the temporary build directory is cleaned up even when a
// step fails -- `os.Exit` skips deferred calls.
func run(m *testing.M) (int, error) {
	root, err := repoRoot()
	if err != nil {
		return 0, err
	}
	testdata = filepath.Join(root, "testdata")

	tmp, err := os.MkdirTemp("", "vale-e2e")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)

	valeBin = filepath.Join(tmp, "vale")
	if runtime.GOOS == "windows" {
		valeBin += ".exe"
	}

	build := exec.Command("go", "build", "-o", valeBin, "./cmd/vale")
	build.Dir = root
	if out, bErr := build.CombinedOutput(); bErr != nil {
		return 0, fmt.Errorf("building vale: %w\n%s", bErr, out)
	}

	return m.Run(), nil
}

func TestScenarios(t *testing.T) {
	suites, err := loadSuites(filepath.Join(testdata, "e2e"))
	if err != nil {
		t.Fatal(err)
	}

	announce(suites)

	started := time.Now()
	t.Cleanup(func() { summarize(time.Since(started)) })

	// -update rewrites the YAML, so each file needs a single writer.
	var edits sync.Map

	for _, s := range suites {
		t.Run(s.Name, func(t *testing.T) {
			for _, tc := range s.Cases {
				t.Run(tc.Name, func(t *testing.T) {
					// `sync` writes into the fixture's StylesPath, so those
					// cases can't share the tree with anything else.
					if !tc.Sync {
						t.Parallel()
					}
					if absent := tc.Requires.missing(); len(absent) > 0 {
						// CI sets VALE_E2E_STRICT so that a converter which
						// failed to install is a failure, not a quiet skip.
						if os.Getenv("VALE_E2E_STRICT") != "" {
							t.Fatalf("needs %s, and VALE_E2E_STRICT is set",
								strings.Join(absent, ", "))
						}
						skip(s.Name, absent)
						t.Skipf("needs %s", strings.Join(absent, ", "))
					}
					e, ok := check(t, s, tc)
					tick(s.Name, ok)
					if !ok && *update {
						edits.Store(s.Path+"\x00"+tc.Name, e)
					}
				})
			}
		})
	}

	t.Cleanup(func() {
		if !*update {
			return
		}
		pending := map[string]map[string]edit{}
		edits.Range(func(k, v any) bool {
			parts := strings.SplitN(k.(string), "\x00", 2)
			if pending[parts[0]] == nil {
				pending[parts[0]] = map[string]edit{}
			}
			pending[parts[0]][parts[1]] = v.(edit)
			return true
		})
		for path, cases := range pending {
			if wErr := rewrite(path, cases); wErr != nil {
				t.Error(wErr)
			} else {
				t.Logf("updated %d case(s) in %s", len(cases), filepath.Base(path))
			}
		}
	})
}

// check runs a case and reports it. It returns what -update would record, and
// ok is false when the case did not match what it declared.
func check(t *testing.T, s *suite, tc testCase) (edit, bool) {
	t.Helper()

	out, code := invoke(t, s, tc)

	// Every check a case makes is counted, so the run reports the assertions
	// behind the scenarios rather than just the scenarios.
	var checked, failed int
	fail := func() { failed++ }

	switch {
	case tc.Contains != "":
		checked++
		if want := clean(tc.Contains); !strings.Contains(out, want) {
			report(t, s, tc, "expected output is missing", diffLines(want, out))
			fail()
		}
	case tc.Want != nil:
		checked++
		if want := clean(*tc.Want); out != want {
			// Under -update the expectation is being replaced, so don't shout.
			if !*update {
				report(t, s, tc, "output does not match", diffLines(want, out))
			}
			fail()
		}
	}

	for _, text := range tc.Absent {
		checked++
		if strings.Contains(out, text) {
			report(t, s, tc, fmt.Sprintf("output should not contain %q", text), indent(out))
			fail()
		}
	}

	checked++
	if code != tc.Exit {
		if !*update {
			report(t, s, tc, fmt.Sprintf("exit status is %d, expected %d", code, tc.Exit), indent(out))
		}
		fail()
	}

	count(checked, failed)
	ok := failed == 0

	// A `contains:` excerpt is written by hand, so there's nothing to record.
	return edit{want: out, hasWant: tc.Want != nil, exit: code}, ok
}

// invoke runs the binary under test and returns its combined output.
//
// Vale writes alerts to stdout and errors to stderr, and cases assert against
// both.
func invoke(t *testing.T, s *suite, tc testCase) (string, int) {
	t.Helper()

	dir := s.workdir(tc)
	if s.inline(tc) {
		var err error
		if dir, err = s.build(tc); err != nil {
			t.Fatal(err)
		}
	}

	env := os.Environ()
	for k, v := range tc.Env {
		env = append(env, k+"="+v)
	}

	if tc.Sync {
		sync := exec.Command(valeBin, "sync")
		sync.Dir = dir
		sync.Env = env
		if out, err := sync.CombinedOutput(); err != nil {
			t.Fatalf("vale sync: %v\n%s", err, out)
		}
	}

	cmd := exec.Command(valeBin, append(append([]string{}, sharedFlags...), tc.Args...)...)
	cmd.Dir = dir
	cmd.Env = env

	if tc.Stdin != "" {
		f, err := os.Open(filepath.Join(dir, tc.Stdin))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		cmd.Stdin = f
	}

	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("running vale: %v\n%s", err, out)
	}

	return clean(string(out)), cmd.ProcessState.ExitCode()
}

// repoRoot walks up from the working directory to the module root.
//
// `go test` starts the binary in its package directory, but `make test` runs
// it straight from the repository root to keep the progress display -- which
// `go test` would discard along with the rest of a passing package's output.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, sErr := os.Stat(filepath.Join(dir, "go.mod")); sErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// ansi matches the escape sequences Vale uses to color its error reports.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// clean makes output comparable across platforms.
func clean(s string) string {
	s = ansi.ReplaceAllString(s, "")
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}
