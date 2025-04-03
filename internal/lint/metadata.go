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

	body, err := frontmatter.Parse(strings.NewReader(f.Content), &metadata)
	if errors.Is(err, frontmatter.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}

	frontmatter, fmErr := extractFrontMatter(f.Content, string(body))
	if fmErr != nil {
		return fmErr
	}

	for key, value := range metadata {
		if s, ok := value.(string); ok {
			i, _ := findBestLineBySubstring(frontmatter, s)
			if i < 0 {
				continue
			}

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

func extractFrontMatter(file, body string) (string, error) {
	startIndex := strings.Index(file, body)
	if startIndex == -1 {
		return "", fmt.Errorf("body not found in the file")
	}
	return file[:startIndex], nil
}
