package core

import (
	"path/filepath"
	"strings"
)

// SyncFile, in a StylesPath, records the last `vale sync` there: a digest
// of what it installed from, then each style it installed, one per line.
const SyncFile = ".vale-sync"

// SyncedStyles is the set of styles `vale sync` installed on the
// configuration's search paths.
func (c *Config) SyncedStyles() map[string]bool {
	styles := map[string]bool{}
	for _, p := range c.SearchPaths() {
		b, err := c.FS.ReadFile(filepath.Join(p, SyncFile))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		for _, name := range lines[1:] {
			if name = strings.TrimSpace(name); name != "" {
				styles[name] = true
			}
		}
	}
	return styles
}
