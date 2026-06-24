package lint

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/system"
)

// mdxErrorRe matches the `line:col: reason` payload that mdx2vast's underlying
// parser reports (e.g. `[3:12: Unexpected character ...]`) so we can surface a
// concise message instead of mdx2vast's raw Node stack trace. See #995.
var mdxErrorRe = regexp.MustCompile(`\[(\d+:\d+: [^\]]+)\]`)

// cleanMDXError extracts a readable parse error from mdx2vast's stderr, which
// otherwise arrives as an unhandled-rejection dump. It falls back to the first
// meaningful line, then to the raw output.
func cleanMDXError(stderr string) string {
	if m := mdxErrorRe.FindStringSubmatch(stderr); m != nil {
		return m[1]
	}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "at ") && !strings.HasPrefix(line, "^") {
			return line
		}
	}
	return strings.TrimSpace(stderr)
}

func (l Linter) lintMDX(f *core.File) error {
	var html string
	var err error

	exe := system.Which([]string{"mdx2vast"})
	if exe == "" {
		return core.NewE100("lintMDX", errors.New("mdx2vast not found"))
	}

	err = l.lintMetadata(f)
	if err != nil {
		return err
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	html, err = system.ExecuteWithInput(exe, s)
	if err != nil {
		return core.NewE100(f.Path,
			fmt.Errorf("failed to parse MDX: %s", cleanMDXError(err.Error())))
	}

	f.Content = prepMarkdown(f.Content)
	return l.lintHTMLTokens(f, []byte(html), 0)
}
