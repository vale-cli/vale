package lint

import (
	"strings"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"

	"github.com/errata-ai/vale/v3/internal/core"
)

func findBestLineBySubstring(s, sub string) (int, string) {
	if strings.Count(sub, "\n") > 0 {
		sub = strings.Split(sub, "\n")[0]
	}

	bestMatchLine := -1
	bestMatch := ""
	bestMatchDistance := -1.0

	metric := metrics.NewLevenshtein()
	for i, line := range strings.Split(s, "\n") {
		if !strings.Contains(line, sub) {
			continue
		}

		distance := strutil.Similarity(line, sub, metric)
		if bestMatchLine == -1 || distance < bestMatchDistance {
			bestMatchDistance = distance
			bestMatchLine = i
			bestMatch = line
		}
	}

	return bestMatchLine, bestMatch
}

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
