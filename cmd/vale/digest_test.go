package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const zeros = "0000000000000000000000000000000000000000000000000000000000000000"

func TestSplitDigest(t *testing.T) {
	d := strings.Repeat("ab", 32)
	for in, want := range map[string][2]string{
		"https://x.test/Pkg.zip":                       {"https://x.test/Pkg.zip", ""},
		"https://x.test/Pkg.zip#sha256:" + d:           {"https://x.test/Pkg.zip", d},
		"Google#sha256:" + strings.ToUpper(d):          {"Google", d},
		"path/to/Pkg.zip#sha256:" + strings.ToLower(d): {"path/to/Pkg.zip", d},
	} {
		pkg, digest, err := splitDigest(in)
		if err != nil || pkg != want[0] || digest != want[1] {
			t.Errorf("%s: got (%q, %q, %v), want %q", in, pkg, digest, err, want)
		}
	}
	for _, bad := range []string{"Pkg.zip#sha256:abc", "Pkg.zip#sha256:" + strings.Repeat("zz", 32), "Pkg.zip#sha256:"} {
		if _, _, err := splitDigest(bad); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
}

// served holds one archive's bytes, served the same on every request.
func served(t *testing.T, floor string) (*httptest.Server, string) {
	t.Helper()
	body := zipOf(t, map[string]string{
		"Pkg/Rule.yml": "extends: existence\nmessage: pinned\ntokens:\n  - x\n",
	})
	if floor != "" {
		// One file at a time: a map's order would change the bytes.
		body = zipOf(t, map[string]string{"Pkg/meta.json": `{"vale_version": "` + floor + `"}`})
	}
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, hex.EncodeToString(sum[:])
}

func TestSyncChecksAPinnedDigest(t *testing.T) {
	srv, digest := served(t, "")
	url := srv.URL + "/releases/download/v0.1.0/Pkg.zip"

	t.Run("the pinned archive installs", func(t *testing.T) {
		styles := t.TempDir()
		got, err := syncPkg(url+"#sha256:"+digest, styles, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != digest || installed(t, styles) != "pinned" {
			t.Errorf("got digest %s and %q installed", got, installed(t, styles))
		}
	})

	t.Run("another archive stops the sync", func(t *testing.T) {
		styles := t.TempDir()
		_, err := syncPkg(url+"#sha256:"+zeros, styles, 0)
		if err == nil || !strings.Contains(err.Error(), "doesn't match the digest") {
			t.Fatalf("got %v, want a mismatch", err)
		}
		if _, serr := os.Stat(filepath.Join(styles, "Pkg")); serr == nil {
			t.Error("a mismatched archive was installed")
		}
	})

	t.Run("an unpinned archive reports its digest", func(t *testing.T) {
		got, err := syncPkg(url, t.TempDir(), 0)
		if err != nil || got != digest {
			t.Errorf("got (%s, %v), want %s", got, err, digest)
		}
	})

	t.Run("a local archive is checked too", func(t *testing.T) {
		resp, err := http.Get(url) //nolint:noctx,gosec // a test server
		if err != nil {
			t.Fatal(err)
		}
		zip := filepath.Join(t.TempDir(), "Pkg.zip")
		f, _ := os.Create(zip)
		_, _ = f.ReadFrom(resp.Body)
		f.Close()
		resp.Body.Close()
		if _, err = syncPkg(zip+"#sha256:"+digest, t.TempDir(), 0); err != nil {
			t.Error(err)
		}
		if _, err = syncPkg(zip+"#sha256:"+zeros, t.TempDir(), 0); err == nil {
			t.Error("want a mismatch for a local archive")
		}
	})

	t.Run("a directory can't be pinned", func(t *testing.T) {
		dir, err := filepath.Abs(filepath.Join(TestData, "write-good"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = syncPkg(dir+"#sha256:"+digest, t.TempDir(), 0)
		if err == nil || !strings.Contains(err.Error(), "only an archive") {
			t.Errorf("got %v, want a directory error", err)
		}
	})
}

// A pinned archive that needs a newer Vale is an error: walking back to
// another release would install other bytes than the ones pinned.
func TestAPinnedArchiveIsNeverSwapped(t *testing.T) {
	withVersion(t, "3.22.0")
	srv, digest := served(t, ">=99.0.0")
	styles := t.TempDir()
	_, err := syncPkg(srv.URL+"/releases/latest/download/Pkg.zip#sha256:"+digest, styles, 0)
	if err == nil || !strings.Contains(err.Error(), "pinned to a digest") {
		t.Fatalf("got %v, want a refusal", err)
	}
}
