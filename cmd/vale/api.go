package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// Style represents an externally-hosted style.
type Style struct {
	// User-provided fields.
	Author      string `json:"author"`
	Description string `json:"description"`
	Deps        string `json:"deps"`
	Feed        string `json:"feed"`
	Homepage    string `json:"homepage"`
	Name        string `json:"name"`
	URL         string `json:"url"`

	// Generated fields.
	HasUpdate bool `json:"has_update"`
	InLibrary bool `json:"in_library"`
	Installed bool `json:"installed"`
	Addon     bool `json:"addon"`
}

// Meta represents an installed style's meta data.
type Meta struct {
	Author      string   `json:"author"`
	Coverage    float64  `json:"coverage"`
	Description string   `json:"description"`
	Email       string   `json:"email"`
	Feed        string   `json:"feed"`
	Lang        string   `json:"lang"`
	License     string   `json:"license"`
	Name        string   `json:"name"`
	Sources     []string `json:"sources"`
	URL         string   `json:"url"`
	Vale        string   `json:"vale_version"`
	Version     string   `json:"version"`

	// Releases lists the package's releases and the Vale each needs, so
	// sync can pick one without walking a host's feed. See pkgver.go.
	Releases []Release `json:"releases"`
}

// A Release is one entry of a package's release manifest.
type Release struct {
	Version string `json:"version"`
	Vale    string `json:"vale_version"`
	URL     string `json:"url"`
}

func init() {
	pflag.BoolVar(&Flags.Remote, "mode-rev-compat", false,
		"Prioritize remote Vale configurations.")
	pflag.StringVar(&Flags.Built, "built", "", "A post-processed file path.")

	commands["install"] = command{
		Run:     install,
		Summary: "Install a package from a URL.",
		Hidden:  true,
	}
}

func fetch(src, dst string) error {
	_, err := fetchPinned(src, dst, "")
	return err
}

// fetchPinned downloads the archive at src and unpacks it into dst,
// returning its SHA-256 digest. A `want` digest it doesn't match stops it
// before anything is unpacked.
func fetchPinned(src, dst, want string) (string, error) {
	// Fetch the resource from the web:
	resp, err := http.Get(src) //nolint:gosec,noctx

	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not fetch '%s' (status code '%d')", src, resp.StatusCode)
	}

	// Create a temp file to represent the archive locally:
	tmpfile, err := os.CreateTemp("", "temp.*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpfile.Name()) // clean up

	// Write to the local archive, hashing it on the way:
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmpfile, h), resp.Body)
	if err != nil {
		return "", err
	} else if err = tmpfile.Close(); err != nil {
		return "", err
	}

	got := hex.EncodeToString(h.Sum(nil))
	if err = checkDigest(src, want, got); err != nil {
		return got, err
	}
	return got, system.Unarchive(tmpfile.Name(), dst)
}

func install(args []string, flags *core.CLIFlags) error {
	cfg, err := core.ReadPipeline(flags, false)
	if err != nil {
		return err
	}

	style := filepath.Join(cfg.StylesPath(), args[0])
	if system.IsDir(style) {
		os.RemoveAll(style) // Remove existing version
	}

	err = fetch(args[1], cfg.StylesPath())
	if err != nil {
		return sendResponse(
			fmt.Sprintf("Failed to install '%s'", args[1]),
			err)
	}

	return sendResponse(fmt.Sprintf(
		"Successfully installed '%s'", args[1]), nil)
}
