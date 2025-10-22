package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cp "github.com/otiai10/copy"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/system"
)

func initPath(cfg *core.Config) error {
	// The first entry is always the default `StylesPath`.
	stylesPath := cfg.StylesPath()

	if !system.IsDir(stylesPath) {
		if err := os.MkdirAll(cfg.StylesPath(), os.ModePerm); err != nil {
			e := fmt.Errorf("unable to initialize StylesPath (value = '%s')", cfg.StylesPath())
			return core.NewE100("initPath", e)
		}
	}

	// Remove any existing .vale-config directory.
	err := os.RemoveAll(filepath.Join(stylesPath, core.PipeDir))
	if err != nil {
		return core.NewE100("initPath", err)
	}

	return nil
}

func readPkg(pkg, path string, idx int) error {
	if core.IsPhrase(pkg) && !system.IsDir(pkg) {
		entry := inLibrary(pkg, path)
		if entry != "" {
			return download(pkg, entry, path, idx)
		}
	}
	return loadPkg(system.FileNameWithoutExt(pkg), pkg, path, idx)
}

func loadPkg(name, urlOrPath, styles string, index int) error {
	if fileInfo, err := os.Stat(urlOrPath); err == nil {
		if fileInfo.IsDir() {
			return loadLocalPkg(name, urlOrPath, styles, index)
		}
		return loadLocalZipPkg(name, urlOrPath, styles, index)
	}
	return download(name, urlOrPath, styles, index)
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

func download(name, url, styles string, index int) error {
	dir, err := os.MkdirTemp("", name)
	if err != nil {
		return err
	}

	if err = fetch(url, dir); err != nil {
		if strings.Contains(err.Error(), "unsupported protocol scheme") {
			err = fmt.Errorf("'%s' is not a valid URL or the directory doesn't exist", url)
		}
		return core.NewE100("download", err)
	}

	return installPkg(dir, name, styles, index)
}

func installPkg(dir, name, styles string, index int) error {
	root := filepath.Join(dir, name)
	path := filepath.Join(root, "styles")
	pipe := filepath.Join(styles, core.PipeDir)
	cfg := filepath.Join(root, ".vale.ini")

	if !system.IsDir(path) && !system.FileExists(cfg) {
		return moveAsset(name, dir, styles) // style-only
	}

	// StylesPath
	if system.IsDir(path) {
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
		entry := fmt.Sprintf("%d-%s.ini", index, name)

		err = os.Rename(cfg, filepath.Join(root, entry))
		if err != nil {
			return err
		} else if err = moveAsset(entry, root, pipe); err != nil {
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
