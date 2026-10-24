package transfer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// hashTree fingerprints names, modes, symlink targets and contents.
func hashTree(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s|%v|", p, info.Mode())
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			fmt.Fprint(h, l)
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func noTmpLeft(t *testing.T, dst string) {
	t.Helper()
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			t.Errorf("leftover temp dir %s", e.Name())
		}
	}
}

func TestCopySemantics(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write(t, filepath.Join(src, "100", "a.txt"), "A", 0o644)
	write(t, filepath.Join(src, "100", "sub", "b.txt"), "B", 0o600)
	write(t, filepath.Join(src, "200", "c.txt"), "C", 0o644)
	write(t, filepath.Join(src, "300", "d.txt"), "D", 0o644) // not selected
	write(t, filepath.Join(src, "remote", "e.txt"), "E", 0o644)
	// stale dst content for a selected id, and an unselected dst folder
	write(t, filepath.Join(dst, "100", "stale.txt"), "old", 0o644)
	write(t, filepath.Join(dst, "300", "keep.txt"), "keep", 0o644)

	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(src, "100", "a.txt"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(src, "100", "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	before := hashTree(t, src)

	type call struct {
		done, total int
		id          string
	}
	var calls []call
	errs := Copy(context.Background(), src, dst, []string{"100", "200"}, func(done, total int, id string) {
		calls = append(calls, call{done, total, id})
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}

	if got := read(t, filepath.Join(dst, "100", "a.txt")); got != "A" {
		t.Errorf("a.txt = %q", got)
	}
	if got := read(t, filepath.Join(dst, "100", "sub", "b.txt")); got != "B" {
		t.Errorf("b.txt = %q", got)
	}
	if got := read(t, filepath.Join(dst, "200", "c.txt")); got != "C" {
		t.Errorf("c.txt = %q", got)
	}
	if exists(filepath.Join(dst, "100", "stale.txt")) {
		t.Error("stale file survived the replace")
	}
	if exists(filepath.Join(dst, "remote")) {
		t.Error("non-numeric folder copied")
	}
	if got := read(t, filepath.Join(dst, "300", "keep.txt")); got != "keep" {
		t.Errorf("unselected dst folder touched: %q", got)
	}
	if exists(filepath.Join(dst, "300", "d.txt")) {
		t.Error("unselected src folder copied")
	}
	noTmpLeft(t, dst)

	// mode, mtime, symlink
	fi, err := os.Stat(filepath.Join(dst, "100", "sub", "b.txt"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode not preserved: %v %v", fi, err)
	}
	fi, err = os.Stat(filepath.Join(dst, "100", "a.txt"))
	if err != nil || !fi.ModTime().Equal(mtime) {
		t.Errorf("mtime not preserved: %v %v", fi.ModTime(), err)
	}
	if l, err := os.Readlink(filepath.Join(dst, "100", "link")); err != nil || l != "a.txt" {
		t.Errorf("symlink not recreated: %q %v", l, err)
	}

	if after := hashTree(t, src); after != before {
		t.Error("source tree changed")
	}

	want := []call{{1, 2, "100"}, {2, 2, "200"}}
	if len(calls) != len(want) {
		t.Fatalf("progress calls = %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("progress[%d] = %v, want %v", i, calls[i], want[i])
		}
	}
}

func TestCopyRejectsNonNumeric(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst", "acct")
	write(t, filepath.Join(src, "100", "a.txt"), "A", 0o644)
	write(t, filepath.Join(root, "x", "f.txt"), "X", 0o644)

	errs := Copy(context.Background(), src, dst, []string{"../x", "abc", "", "1/2", "100"}, nil)
	for _, id := range []string{"../x", "abc", "", "1/2"} {
		if errs[id] == nil {
			t.Errorf("id %q not rejected", id)
		}
	}
	if errs["100"] != nil {
		t.Errorf("valid id failed: %v", errs["100"])
	}
	if exists(filepath.Join(root, "dst", "x")) || exists(filepath.Join(dst, "x")) || exists(filepath.Join(dst, "abc")) {
		t.Error("something written for a rejected id")
	}
	if got := read(t, filepath.Join(dst, "100", "a.txt")); got != "A" {
		t.Errorf("valid id not copied: %q", got)
	}
}

func TestCopyMissingSource(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write(t, filepath.Join(src, "1", "a"), "a", 0o644)
	write(t, filepath.Join(dst, "5", "old"), "old", 0o644)
	errs := Copy(context.Background(), src, dst, []string{"5"}, nil)
	if errs["5"] == nil {
		t.Fatal("missing source id should error")
	}
	if got := read(t, filepath.Join(dst, "5", "old")); got != "old" {
		t.Error("existing dst data destroyed by missing source")
	}
}

func TestCopyCancelled(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write(t, filepath.Join(src, "1", "new"), "new", 0o644)
	write(t, filepath.Join(src, "2", "new"), "new", 0o644)
	write(t, filepath.Join(dst, "1", "old"), "old", 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	errs := Copy(ctx, src, dst, []string{"1", "2"}, func(int, int, string) { called = true })
	for _, id := range []string{"1", "2"} {
		if !errors.Is(errs[id], context.Canceled) {
			t.Errorf("id %s err = %v, want context.Canceled", id, errs[id])
		}
	}
	if called {
		t.Error("progress called for cancelled ids")
	}
	if got := read(t, filepath.Join(dst, "1", "old")); got != "old" {
		t.Error("existing dst data changed on cancel")
	}
	if exists(filepath.Join(dst, "1", "new")) || exists(filepath.Join(dst, "2")) {
		t.Error("cancelled copy wrote data")
	}
	noTmpLeft(t, dst)
}

func TestCopyCancelMidway(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	for _, id := range []string{"1", "2", "3"} {
		write(t, filepath.Join(src, id, "f"), id, 0o644)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errs := Copy(ctx, src, dst, []string{"1", "2", "3"}, func(done, _ int, _ string) {
		if done == 1 {
			cancel()
		}
	})
	if errs["1"] != nil {
		t.Errorf("first id failed: %v", errs["1"])
	}
	for _, id := range []string{"2", "3"} {
		if !errors.Is(errs[id], context.Canceled) {
			t.Errorf("id %s err = %v", id, errs[id])
		}
	}
	if read(t, filepath.Join(dst, "1", "f")) != "1" || exists(filepath.Join(dst, "2")) {
		t.Error("unexpected dst state after mid-way cancel")
	}
	noTmpLeft(t, dst)
}

func TestCopyFailureKeepsDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write(t, filepath.Join(src, "7", "a.txt"), "a", 0o644)
	secret := filepath.Join(src, "7", "z", "secret.bin")
	write(t, secret, "x", 0o644)
	if err := os.Chmod(secret, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(secret, 0o644) })
	write(t, filepath.Join(dst, "7", "old.txt"), "old", 0o644)
	// a stale temp from a "crashed" earlier run must be cleaned too
	write(t, filepath.Join(dst, tmpPrefix+"7", "junk"), "junk", 0o644)

	var progressed []string
	errs := Copy(context.Background(), src, dst, []string{"7"}, func(_, _ int, id string) { progressed = append(progressed, id) })
	if errs["7"] == nil {
		t.Fatal("expected an error for unreadable source file")
	}
	if got := read(t, filepath.Join(dst, "7", "old.txt")); got != "old" {
		t.Error("existing dst data destroyed on failure")
	}
	if exists(filepath.Join(dst, "7", "a.txt")) {
		t.Error("partial copy leaked into dst")
	}
	noTmpLeft(t, dst)
	if len(progressed) != 1 {
		t.Errorf("failed id should still report progress, got %v", progressed)
	}
}

func TestRunningIn(t *testing.T) {
	mk := func(comms ...string) string {
		proc := t.TempDir()
		for i, c := range comms {
			write(t, filepath.Join(proc, fmt.Sprint(100+i), "comm"), c+"\n", 0o644)
		}
		write(t, filepath.Join(proc, "self-not-a-pid", "comm"), "steam\n", 0o644) // non-numeric dir ignored
		return proc
	}
	cases := []struct {
		name  string
		comms []string
		want  bool
	}{
		{"steam", []string{"bash", "steam"}, true},
		{"steam.exe", []string{"steam.exe"}, true},
		{"webhelper only", []string{"steamwebhelper", "bash"}, false},
		{"none", nil, false},
	}
	for _, c := range cases {
		if got := runningIn(mk(c.comms...)); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if runningIn(filepath.Join(t.TempDir(), "missing")) {
		t.Error("missing proc dir should be false")
	}
}

func TestFakeAndWaitClosed(t *testing.T) {
	f := &Fake{IsRunning: true}
	var s Steam = f
	if r, _ := s.Running(); !r {
		t.Fatal("fake should be running")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if f.IsRunning || f.Closes != 1 {
		t.Errorf("after Close: %+v", f)
	}
	if err := WaitClosed(context.Background(), f, time.Millisecond); err != nil {
		t.Errorf("WaitClosed on closed steam: %v", err)
	}

	stuck := &Fake{IsRunning: true, CloseErr: errors.New("boom")}
	if err := stuck.Close(); err == nil || !stuck.IsRunning || stuck.Closes != 1 {
		t.Errorf("CloseErr handling wrong: %+v", stuck)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := WaitClosed(ctx, stuck, time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("WaitClosed timeout err = %v", err)
	}
}

// failRenames replaces rename so that the calls numbered in failOn (1-based)
// fail; the others go through to os.Rename.
func failRenames(t *testing.T, failOn ...int) {
	t.Helper()
	orig := rename
	n := 0
	rename = func(a, b string) error {
		n++
		for _, f := range failOn {
			if n == f {
				return errors.New("injected rename failure")
			}
		}
		return os.Rename(a, b)
	}
	t.Cleanup(func() { rename = orig })
}

func TestSwapRollsBackWhenFinalRenameFails(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write(t, filepath.Join(src, "5", "new.txt"), "new", 0o644)
	write(t, filepath.Join(dst, "5", "old.txt"), "old", 0o644)
	failRenames(t, 2) // 1: final->old, 2: tmp->final (fails), 3: rollback old->final

	errs := Copy(context.Background(), src, dst, []string{"5"}, nil)
	if errs["5"] == nil {
		t.Fatal("expected the injected failure to be reported")
	}
	if got := read(t, filepath.Join(dst, "5", "old.txt")); got != "old" {
		t.Error("old data not restored after failed swap")
	}
	if exists(filepath.Join(dst, "5", "new.txt")) {
		t.Error("new data leaked into restored folder")
	}
	noTmpLeft(t, dst)
	if exists(filepath.Join(dst, oldPrefix+"5")) {
		t.Error("old folder left behind after rollback")
	}
}

func TestSwapKeepsBothWhenRollbackFails(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write(t, filepath.Join(src, "5", "new.txt"), "new", 0o644)
	write(t, filepath.Join(dst, "5", "old.txt"), "old", 0o644)
	failRenames(t, 2, 3)

	errs := Copy(context.Background(), src, dst, []string{"5"}, nil)
	err := errs["5"]
	if err == nil {
		t.Fatal("expected an error")
	}
	oldDir, tmpDir := filepath.Join(dst, oldPrefix+"5"), filepath.Join(dst, tmpPrefix+"5")
	if !strings.Contains(err.Error(), oldDir) || !strings.Contains(err.Error(), tmpDir) {
		t.Errorf("error does not name where the data is: %v", err)
	}
	if read(t, filepath.Join(oldDir, "old.txt")) != "old" || read(t, filepath.Join(tmpDir, "new.txt")) != "new" {
		t.Error("old or new data lost")
	}
}

func TestCopySweepsLeftovers(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	write(t, filepath.Join(src, "1", "f"), "f", 0o644)
	write(t, filepath.Join(dst, tmpPrefix+"5", "junk"), "junk", 0o644)
	write(t, filepath.Join(dst, oldPrefix+"6", "junk"), "junk", 0o644)
	write(t, filepath.Join(dst, "6", "cur"), "cur", 0o644)
	write(t, filepath.Join(dst, oldPrefix+"7", "precious"), "precious", 0o644)
	write(t, filepath.Join(dst, "8", "other"), "other", 0o644)
	write(t, filepath.Join(dst, ".steamalt-other", "x"), "x", 0o644)

	if errs := Copy(context.Background(), src, dst, []string{"1"}, nil); errs != nil {
		t.Fatal(errs)
	}
	if exists(filepath.Join(dst, tmpPrefix+"5")) {
		t.Error("stale tmp not removed")
	}
	if exists(filepath.Join(dst, oldPrefix+"6")) || read(t, filepath.Join(dst, "6", "cur")) != "cur" {
		t.Error("stale old next to existing id not removed, or id damaged")
	}
	if exists(filepath.Join(dst, oldPrefix+"7")) || read(t, filepath.Join(dst, "7", "precious")) != "precious" {
		t.Error("old without id not restored")
	}
	if read(t, filepath.Join(dst, "8", "other")) != "other" || read(t, filepath.Join(dst, ".steamalt-other", "x")) != "x" {
		t.Error("unrelated entries touched")
	}
}

func TestShutdownCmd(t *testing.T) {
	look := func(have ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, h := range have {
				if h == name {
					return "/fake/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	cmd, err := shutdownCmd(look("steam", "flatpak"))
	if err != nil || strings.Join(cmd.Args, " ") != "steam -shutdown" {
		t.Errorf("steam on PATH: %v %v", cmd, err)
	}
	cmd, err = shutdownCmd(look("flatpak"))
	if err != nil || strings.Join(cmd.Args, " ") != "flatpak run com.valvesoftware.Steam -shutdown" {
		t.Errorf("flatpak only: %v %v", cmd, err)
	}
	if cmd, err = shutdownCmd(look()); err == nil || cmd != nil || !strings.Contains(err.Error(), "close Steam manually") {
		t.Errorf("neither: %v %v", cmd, err)
	}
}

func TestCopyFollowsSymlinkedAppFolder(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	real := filepath.Join(root, "elsewhere", "game")
	write(t, filepath.Join(real, "a.txt"), "A", 0o644)
	write(t, filepath.Join(real, "sub", "b.txt"), "B", 0o644)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(src, "9")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if errs := Copy(context.Background(), src, dst, []string{"9"}, nil); errs != nil {
		t.Fatal(errs)
	}
	fi, err := os.Lstat(filepath.Join(dst, "9"))
	if err != nil || !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		t.Fatalf("dst/9 is not a real dir: %v %v", fi, err)
	}
	if read(t, filepath.Join(dst, "9", "a.txt")) != "A" || read(t, filepath.Join(dst, "9", "sub", "b.txt")) != "B" {
		t.Error("contents not copied")
	}
}
