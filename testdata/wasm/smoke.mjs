// Exercises the wasm build's JavaScript API the way a browser host does:
// no filesystem, so wasm_exec.js's stub answers every os call, and every
// asset arrives through ValeAddFile. Run with `make test-wasm`.
//
//   node smoke.mjs <wasm_exec.js> <vale.wasm>
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import assert from "node:assert/strict";

const require = createRequire(import.meta.url);
globalThis.require = require;
globalThis.TextEncoder = TextEncoder;
globalThis.TextDecoder = TextDecoder;
globalThis.performance ??= { now: () => Date.now() };
const crypto = await import("node:crypto");
globalThis.crypto ??= crypto.webcrypto;

require(path.resolve(process.argv[2]));
const go = new Go();
const { instance } = await WebAssembly.instantiate(fs.readFileSync(path.resolve(process.argv[3])), go.importObject);
go.run(instance);
await new Promise((r) => setTimeout(r, 50));

for (const name of ["ValeLint", "ValeSuggest", "ValeTransform", "ValeAddFile", "ValeAddPackage", "ValeResetFS", "ValeListFS"]) {
  assert.equal(typeof globalThis[name], "function", `${name} is installed`);
}

// The built-in style needs no StylesPath.
const builtin = "MinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = Vale\n";
let out = ValeLint("This is a very important sentence. javascript agendize\n", "md", builtin);
assert.equal(out.error, undefined, out.error);
let alerts = JSON.parse(out.result)["input.md"];
assert.deepEqual(alerts.map((a) => [a.Check, a.Match]), [
  ["Vale.Spelling", "javascript"],
  ["Vale.Spelling", "agendize"],
]);

// A fix is resolved from the alert.
out = ValeSuggest(JSON.stringify(alerts[0]), builtin);
assert.deepEqual(out.suggestions, ["JavaScript"]);

// A style added to the in-memory filesystem is found through StylesPath.
ValeAddFile("/styles/T/W.yml", "extends: existence\nmessage: \"'%s'\"\nlevel: error\ntokens:\n  - very\n");
assert.deepEqual(ValeListFS().paths, ["/styles/T/W.yml"]);
const custom = "StylesPath = /styles\nMinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = T\n";
out = ValeLint("This is very good.\n", "md", custom);
assert.equal(out.error, undefined, out.error);
alerts = JSON.parse(out.result)["input.md"];
assert.deepEqual(alerts.map((a) => [a.Check, a.Line, a.Span]), [["T.W", 1, [9, 12]]]);

// Markup is understood: a code span is not linted.
out = ValeLint("Use `very` here.\n", "md", custom);
assert.deepEqual(JSON.parse(out.result)["input.md"], []);

// A code file's comments are read by their delimiters until its grammar is
// added, so a string that looks like a comment is read too.
const js = "// This is very good.\nconst a = \"// very\";\n";
out = ValeLint(js, "js", custom);
assert.equal(out.error, undefined, out.error);
assert.deepEqual(JSON.parse(out.result)["input.js"].map((a) => a.Line), [1, 2]);

// With the grammar in place, under config/grammars, the file is parsed and
// only the comment is linted. The module comes from grammars/build.sh.
const grammar = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..", "..", "grammars", "dist", "javascript.wasm");
if (fs.existsSync(grammar)) {
  ValeAddFile("/styles/config/grammars/javascript.wasm", new Uint8Array(fs.readFileSync(grammar)));
  out = ValeLint(js, "js", custom);
  assert.equal(out.error, undefined, out.error);
  assert.deepEqual(JSON.parse(out.result)["input.js"].map((a) => [a.Line, a.Span]), [[1, [12, 15]]]);
} else {
  console.log("skipping the grammar case: run grammars/build.sh");
}

// Plain text and HTML are read.
out = ValeLint("This is very good.\n", "txt", custom);
assert.equal(JSON.parse(out.result)["input.txt"].length, 1);
out = ValeLint("<p>This is <code>very</code> very good.</p>\n", "html", custom);
assert.deepEqual(JSON.parse(out.result)["input.html"].map((a) => a.Span), [[30, 33]]);

// A format Vale reads through a converter is linted from HTML the host
// converted, with the same arguments Vale would have used. The fixtures
// were produced by the real converters with those arguments.
const fixtures = path.join(path.dirname(new URL(import.meta.url).pathname), "fixtures");
const built = (name) => fs.readFileSync(path.join(fixtures, name), "utf8");
const builtCases = [
  // A source block, a line comment, and a text-less xref are not prose.
  ["adoc", [[3, [9, 12]], [15, [6, 9]]]],
  ["rst", [[4, [9, 12]], [10, [6, 9]]]],
  ["xml", [[4, [17, 20]], [6, [14, 17]]]],
  ["dita", [[6, [16, 19]], [8, [13, 16]]]],
];
for (const [format, want] of builtCases) {
  out = ValeLintBuilt(built(`doc.${format}`), format, built(`doc.${format}.html`), custom);
  assert.equal(out.error, undefined, `${format}: ${out.error}`);
  alerts = JSON.parse(out.result)[`input.${format}`];
  assert.deepEqual(alerts.map((a) => [a.Line, a.Span]), want, format);
}
out = ValeLintBuilt("# Title\n", "md", "<h1>Title</h1>", custom);
assert.match(out.error, /no built form/);

// The host is told what to give its converter.
out = ValeConverterOptions("adoc", custom);
assert.ok(out.attributes.includes("notitle!") && out.attributes.includes("sectnums!"), "adoc attributes");
assert.equal(out.safe, "secure");
assert.match(out.converter, /ValeHtml5Converter/);
assert.deepEqual(ValeConverterOptions("xml", custom).params, { "use.extensions": "0", "generate.toc": "nop" });
assert.ok(ValeConverterOptions("rst", custom).args.includes("--no-section-numbering"));
assert.match(ValeConverterOptions("md", custom).error, /no converter options/);

// A configuration resolves to each section's rules and their states.
ValeAddFile("/styles/T/W.yml", "extends: existence\nmessage: \"'%s'\"\nlevel: error\ntokens:\n  - very\n");
ValeAddFile("/styles/T/Off.yml", "extends: existence\nmessage: \"'%s'\"\nlevel: error\ntokens:\n  - off\n");
out = ValeResolveConfig("StylesPath = /styles\nMinAlertLevel = warning\n\n[*.md]\nBasedOnStyles = T\nT.Off = NO\nT.W = suggestion\n");
assert.equal(out.error, undefined, out.error);
const resolved = JSON.parse(out.result);
assert.equal(resolved.error, undefined, resolved.error);
assert.equal(resolved.global.minAlertLevel, "warning");
assert.deepEqual(resolved.sections.map((s) => [s.glob, s.active, s.overridden, s.disabled]), [["*.md", 0, 1, 1]]);
out = ValeResolveConfig("MinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = Vale\n");
assert.deepEqual(JSON.parse(out.result).sections.map((s) => [s.glob, s.active > 0]), [["*", true]]);
assert.deepEqual(resolved.sections[0].rules.map((r) => [r.ref, r.state, r.level ?? ""]), [["T.Off", "disabled", ""], ["T.W", "override", "suggestion"]]);

ValeResetFS();
assert.deepEqual(ValeListFS().paths, []);

console.log("ok");
process.exit(0);
