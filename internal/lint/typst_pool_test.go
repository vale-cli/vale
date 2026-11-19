package lint

import (
	"runtime"
	"testing"
)

// A pool with no owner is a pool nothing stops. The CLI gets away with it --
// exiting reaps the children -- but the language server lints many times in
// one process, and every run would leave its helpers behind.
func TestTypstPoolDoesNotLeakProcesses(t *testing.T) {
	if typstFastPath() == nil {
		t.Skip("typst2vast not resolvable on this machine")
	}

	before := childProcessCount(t)

	linter, err := initLinter()
	if err != nil {
		t.Fatal(err)
	}

	// A real Typst file, so the run actually starts the pool -- a path that
	// lints nothing would pass this test without testing anything.
	linted, err := linter.Lint([]string{"../../testdata/fixtures/formats/test.typ"}, "*")
	if err != nil {
		t.Fatal(err)
	}
	if len(linted) == 0 {
		t.Fatal("nothing linted; the pool was never started")
	}
	if linter.typst != nil {
		t.Error("pool outlived the run")
	}

	// Give the OS a moment to reap.
	runtime.Gosched()
	if after := childProcessCount(t); after > before {
		t.Errorf("leaked processes: %d before, %d after", before, after)
	}
}
