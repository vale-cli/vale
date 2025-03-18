package lint

import (
	"errors"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/system"
)

func (l Linter) lintMDX(f *core.File) error {
	var html string
	var err error

	exe := system.Which([]string{"mdx2vast"})
	if exe == "" {
		return core.NewE100("lintMDX", errors.New("mdx2vast not found"))
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	html, err = system.ExecuteWithInput(exe, s)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	f.Content = prepMarkdown(f.Content)
	return l.lintHTMLTokens(f, []byte(html), 0)
}
