package spell

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// utf8BOM is the byte order mark some dictionaries start with.
var utf8BOM = []byte("\xef\xbb\xbf")

// decodeDictionary converts a dictionary's files to UTF-8, as the reader
// expects. The `.aff` says how it is encoded with SET; Hunspell's default is
// ISO8859-1, but a file without SET that is valid UTF-8 is read as UTF-8, as
// it always was here.
func decodeDictionary(aff, dic []byte) ([]byte, []byte, error) {
	aff = bytes.TrimPrefix(aff, utf8BOM)
	dic = bytes.TrimPrefix(dic, utf8BOM)

	charset := declaredCharset(aff)
	switch {
	case charset == "" && utf8.Valid(aff) && utf8.Valid(dic):
		return aff, dic, nil
	case charset == "":
		charset = "ISO8859-1"
	case strings.EqualFold(charset, "UTF-8"), strings.EqualFold(charset, "UTF8"):
		return aff, dic, nil
	}

	decode, err := lookupCharset(charset)
	if err != nil {
		return nil, nil, err
	}
	if aff, err = decode(aff); err != nil {
		return nil, nil, fmt.Errorf("decode .aff as %s: %w", charset, err)
	}
	if dic, err = decode(dic); err != nil {
		return nil, nil, fmt.Errorf("decode .dic as %s: %w", charset, err)
	}
	return aff, dic, nil
}

// declaredCharset returns the SET value of a `.aff`, or "".
func declaredCharset(aff []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(aff))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "SET" {
			return fields[1]
		}
	}
	return ""
}
