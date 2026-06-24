package system

import (
	"fmt"
	"os"
	"path/filepath"
)

func walk(filename string, linkDirname string, walkFn filepath.WalkFunc) error {
	symWalkFunc := func(path string, info os.FileInfo, err error) error {
		if fname, ferr := filepath.Rel(filename, path); ferr == nil {
			path = filepath.Join(linkDirname, fname)
		} else {
			return ferr
		}

		if err == nil && info.Mode()&os.ModeSymlink == os.ModeSymlink {
			finalPath, ferr := filepath.EvalSymlinks(path)
			if ferr != nil {
				return fmt.Errorf("unable to resolve symlink '%s': %w", path, ferr)
			}
			linfo, ierr := os.Lstat(finalPath)
			if ierr != nil {
				return walkFn(path, info, err)
			}
			if linfo.IsDir() {
				return walk(finalPath, path, walkFn)
			}
		}

		return walkFn(path, info, err)
	}
	return filepath.Walk(filename, symWalkFunc)
}

// Walk extends filepath.Walk to also follow symlinks
func Walk(path string, walkFn filepath.WalkFunc) error {
	return walk(path, path, walkFn)
}
