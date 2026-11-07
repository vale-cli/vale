package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

// Profiling is written when VALE_CPUPROFILE or VALE_MEMPROFILE names a file.
//
// Environment variables rather than flags: Vale's flag set is user-facing and
// stable, while this is a development tool. It also means a profile can be
// captured from a run driven by an editor or a script without changing the
// command line.
type profiler struct {
	cpu *os.File
	mem string
}

// startProfiling begins any profiling the environment asks for. The returned
// function must be called before the process exits.
func startProfiling() func() {
	p := &profiler{mem: os.Getenv("VALE_MEMPROFILE")}

	if path := os.Getenv("VALE_CPUPROFILE"); path != "" {
		f, err := os.Create(path) //nolint:gosec // where the flag says
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "vale: creating CPU profile: %v\n", err)
		default:
			if serr := pprof.StartCPUProfile(f); serr != nil {
				fmt.Fprintf(os.Stderr, "vale: starting CPU profile: %v\n", serr)
				f.Close()
			} else {
				p.cpu = f
			}
		}
	}

	return p.stop
}

func (p *profiler) stop() {
	if p.cpu != nil {
		pprof.StopCPUProfile()
		p.cpu.Close()
	}

	if p.mem == "" {
		return
	}

	f, err := os.Create(p.mem)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vale: creating memory profile: %v\n", err)
		return
	}
	defer f.Close()

	// A profile of the live heap, so the numbers describe what Vale is holding
	// rather than everything it ever allocated.
	runtime.GC()
	if werr := pprof.WriteHeapProfile(f); werr != nil {
		fmt.Fprintf(os.Stderr, "vale: writing memory profile: %v\n", werr)
	}
}
