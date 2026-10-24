//go:build sandbox

package main

// Integration test against a COPY of real Steam data (D16: never test on live Steam).
//
//	go test -tags sandbox -run Sandbox -v -count=1 .
//
// The sandbox root is /tmp/claude-1000/steamalt-sandbox/root; the test skips when it
// is missing. HOME and the XDG dirs are redirected into temp dirs, so no real config
// or cache is touched and auto-detection can never reach the live Steam folder. The
// Steam seam (transfer.System / Close) is not used at all.
//
// The sandbox holds real account and game names: this test logs counts only and
// never writes names anywhere. The copy test works on a fresh copy of two accounts'
// userdata folders in a temp dir, so the shared sandbox is never modified.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ritze03/SteamAlt/lookup"
	"github.com/Ritze03/SteamAlt/steam"
	"github.com/Ritze03/SteamAlt/transfer"
)

const sandboxRoot = "/tmp/claude-1000/steamalt-sandbox/root"

// sandboxSetup skips without a sandbox, isolates HOME/XDG, writes the D10 config
// pointing at the sandbox and returns the root as steam.ResolveRoot reports it.
func sandboxSetup(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(sandboxRoot, "userdata")); err != nil {
		t.Skipf("no sandbox at %s", sandboxRoot)
	}
	home := t.TempDir()
	cfgHome := filepath.Join(home, "config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	cfg, _ := json.Marshal(map[string]string{"steam_path": sandboxRoot})
	if err := os.MkdirAll(filepath.Join(cfgHome, "steamalt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgHome, "steamalt", "config"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	root, warn, err := steam.ResolveRoot()
	if err != nil || warn != "" {
		t.Fatalf("ResolveRoot: err %v, warning %q", err, warn)
	}
	if root != sandboxRoot {
		t.Fatalf("ResolveRoot = %q, want the sandbox root", root)
	}
	return root
}

func TestSandboxListing(t *testing.T) {
	root := sandboxSetup(t)

	accs, err := steam.Accounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) < 2 {
		t.Fatalf("%d accounts, want at least 2", len(accs))
	}
	for _, a := range accs {
		if a.ID == "0" {
			t.Error("the dummy 0 folder is listed as an account")
		}
		if a.Name == steam.UnknownName || a.Name == "" {
			t.Errorf("account %s has no real name", a.ID)
		}
	}
	t.Logf("%d accounts, all named", len(accs))

	games, err := steam.Games(root, accs[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) == 0 {
		t.Fatalf("first account has no games")
	}
	ids := make([]string, len(games))
	for i, g := range games {
		ids[i] = g.AppID
	}
	local := steam.InstalledNames(root)

	var mu sync.Mutex
	names := map[string]string{}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	lookup.New(t.TempDir()).Names(ctx, ids, local, func(id, name string) {
		mu.Lock()
		names[id] = name
		mu.Unlock()
	})

	resolved := 0
	for _, id := range ids {
		n, ok := names[id]
		switch {
		case !ok || n == "":
			t.Errorf("app %s got no name", id)
		case n != lookup.FallbackName(id):
			resolved++
		}
	}
	if resolved*2 < len(ids) {
		t.Errorf("only %d of %d games resolved to a real name", resolved, len(ids))
	}
	t.Logf("%d games, %d with a resolved name (%d appmanifest names known)", len(ids), resolved, len(local))
}

// snapshot maps every path under dir (relative, slash-separated, "." excluded) to a
// fingerprint of its type, mode and content.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			out[rel] = "l:" + l
		case d.IsDir():
			out[rel] = "d:" + info.Mode().Perm().String()
		case info.Mode().IsRegular():
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				return err
			}
			out[rel] = "f:" + info.Mode().Perm().String() + ":" + hex.EncodeToString(h.Sum(nil))
		default:
			out[rel] = "other:" + info.Mode().String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// copyAll copies the tree src to dst (files, dirs, symlinks, modes).
func copyAll(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(l, target)
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// under reports whether the snapshot key rel is id or inside it.
func under(rel, id string) bool { return rel == id || strings.HasPrefix(rel, id+"/") }

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// pickPair finds a source and a destination account and two source app ids: one the
// destination already has (an overwrite) and, if possible, one it lacks (a new
// folder). It prefers the pair with the smallest number of app folders.
func pickPair(t *testing.T, root string, accs []steam.Account) (src, dst string, ids [2]string) {
	t.Helper()
	best := -1
	for _, s := range accs {
		for _, d := range accs {
			if s.ID == d.ID {
				continue
			}
			gs, err := steam.Games(root, s.ID, d.ID)
			if err != nil || len(gs) < 2 {
				continue
			}
			dgs, err := steam.Games(root, d.ID, "")
			if err != nil || len(dgs) < 2 {
				continue
			}
			var over, fresh []string
			for _, g := range gs {
				if g.Overwrites {
					over = append(over, g.AppID)
				} else {
					fresh = append(fresh, g.AppID)
				}
			}
			if len(over) == 0 {
				continue
			}
			cost := len(gs) + len(dgs)
			if best >= 0 && cost >= best {
				continue
			}
			best, src, dst = cost, s.ID, d.ID
			ids[0] = over[0]
			switch {
			case len(fresh) > 0:
				ids[1] = fresh[0]
			case len(over) > 1:
				ids[1] = over[1]
			default:
				best = -1
			}
		}
	}
	if best < 0 {
		t.Skip("no two sandbox accounts share an app folder and have two to copy")
	}
	return src, dst, ids
}

func TestSandboxCopy(t *testing.T) {
	root := sandboxSetup(t)
	accs, err := steam.Accounts(root)
	if err != nil {
		t.Fatal(err)
	}
	srcID, dstID, ids := pickPair(t, root, accs)

	// A fresh root with just the two accounts; the shared sandbox stays untouched.
	work := filepath.Join(t.TempDir(), "root")
	srcDir, dstDir := steam.UserdataDir(work, srcID), steam.UserdataDir(work, dstID)
	copyAll(t, steam.UserdataDir(root, srcID), srcDir)
	copyAll(t, steam.UserdataDir(root, dstID), dstDir)

	srcBefore, dstBefore := snapshot(t, srcDir), snapshot(t, dstDir)

	var lastDone, lastTotal int
	errs := transfer.Copy(context.Background(), srcDir, dstDir, ids[:], func(done, total int, id string) {
		lastDone, lastTotal = done, total
	})
	if len(errs) != 0 {
		t.Fatalf("Copy returned %d errors: %v", len(errs), errs)
	}
	if lastDone != 2 || lastTotal != 2 {
		t.Errorf("progress ended at %d/%d, want 2/2", lastDone, lastTotal)
	}

	srcAfter, dstAfter := snapshot(t, srcDir), snapshot(t, dstDir)
	if !equalMaps(srcBefore, srcAfter) {
		t.Error("the source tree changed")
	}

	copied := func(rel string) bool { return under(rel, ids[0]) || under(rel, ids[1]) }

	// Outside the two folders nothing may differ: no changed, missing or extra path
	// (this also catches a leftover temp folder).
	outsideBefore, outsideAfter := map[string]string{}, map[string]string{}
	for k, v := range dstBefore {
		if !copied(k) {
			outsideBefore[k] = v
		}
	}
	for k, v := range dstAfter {
		if !copied(k) {
			outsideAfter[k] = v
		}
	}
	if !equalMaps(outsideBefore, outsideAfter) {
		var diff []string
		for k, v := range outsideAfter {
			if outsideBefore[k] != v {
				diff = append(diff, k)
			}
		}
		for k := range outsideBefore {
			if _, ok := outsideAfter[k]; !ok {
				diff = append(diff, k)
			}
		}
		sort.Strings(diff)
		if len(diff) > 5 {
			diff = diff[:5]
		}
		t.Errorf("paths outside the two copied folders changed: %d before, %d after, e.g. %v", len(outsideBefore), len(outsideAfter), diff)
	}

	// Inside them the destination now equals the source byte for byte.
	srcPart, dstPart := map[string]string{}, map[string]string{}
	for k, v := range srcAfter {
		if copied(k) {
			srcPart[k] = v
		}
	}
	for k, v := range dstAfter {
		if copied(k) {
			dstPart[k] = v
		}
	}
	if !equalMaps(srcPart, dstPart) {
		t.Errorf("destination's copied folders differ from the source's: %d vs %d paths", len(dstPart), len(srcPart))
	}

	// The test is only meaningful if the copy changed something.
	changed := 0
	for k, v := range dstAfter {
		if copied(k) && dstBefore[k] != v {
			changed++
		}
	}
	if changed == 0 {
		t.Error("the copy changed nothing in the destination")
	}
	t.Logf("source %d paths, destination %d -> %d paths; %d paths changed, %d outside the two folders identical",
		len(srcBefore), len(dstBefore), len(dstAfter), changed, len(outsideAfter))
}
