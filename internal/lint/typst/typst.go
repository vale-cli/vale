//go:build !vale_nocode

// Package typst reads Typst with typst2vast, compiled to WebAssembly and
// embedded: the Typst compiler's own parser, with nothing to install.
// typst2vast parses without evaluating and emits the HTML Vale's walker
// reads. See module/ for the source of typst.wasm and build.sh to rebuild it.
package typst

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/vale-cli/vale/v3/internal/lint/code/tsw"
)

//go:embed typst.wasm
var module []byte

// Built reports whether this build carries the reader.
const Built = true

// instances is the most instances kept for reuse; see tsw.
const instances = 8

var (
	once     sync.Once
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	errLoad  error

	free = make(chan *instance, instances)
)

// An instance is one running copy of the module. It is single-threaded, so
// each conversion borrows one.
type instance struct {
	mod                  api.Module
	alloc, release, conv api.Function
}

func acquire() (*instance, error) {
	once.Do(func() { rt, compiled, errLoad = tsw.Compile(module) })
	if errLoad != nil {
		return nil, fmt.Errorf("typst: %w", errLoad)
	}

	select {
	case inst := <-free:
		return inst, nil
	default:
	}

	mod, err := rt.InstantiateModule(
		context.Background(), compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return nil, fmt.Errorf("typst: %w", err)
	}
	return &instance{
		mod:     mod,
		alloc:   mod.ExportedFunction("t2v_alloc"),
		release: mod.ExportedFunction("t2v_free"),
		conv:    mod.ExportedFunction("t2v_convert"),
	}, nil
}

// Convert returns the HTML for a Typst document.
func Convert(doc string) (string, error) {
	inst, err := acquire()
	if err != nil {
		return "", err
	}

	html, err := inst.convert(doc)
	if err != nil {
		// A trap leaves the instance in no state to reuse.
		_ = inst.mod.Close(context.Background())
		return "", fmt.Errorf("typst: %w", err)
	}

	select {
	case free <- inst:
	default:
		_ = inst.mod.Close(context.Background())
	}
	return html, nil
}

func (in *instance) convert(doc string) (string, error) {
	ctx := context.Background()
	n := uint64(len(doc))

	out, err := in.alloc.Call(ctx, n)
	if err != nil {
		return "", err
	}
	src := out[0]
	if !in.mod.Memory().WriteString(uint32(src), doc) { //nolint:gosec // a wasm32 address
		return "", errors.New("document does not fit in memory")
	}

	out, err = in.conv.Call(ctx, src, n)
	if err != nil {
		return "", err
	}
	at, size := uint32(out[0]>>32), uint32(out[0]) //nolint:gosec // the two halves of the result

	// Read returns a view of the module's memory, which the next call may
	// move: the string is copied out before anything else runs.
	view, ok := in.mod.Memory().Read(at, size)
	if !ok {
		return "", errors.New("result is out of range")
	}
	html := string(view)

	if _, err = in.release.Call(ctx, src, n); err != nil {
		return "", err
	}
	if _, err = in.release.Call(ctx, uint64(at), uint64(size)); err != nil {
		return "", err
	}
	return html, nil
}
