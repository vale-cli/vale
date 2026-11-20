//go:build !vale_nodict

package spell

import (
	"fmt"
	"strings"

	"golang.org/x/text/encoding/ianaindex"
)

// lookupCharset finds a decoder by the name a `.aff` uses; Hunspell writes
// `ISO8859-1` where the registry spells it `ISO-8859-1`.
func lookupCharset(name string) (func([]byte) ([]byte, error), error) {
	iana := strings.Replace(strings.ToUpper(name), "ISO8859-", "ISO-8859-", 1)
	enc, err := ianaindex.IANA.Encoding(iana)
	if err != nil || enc == nil {
		return nil, fmt.Errorf("SET %s: unsupported encoding", name)
	}
	return enc.NewDecoder().Bytes, nil
}
