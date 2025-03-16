package lint

import (
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
)

func findLineBySubstring(s, sub string, seen map[string]int) (int, string) {
	if strings.Count(sub, "\n") > 0 {
		sub = strings.Split(sub, "\n")[0]
	}

	for i, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			if j, ok := seen[line]; !ok || j-1 != i {
				return i + 1, line
			}
		}
	}

	return -1, ""
}

func adjustPos(alerts []core.Alert, last, line, padding int) []core.Alert {
	for i := range alerts {
		if i >= last {
			alerts[i].Line += line - 1
			alerts[i].Span = []int{
				alerts[i].Span[0] + padding,
				alerts[i].Span[1] + padding,
			}
		}
	}
	return alerts
}
