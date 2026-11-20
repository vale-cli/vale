//go:build vale_nodict

package spell

import (
	"errors"
	"testing"
)

// Without the default dictionary, a checker that needs it says so, and a
// rule's own dictionary still loads from UTF-8 or Latin-1.
func TestNoDefaultDictionary(t *testing.T) {
	if _, err := NewChecker(); !errors.Is(err, ErrNoDictionary) {
		t.Errorf("NewChecker() = %v, want ErrNoDictionary", err)
	}

	aff := []byte("SET ISO8859-1\n")
	dic := []byte("1\ncaf\xe9\n")
	_, got, err := decodeDictionary(aff, dic)
	if err != nil || string(got) != "1\ncafé\n" {
		t.Errorf("Latin-1 dictionary = %q, %v", got, err)
	}
	if _, _, kerr := decodeDictionary([]byte("SET KOI8-R\n"), dic); kerr == nil {
		t.Error("an encoding this build can't read was accepted")
	}
}
