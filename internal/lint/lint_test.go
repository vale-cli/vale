package lint

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/system"
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
