package ui

// The SteamAlt window: all app state, the responsive page layout, the modals and
// the background work (root detection, avatars, name lookups, the copy).
//
// Threading: every field below is touched on the UI goroutine only (Layout, or the
// test goroutine standing in for it). Goroutines never write fields; they hand a
// closure to post, which update runs (via drain) at the start of the next frame.
// The only mutex guards that queue.
//
// State fields a render test can set to force a screen (all plain fields):
//
//	default page        a.load(root) on a synthetic root, then Layout
//	confirm modal       a.modal = modalConfirm            (needs checked rows and a destination)
//	Steam-running modal a.modal = modalSteam
//	path modal          a.modal = modalPath; a.pathEd.SetText("/nope")   (invalid -> DANGER line, Save disabled)
//	                    or a.openPath() to prefill with a.root
//	copying             a.copying = true; a.progress = 0.67; a.status = "Copying Portal 2 (2/3)…"
//	result line         a.msg = "Copied 3 games"; a.msgBad = false (GOOD), true (DANGER)
//	startup warning     a.warning = "..."
//	narrow / wide       the Layout constraints (wide when the width is >= 760 dp)
//	checks              a.rows[i].check.Value = true
//	account choice      a.src.Selected = i; a.onSource()   /   a.dst.Selected = i+1 (0 = none); a.onDest()
//
// Synchronous seams for tests: load(root) reads and applies a Steam root in one go,
// drain() runs the posted closures, and confirmCopy / closeSteam / startCopy drive
// the copy flow without clicks.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/Ritze03/SteamAlt/assets"
	"github.com/Ritze03/SteamAlt/lookup"
	"github.com/Ritze03/SteamAlt/steam"
	"github.com/Ritze03/SteamAlt/transfer"
)

// wideBreakpoint is the window width (dp) from which the two-column layout is used.
const wideBreakpoint unit.Dp = 760

// modalKind says which modal (if any) is open.
type modalKind int

const (
	modalNone    modalKind = iota
	modalConfirm           // "Copy N games from A → B?"
	modalSteam             // "Steam is running"
	modalPath              // "Steam folder"
)

// gameRow is one app-id folder of the source account.
type gameRow struct {
	id         string
	overwrites bool // the destination already has this folder
	check      widget.Bool
}

// App is the whole UI state. Build it with NewApp, call Start once, call Layout every frame.
type App struct {
	invalidate func()
	sys        transfer.Steam
	lk         *lookup.Client
	th         *Theme
	title      TitleBar

	mu    sync.Mutex // guards queue only
	queue []func()

	// Steam root and accounts.
	looking   bool // Start is still resolving the root
	root      string
	warning   string // from ResolveRoot (saved path invalid, detection used)
	accounts  []steam.Account
	installed map[string]string      // appmanifest names of root
	avatars   map[string]image.Image // by avatar hash
	nameMap   map[string]string      // app id -> shown name (a real one, or the "App <id>" fallback)
	fallback  map[string]bool        // ids whose nameMap entry is only the fallback: final to look at, unknown to setSource

	rootCancel  context.CancelFunc // stops the avatar fetches of the previous root
	namesCancel context.CancelFunc // stops the name lookups of the previous source
	copyCancel  context.CancelFunc // stops the running copy (Shutdown)
	copyDone    chan struct{}      // closed when the copy goroutine has returned

	// Account pickers. dst.Items[0] is a "Select account" placeholder (drawn DIM,
	// dst.Placeholder), so account i of a.accounts is dst index i+1 and index 0
	// means no destination.
	src, dst     Dropdown
	srcAv, dstAv Avatar

	// Games of the source account.
	rows    []*gameRow
	visible []*gameRow // rows matching the filter, rebuilt each frame
	list    widget.List
	filter  widget.Editor

	bAll, bNone, bChange, bCopy widget.Clickable

	// Modals.
	modal                   modalKind
	shown                   modalKind // modal the focus was last set up for
	confirmM, steamM, pathM Modal
	pathEd                  widget.Editor
	pathText                string // text the path line below was computed for
	pathSeen                bool
	pathValid               bool
	pathN                   int    // accounts found at a valid path
	pathErr                 string // last save failure, shown instead of the live line

	// Copy flow. copying also covers the Steam check and the wait for Steam to quit.
	copying  bool
	progress float32 // 0..1
	status   string  // footer text next to the progress bar
	msg      string  // result / error line in the footer
	msgBad   bool    // msg is DANGER instead of GOOD

	closeWait time.Duration // how long to wait for Steam to quit
	closePoll time.Duration

	wide bool // layout mode of the last frame
}

// NewApp builds the app state. invalidate must be safe to call from any goroutine
// (window.Invalidate). s is the Steam seam (transfer.System() in production).
func NewApp(invalidate func(), s transfer.Steam, names *lookup.Client) *App {
	if invalidate == nil {
		invalidate = func() {}
	}
	a := &App{
		invalidate: invalidate,
		sys:        s,
		lk:         names,
		th:         NewTheme(),
		avatars:    map[string]image.Image{},
		nameMap:    map[string]string{},
		fallback:   map[string]bool{},
		closeWait:  30 * time.Second,
		closePoll:  500 * time.Millisecond,
	}
	a.title = TitleBar{Wordmark: "SteamAlt", Pill: "Userdata copier"}
	if img, err := png.Decode(bytes.NewReader(assets.IconPNG)); err == nil {
		a.title.Logo = img
	}
	a.confirmM = Modal{CancelLabel: "Cancel", Danger: true}
	a.steamM = Modal{Title: "Steam is running", ConfirmLabel: "Close Steam", CancelLabel: "Cancel"}
	a.pathM = Modal{Title: "Steam folder", ConfirmLabel: "Save", CancelLabel: "Cancel", Width: 460}
	a.src.Selected = -1
	a.dst.Items = []DropdownItem{{Name: "Select account"}}
	a.dst.Placeholder = true
	return a
}

// ---- background plumbing ------------------------------------------------------

// post queues f to run on the UI goroutine at the start of the next frame.
func (a *App) post(f func()) {
	a.mu.Lock()
	a.queue = append(a.queue, f)
	a.mu.Unlock()
	a.invalidate()
}

// drain runs the closures posted so far.
func (a *App) drain() {
	a.mu.Lock()
	q := a.queue
	a.queue = nil
	a.mu.Unlock()
	for _, f := range q {
		f()
	}
}

// rootData is what is read from disk for a Steam root.
type rootData struct {
	accounts  []steam.Account
	err       error
	installed map[string]string
}

func readRoot(root string) rootData {
	accs, err := steam.Accounts(root)
	return rootData{accounts: accs, err: err, installed: steam.InstalledNames(root)}
}

// Start resolves the Steam root, reads the accounts and fetches avatars, all in the
// background. With no Steam found it opens the path modal.
func (a *App) Start() {
	a.looking = true
	go func() {
		root, warn, err := steam.ResolveRoot()
		if err != nil {
			a.post(func() {
				a.looking = false
				a.warning = warn
				a.openPath()
			})
			return
		}
		d := readRoot(root)
		a.post(func() {
			a.warning = warn
			a.apply(root, d)
		})
	}()
}

// load reads and applies root synchronously (tests; the app itself uses loadAsync).
func (a *App) load(root string) { a.apply(root, readRoot(root)) }

// loadAsync reads root in the background and applies it on the UI goroutine.
func (a *App) loadAsync(root string) {
	a.looking = true
	go func() {
		d := readRoot(root)
		a.post(func() { a.apply(root, d) })
	}()
}

// apply installs a freshly read Steam root: account pickers, avatars, source games.
func (a *App) apply(root string, d rootData) {
	a.looking = false
	a.root = root
	a.accounts = d.accounts
	a.installed = d.installed
	if d.err != nil {
		a.setMsg("Could not read accounts: "+d.err.Error(), true)
	}
	items := make([]DropdownItem, len(d.accounts))
	for i, acc := range d.accounts {
		items[i] = DropdownItem{Name: acc.Name, ID: acc.ID}
	}
	a.src.Items = items
	a.src.Selected = -1
	if len(items) > 0 {
		a.src.Selected = 0
	}
	a.dst.Items = append([]DropdownItem{{Name: "Select account"}}, items...)
	a.dst.Selected = 0
	a.src.Close()
	a.dst.Close()

	if a.rootCancel != nil {
		a.rootCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.rootCancel = cancel
	if a.lk != nil {
		for _, acc := range d.accounts {
			if acc.AvatarHash == "" || a.avatars[acc.AvatarHash] != nil {
				continue
			}
			go func(hash string) {
				if img, err := a.lk.Avatar(ctx, hash); err == nil {
					a.post(func() { a.avatars[hash] = img })
				}
			}(acc.AvatarHash)
		}
	}
	a.setSource()
}

// ---- accounts and games -------------------------------------------------------

func (a *App) srcAcc() (steam.Account, bool) {
	if i := a.src.Selected; i >= 0 && i < len(a.accounts) {
		return a.accounts[i], true
	}
	return steam.Account{}, false
}

func (a *App) dstAcc() (steam.Account, bool) {
	if i := a.dst.Selected - 1; i >= 0 && i < len(a.accounts) {
		return a.accounts[i], true
	}
	return steam.Account{}, false
}

func (a *App) dstID() string {
	if acc, ok := a.dstAcc(); ok {
		return acc.ID
	}
	return ""
}

// sameAccount reports that source and destination are the same account.
func (a *App) sameAccount() bool {
	s, ok1 := a.srcAcc()
	d, ok2 := a.dstAcc()
	return ok1 && ok2 && s.ID == d.ID
}

// onSource must run after the user picked another source account.
func (a *App) onSource() {
	a.msg = ""
	a.setSource()
}

// onDest must run after the user picked another destination: only the overwrite
// marks change, the checks stay.
func (a *App) onDest() {
	a.msg = ""
	src, ok := a.srcAcc()
	if !ok {
		return
	}
	gs, err := steam.Games(a.root, src.ID, a.dstID())
	if err != nil {
		return
	}
	over := map[string]bool{}
	for _, g := range gs {
		over[g.AppID] = g.Overwrites
	}
	for _, r := range a.rows {
		r.overwrites = over[r.id]
	}
}

// setSource lists the source account's games (all unchecked) and resolves the
// names that are not known yet in the background.
func (a *App) setSource() {
	if a.namesCancel != nil {
		a.namesCancel()
		a.namesCancel = nil
	}
	a.rows = nil
	a.list.List.Position = layout.Position{}
	src, ok := a.srcAcc()
	if !ok || a.root == "" {
		return
	}
	gs, err := steam.Games(a.root, src.ID, a.dstID())
	if err != nil {
		a.setMsg("Could not list games: "+err.Error(), true)
		return
	}
	var need []string
	for _, g := range gs {
		a.rows = append(a.rows, &gameRow{id: g.AppID, overwrites: g.Overwrites})
		if _, known := a.nameMap[g.AppID]; !known || a.fallback[g.AppID] {
			need = append(need, g.AppID)
		}
	}
	if len(need) == 0 || a.lk == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.namesCancel = cancel
	lk, local := a.lk, a.installed
	go lk.Names(ctx, need, local, func(id, name string) {
		a.post(func() { a.setName(id, name) })
	})
}

// setName stores a looked-up name (UI goroutine). The "App <id>" fallback only
// fills a gap: it is final for display but flagged in a.fallback, so the next
// setSource asks again (offline, 429, cancelled). It never overwrites a real name;
// a real name always wins and clears the flag.
func (a *App) setName(id, name string) {
	if name == lookup.FallbackName(id) {
		if _, have := a.nameMap[id]; !have {
			a.nameMap[id] = name
			a.fallback[id] = true
		}
		return
	}
	a.nameMap[id] = name
	delete(a.fallback, id)
}

// displayName is the name to show; resolved is false only while the lookup still
// runs (the fallback shown DIM). A settled fallback counts as resolved here.
func (a *App) displayName(id string) (name string, resolved bool) {
	if n, ok := a.nameMap[id]; ok {
		return n, true
	}
	return lookup.FallbackName(id), false
}

// filterRows returns the rows whose id or resolved name contains the filter text.
func (a *App) filterRows() []*gameRow {
	q := strings.ToLower(strings.TrimSpace(a.filter.Text()))
	if q == "" {
		return a.rows
	}
	var out []*gameRow
	for _, r := range a.rows {
		name, ok := a.displayName(r.id)
		if strings.Contains(r.id, q) || (ok && strings.Contains(strings.ToLower(name), q)) {
			out = append(out, r)
		}
	}
	return out
}

// setVisible checks or unchecks every row that matches the filter.
func (a *App) setVisible(on bool) {
	for _, r := range a.filterRows() {
		r.check.Value = on
	}
}

func (a *App) checkedIDs() []string {
	var ids []string
	for _, r := range a.rows {
		if r.check.Value {
			ids = append(ids, r.id)
		}
	}
	return ids
}

func (a *App) overwriteCount() int {
	n := 0
	for _, r := range a.rows {
		if r.check.Value && r.overwrites {
			n++
		}
	}
	return n
}

// canCopy is the enabled state of the Copy button.
func (a *App) canCopy() bool {
	src, ok := a.srcAcc()
	return ok && a.root != "" && !a.copying && a.dstID() != "" && a.dstID() != src.ID && len(a.checkedIDs()) > 0
}

// ellipsize cuts s to at most n runes, ending in "…" when it was longer.
func ellipsize(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func gamesLabel(n int) string {
	if n == 1 {
		return "1 game"
	}
	return fmt.Sprintf("%d games", n)
}

func (a *App) setMsg(s string, bad bool) { a.msg, a.msgBad = s, bad }

// ---- copy flow ----------------------------------------------------------------

// requestCopy opens the confirm modal.
func (a *App) requestCopy() {
	if a.canCopy() {
		a.modal = modalConfirm
	}
}

func (a *App) beginBusy(status string) {
	a.copying = true
	a.progress = 0
	a.status = status
	a.msg = ""
}

func (a *App) endBusy() { a.copying = false }

// confirmCopy runs after the confirm modal: it checks whether Steam runs (in the
// background), then either asks to close it or starts the copy.
func (a *App) confirmCopy() {
	a.modal = modalNone
	a.beginBusy("Checking Steam…")
	go func() {
		running, err := a.sys.Running()
		a.post(func() {
			switch {
			case err != nil:
				a.endBusy()
				a.setMsg("Could not check whether Steam is running: "+err.Error(), true)
			case running:
				a.endBusy()
				a.modal = modalSteam
			default:
				a.startCopy()
			}
		})
	}()
}

// closeSteam runs after the Steam-running modal: it asks Steam to quit, waits for
// it (closeWait), then starts the copy. A failure or timeout aborts with a message.
func (a *App) closeSteam() {
	a.modal = modalNone
	a.beginBusy("Closing Steam…")
	wait, poll := a.closeWait, a.closePoll
	go func() {
		err := a.sys.Close()
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), wait)
			err = transfer.WaitClosed(ctx, a.sys, poll)
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("Steam did not close in time")
			}
		}
		a.post(func() {
			if err != nil {
				a.endBusy()
				a.setMsg("Could not close Steam: "+err.Error(), true)
				return
			}
			a.startCopy()
		})
	}()
}

// startCopy copies the checked games in the background, reporting progress.
func (a *App) startCopy() {
	src, ok := a.srcAcc()
	dst, ok2 := a.dstAcc()
	ids := a.checkedIDs()
	if !ok || !ok2 || len(ids) == 0 {
		a.endBusy()
		return
	}
	a.copying = true
	a.progress = 0
	a.msg = ""
	a.status = fmt.Sprintf("Copying 0/%d…", len(ids))
	srcDir, dstDir := steam.UserdataDir(a.root, src.ID), steam.UserdataDir(a.root, dst.ID)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	a.copyCancel, a.copyDone = cancel, finished
	go func() {
		defer close(finished)
		errs := transfer.Copy(ctx, srcDir, dstDir, ids, func(done, total int, id string) {
			a.post(func() {
				a.progress = float32(done) / float32(total)
				name, _ := a.displayName(id)
				a.status = fmt.Sprintf("Copying %s (%d/%d)…", name, done, total)
			})
		})
		a.post(func() { a.finishCopy(len(ids), errs) })
	}()
}

// Shutdown stops the background work for a closing window: it cancels the running
// copy and waits up to 5 s for it to return, so transfer.Copy removes its
// .steamalt-tmp-<id> folder before the process exits. Call it on the UI goroutine.
func (a *App) Shutdown() {
	if a.rootCancel != nil {
		a.rootCancel()
	}
	if a.namesCancel != nil {
		a.namesCancel()
	}
	if a.copyCancel == nil {
		return
	}
	a.copyCancel()
	select {
	case <-a.copyDone:
	case <-time.After(5 * time.Second):
	}
}

// finishCopy shows the result, then re-lists the games (fresh overwrite marks,
// everything unchecked).
func (a *App) finishCopy(n int, errs map[string]error) {
	a.copying = false
	if len(errs) == 0 {
		a.setMsg("Copied "+gamesLabel(n), false)
	} else {
		ids := make([]string, 0, len(errs))
		for id := range errs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		name, _ := a.displayName(ids[0])
		a.setMsg(fmt.Sprintf("%d of %d games failed. %s: %v", len(errs), n, name, errs[ids[0]]), true)
	}
	a.setSource()
}

// ---- Steam folder modal -------------------------------------------------------

// openPath opens the Steam folder modal prefilled with the current root.
func (a *App) openPath() {
	a.pathEd.SetText(a.root)
	a.pathSeen = false
	a.pathErr = ""
	a.modal = modalPath
	a.revalidate()
}

// checkPath turns what the user typed into a Steam root and validates it.
func checkPath(text string) (root string, accounts int, err error) {
	text = strings.TrimSpace(text)
	if text == "~" || strings.HasPrefix(text, "~/") {
		if h, herr := os.UserHomeDir(); herr == nil {
			text = filepath.Join(h, text[1:])
		}
	}
	root = steam.NormalizeRoot(text)
	if root == "" {
		return "", 0, errors.New("empty path")
	}
	accounts, err = steam.CheckRoot(root)
	return root, accounts, err
}

// revalidate recomputes the live line, only when the text changed.
func (a *App) revalidate() {
	t := a.pathEd.Text()
	if a.pathSeen && t == a.pathText {
		return
	}
	a.pathSeen, a.pathText = true, t
	a.pathErr = ""
	_, n, err := checkPath(t)
	a.pathN, a.pathValid = n, err == nil
}

// pathLine is the live line under the path field.
func (a *App) pathLine() (string, color.NRGBA) {
	switch {
	case a.pathErr != "":
		return a.pathErr, DANGER
	case a.pathValid && a.pathN == 1:
		return "1 account found", GOOD
	case a.pathValid:
		return fmt.Sprintf("%d accounts found", a.pathN), GOOD
	}
	return "No userdata folder here", DANGER
}

// savePath writes the config and reloads everything from the new root.
func (a *App) savePath() {
	a.revalidate()
	root, _, err := checkPath(a.pathEd.Text())
	if err != nil {
		return
	}
	if err := steam.SaveConfig(steam.Config{SteamPath: root}); err != nil {
		a.pathErr = "Could not save: " + err.Error()
		return
	}
	a.modal = modalNone
	a.warning = ""
	a.msg = ""
	a.loadAsync(root)
}

// ---- frame ----------------------------------------------------------------------

// update runs the posted closures and polls all input before anything is laid out
// (Gio widgets drain their events inside Layout).
func (a *App) update(gtx layout.Context) {
	a.drain()

	if a.modal != a.shown {
		a.shown = a.modal
		switch a.modal {
		case modalPath:
			gtx.Execute(key.FocusCmd{Tag: &a.pathEd})
			a.pathEd.SetCaret(0, a.pathEd.Len())
		case modalConfirm, modalSteam:
			gtx.Execute(key.FocusCmd{Tag: nil})
		}
	}

	Submitted(gtx, &a.filter) // also processes typing, so the text read below is current
	a.visible = a.filterRows()
	for _, r := range a.visible {
		r.check.Update(gtx)
	}
	all, none := a.bAll.Clicked(gtx), a.bNone.Clicked(gtx)
	change, cp := a.bChange.Clicked(gtx), a.bCopy.Clicked(gtx)

	switch a.modal {
	case modalConfirm:
		if ok, cancel := a.confirmM.Update(gtx); ok {
			a.confirmCopy()
		} else if cancel {
			a.modal = modalNone
		}
		return
	case modalSteam:
		if ok, cancel := a.steamM.Update(gtx); ok {
			a.closeSteam()
		} else if cancel {
			a.modal = modalNone
		}
		return
	case modalPath:
		sub := Submitted(gtx, &a.pathEd)
		a.revalidate()
		a.pathM.ConfirmDisabled = !a.pathValid
		ok, cancel := a.pathM.Update(gtx)
		switch {
		case (ok || sub) && a.pathValid:
			a.savePath()
		case cancel:
			a.modal = modalNone
		}
		return
	}

	if a.copying {
		return
	}
	switch {
	case all:
		a.setVisible(true)
	case none:
		a.setVisible(false)
	case change:
		a.openPath()
	case cp:
		a.requestCopy()
	}
}

// Layout draws the whole window for this frame.
func (a *App) Layout(gtx layout.Context) layout.Dimensions {
	a.update(gtx)
	size := gtx.Constraints.Max
	a.wide = size.X >= gtx.Dp(wideBreakpoint)
	Fill(gtx, PANEL)
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.title.Layout(gtx, a.th) }),
		layout.Flexed(1, a.layoutBody),
		layout.Rigid(a.layoutFooter),
	)
	a.layoutModal(gtx)

	// Dropdowns report a pick from inside their Layout: handle it now and redraw.
	if a.src.Changed() {
		a.onSource()
		gtx.Execute(op.InvalidateCmd{})
	}
	if a.dst.Changed() {
		a.onDest()
		gtx.Execute(op.InvalidateCmd{})
	}
	return layout.Dimensions{Size: size}
}

func (a *App) layoutBody(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	if a.copying {
		gtx = gtx.Disabled() // no input while a copy runs
	}
	src, sok := a.srcAcc()
	dst, dok := a.dstAcc()
	srcCard := func(gtx layout.Context) layout.Dimensions {
		return a.accountCard(gtx, "Source", &a.src, &a.srcAv, src, sok, "?")
	}
	dstCard := func(gtx layout.Context) layout.Dimensions {
		return a.accountCard(gtx, "Destination", &a.dst, &a.dstAv, dst, dok, "?")
	}
	layout.UniformInset(Gap).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if a.wide {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(srcCard),
						layout.Rigid(layout.Spacer{Height: Gap}.Layout),
						layout.Rigid(dstCard),
					)
				}),
				layout.Rigid(layout.Spacer{Width: Gap}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
					return a.gamesCard(gtx)
				}),
			)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(srcCard),
			layout.Rigid(layout.Spacer{Height: Gap}.Layout),
			layout.Rigid(dstCard),
			layout.Rigid(layout.Spacer{Height: Gap}.Layout),
			layout.Flexed(1, a.gamesCard),
		)
	})
	if a.copying {
		// The mockup dims the panes to 55 % opacity. Gio blends in linear light, so a
		// 45 % PANEL wash would leave the content near 77 % of its sRGB brightness;
		// 0.72 lands on about 55 %.
		paint.FillShape(gtx.Ops, fade(PANEL, .72), clip.Rect(image.Rectangle{Max: size}).Op())
	}
	return layout.Dimensions{Size: size}
}

// avatarFor returns the account's decoded avatar (nil while loading or on failure)
// and the placeholder letter.
func (a *App) avatarFor(acc steam.Account, ok bool, empty string) (image.Image, string) {
	if !ok {
		return nil, empty
	}
	letter := empty
	if r, _ := utf8.DecodeRuneInString(acc.Name); r != utf8.RuneError {
		letter = string(r)
	}
	return a.avatars[acc.AvatarHash], letter
}

func (a *App) accountCard(gtx layout.Context, title string, dd *Dropdown, av *Avatar, acc steam.Account, ok bool, empty string) layout.Dimensions {
	return Card(gtx, a.th, title, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				img, letter := a.avatarFor(acc, ok, empty)
				return av.Layout(gtx, a.th, img, letter, 64)
			}),
			layout.Rigid(layout.Spacer{Width: Gap}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return dd.Layout(gtx, a.th) }),
		)
	})
}

func (a *App) gamesCard(gtx layout.Context) layout.Dimensions {
	empty := "No games match"
	if len(a.rows) == 0 {
		empty = "No games found"
	}
	return Card(gtx, a.th, "Games", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return TextField(gtx, a.th, &a.filter, "Filter", false)
					}),
					layout.Rigid(layout.Spacer{Width: Gap}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SecondaryButton(gtx, a.th, &a.bAll, "All", true)
					}),
					layout.Rigid(layout.Spacer{Width: Gap}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SecondaryButton(gtx, a.th, &a.bNone, "None", true)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: RowGap}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ListBox(gtx, a.th, &a.list, len(a.visible), empty, func(gtx layout.Context, i int) layout.Dimensions {
					r := a.visible[i]
					name, resolved := a.displayName(r.id)
					it := CheckItem{Name: name, ID: r.id, Pending: !resolved}
					if r.overwrites {
						it.Warn = "overwrites"
					}
					return CheckRow(gtx, a.th, &r.check, it)
				})
			}),
		)
	})
}

// ---- footer ---------------------------------------------------------------------

// tildePath shortens a leading home directory to "~" for display.
func tildePath(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && (p == h || strings.HasPrefix(p, h+string(filepath.Separator))) {
		return "~" + p[len(h):]
	}
	return p
}

// footerPath is the Mono text at the left of the footer.
func (a *App) footerPath() string {
	switch {
	case a.root != "":
		return tildePath(filepath.Join(a.root, "userdata"))
	case a.looking:
		return "Looking for Steam…"
	}
	return "No Steam folder set"
}

// footerMsg is the line above the footer controls: a conflicting pick first, then
// the copy result, then the startup warning.
func (a *App) footerMsg() (string, color.NRGBA) {
	switch {
	case a.sameAccount():
		return "Source and destination are the same account.", DANGER
	case a.msg != "" && a.msgBad:
		return a.msg, DANGER
	case a.msg != "":
		return a.msg, GOOD
	case a.warning != "":
		return a.warning, WARN
	}
	return "", DIM
}

func (a *App) btnWidth(gtx layout.Context, primary bool, label string) int {
	gtx.Constraints.Min = image.Point{}
	_, d := measure(gtx, func(gtx layout.Context) layout.Dimensions {
		c := new(widget.Clickable)
		if primary {
			return PrimaryButton(gtx, a.th, c, label, true)
		}
		return SecondaryButton(gtx, a.th, c, label, true)
	})
	return d.Size.X
}

func (a *App) layoutFooter(gtx layout.Context) layout.Dimensions {
	return StatusBar(gtx, a.th, func(gtx layout.Context) layout.Dimensions {
		var top layout.Widget
		if a.copying {
			top = a.progressLine
		} else if txt, col := a.footerMsg(); txt != "" {
			top = func(gtx layout.Context) layout.Dimensions {
				// At most 2 lines: a long error must not squeeze the list.
				return widget.Label{MaxLines: 2, Truncator: "…"}.Layout(gtx, a.th.Shaper, a.th.font(Regular), SizeBody, txt, colorOp(gtx, col))
			}
		}
		children := []layout.FlexChild{}
		if top != nil {
			children = append(children, layout.Rigid(top), layout.Rigid(layout.Spacer{Height: Gap}.Layout))
		}
		children = append(children, layout.Rigid(a.controlsRow))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// progressLine is the ProgressBar plus its status text, shown while copying.
func (a *App) progressLine(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			d := ProgressBar(gtx, a.th, a.progress)
			return d
		}),
		layout.Rigid(layout.Spacer{Width: Gap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Constraints.Max.X * 55 / 100
			return a.th.Line(gtx, Regular, SizeBody, DIM, a.status)
		}),
	)
}

// controlsRow is: path (ellipsised), Change, spacer, Copy button.
func (a *App) controlsRow(gtx layout.Context) layout.Dimensions {
	label := "Copy " + gamesLabel(len(a.checkedIDs()))
	if a.copying {
		label = "Copying…"
	}
	gap := gtx.Dp(Gap)
	pathMax := gtx.Constraints.Max.X - a.btnWidth(gtx, false, "Change") - a.btnWidth(gtx, true, label) - 2*gap
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = 0
			gtx.Constraints.Max.X = max(0, pathMax)
			col := DIM
			if a.root == "" && !a.looking {
				col = WARN
			}
			return a.th.Line(gtx, Mono, SizeMono, col, a.footerPath())
		}),
		layout.Rigid(layout.Spacer{Width: Gap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SecondaryButton(gtx, a.th, &a.bChange, "Change", !a.copying)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return PrimaryButton(gtx, a.th, &a.bCopy, label, a.canCopy())
		}),
	)
}

// ---- modals ---------------------------------------------------------------------

func (a *App) layoutModal(gtx layout.Context) {
	dim := func(text string) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, Regular, SizeBody, DIM, text) }
	}
	switch a.modal {
	case modalConfirm:
		n, over := len(a.checkedIDs()), a.overwriteCount()
		sName, dName := "?", "?"
		if s, ok := a.srcAcc(); ok {
			sName = s.Name
		}
		if d, ok := a.dstAcc(); ok {
			dName = d.Name
		}
		body := "No existing data in " + dName + " will be overwritten."
		a.confirmM.ConfirmLabel = "Copy"
		if over > 0 {
			who := fmt.Sprintf("%d of them", over)
			if n == 1 {
				who = "it"
			}
			body = "Existing data for " + who + " in " + dName + " will be overwritten."
			a.confirmM.ConfirmLabel = "Overwrite & copy"
		}
		a.confirmM.Title = fmt.Sprintf("Copy %s from %s → %s?", gamesLabel(n), ellipsize(sName, 24), ellipsize(dName, 24))
		a.confirmM.Layout(gtx, a.th, dim(body))
	case modalSteam:
		a.steamM.Layout(gtx, a.th, dim("Close Steam before copying? Copying while Steam runs can corrupt saves."))
	case modalPath:
		a.pathM.ConfirmDisabled = !a.pathValid
		a.pathM.Layout(gtx, a.th, func(gtx layout.Context) layout.Dimensions {
			txt, col := a.pathLine()
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return TextField(gtx, a.th, &a.pathEd, "", true)
				}),
				layout.Rigid(layout.Spacer{Height: Gap}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, Regular, SizeBody, col, txt)
				}),
			)
		})
	}
}
