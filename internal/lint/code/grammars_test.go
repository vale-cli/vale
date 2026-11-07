package code

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The tests read the modules grammars/build.sh writes.
func TestMain(m *testing.M) {
	Loader = func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "grammars", "dist", name+".wasm"))
		if err != nil {
			return nil, fmt.Errorf("grammar %q: run grammars/build.sh first: %w", name, err)
		}
		return b, nil
	}
	os.Exit(m.Run())
}
