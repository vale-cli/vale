package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipOf builds an archive holding the given files.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pkgServer serves a package the way GitHub does: the newest release under
// `releases/latest/download`, each release under `releases/download/<tag>`,
// and the release list as an Atom feed.
func pkgServer(t *testing.T, releases []string, floors map[string]string, withFeed bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	feed := ""
	if withFeed {
		feed = srv.URL
	}
	srv.Config.Handler = mux(t, releases, floors, feed)
	return srv
}

// mux serves the releases; feed, when set, is the host the archives' meta
// names as their feed.
func mux(t *testing.T, releases []string, floors map[string]string, feed string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	archive := func(tag string) []byte {
		meta := `{"vale_version": "` + floors[tag] + `"}`
		if feed != "" {
			meta = `{"vale_version": "` + floors[tag] + `", "feed": "` + feed + `/releases.atom"}`
		}
		return zipOf(t, map[string]string{
			"Pkg/meta.json": meta,
			"Pkg/Rule.yml":  "extends: existence\nmessage: " + tag + "\ntokens:\n  - x\n",
		})
	}
	mux.HandleFunc("/releases/latest/download/Pkg.zip", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive(releases[0]))
	})
	for _, tag := range releases {
		mux.HandleFunc("/releases/download/"+tag+"/Pkg.zip", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(archive(tag))
		})
	}
	mux.HandleFunc("/releases.atom", func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom">`)
		for _, tag := range releases {
			b.WriteString(`<entry><id>tag:github.com,2008:Repository/1/` + tag + `</id>` +
				`<link rel="alternate" type="text/html" href="https://example.com/releases/tag/` + tag + `"/></entry>`)
		}
		b.WriteString(`</feed>`)
		_, _ = w.Write([]byte(b.String()))
	})
	return mux
}

// installed returns the release whose rule landed in styles, by its message.
func installed(t *testing.T, styles string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(styles, "Pkg", "Rule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "message: ") {
			return strings.TrimPrefix(line, "message: ")
		}
	}
	return ""
}

func withVersion(t *testing.T, v string) {
	t.Helper()
	was := version
	version = v
	t.Cleanup(func() { version = was })
}

func TestSyncInstallsTheReleaseThisValeSupports(t *testing.T) {
	releases := []string{"v0.3.0", "v0.2.0", "v0.1.0"}
	floors := map[string]string{"v0.3.0": ">=99.0.0", "v0.2.0": ">=98.0.0", "v0.1.0": ">=3.0.0"}
	srv := pkgServer(t, releases, floors, true)
	latest := srv.URL + "/releases/latest/download/Pkg.zip"

	t.Run("walks back to a supported release", func(t *testing.T) {
		withVersion(t, "3.22.0")
		styles := t.TempDir()
		if _, err := download("Pkg", latest, styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "v0.1.0" {
			t.Errorf("installed %s, want v0.1.0", got)
		}
	})

	t.Run("takes the latest when it fits", func(t *testing.T) {
		withVersion(t, "99.1.0")
		styles := t.TempDir()
		if _, err := download("Pkg", latest, styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "v0.3.0" {
			t.Errorf("installed %s, want v0.3.0", got)
		}
	})

	t.Run("a development build meets every floor", func(t *testing.T) {
		withVersion(t, "master")
		styles := t.TempDir()
		if _, err := download("Pkg", latest, styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "v0.3.0" {
			t.Errorf("installed %s, want v0.3.0", got)
		}
	})

	t.Run("a pinned release is installed as asked", func(t *testing.T) {
		withVersion(t, "3.22.0")
		styles := t.TempDir()
		if _, err := download("Pkg", srv.URL+"/releases/download/v0.2.0/Pkg.zip", styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "v0.2.0" {
			t.Errorf("installed %s, want v0.2.0", got)
		}
	})

	t.Run("derives the feed from the URL when meta has none", func(t *testing.T) {
		withVersion(t, "3.22.0")
		bare := pkgServer(t, releases, floors, false)
		styles := t.TempDir()
		if _, err := download("Pkg", bare.URL+"/releases/latest/download/Pkg.zip", styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "v0.1.0" {
			t.Errorf("installed %s, want v0.1.0", got)
		}
	})

	t.Run("nothing fitting installs the latest, as before", func(t *testing.T) {
		withVersion(t, "2.0.0")
		styles := t.TempDir()
		if _, err := download("Pkg", latest, styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "v0.3.0" {
			t.Errorf("installed %s, want v0.3.0", got)
		}
	})

	t.Run("an unreadable feed installs the latest, as before", func(t *testing.T) {
		withVersion(t, "3.22.0")
		mute := pkgServer(t, releases, floors, true)
		mute.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".atom") {
				http.NotFound(w, r)
				return
			}
			mux(t, releases, floors, mute.URL).ServeHTTP(w, r)
		})
		styles := t.TempDir()
		if _, err := download("Pkg", mute.URL+"/releases/latest/download/Pkg.zip", styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "v0.3.0" {
			t.Errorf("installed %s, want v0.3.0", got)
		}
	})
}

func TestSupportsVale(t *testing.T) {
	withVersion(t, "3.22.0")
	for constraint, want := range map[string]bool{
		"":          true,
		">=3.20.0":  true,
		">=3.22.0":  true,
		">=3.23.0":  false,
		"not a ver": true,
	} {
		if got := supportsVale(constraint); got != want {
			t.Errorf("supportsVale(%q) = %v, want %v", constraint, got, want)
		}
	}
}

// A manifest in meta.json names each release and the Vale it needs, on any
// host: the archive to install is chosen before it is downloaded, and the
// URLs can be anything.
func TestSyncInstallsFromTheReleaseManifest(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	manifest := `[` +
		`{"version": "0.1.0", "vale_version": ">=3.0.0", "url": "` + srv.URL + `/archive/one.zip"},` +
		`{"version": "0.3.0", "vale_version": ">=99.0.0", "url": "` + srv.URL + `/archive/three.zip"},` +
		`{"version": "0.2.0", "vale_version": ">=3.20.0", "url": "` + srv.URL + `/archive/two.zip"}]`
	archive := func(tag, floor string) []byte {
		return zipOf(t, map[string]string{
			"Pkg/meta.json": `{"vale_version": "` + floor + `", "releases": ` + manifest + `}`,
			"Pkg/Rule.yml":  "extends: existence\nmessage: " + tag + "\ntokens:\n  - x\n",
		})
	}
	for path, body := range map[string][]byte{
		"/current/Pkg.zip":   archive("0.3.0", ">=99.0.0"),
		"/archive/one.zip":   archive("0.1.0", ">=3.0.0"),
		"/archive/two.zip":   archive("0.2.0", ">=3.20.0"),
		"/archive/three.zip": archive("0.3.0", ">=99.0.0"),
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	}

	t.Run("picks the newest release this Vale supports", func(t *testing.T) {
		withVersion(t, "3.22.0")
		styles := t.TempDir()
		if _, err := download("Pkg", srv.URL+"/current/Pkg.zip", styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "0.2.0" {
			t.Errorf("installed %s, want 0.2.0", got)
		}
	})

	t.Run("nothing fitting installs the latest, as before", func(t *testing.T) {
		withVersion(t, "2.0.0")
		styles := t.TempDir()
		if _, err := download("Pkg", srv.URL+"/current/Pkg.zip", styles, 0, ""); err != nil {
			t.Fatal(err)
		}
		if got := installed(t, styles); got != "0.3.0" {
			t.Errorf("installed %s, want 0.3.0", got)
		}
	})
}

func TestSortedReleases(t *testing.T) {
	got := sortedReleases([]Release{{Version: "v0.1.0"}, {Version: "draft"}, {Version: "0.10.0"}, {Version: "v0.2.0"}})
	want := "0.10.0 v0.2.0 v0.1.0 draft"
	var names []string
	for _, r := range got {
		names = append(names, r.Version)
	}
	if s := strings.Join(names, " "); s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}
