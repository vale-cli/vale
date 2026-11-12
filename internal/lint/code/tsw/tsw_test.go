//go:build !vale_nocode

package tsw

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// grammar loads a module built by grammars/build.sh, or skips.
func grammar(t *testing.T, name string) *Language {
	t.Helper()
	wasm, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "grammars", "dist", name+".wasm"))
	if err != nil {
		t.Skipf("grammars/dist/%s.wasm not built: %v", name, err)
	}
	lang, err := Load(name, wasm)
	if err != nil {
		t.Fatal(err)
	}
	return lang
}

const goSrc = "package main\n\n// Package main says hello.\nfunc main() {} // trailing\n\n/* block\n   comment */\nvar x = 1\n"

func TestParseAndQuery(t *testing.T) {
	lang := grammar(t, "go")
	tree, err := lang.Parse([]byte(goSrc))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	if got := tree.RootNode().Type(); got != "source_file" {
		t.Errorf("root type = %q", got)
	}

	q, err := tree.Query("(comment) @comment")
	if err != nil {
		t.Fatal(err)
	}
	if q.CaptureNameForID(0) != "comment" {
		t.Errorf("capture name = %q", q.CaptureNameForID(0))
	}

	var got []string
	for _, m := range tree.Matches(q, []byte(goSrc)) {
		for _, c := range m.Captures {
			got = append(got, c.Node.Content([]byte(goSrc)))
		}
	}
	want := []string{"// Package main says hello.", "// trailing", "/* block\n   comment */"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("comments = %q, want %q", got, want)
	}

	// Node navigation, as a doc-name lookup uses it.
	first := tree.Matches(q, []byte(goSrc))[0].Captures[0].Node
	if p := first.StartPoint(); p.Row != 2 || p.Column != 0 {
		t.Errorf("start point = %+v", p)
	}
	next := first.NextNamedSibling()
	if next == nil || next.Type() != "function_declaration" {
		t.Fatalf("next named sibling = %v", next)
	}
	if name := next.ChildByFieldName("name"); name == nil || name.Content([]byte(goSrc)) != "main" {
		t.Errorf("function name = %v", name)
	}
	if next.ChildByFieldName("nope") != nil || next.NamedChildCount() == 0 {
		t.Error("field lookup and child count disagree with the tree")
	}
	if !strings.HasPrefix(next.String(), "(function_declaration") {
		t.Errorf("sexp = %q", next.String())
	}
}

// Predicates are evaluated as the CGo binding evaluated them.
func TestPredicates(t *testing.T) {
	lang := grammar(t, "go")
	src := []byte(goSrc)
	tree, err := lang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	count := func(expr string) int {
		q, qerr := tree.Query(expr)
		if qerr != nil {
			t.Fatalf("%s: %v", expr, qerr)
		}
		return len(tree.Matches(q, src))
	}
	if n := count(`((comment) @c (#match? @c "^//"))`); n != 2 {
		t.Errorf("match? = %d, want 2", n)
	}
	if n := count(`((comment) @c (#not-match? @c "^//"))`); n != 1 {
		t.Errorf("not-match? = %d, want 1", n)
	}
	if n := count(`((comment) @c (#eq? @c "// trailing"))`); n != 1 {
		t.Errorf("eq? = %d, want 1", n)
	}
	if n := count(`((comment) @c (#not-eq? @c "// trailing"))`); n != 2 {
		t.Errorf("not-eq? = %d, want 2", n)
	}

	if _, bad := tree.Query("(comment @c"); bad == nil || !strings.Contains(bad.Error(), "query:") {
		t.Errorf("a bad query should fail: %v", bad)
	}
}

// Trees borrow instances, so parsing from many goroutines at once is safe.
func TestConcurrentParses(t *testing.T) {
	lang := grammar(t, "go")
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				tree, err := lang.Parse([]byte(goSrc))
				if err != nil {
					t.Error(err)
					return
				}
				q, _ := tree.Query("(comment) @comment")
				if n := len(tree.Matches(q, []byte(goSrc))); n != 3 {
					t.Errorf("got %d comments", n)
				}
				tree.Close()
			}
		}()
	}
	wg.Wait()
}
