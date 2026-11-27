package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/pterm/pterm"
)

// A package declares the Vale it needs in its meta.json, as `vale_version`,
// and its earlier releases either as a `releases` manifest, each with its
// own floor and archive URL, or by a `feed` of them. Sync reads all three:
// when the release it fetched needs a newer Vale than this one, the newest
// release that this Vale supports is installed instead -- picked from the
// manifest on any host, or, for a package served from a GitHub
// latest-release URL, found by walking the feed.
//
// When nothing fits, the fetched release is installed with a warning, which
// is what sync always did. The metadata can only ever improve on that: a
// floor mistyped too high must not turn into a package nobody can install.

// latestRelease is the path GitHub serves a repository's newest release
// asset under, which is how the package library names every package.
const latestRelease = "/releases/latest/download/"

// releaseWalk is how many releases back sync looks for one it supports.
const releaseWalk = 10

// pkgMeta reads the meta.json of the package under dir, or nothing.
func pkgMeta(dir, name string) Meta {
	var meta Meta
	b, err := os.ReadFile(filepath.Join(dir, pkgRoot(dir, name), "meta.json"))
	if err == nil {
		_ = json.Unmarshal(b, &meta)
	}
	return meta
}

// supportsVale reports whether this Vale meets a package's `vale_version`.
//
// A development build, `master`, meets every floor, as does a floor that
// is missing or that cannot be read: a package's metadata is not a reason
// to refuse it.
func supportsVale(constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || version == "master" {
		return true
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return true
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		return true
	}
	return c.Check(v)
}

// supportedRelease returns the directory to install the package from: dir,
// where url was unpacked, when this Vale supports what is there, or else a
// release from the package's feed that it does support.
func supportedRelease(name, url, dir string) (string, error) {
	meta := pkgMeta(dir, name)
	if supportsVale(meta.Vale) {
		return dir, nil
	}

	need := fmt.Sprintf("'%s' needs Vale %s; this is %s", name, meta.Vale, version)

	// A manifest names each release and what it needs, so the one to install
	// is known before anything else is downloaded, on any host. Without one,
	// a GitHub latest-release URL has a feed to walk.
	var candidates []Release
	switch {
	case len(meta.Releases) > 0:
		for _, rel := range sortedReleases(meta.Releases) {
			if supportsVale(rel.Vale) && rel.URL != "" && rel.URL != url {
				candidates = append(candidates, rel)
			}
		}
	case strings.Contains(url, latestRelease):
		feed := meta.Feed
		if feed == "" {
			feed = strings.SplitN(url, latestRelease, 2)[0] + "/releases.atom"
		}
		tags, err := releaseTags(feed)
		if err != nil {
			pterm.Warning.Printfln("%s, and its releases could not be read: %s.", need, err)
			return dir, nil
		}
		for i, tag := range tags {
			if i == releaseWalk {
				break
			}
			candidates = append(candidates, Release{
				Version: tag,
				URL:     strings.Replace(url, latestRelease, "/releases/download/"+tag+"/", 1),
			})
		}
	}

	for _, rel := range candidates {
		tmp, err := os.MkdirTemp("", name)
		if err != nil {
			return "", err
		}
		if fetch(rel.URL, tmp) != nil {
			os.RemoveAll(tmp)
			continue
		}
		// A manifest entry has stated its floor; a feed entry is checked
		// from the archive it came in.
		if len(meta.Releases) > 0 || supportsVale(pkgMeta(tmp, name).Vale) {
			pterm.Info.Printfln("%s: installed %s instead.", need, rel.Version)
			return tmp, nil
		}
		os.RemoveAll(tmp)
	}

	// Nothing fits, or the URL is pinned: what was asked for is installed,
	// as it always was, and the user knows what it needs.
	pterm.Warning.Println(need + ".")
	return dir, nil
}

// releaseTags lists a package's releases, newest first, from its Atom feed.
func releaseTags(feed string) ([]string, error) {
	resp, err := httpGet(feed)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not fetch '%s' (status code '%d')", feed, resp.StatusCode)
	}

	// A feed is a few kilobytes; one that is not stops here.
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var atom struct {
		Entries []struct {
			ID   string `xml:"id"`
			Link struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err = xml.Unmarshal(b, &atom); err != nil {
		return nil, err
	}

	var tags []string
	for _, entry := range atom.Entries {
		// GitHub's entries carry the tag at the end of both the id and the
		// link, `.../releases/tag/v0.1.1`.
		ref := entry.Link.Href
		if ref == "" {
			ref = entry.ID
		}
		if tag := ref[strings.LastIndex(ref, "/")+1:]; tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

// sortedReleases orders a manifest newest first by version, leaving entries
// whose version does not parse where the author put them, after the rest.
func sortedReleases(releases []Release) []Release {
	out := make([]Release, len(releases))
	copy(out, releases)
	sort.SliceStable(out, func(i, j int) bool {
		vi, ei := semver.NewVersion(out[i].Version)
		vj, ej := semver.NewVersion(out[j].Version)
		if ei != nil || ej != nil {
			return ei == nil && ej != nil
		}
		return vi.GreaterThan(vj)
	})
	return out
}
