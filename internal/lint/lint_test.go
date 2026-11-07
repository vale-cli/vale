package lint

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

func TestSymlinkFixture(t *testing.T) {
	// This is an integration test: it shells out to an installed `vale`
	// binary. Skip when one isn't on PATH (e.g., local `go test ./...` or a
	// CI job that builds the binary without installing it) rather than failing.
	if _, err := exec.LookPath("vale"); err != nil {
		t.Skip("vale binary not found on PATH")
	}

	fixture := "../../testdata/fixtures/misc/symlinks"

	targetSrc := system.AbsPath(filepath.Join(fixture, "Symlinked"))
	targetDst := system.AbsPath(filepath.Join(fixture, "styles", "Symlinked"))

	if _, err := os.Stat(targetSrc); os.IsNotExist(err) {
		t.Fatalf("Target source does not exist: %v", targetSrc)
	}

	if err := os.Symlink(targetSrc, targetDst); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	t.Cleanup(func() {
		err := os.Remove(targetDst)
		if err != nil {
			t.Fatalf("Failed to remove symlink: %v", err)
		}
	})

	info, err := os.Lstat(targetDst)
	if err != nil {
		t.Fatalf("Failed to stat symlink: %v", err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Expected %v to be a symlink", targetDst)
	}

	resolvedPath, err := os.Readlink(targetDst)
	if err != nil {
		t.Fatalf("Failed to read symlink: %v", err)
	}

	if resolvedPath != targetSrc {
		t.Fatalf("Symlink points to %v, expected %v", resolvedPath, targetSrc)
	}

	// Call Vale on the symlinked file.
	cmd := exec.Command("vale", "--output=JSON", "--no-global", "test.md")
	cmd.Dir = fixture

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run Vale: %s", string(out))
	}

	if !bytes.Contains(out, []byte("Symlinked")) {
		t.Fatalf("Expected output from Vale, got %s", string(out))
	}
}

func TestGenderBias(t *testing.T) {
	reToMatches := map[string][]string{
		"(?:alumna|alumnus)":          {"alumna", "alumnus"},
		"(?:alumnae|alumni)":          {"alumnae", "alumni"},
		"(?:mother|father)land":       {"motherland", "fatherland"},
		"air(?:m[ae]n|wom[ae]n)":      {"airman", "airwoman", "airmen", "airwomen"},
		"anchor(?:m[ae]n|wom[ae]n)":   {"anchorman", "anchorwoman", "anchormen", "anchorwomen"},
		"camera(?:m[ae]n|wom[ae]n)":   {"cameraman", "camerawoman", "cameramen", "camerawomen"},
		"chair(?:m[ae]n|wom[ae]n)":    {"chairman", "chairwoman", "chairmen", "chairwomen"},
		"congress(?:m[ae]n|wom[ae]n)": {"congressman", "congresswoman", "congressmen", "congresswomen"},
		"door(?:m[ae]n|wom[ae]n)":     {"doorman", "doorwoman", "doormen", "doorwomen"},
		"drafts(?:m[ae]n|wom[ae]n)":   {"draftsman", "draftswoman", "draftsmen", "draftswomen"},
		"fire(?:m[ae]n|wom[ae]n)":     {"fireman", "firewoman", "firemen", "firewomen"},
		"fisher(?:m[ae]n|wom[ae]n)":   {"fisherman", "fisherwoman", "fishermen", "fisherwomen"},
		"fresh(?:m[ae]n|wom[ae]n)":    {"freshman", "freshwoman", "freshmen", "freshwomen"},
		"garbage(?:m[ae]n|wom[ae]n)":  {"garbageman", "garbagewoman", "garbagemen", "garbagewomen"},
		"mail(?:m[ae]n|wom[ae]n)":     {"mailman", "mailwoman", "mailmen", "mailwomen"},
		"middle(?:m[ae]n|wom[ae]n)":   {"middleman", "middlewoman", "middlemen", "middlewomen"},
		"news(?:m[ae]n|wom[ae]n)":     {"newsman", "newswoman", "newsmen", "newswomen"},
		"ombuds(?:man|woman)":         {"ombudsman", "ombudswoman"},
		"work(?:m[ae]n|wom[ae]n)":     {"workman", "workwoman", "workmen", "workwomen"},
		"police(?:m[ae]n|wom[ae]n)":   {"policeman", "policewoman", "policemen", "policewomen"},
		"repair(?:m[ae]n|wom[ae]n)":   {"repairman", "repairwoman", "repairmen", "repairwomen"},
		"sales(?:m[ae]n|wom[ae]n)":    {"salesman", "saleswoman", "salesmen", "saleswomen"},
		"service(?:m[ae]n|wom[ae]n)":  {"serviceman", "servicewoman", "servicemen", "servicewomen"},
		"steward(?:ess)?":             {"steward", "stewardess"},
		"tribes(?:m[ae]n|wom[ae]n)":   {"tribesman", "tribeswoman", "tribesmen", "tribeswomen"},
	}
	for re, matches := range reToMatches {
		regex := regexp.MustCompile(re)
		for _, match := range matches {
			if !regex.MatchString(match) {
				t.Errorf("expected = %v, got = %v", true, false)
			}
		}
	}
}

func initLinter() (*Linter, error) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return nil, err
	}

	cfg.MinAlertLevel = 0
	cfg.GBaseStyles = []string{"Vale"}
	cfg.Flags.InExt = ".txt" // default value

	return NewLinter(cfg)
}

func benchmarkLint(b *testing.B, path string) {
	b.Helper()

	linter, err := initLinter()
	if err != nil {
		b.Fatal(err)
	}

	path, err = filepath.Abs(path)
	if err != nil {
		b.Fatal(err)
	}

	for n := 0; n < b.N; n++ {
		_, err = linter.Lint([]string{path}, "*")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLintRST(b *testing.B) {
	benchmarkLint(b, "../../testdata/fixtures/benchmarks/bench.rst")
}

func BenchmarkLintMD(b *testing.B) {
	benchmarkLint(b, "../../testdata/fixtures/benchmarks/bench.md")
}

// generatedReference builds a document shaped like machine-written API
// reference: many short blocks, each naming a symbol nothing else names.
//
// The shape matters more than the prose. Vale lints a passage once and reuses
// the result, so a fixture built by repeating a file measures the cache rather
// than the parser -- which is how a document that takes seconds in the wild can
// look fast here. Unique identifiers defeat that, and they are what real
// generated references contain.
func generatedReference(size int) string {
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "### `Widget%dOptions`\n\n", i)
		fmt.Fprintf(&b, "Options accepted by the widget%d endpoint. The retry\n", i)
		fmt.Fprintf(&b, "field controls how often request%d is reissued.\n\n", i)
		fmt.Fprintf(&b, "- `timeout%d` -- seconds to wait before giving up.\n", i)
		fmt.Fprintf(&b, "- `retries%d` -- how many attempts to make.\n\n", i)
	}
	return b.String()
}

// BenchmarkLintGenerated lints one large file, which is where cost stops
// tracking size.
//
// Real documentation sets contain these: an API reference nobody hand-writes,
// hundreds of kilobytes in a single file. They are also where Vale is slowest
// per byte -- searching for a block's position walks the document, so the work
// grows with the square of the length. Sizes here bracket what shows up in
// practice; the largest file in Airbyte's docs is 869 KB.
//
// Divide ns/op by the size: the cost per kilobyte should not climb.
func BenchmarkLintGenerated(b *testing.B) {
	dir := b.TempDir()
	for _, kb := range []int{128, 512, 1024} {
		doc := generatedReference(kb * 1024)

		path := filepath.Join(dir, fmt.Sprintf("gen-%dkb.md", kb))
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("%dKB", kb), func(b *testing.B) {
			benchmarkLint(b, path)
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(kb), "ns/KB")
		})
	}
}

// BenchmarkLintMDScale lints one document truncated to a range of sizes.
//
// Linting cost should track the amount of prose: double the file, double the
// time. This benchmark exists because it does not. Every size is a prefix of
// the same fixture, so the prose is distinct rather than repeated -- repeating
// it would be measured once and reused, hiding the effect.
//
// Read the result as a curve, not as individual numbers. Cost per kilobyte
// falls at first -- a run has a fixed cost of roughly 15 ms, and the larger the
// file the further that is spread -- then bottoms out and climbs again. The
// climb is the defect: past about 64 KB each additional kilobyte costs more
// than the last, because part of the pipeline scans from the start of the
// document rather than from where it left off. Marginal cost per KB currently
// runs 0.32 (8->16 KB), 0.49 (16->32), 0.67 (32->64), 0.89 (64->112).
//
// A fix should flatten the tail. Watch the last two sizes.
func BenchmarkLintMDScale(b *testing.B) {
	src, err := os.ReadFile("../../testdata/fixtures/benchmarks/bench.md")
	if err != nil {
		b.Fatal(err)
	}

	dir := b.TempDir()
	for _, kb := range []int{4, 8, 16, 32, 64, 112} {
		size := kb * 1024
		if size > len(src) {
			// The fixture bounds the range. Sizes past it are skipped rather
			// than fatal, so shrinking the fixture cannot break the benchmark.
			continue
		}

		// Cut back to a line break so the prefix is still valid Markdown.
		chunk := src[:size]
		if i := bytes.LastIndexByte(chunk, '\n'); i > 0 {
			chunk = chunk[:i]
		}

		path := filepath.Join(dir, fmt.Sprintf("bench-%dkb.md", kb))
		if err = os.WriteFile(path, chunk, 0o600); err != nil { //nolint:gosec // a test file
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("%dKB", kb), func(b *testing.B) {
			benchmarkLint(b, path)
			// The trend is the point, so report the derived figure directly
			// rather than making a reader divide six numbers by hand.
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(kb), "ns/KB")
		})
	}
}

// A setting written for a rule takes precedence over one written for its
// style, which is what lets a style-wide default be overridden rule by rule.
func TestLookupPrefersTheRule(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]bool
		want     bool
		found    bool
	}{
		{"neither", map[string]bool{}, false, false},
		{"style only", map[string]bool{"proselint": false}, false, true},
		{"rule only", map[string]bool{"proselint.Very": true}, true, true},
		{
			"rule overrides style",
			map[string]bool{"proselint": false, "proselint.Very": true},
			true, true,
		},
		{
			"rule overrides style, the other way",
			map[string]bool{"proselint": true, "proselint.Very": false},
			false, true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := lookup(tt.settings, "proselint.Very", "proselint")
			if got != tt.want || found != tt.found {
				t.Errorf("lookup = (%v, %v), want (%v, %v)",
					got, found, tt.want, tt.found)
			}
		})
	}
}
