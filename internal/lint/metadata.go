package lint

import (
	"errors"
	"fmt"
	"strings"

	"github.com/adrg/frontmatter"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

func (l Linter) lintMetadata(f *core.File) error {
	metadata := make(map[string]any)

	_, err := frontmatter.Parse(strings.NewReader(f.Content), &metadata)
	if errors.Is(err, frontmatter.ErrNotFound) {
		// No front matter found, return the original content.
		return nil
	} else if err != nil {
		return err
	}

	seen := make(map[string]int)
	for key, value := range metadata {
		if s, ok := value.(string); ok {
			i, line := findLineBySubstring(f.Content, s, seen)
			if i < 0 {
				return core.NewE100(f.Path, fmt.Errorf("'%s' not found", s))
			}
			seen[line] = i

			scope := "text.frontmatter." + key + f.RealExt
			block := nlp.NewLinedBlock(f.Content, s, scope, i-1)

			lErr := l.lintBlock(f, block, len(f.Lines), 0, false)
			if lErr != nil {
				return lErr
			}
		}
	}

	return nil
}
