package lint

import "testing"

func TestCleanMDXError(t *testing.T) {
	// mdx2vast surfaces parse failures as an unhandled-rejection dump; we want
	// the concise `line:col: reason` payload instead. See #995.
	dump := `node:internal/process/promises:394
            triggerUncaughtException(err, true /* fromPromise */);
            ^

[288:7: Unexpected character ` + "`!`" + ` (U+0021) before name (note: to create a comment in MDX, use ` + "`{/* text */}`" + `)] {
  ancestors: undefined,
  reason: 'Unexpected character',
}

Node.js v18.19.0`

	want := "288:7: Unexpected character `!` (U+0021) before name (note: to create a comment in MDX, use `{/* text */}`)"
	if got := cleanMDXError(dump); got != want {
		t.Errorf("cleanMDXError(dump) =\n  %q\nwant\n  %q", got, want)
	}

	// Fallback: no bracketed payload -> first meaningful line.
	fallback := "\n\nsome other failure\n  at Object.<anonymous>\n"
	if got := cleanMDXError(fallback); got != "some other failure" {
		t.Errorf("cleanMDXError(fallback) = %q, want %q", got, "some other failure")
	}
}
