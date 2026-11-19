//go:build js && wasm

package main

import (
	"errors"
	"syscall/js"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint"
	"github.com/vale-cli/vale/v3/internal/system"
)

// memFS is the in-memory filesystem populated by the JavaScript host via
// ValeAddFile / ValeAddPackage. All Vale read paths (rule files, ignore
// lists, vocab, dictionaries) go through it once SetDefault has been
// called below.
var memFS = system.NewMemFS()

func errResult(msg string) interface{} {
	return map[string]interface{}{"error": msg}
}

// cached is the linter for the last configuration linted under. Building
// one compiles every rule, so a host that lints as its user types reuses it;
// a write to the filesystem drops it, since that is where the rules are read.
var cached struct {
	ini    string
	linter *lint.Linter
}

// linterFor returns the linter for a configuration, building it if needed.
func linterFor(ini string) (*lint.Linter, error) {
	if cached.linter != nil && cached.ini == ini {
		return cached.linter, nil
	}
	cfg, err := core.LoadConfigFromString(ini)
	if err != nil {
		return nil, err
	}
	linter, err := lint.NewLinter(cfg)
	if err != nil {
		return nil, err
	}
	linter.Memoize()
	cached.ini, cached.linter = ini, linter
	return linter, nil
}

// readBytes accepts either a JS string or a Uint8Array / ArrayBuffer and
// returns the raw bytes.
func readBytes(v js.Value) ([]byte, error) {
	switch v.Type() {
	case js.TypeString:
		return []byte(v.String()), nil
	case js.TypeObject:
		// Uint8Array / ArrayBuffer / typed array all expose a length and
		// can be copied with js.CopyBytesToGo.
		if !v.Truthy() {
			return nil, errors.New("empty value")
		}
		// ArrayBuffer needs to be wrapped in a Uint8Array view first.
		if v.Get("byteLength").IsUndefined() && v.Get("length").IsUndefined() {
			return nil, errors.New("not a binary value")
		}
		view := v
		if !v.Get("BYTES_PER_ELEMENT").Truthy() {
			view = js.Global().Get("Uint8Array").New(v)
		}
		n := view.Get("length").Int()
		buf := make([]byte, n)
		js.CopyBytesToGo(buf, view)
		return buf, nil
	}
	return nil, errors.New("unsupported value type")
}

// ValeAddFile(path, contents) stores a single file in the in-memory
// filesystem. `contents` may be a string or a Uint8Array.
func ValeAddFile(_ js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("ValeAddFile(path, contents) requires 2 arguments")
	}
	data, err := readBytes(args[1])
	if err != nil {
		return errResult(err.Error())
	}
	memFS.AddFile(args[0].String(), data)
	cached.linter = nil
	return map[string]interface{}{"ok": true}
}

// ValeAddPackage(prefix, zipBytes) extracts every file in the supplied
// ZIP archive under `prefix`. Typical usage from JS:
//
//	const buf = new Uint8Array(await pkg.arrayBuffer());
//	ValeAddPackage("/styles", buf);
func ValeAddPackage(_ js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("ValeAddPackage(prefix, zipBytes) requires 2 arguments")
	}
	data, err := readBytes(args[1])
	if err != nil {
		return errResult(err.Error())
	}
	if err := memFS.AddZip(args[0].String(), data); err != nil {
		return errResult(err.Error())
	}
	cached.linter = nil
	return map[string]interface{}{"ok": true}
}

// ValeResetFS() drops every entry from the in-memory filesystem.
func ValeResetFS(_ js.Value, _ []js.Value) interface{} {
	memFS.Reset()
	cached.linter = nil
	return map[string]interface{}{"ok": true}
}

// ValeListFS() returns every path currently stored, for debugging.
func ValeListFS(_ js.Value, _ []js.Value) interface{} {
	paths := memFS.List()
	out := make([]interface{}, len(paths))
	for i, p := range paths {
		out[i] = p
	}
	return map[string]interface{}{"paths": out}
}

// ValeSuggest(alertJSON, configINI) returns the fix suggestions for a
// single alert. The alert is expected to be one of the entries from a
// prior ValeLint call -- the Action field carries the fix logic Vale
// resolves here (replace / suggest / remove / convert / edit). On
// success the result is `{ suggestions: [<string>, ...] }`; alerts
// with no actionable fix return an empty list.
func ValeSuggest(_ js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("ValeSuggest(alertJSON, configINI) requires 2 arguments")
	}
	alertJSON := args[0].String()
	configINI := args[1].String()

	cfg, err := core.LoadConfigFromString(configINI)
	if err != nil {
		return errResult(err.Error())
	}

	sol, err := check.ParseAlert(alertJSON, cfg)
	if err != nil {
		return errResult(err.Error())
	}
	if sol.Error != "" {
		return errResult(sol.Error)
	}

	out := make([]interface{}, len(sol.Suggestions))
	for i, s := range sol.Suggestions {
		out[i] = s
	}
	return map[string]interface{}{"suggestions": out}
}

// ValeLint(text, format, configINI) is the primary lint entry point.
// Styles, ignore lists, vocabularies and dictionaries referenced by the
// INI are resolved against the in-memory filesystem populated by the
// other globals.
func ValeLint(_ js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return errResult("ValeLint(text, format, configINI) requires 3 arguments")
	}

	text := args[0].String()
	format := args[1].String() // e.g., "md", "adoc"
	configINI := args[2].String()

	linter, err := linterFor(configINI)
	if err != nil {
		return errResult(err.Error())
	}

	virtualName := "input." + format

	linted, err := linter.LintStringAs(text, virtualName)
	if err != nil {
		return errResult(err.Error())
	}

	out, err := core.ToJSON(linted)
	if err != nil {
		return errResult(err.Error())
	}

	return map[string]interface{}{"result": string(out)}
}

// ValeLintBuilt(text, format, html, configINI) lints a document the host
// has already converted to HTML -- AsciiDoc through Asciidoctor.js, XML
// through XSLTProcessor -- as ValeLint would have after running the
// converter itself. ValeConverterOptions says what to pass the converter.
func ValeLintBuilt(_ js.Value, args []js.Value) interface{} {
	if len(args) < 4 {
		return errResult("ValeLintBuilt(text, format, html, configINI) requires 4 arguments")
	}

	text := args[0].String()
	format := args[1].String()
	html := args[2].String()
	configINI := args[3].String()

	linter, err := linterFor(configINI)
	if err != nil {
		return errResult(err.Error())
	}

	linted, err := linter.LintBuilt(text, "input."+format, html)
	if err != nil {
		return errResult(err.Error())
	}

	out, err := core.ToJSON(linted)
	if err != nil {
		return errResult(err.Error())
	}

	return map[string]interface{}{"result": string(out)}
}

// ValeConverterOptions(format, configINI) returns what the host must give
// its converter so the HTML it hands ValeLintBuilt is what Vale would have
// produced: for `adoc`, Asciidoctor's attributes, safe mode, and the
// converter class to register; for `rst`, Docutils' flags; for `xml`,
// xsltproc's parameters.
func ValeConverterOptions(_ js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("ValeConverterOptions(format, configINI) requires 2 arguments")
	}

	cfg, err := core.LoadConfigFromString(args[1].String())
	if err != nil {
		return errResult(err.Error())
	}

	opts, err := lint.ConverterOptions(cfg, args[0].String())
	if err != nil {
		return errResult(err.Error())
	}
	return toJS(opts)
}

// toJS converts the string slices and maps a Go API returns into the shapes
// js.ValueOf accepts, which are only []interface{} and map[string]interface{}.
func toJS(v interface{}) interface{} {
	switch t := v.(type) {
	case []string:
		out := make([]interface{}, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(t))
		for k, s := range t {
			out[k] = s
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, item := range t {
			out[k] = toJS(item)
		}
		return out
	default:
		return v
	}
}

// ValeTransform(text, format, configINI) applies the config's block/token
// ignore patterns to the text and returns the transformed result. Ignored
// spans are rewritten as the format's code markup (e.g., backticks / fences),
// which downstream scoping then ignores -- useful for previewing what a config
// excludes from linting.
func ValeTransform(_ js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return errResult("ValeTransform(text, format, configINI) requires 3 arguments")
	}

	text := args[0].String()
	format := args[1].String()
	configINI := args[2].String()

	linter, err := linterFor(configINI)
	if err != nil {
		return errResult(err.Error())
	}

	file, err := core.NewFileFromContent(text, "input."+format, linter.Manager.Config)
	if err != nil {
		return errResult(err.Error())
	}

	out, err := linter.Transform(file)
	if err != nil {
		return errResult(err.Error())
	}

	return map[string]interface{}{"result": out}
}

func main() {
	system.SetDefault(memFS)

	g := js.Global()
	g.Set("ValeLint", js.FuncOf(ValeLint))
	g.Set("ValeLintBuilt", js.FuncOf(ValeLintBuilt))
	g.Set("ValeConverterOptions", js.FuncOf(ValeConverterOptions))
	g.Set("ValeResolveConfig", js.FuncOf(ValeResolveConfig))
	g.Set("ValeTransform", js.FuncOf(ValeTransform))
	g.Set("ValeSuggest", js.FuncOf(ValeSuggest))
	g.Set("ValeAddFile", js.FuncOf(ValeAddFile))
	g.Set("ValeAddPackage", js.FuncOf(ValeAddPackage))
	g.Set("ValeResetFS", js.FuncOf(ValeResetFS))
	g.Set("ValeListFS", js.FuncOf(ValeListFS))

	// Register a no-op setInterval so Go's scheduler always has a pending
	// JS event. Without this, select{} triggers Go's deadlock detector
	// (all goroutines asleep, no external event source → runtime panic/exit).
	keepAlive := js.FuncOf(func(js.Value, []js.Value) interface{} { return nil })
	js.Global().Call("setInterval", keepAlive, 1000)

	select {} // keep alive
}
