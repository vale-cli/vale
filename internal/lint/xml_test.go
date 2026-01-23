package lint

import (
	"testing"
)

func TestXSLTArgsNotPolluted(t *testing.T) {
	// We want to test that xsltArgs in xml.go is not mutated.
	initialLen := len(xsltArgs)

	// Simulate what lintXML does multiple times
	for i := 0; i < 3; i++ {
		args := append([]string{}, xsltArgs...)
		args = append(args, []string{"transform.xsl", "-"}...)

		if len(xsltArgs) != initialLen {
			t.Fatalf("xsltArgs was modified! original: %d, current: %d", initialLen, len(xsltArgs))
		}

		expectedArgsLen := initialLen + 2
		if len(args) != expectedArgsLen {
			t.Errorf("local args have wrong length: %d, expected %d", len(args), expectedArgsLen)
		}
	}
}
