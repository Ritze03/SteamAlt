// Package transfer copies Steam game folders between userdata accounts and
// controls the Steam client (detect / ask it to quit) behind a seam that tests
// replace. It is pure stdlib and has no goroutines of its own except a reaper
// for the spawned `steam -shutdown` process.
package transfer

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	tmpPrefix = ".steamalt-tmp-"
	oldPrefix = ".steamalt-old-"
)

// rename is os.Rename; tests replace it to inject swap failures.
var rename = os.Rename

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Copy copies each id from srcDir/<id> to dstDir/<id>, replacing whatever is
// at dstDir/<id> (rmtree+copytree semantics). The source is never modified.
//
// Each id is first copied into the sibling dstDir/.steamalt-tmp-<id>; only
// after that fully succeeds is dstDir/<id> renamed to .steamalt-old-<id>, the
// copy renamed into place and the old folder removed. On error or cancel the
// existing dstDir/<id> is left untouched (restored if the swap fails midway).
// A symlinked srcDir/<id> is followed and copied as a real folder. At the
// start, leftovers of an interrupted run in dstDir are swept (see sweep).
//
// ids must be purely numeric (ASCII digits); anything else gets an error and
// nothing is written. progress (may be nil) is called after every id that was
// attempted, successful or failed: done is the number of ids processed so far,
// total is len(ids), id is the one just finished. It is not called for ids
// skipped because ctx was cancelled.
//
// The returned map holds one error per failed id and is nil when everything
// succeeded. When ctx is cancelled, the id being copied and every remaining id
// get ctx.Err(). Copy is synchronous; call it from your own goroutine.
func Copy(ctx context.Context, srcDir, dstDir string, ids []string, progress func(done, total int, id string)) map[string]error {
	var errs map[string]error
	fail := func(id string, err error) {
		if errs == nil {
			errs = map[string]error{}
		}
		errs[id] = err
	}
	total := len(ids)
	if total > 0 && ctx.Err() == nil && os.MkdirAll(dstDir, 0o755) == nil {
		sweep(dstDir)
	}
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			for _, rest := range ids[i:] {
				fail(rest, err)
			}
			return errs
		}
		if err := copyOne(ctx, srcDir, dstDir, id); err != nil {
			fail(id, err)
			if ctx.Err() != nil {
				// cancelled mid-copy: the rest are reported at the top of the next iteration
				continue
			}
		}
		if progress != nil {
			progress(i+1, total, id)
		}
	}
	return errs
}

func isNumeric(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func copyOne(ctx context.Context, srcDir, dstDir, id string) (err error) {
	if !isNumeric(id) {
		return fmt.Errorf("invalid app id %q: must be numeric", id)
	}
	src := filepath.Join(srcDir, id)
	fi, err := os.Stat(src) // follows a symlinked app folder
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if src, err = filepath.EvalSymlinks(src); err != nil { // WalkDir does not follow a root symlink
		return err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dstDir, tmpPrefix+id)
	if err := os.RemoveAll(tmp); err != nil { // stale leftover from a crashed run
		return err
	}
	// The tmp copy is only discarded while the old data is still untouched;
	// once the swap has begun it may be the only complete copy left.
	swapping := false
	defer func() {
		if err != nil && !swapping {
			os.RemoveAll(tmp)
		}
	}()
	if err := copyTree(ctx, src, tmp); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	final := filepath.Join(dstDir, id)
	old := filepath.Join(dstDir, oldPrefix+id)
	if !pathExists(final) {
		return rename(tmp, final)
	}
	if err := os.RemoveAll(old); err != nil { // stale from an earlier failed cleanup
		return err
	}
	if err := rename(final, old); err != nil { // atomic: same directory
		return err
	}
	swapping = true
	if err := rename(tmp, final); err != nil {
		if rerr := rename(old, final); rerr != nil {
			return fmt.Errorf("replacing %s failed (%v) and restoring failed (%v): your previous data is in %s and the new copy in %s",
				final, err, rerr, old, tmp)
		}
		os.RemoveAll(tmp) // old data is back in place; tmp is just a duplicate
		return err
	}
	os.RemoveAll(old) // best effort; a stale one is swept on the next Copy
	return nil
}

// sweep cleans up after an interrupted Copy: every .steamalt-tmp-* is
// removed; a .steamalt-old-<id> is removed when <id> exists, otherwise (crash
// between the two swap renames) it is restored as <id>.
func sweep(dstDir string) {
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, tmpPrefix):
			os.RemoveAll(filepath.Join(dstDir, name))
		case strings.HasPrefix(name, oldPrefix):
			id := strings.TrimPrefix(name, oldPrefix)
			if !isNumeric(id) {
				continue
			}
			oldPath, final := filepath.Join(dstDir, name), filepath.Join(dstDir, id)
			if pathExists(final) {
				os.RemoveAll(oldPath)
			} else {
				rename(oldPath, final)
			}
		}
	}
}

// copyTree copies src to dst like Python's copytree+copy2: modes and mtimes
// preserved, symlinks recreated as symlinks, other special files skipped.
func copyTree(ctx context.Context, src, dst string) error {
	type dirMeta struct {
		path string
		info fs.FileInfo
	}
	var dirs []dirMeta
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			// owner rwx while filling; real mode is applied after the walk
			if err := os.Mkdir(target, info.Mode().Perm()|0o700); err != nil {
				return err
			}
			dirs = append(dirs, dirMeta{target, info})
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(path, target, info)
		}
		return nil // sockets, devices, pipes: skipped
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- { // children before parents
		m := dirs[i]
		if err := os.Chmod(m.path, m.info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Chtimes(m.path, m.info.ModTime(), m.info.ModTime()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
