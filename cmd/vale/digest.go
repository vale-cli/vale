package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
)

// digestMark pins a package to the SHA-256 digest of its archive:
//
//	Packages = https://example.com/Style.zip#sha256:<64 hexadecimal digits>
//
// `sync` checks the archive before unpacking it and stops on a mismatch, so
// an archive replaced since it was pinned is never installed. See #1236.
const digestMark = "#sha256:"

// splitDigest separates a package from the digest it's pinned to, which is
// "" for an unpinned one.
func splitDigest(pkg string) (string, string, error) {
	i := strings.LastIndex(pkg, digestMark)
	if i < 0 {
		return pkg, "", nil
	}
	d := strings.ToLower(pkg[i+len(digestMark):])
	if _, err := hex.DecodeString(d); err != nil || len(d) != sha256.Size*2 {
		return "", "", core.NewE100("sync",
			fmt.Errorf("'%s': a sha256 digest is %d hexadecimal digits", pkg, sha256.Size*2))
	}
	return pkg[:i], d, nil
}

// fileDigest is the SHA-256 digest of the file at path.
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkDigest reports a pinned archive that isn't the one pinned.
func checkDigest(name, want, got string) error {
	if want == "" || want == got {
		return nil
	}
	return core.NewE100("sync", fmt.Errorf(
		"'%s' doesn't match the digest it's pinned to: expected sha256:%s, got sha256:%s",
		name, want, got))
}
