package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
)

// stampFile records what the last sync installed, so `vale sync --if-needed`
// can skip one that would install the same thing. It sits outside PipeDir,
// whose every file is read as configuration.
const stampFile = core.SyncFile

// installedStyles collects the styles the running sync installs, for the
// stamp: a rule from one of them that won't compile is skipped, not fatal.
var installedStyles = map[string]bool{}

// syncStamp identifies a sync by what it would install and the Vale doing it,
// since a different Vale can pick a different release of the same package.
func syncStamp(cfg *core.Config, pkgs []string) string {
	grammars, _ := grammarsToSync(cfg)

	h := sha256.New()
	for _, part := range [][]string{{version}, pkgs, cfg.Languages(), grammars} {
		h.Write([]byte(strings.Join(part, "\n")))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func stampMatches(stylesPath, stamp string) bool {
	b, err := os.ReadFile(filepath.Join(stylesPath, stampFile))
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(first) == stamp
}

func writeStamp(stylesPath, stamp string) error {
	names := make([]string, 0, len(installedStyles))
	for name := range installedStyles {
		names = append(names, name)
	}
	sort.Strings(names)

	body := strings.Join(append([]string{stamp}, names...), "\n") + "\n"
	return os.WriteFile(filepath.Join(stylesPath, stampFile), []byte(body), 0o600)
}
