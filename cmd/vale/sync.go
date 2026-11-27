package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cp "github.com/otiai10/copy"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

func initPath(cfg *core.Config) error {
	// The first entry is always the default `StylesPath`.
	stylesPath := cfg.StylesPath()

	if !system.IsDir(stylesPath) {
		if err := os.MkdirAll(stylesPath, os.ModePerm); err != nil {
			e := fmt.Errorf("unable to initialize StylesPath (value = '%s')", stylesPath)
			return core.NewE100("sync", e)
		}
	}

	// Remove any existing .vale-config directory.
	err := os.RemoveAll(filepath.Join(stylesPath, core.PipeDir))
	if err != nil {
		return core.NewE100("sync", err)
	}

	return nil
}

func readPkg(pkg, path string, idx int) error {
	_, err := syncPkg(pkg, path, idx)
	return err
}

// syncPkg installs one entry of `Packages` and returns the SHA-256 digest
// of its archive, "" for a directory.
func syncPkg(pkg, path string, idx int) (string, error) {
	pkg, want, err := splitDigest(pkg)
	if err != nil {
		return "", err
	}
	if core.IsPhrase(pkg) && !system.IsDir(pkg) {
		entry, lerr := inLibrary(pkg, path)
		if entry != "" {
			return download(pkg, entry, path, idx, want)
		}
		// Without the library a name can't be told from a typo, so the
		// failure to read it is the error, unless the name is a local file.
		if lerr != nil && !system.FileExists(pkg) {
			return "", core.NewE100("sync", fmt.Errorf(
				"could not look up '%s' in the package library: %w", pkg, lerr))
		}
	}
	return loadPkg(system.FileNameWithoutExt(pkg), pkg, path, idx, want)
}

func loadPkg(name, urlOrPath, styles string, index int, want string) (string, error) {
	if fileInfo, err := os.Stat(urlOrPath); err == nil {
		if fileInfo.IsDir() {
			if want != "" {
				return "", core.NewE100("sync", fmt.Errorf(
					"'%s' is a directory: only an archive can be pinned to a digest", urlOrPath))
			}
			return "", loadLocalPkg(name, urlOrPath, styles, index)
		}
		got, derr := fileDigest(urlOrPath)
		if derr != nil {
			return "", derr
		}
		if derr = checkDigest(urlOrPath, want, got); derr != nil {
			return got, derr
		}
		return got, loadLocalZipPkg(name, urlOrPath, styles, index)
	}
	return download(name, urlOrPath, styles, index, want)
}

func loadLocalPkg(name, pkgPath, styles string, index int) error {
	return installPkg(filepath.Dir(pkgPath), name, styles, index)
}

func loadLocalZipPkg(name, pkgPath, styles string, index int) error {
	dir, err := os.MkdirTemp("", name)
	if err != nil {
		return err
	}

	if err = system.Unarchive(pkgPath, dir); err != nil {
		return err
	}

	return installPkg(dir, name, styles, index)
}

func download(name, url, styles string, index int, want string) (string, error) {
	dir, err := os.MkdirTemp("", name)
	if err != nil {
		return "", err
	}

	got, err := fetchPinned(url, dir, want)
	if err != nil {
		if strings.Contains(err.Error(), "unsupported protocol scheme") {
			err = fmt.Errorf("'%s' is not a valid URL or the directory doesn't exist", url)
		}
		// A digest mismatch is already Vale's own error, and passes through.
		return got, core.NewE100("download", err)
	}

	if want != "" {
		// A pinned archive is the one installed: another release would be
		// other bytes than the ones pinned.
		if meta := pkgMeta(dir, name); !supportsVale(meta.Vale) {
			return got, core.NewE100("sync", fmt.Errorf(
				"'%s' needs Vale %s; this is %s, and it's pinned to a digest, so no other release is installed",
				name, meta.Vale, version))
		}
		return got, installPkg(dir, name, styles, index)
	}

	// The release that landed may need a newer Vale than this one; see
	// pkgver.go for the one that gets installed instead.
	if dir, err = supportedRelease(name, url, dir); err != nil {
		return got, err
	}

	return got, installPkg(dir, name, styles, index)
}

// pkgRoot returns the entry in dir that holds the package named name: an
// exact match, one that differs only in case, or the archive's only
// top-level directory.
//
// A package is named after its URL, file, or library entry, none of which
// has to match the directory inside the archive. See #1181.
func pkgRoot(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return name
	}

	for _, entry := range entries {
		if entry.Name() == name {
			return name
		}
	}

	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), name) {
			return entry.Name()
		}
	}

	if len(entries) == 1 && entries[0].IsDir() {
		return entries[0].Name()
	}

	return name
}

func installPkg(dir, name, styles string, index int) error {
	name = pkgRoot(dir, name)

	root := filepath.Join(dir, name)
	path := filepath.Join(root, "styles")
	pipe := filepath.Join(styles, core.PipeDir)
	cfg := filepath.Join(root, ".vale.ini")

	if !system.IsDir(path) && !system.FileExists(cfg) {
		installedStyles[name] = true
		return moveAsset(name, dir, styles) // style-only
	}

	// StylesPath
	if system.IsDir(path) {
		if entries, err := os.ReadDir(path); err == nil {
			for _, e := range entries {
				if e.IsDir() && e.Name() != "config" {
					installedStyles[e.Name()] = true
				}
			}
		}
		if err := moveDir(path, styles); err != nil {
			return err
		}
		// $StylesPath/config
		//
		// NOTE: We treat this directory differently than the rest of the
		// entries on the path: we merge its contents with the existing
		// $StylesPath/config directory.
		for _, dir := range core.ConfigDirs {
			loc1 := filepath.Join(path, dir)
			if system.IsDir(loc1) {
				loc2 := filepath.Join(styles, dir)
				if err := moveDir(loc1, loc2); err != nil {
					return err
				}
			}
		}
	}

	// .vale.ini
	if system.FileExists(cfg) {
		pkgs, err := core.GetPackages(cfg)
		if err != nil {
			return err
		}

		for idx, pkg := range pkgs {
			if err = readPkg(pkg, styles, idx); err != nil {
				return err
			}
		}
		// Copy the package's .vale.ini into the pipeline directory under its
		// indexed name. We must not rename it in place: for a local directory
		// package, `root` is the user's actual source directory, so renaming
		// would clobber their original .vale.ini. See #991 (and #583).
		dst := filepath.Join(pipe, fmt.Sprintf("%d-%s.ini", index, name))
		if system.FileExists(dst) {
			if err = os.RemoveAll(dst); err != nil {
				return err
			}
		}
		if err = os.MkdirAll(pipe, os.ModePerm); err != nil {
			return err
		}
		if err = cp.Copy(cfg, dst); err != nil {
			return err
		}
	}

	return nil
}

func moveDir(oldPath, newPath string) error {
	files, err := os.ReadDir(oldPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !file.IsDir() || file.Name() != "config" {
			if err = moveAsset(file.Name(), oldPath, newPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func moveAsset(name, oldPath, newPath string) error {
	src := filepath.Join(oldPath, name)
	dst := filepath.Join(newPath, name)

	if system.FileExists(dst) || system.IsDir(dst) {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}

	err := os.MkdirAll(newPath, os.ModePerm)
	if err != nil {
		return err
	}

	return cp.Copy(src, dst)
}
