package ui

import (
	"errors"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/Ritze03/SteamAlt/lookup"
	"github.com/Ritze03/SteamAlt/steam"
	"github.com/Ritze03/SteamAlt/transfer"
)

// Logic tests for the app state machine. No rendering; the Steam seam is a
// transfer.Fake and the name lookups hit an httptest server, never the network
// and never a real Steam folder.

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// steamRoot builds a synthetic Steam root:
//
//	1000 Alice: apps 620, 367520, 413150 (plus a non-numeric "config" folder)
//	2000 Bob:   apps 620 (with an old file), 504230
//	3000 Carol: no apps
//
// and appmanifest_620.acf naming 620 "Portal 2".
func steamRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ud := filepath.Join(root, "userdata")
	writeFile(t, filepath.Join(ud, "0", "config", "x"), "x")
	writeFile(t, filepath.Join(ud, "1000", "config", "localconfig.vdf"), `"UserLocalConfigStore" {}`)
	writeFile(t, filepath.Join(ud, "1000", "620", "remote", "save.dat"), "alice-620")
	writeFile(t, filepath.Join(ud, "1000", "620", "new.cfg"), "new")
	writeFile(t, filepath.Join(ud, "1000", "367520", "remote", "s.dat"), "alice-367520")
	writeFile(t, filepath.Join(ud, "1000", "413150", "s.dat"), "alice-413150")
	writeFile(t, filepath.Join(ud, "2000", "620", "old.cfg"), "old")
	writeFile(t, filepath.Join(ud, "2000", "504230", "s.dat"), "bob-504230")
	if err := os.MkdirAll(filepath.Join(ud, "3000"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "config", "loginusers.vdf"), `"users" {
	"76561197960266728" { "PersonaName" "Alice" }
	"76561197960267728" { "PersonaName" "Bob" }
	"76561197960268728" { "PersonaName" "Carol" }
}`)
	writeFile(t, filepath.Join(root, "steamapps", "appmanifest_620.acf"),
		"\"AppState\"\n{\n\t\"appid\"\t\"620\"\n\t\"name\"\t\"Portal 2\"\n}\n")
	return root
}

// newTestApp returns an App on the synthetic root, loaded and with the names
// settled. The returned Fake is the Steam seam.
func newTestApp(t *testing.T) (*App, string, *transfer.Fake) {
	t.Helper()
	// Anything that touches the user's config or cache must stay in the sandbox.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("appids")
		if id == "367520" {
			fmt.Fprintf(w, `{%q:{"success":true,"data":{"name":"Hollow Knight"}}}`, id)
			return
		}
		fmt.Fprintf(w, `{%q:{"success":false}}`, id)
	}))
	t.Cleanup(srv.Close)
	lk := lookup.New(t.TempDir())
	lk.StoreURL = srv.URL
	lk.AvatarURL = srv.URL + "/"

	fake := &transfer.Fake{}
	a := NewApp(nil, fake, lk)
	root := steamRoot(t)
	a.load(root)
	a.closePoll = 5 * time.Millisecond
	waitApp(t, a, "names", func() bool { return len(a.nameMap) == len(a.rows) })
	return a, root, fake
}

// waitApp acts as the UI goroutine: it drains posted closures until cond holds.
func waitApp(t *testing.T, a *App, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.drain()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func rowIDs(rows []*gameRow) []string {
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	return ids
}

func eq(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func checkedSet(a *App, ids ...string) {
	for _, r := range a.rows {
		r.check.Value = false
		for _, id := range ids {
			if r.id == id {
				r.check.Value = true
			}
		}
	}
}

func TestLoadListsAccounts(t *testing.T) {
	a, _, _ := newTestApp(t)
	var names []string
	for _, acc := range a.accounts {
		names = append(names, acc.Name+":"+acc.ID)
	}
	if got := strings.Join(names, ","); got != "Alice:1000,Bob:2000,Carol:3000" {
		t.Fatalf("accounts = %s", got)
	}
	if len(a.src.Items) != 3 || a.src.Selected != 0 || a.src.Items[0].Name != "Alice" || a.src.Items[0].ID != "1000" {
		t.Errorf("source dropdown = %+v selected %d", a.src.Items, a.src.Selected)
	}
	if len(a.dst.Items) != 4 || a.dst.Selected != 0 || a.dstID() != "" {
		t.Errorf("destination dropdown = %+v selected %d, want placeholder + 3 accounts and no destination", a.dst.Items, a.dst.Selected)
	}
	if a.footerPath() == "" || a.root == "" {
		t.Errorf("footer path %q root %q", a.footerPath(), a.root)
	}
}

func TestSourceGamesUnchecked(t *testing.T) {
	a, _, _ := newTestApp(t)
	if got := rowIDs(a.rows); !eq(got, []string{"620", "367520", "413150"}) {
		t.Fatalf("Alice's games = %v", got)
	}
	if n := len(a.checkedIDs()); n != 0 {
		t.Fatalf("%d games checked by default", n)
	}
	// Names: appmanifest tier, store tier, fallback.
	for id, want := range map[string]string{"620": "Portal 2", "367520": "Hollow Knight", "413150": "App 413150"} {
		if got, ok := a.displayName(id); got != want || !ok {
			t.Errorf("name of %s = %q (resolved %v), want %q", id, got, ok, want)
		}
	}

	// A new source resets the checks and lists the other account's games.
	checkedSet(a, "620")
	a.src.Selected = 1
	a.onSource()
	if got := rowIDs(a.rows); !eq(got, []string{"620", "504230"}) {
		t.Fatalf("Bob's games = %v", got)
	}
	if n := len(a.checkedIDs()); n != 0 {
		t.Errorf("%d games still checked after changing the source", n)
	}
	waitApp(t, a, "names of the new source", func() bool { return len(a.nameMap) >= 4 })

	// An account without games shows the empty list, not an error.
	a.src.Selected = 2
	a.onSource()
	if len(a.rows) != 0 || a.msg != "" {
		t.Errorf("Carol: rows %v msg %q", rowIDs(a.rows), a.msg)
	}
}

func TestPendingUntilNameArrives(t *testing.T) {
	a, _, _ := newTestApp(t)
	delete(a.nameMap, "413150")
	if name, ok := a.displayName("413150"); ok || name != "App 413150" {
		t.Fatalf("unresolved name = %q resolved %v", name, ok)
	}
}

func TestFilterAndAllNoneActOnVisibleRows(t *testing.T) {
	a, _, _ := newTestApp(t)

	a.filter.SetText("62") // id substring
	if got := rowIDs(a.filterRows()); !eq(got, []string{"620"}) {
		t.Fatalf("filter 62 -> %v", got)
	}
	a.filter.SetText("HOLLOW") // name, case-insensitive
	if got := rowIDs(a.filterRows()); !eq(got, []string{"367520"}) {
		t.Fatalf("filter HOLLOW -> %v", got)
	}
	a.filter.SetText("portal")
	a.setVisible(true)
	if got := a.checkedIDs(); !eq(got, []string{"620"}) {
		t.Fatalf("All with filter portal checked %v", got)
	}
	a.filter.SetText("")
	a.setVisible(true)
	if got := a.checkedIDs(); len(got) != 3 {
		t.Fatalf("All checked %v", got)
	}
	a.filter.SetText("hollow")
	a.setVisible(false)
	if got := a.checkedIDs(); !eq(got, []string{"620", "413150"}) {
		t.Fatalf("None with filter hollow left %v", got)
	}
	a.filter.SetText("zzz")
	if len(a.filterRows()) != 0 {
		t.Fatalf("filter zzz matched %v", rowIDs(a.filterRows()))
	}
	// A pending (unresolved) name is not matched by its fallback text.
	a.filter.SetText("app")
	delete(a.nameMap, "620")
	if got := rowIDs(a.filterRows()); !eq(got, []string{"413150"}) {
		t.Fatalf("filter app -> %v (413150 resolved to the fallback, 620 pending)", got)
	}
}

func TestOverwriteMarksFollowDestination(t *testing.T) {
	a, _, _ := newTestApp(t)
	for _, r := range a.rows {
		if r.overwrites {
			t.Fatalf("%s marked with no destination", r.id)
		}
	}
	checkedSet(a, "367520")
	a.dst.Selected = 2 // Bob
	a.onDest()
	marks := map[string]bool{}
	for _, r := range a.rows {
		marks[r.id] = r.overwrites
	}
	if !marks["620"] || marks["367520"] || marks["413150"] {
		t.Errorf("marks for Bob = %v, want only 620", marks)
	}
	if got := a.checkedIDs(); !eq(got, []string{"367520"}) {
		t.Errorf("checks after destination change = %v, want 367520 kept", got)
	}
	a.dst.Selected = 3 // Carol has nothing
	a.onDest()
	for _, r := range a.rows {
		if r.overwrites {
			t.Errorf("%s marked for Carol", r.id)
		}
	}
	a.dst.Selected = 0
	a.onDest()
	if a.dstID() != "" {
		t.Errorf("destination %q after choosing the placeholder", a.dstID())
	}
}

func TestCanCopyRules(t *testing.T) {
	a, _, _ := newTestApp(t)
	if a.canCopy() {
		t.Error("enabled with no destination and no games")
	}
	checkedSet(a, "620")
	if a.canCopy() {
		t.Error("enabled with no destination")
	}
	a.dst.Selected = 1 // Alice = the source
	a.onDest()
	if a.canCopy() || !a.sameAccount() {
		t.Error("enabled although source == destination")
	}
	if txt, _ := a.footerMsg(); !strings.Contains(txt, "same account") {
		t.Errorf("no same-account hint, footer message %q", txt)
	}
	a.dst.Selected = 2
	a.onDest()
	if !a.canCopy() {
		t.Error("disabled with destination and a checked game")
	}
	checkedSet(a)
	if a.canCopy() {
		t.Error("enabled with 0 games")
	}
	checkedSet(a, "620")
	a.copying = true
	if a.canCopy() {
		t.Error("enabled while copying")
	}
	a.copying = false

	for n, want := range map[int]string{0: "0 games", 1: "1 game", 2: "2 games"} {
		if got := gamesLabel(n); got != want {
			t.Errorf("gamesLabel(%d) = %q, want %q", n, got, want)
		}
	}
}

// copyThroughModals drives a copy of the checked games from the confirm step on.
func copyThroughModals(t *testing.T, a *App, fake *transfer.Fake) {
	t.Helper()
	a.requestCopy()
	if a.modal != modalConfirm {
		t.Fatalf("modal = %d after Copy, want the confirm modal", a.modal)
	}
	a.confirmCopy()
	if fake.IsRunning {
		waitApp(t, a, "Steam-running modal", func() bool { return a.modal == modalSteam })
		if a.copying {
			t.Fatal("still busy while the Steam modal waits for an answer")
		}
		a.closeSteam()
	}
	waitApp(t, a, "copy to finish", func() bool { return !a.copying && a.msg != "" })
}

func TestCopyFlowWithSteamRunning(t *testing.T) {
	a, root, fake := newTestApp(t)
	fake.IsRunning = true
	a.dst.Selected = 2 // Bob
	a.onDest()
	checkedSet(a, "620", "367520")
	if a.overwriteCount() != 1 {
		t.Fatalf("overwriteCount = %d, want 1", a.overwriteCount())
	}

	copyThroughModals(t, a, fake)

	if fake.Closes != 1 {
		t.Errorf("Steam closed %d times, want 1", fake.Closes)
	}
	if a.msgBad || a.msg != "Copied 2 games" {
		t.Errorf("msg = %q bad=%v, want a GOOD \"Copied 2 games\"", a.msg, a.msgBad)
	}
	if a.progress != 1 {
		t.Errorf("progress = %v, want 1", a.progress)
	}
	dst := steam.UserdataDir(root, "2000")
	got, err := os.ReadFile(filepath.Join(dst, "620", "remote", "save.dat"))
	if err != nil || string(got) != "alice-620" {
		t.Errorf("copied 620 save: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "620", "old.cfg")); !os.IsNotExist(err) {
		t.Errorf("old destination file survived the overwrite: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "367520", "remote", "s.dat")); err != nil || string(got) != "alice-367520" {
		t.Errorf("copied 367520 save: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "413150")); !os.IsNotExist(err) {
		t.Errorf("unchecked 413150 was copied: %v", err)
	}
	// Source untouched; list refreshed with unchecked rows and new overwrite marks.
	if _, err := os.Stat(filepath.Join(steam.UserdataDir(root, "1000"), "620", "new.cfg")); err != nil {
		t.Errorf("source modified: %v", err)
	}
	if n := len(a.checkedIDs()); n != 0 {
		t.Errorf("%d games still checked after the copy", n)
	}
	marks := map[string]bool{}
	for _, r := range a.rows {
		marks[r.id] = r.overwrites
	}
	if !marks["620"] || !marks["367520"] || marks["413150"] {
		t.Errorf("marks after the copy = %v", marks)
	}
}

func TestCopyFlowSteamNotRunning(t *testing.T) {
	a, root, fake := newTestApp(t)
	a.dst.Selected = 3 // Carol
	a.onDest()
	checkedSet(a, "413150")
	copyThroughModals(t, a, fake)
	if fake.Closes != 0 {
		t.Errorf("Close called %d times with Steam not running", fake.Closes)
	}
	if a.msg != "Copied 1 game" {
		t.Errorf("msg = %q", a.msg)
	}
	if _, err := os.Stat(filepath.Join(steam.UserdataDir(root, "3000"), "413150", "s.dat")); err != nil {
		t.Errorf("not copied: %v", err)
	}
}

func TestCopyAbortsWhenSteamWillNotClose(t *testing.T) {
	a, root, fake := newTestApp(t)
	fake.IsRunning, fake.CloseErr = true, errors.New("denied")
	a.dst.Selected = 3
	a.onDest()
	checkedSet(a, "620")
	copyThroughModals(t, a, fake)
	if !a.msgBad || !strings.Contains(a.msg, "denied") {
		t.Errorf("msg = %q bad=%v, want a DANGER message with the close error", a.msg, a.msgBad)
	}
	if _, err := os.Stat(filepath.Join(steam.UserdataDir(root, "3000"), "620")); !os.IsNotExist(err) {
		t.Errorf("copied although Steam stayed open: %v", err)
	}
}

func TestCopyAbortsOnCloseTimeout(t *testing.T) {
	a, _, fake := newTestApp(t)
	fake.IsRunning = true
	a.closeWait = 30 * time.Millisecond
	// A Steam that never quits: Close succeeds but Running stays true.
	a.sys = stubbornSteam{}
	a.dst.Selected = 3
	a.onDest()
	checkedSet(a, "620")
	a.requestCopy()
	a.confirmCopy()
	waitApp(t, a, "Steam modal", func() bool { return a.modal == modalSteam })
	a.closeSteam()
	waitApp(t, a, "timeout", func() bool { return !a.copying && a.msg != "" })
	if !a.msgBad || !strings.Contains(a.msg, "did not close") {
		t.Errorf("msg = %q bad=%v", a.msg, a.msgBad)
	}
}

type stubbornSteam struct{}

func (stubbornSteam) Running() (bool, error) { return true, nil }
func (stubbornSteam) Close() error           { return nil }

func TestCopyReportsPerGameErrors(t *testing.T) {
	a, root, fake := newTestApp(t)
	a.dst.Selected = 3
	a.onDest()
	checkedSet(a, "620", "413150")
	// Break one source after listing: its folder is gone when the copy runs.
	if err := os.RemoveAll(filepath.Join(steam.UserdataDir(root, "1000"), "413150")); err != nil {
		t.Fatal(err)
	}
	copyThroughModals(t, a, fake)
	if !a.msgBad || !strings.HasPrefix(a.msg, "1 of 2 games failed. 413150:") {
		t.Errorf("msg = %q bad=%v", a.msg, a.msgBad)
	}
	if _, err := os.Stat(filepath.Join(steam.UserdataDir(root, "3000"), "620")); err != nil {
		t.Errorf("the good game was not copied: %v", err)
	}
}

func TestPathValidationLine(t *testing.T) {
	a, root, _ := newTestApp(t)

	a.openPath()
	if a.modal != modalPath || a.pathEd.Text() != root {
		t.Fatalf("openPath: modal %d text %q", a.modal, a.pathEd.Text())
	}
	if txt, col := a.pathLine(); txt != "3 accounts found" || col != GOOD || !a.pathValid {
		t.Errorf("valid root: %q %v valid=%v", txt, col, a.pathValid)
	}

	a.pathEd.SetText(filepath.Join(root, "userdata")) // userdata is stripped
	a.revalidate()
	if txt, _ := a.pathLine(); txt != "3 accounts found" {
		t.Errorf("userdata path: %q", txt)
	}

	a.pathEd.SetText(filepath.Join(root, "nope"))
	a.revalidate()
	if txt, col := a.pathLine(); txt != "No userdata folder here" || col != DANGER || a.pathValid {
		t.Errorf("invalid root: %q %v valid=%v", txt, col, a.pathValid)
	}

	empty := t.TempDir()
	if err := os.MkdirAll(filepath.Join(empty, "userdata", "5"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.pathEd.SetText(empty)
	a.revalidate()
	if txt, _ := a.pathLine(); txt != "1 account found" {
		t.Errorf("one account: %q", txt)
	}
	a.pathEd.SetText("   ")
	a.revalidate()
	if a.pathValid {
		t.Error("blank path is valid")
	}
}

func TestSavePathWritesConfigAndReloads(t *testing.T) {
	a, _, _ := newTestApp(t)
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "userdata", "7000", "730", "s.dat"), "x")

	a.openPath()
	a.pathEd.SetText(filepath.Join(other, "userdata")) // pasted with userdata: resolves to the root
	a.revalidate()
	a.savePath()
	if a.modal != modalNone {
		t.Fatalf("modal %d after Save", a.modal)
	}
	waitApp(t, a, "reload", func() bool { return a.root == other })
	if len(a.accounts) != 1 || a.accounts[0].ID != "7000" || !eq(rowIDs(a.rows), []string{"730"}) {
		t.Errorf("after reload: accounts %+v rows %v", a.accounts, rowIDs(a.rows))
	}
	cfg, err := steam.LoadConfig()
	if err != nil || cfg.SteamPath != other {
		t.Errorf("saved config = %+v, %v; want steam_path %s", cfg, err, other)
	}

	// An invalid path never saves.
	a.openPath()
	a.pathEd.SetText(filepath.Join(other, "missing"))
	a.revalidate()
	a.savePath()
	if a.modal != modalPath {
		t.Error("modal closed on an invalid path")
	}
	cfg, _ = steam.LoadConfig()
	if cfg.SteamPath != other {
		t.Errorf("invalid path overwrote the config: %+v", cfg)
	}
}

func TestStartWithoutSteamOpensPathModal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // nothing to detect here, never the real Steam
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	a := NewApp(nil, &transfer.Fake{}, lookup.New(t.TempDir()))
	a.Start()
	waitApp(t, a, "path modal", func() bool { return a.modal == modalPath })
	if a.looking || a.root != "" || len(a.accounts) != 0 {
		t.Errorf("empty state wrong: looking=%v root=%q accounts=%d", a.looking, a.root, len(a.accounts))
	}
	// Cancel leaves the empty app standing.
	a.modal = modalNone
	if got := a.footerPath(); got != "No Steam folder set" {
		t.Errorf("footer path = %q", got)
	}
	a.layoutSmoke(t, image.Pt(520, 760))
}

// ---- layout smoke (no GPU: only builds the op list) ----------------------------

func (a *App) layoutSmoke(t *testing.T, size image.Point) {
	t.Helper()
	var ops op.Ops
	var r input.Router
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Constraints{Max: size},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
		Source:      r.Source(),
	}
	dims := a.Layout(gtx)
	if dims.Size != size {
		t.Errorf("Layout size = %v, want %v", dims.Size, size)
	}
	r.Frame(&ops)
}

func TestLayoutModesAndModals(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.layoutSmoke(t, image.Pt(520, 760))
	if a.wide {
		t.Error("520 dp is the single-column layout")
	}
	a.layoutSmoke(t, image.Pt(960, 600))
	if !a.wide {
		t.Error("960 dp is the two-column layout")
	}
	a.layoutSmoke(t, image.Pt(759, 600))
	if a.wide {
		t.Error("759 dp must stay single column")
	}
	a.layoutSmoke(t, image.Pt(760, 600))
	if !a.wide {
		t.Error("760 dp switches to two columns")
	}

	a.dst.Selected = 2
	a.onDest()
	checkedSet(a, "620", "413150")
	for _, m := range []modalKind{modalConfirm, modalSteam, modalPath} {
		a.modal = m
		a.layoutSmoke(t, image.Pt(520, 760))
		a.layoutSmoke(t, image.Pt(960, 600))
	}
	a.modal = modalNone
	a.copying, a.progress, a.status = true, 0.67, "Copying Portal 2 (2/3)…"
	a.layoutSmoke(t, image.Pt(520, 760))
	a.layoutSmoke(t, image.Pt(960, 600))
}
