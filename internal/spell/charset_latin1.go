//go:build vale_nodict

// The build without the encoding registry reads UTF-8 and ISO-8859-1, the
// two encodings Hunspell dictionaries default to.
package spell

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func lookupCharset(name string) (func([]byte) ([]byte, error), error) {
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "")) {
	case "ISO88591", "LATIN1":
		return latin1, nil
	}
	return nil, fmt.Errorf("SET %s: unsupported encoding in this build", name)
}

// latin1 decodes ISO-8859-1, whose bytes are the first 256 code points.
func latin1(b []byte) ([]byte, error) {
	out := make([]byte, 0, len(b)+len(b)/8)
	for _, c := range b {
		out = utf8.AppendRune(out, rune(c))
	}
	return out, nil
}
