// Package tsw runs tree-sitter grammars compiled to WebAssembly.
//
// A grammar module holds tree-sitter's core and one grammar behind the small
// API in grammars/shim.c, and wazero runs it in pure Go: no CGo, and so no C
// toolchain to build Vale and no native code to ship a grammar as. The
// package mirrors the slice of the tree-sitter API Vale reads -- parse, a
// few node operations, and queries with their predicates -- and nothing
// more.
//
// A Language is compiled once and instantiated as needed; instances are
// pooled, since a module instance is single-threaded and Vale lints files
// concurrently. A Tree borrows an instance for its lifetime, and its nodes
// and queries belong to that instance: close the tree when done with them.
package tsw

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// The runtime is shared by every grammar, so their compiled code is cached
// together. Compiling a module is the expensive step, about a hundred
// milliseconds per grammar, so the result is kept on disk in the user's
// cache directory and reused by the next run; instantiating one is cheap.
var (
	runtimeOnce sync.Once
	runtime     wazero.Runtime
)

func sharedRuntime() wazero.Runtime {
	runtimeOnce.Do(func() {
		ctx := context.Background()
		cfg := wazero.NewRuntimeConfig()
		if dir := cacheDir(); dir != "" {
			if cache, err := wazero.NewCompilationCacheWithDir(dir); err == nil {
				cfg = cfg.WithCompilationCache(cache)
			}
		}
		runtime = wazero.NewRuntimeWithConfig(ctx, cfg)
		wasi_snapshot_preview1.MustInstantiate(ctx, runtime)
	})
	return runtime
}

// cacheDir is where compiled grammars are kept between runs, or "" where
// there is no such place.
func cacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "vale", "grammars")
}

// instances is the most of a grammar's instances kept for reuse. A lint run
// has a handful of files in flight; one instance each is enough, and one
// past that is made and dropped.
const instances = 8

// A Language is one compiled grammar module.
type Language struct {
	name     string
	compiled wazero.CompiledModule

	// free holds instances between uses. A free list rather than a
	// sync.Pool: the pool drops its contents at every collection, and a new
	// instance grows its memory from nothing on every parse again.
	free chan *instance
}

// Load compiles a grammar module. name labels it in errors.
func Load(name string, wasm []byte) (*Language, error) {
	compiled, err := sharedRuntime().CompileModule(context.Background(), wasm)
	if err != nil {
		return nil, fmt.Errorf("grammar %s: %w", name, err)
	}
	return &Language{name: name, compiled: compiled, free: make(chan *instance, instances)}, nil
}

// acquire takes a free instance or makes one.
func (l *Language) acquire() (*instance, error) {
	select {
	case inst := <-l.free:
		return inst, nil
	default:
		return l.instantiate()
	}
}

// put returns an instance, unless enough are already waiting.
func (l *Language) put(inst *instance) {
	select {
	case l.free <- inst:
	default:
		_ = inst.mod.Close(context.Background())
	}
}

// Name returns the label the grammar was loaded under.
func (l *Language) Name() string { return l.name }

// An instance is one running copy of the module, with its exports in hand.
type instance struct {
	lang *Language
	mod  api.Module
	mem  api.Memory

	alloc, release, lastLen                         api.Function
	parse                                           api.Function
	nodeType, nodeStart, nodeEnd, nodeRow, nodeCol  api.Function
	namedChildCount, namedChild, nextNamedSibling   api.Function
	childByField, nodeSexp                          api.Function
	queryNew, queryError, captureCount, captureName api.Function
	stringValue, patternCount, predicates           api.Function
	wordsPtr, wordsLen, queryExec                   api.Function

	queries map[string]*Query
}

func (l *Language) instantiate() (*instance, error) {
	ctx := context.Background()
	mod, err := sharedRuntime().InstantiateModule(ctx, l.compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return nil, fmt.Errorf("grammar %s: %w", l.name, err)
	}
	fn := func(name string) api.Function {
		if f := mod.ExportedFunction(name); f != nil {
			return f
		}
		panic("tsw: grammar " + l.name + " has no export " + name)
	}
	return &instance{
		lang: l, mod: mod, mem: mod.Memory(),
		alloc: fn("alloc"), release: fn("release"), lastLen: fn("last_len"),
		parse:    fn("parse"),
		nodeType: fn("node_type"), nodeStart: fn("node_start_byte"), nodeEnd: fn("node_end_byte"),
		nodeRow: fn("node_start_row"), nodeCol: fn("node_start_col"),
		namedChildCount: fn("node_named_child_count"), namedChild: fn("node_named_child"),
		nextNamedSibling: fn("node_next_named_sibling"), childByField: fn("node_child_by_field_name"),
		nodeSexp: fn("node_sexp"),
		queryNew: fn("query_new"), queryError: fn("query_error"), captureCount: fn("query_capture_count"),
		captureName: fn("query_capture_name"), stringValue: fn("query_string_value"),
		patternCount: fn("query_pattern_count"), predicates: fn("query_predicates"),
		wordsPtr: fn("words_ptr"), wordsLen: fn("words_len"), queryExec: fn("query_exec"),
		queries: map[string]*Query{},
	}, nil
}

// call invokes an export, which never fails short of a bug in the module.
func (in *instance) call(f api.Function, args ...uint64) uint64 {
	out, err := f.Call(context.Background(), args...)
	if err != nil {
		panic("tsw: grammar " + in.lang.name + ": " + err.Error())
	}
	if len(out) == 0 {
		return 0
	}
	return out[0]
}

// str reads a string the module returned: a pointer, with its length in
// last_len.
func (in *instance) str(ptr uint64) string {
	n := u32(in.call(in.lastLen))
	b, ok := in.mem.Read(u32(ptr), n)
	if !ok {
		panic("tsw: grammar " + in.lang.name + ": string out of memory bounds")
	}
	return string(b)
}

// write copies b into the module's memory and returns where; the caller
// releases it.
func (in *instance) write(b []byte) uint32 {
	ptr := u32(in.call(in.alloc, uint64(len(b))))
	if !in.mem.Write(ptr, b) {
		panic("tsw: grammar " + in.lang.name + ": write out of memory bounds")
	}
	return ptr
}

// words reads the module's output buffer.
func (in *instance) words() []uint32 {
	n := u32(in.call(in.wordsLen))
	if n == 0 {
		return nil
	}
	ptr := u32(in.call(in.wordsPtr))
	b, ok := in.mem.Read(ptr, n*4)
	if !ok {
		panic("tsw: grammar " + in.lang.name + ": words out of memory bounds")
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return out
}

// A Tree is a parsed document and the instance that holds it.
type Tree struct {
	inst *instance
	src  []byte
	ptr  uint32
	root *Node
}

// Parse reads src and returns its tree, which borrows an instance of the
// grammar until Close.
func (l *Language) Parse(src []byte) (*Tree, error) {
	inst, err := l.acquire()
	if err != nil {
		return nil, err
	}
	ptr := inst.write(src)
	h := i32(inst.call(inst.parse, uint64(ptr), uint64(len(src))))
	t := &Tree{inst: inst, src: src, ptr: ptr}
	t.root = &Node{tree: t, h: h}
	return t, nil
}

// RootNode is the document's root.
func (t *Tree) RootNode() *Node { return t.root }

// Close returns the instance to its language. The tree's nodes and queries
// must not be used afterwards.
func (t *Tree) Close() {
	if t.inst == nil {
		return
	}
	t.inst.call(t.inst.release, uint64(t.ptr))
	t.inst.lang.put(t.inst)
	t.inst = nil
}

// Handles and offsets cross the boundary as wasm's unsigned 64-bit words;
// these narrow them back, which never overflows: a handle indexes a table
// and an offset addresses a 32-bit memory.
func i32(v uint64) int32 { return int32(v) } //nolint:gosec // see above

func u32(v uint64) uint32 { return uint32(v) } //nolint:gosec // see above
