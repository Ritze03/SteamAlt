package ui

// Headless render harness: the whole App on synthetic data, in every key state, at
// several window sizes. Two jobs:
//
//  1. PNGs for the visual review (D18). Set RENDER_OUT=<dir> and every case writes
//     <state>-<w>x<h>.png there (sizes in dp; a scale suffix like @2x is added when
//     1 dp != 1 px). Without RENDER_OUT nothing is written, the assertions still run.
//  2. The pane-overflow check (D9): the App is laid out in a window-sized constraint
//     box placed with a margin inside a bigger canvas, and no pixel outside the box
//     may differ from the canvas background. That holds for the modals (drawn with
//     op.Defer) and the open dropdown popup too: they may float over panes, never
//     over the window edge. Together with TestPaintsInsideConstraints in
//     widgets_test.go (every widget inside its own box) this is D9's "no widget
//     paints outside its pane".
//
// Everything is synthetic: the Steam root is a temp dir, the Steam seam is a
// transfer.Fake, names and avatars come from an httptest server, HOME and the XDG
// dirs point into temp dirs. Needs a GPU context (skips without one); run it in a
// headless sway like the other render tests.

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/Ritze03/SteamAlt/lookup"
	"github.com/Ritze03/SteamAlt/transfer"
)

// ---- synthetic data -------------------------------------------------------------

type fakeAcc struct {
	id, name string
	avatar   string // 40-hex avatar hash, "" for none
	apps     []string
}

// renderEnv is one synthetic Steam root plus a lookup client wired to a local server.
type renderEnv struct {
	root string
	lk   *lookup.Client
	// avatars is how many avatar hashes the accounts have (the app must have
	// decoded that many before a render is representative).
	avatars int
}

const avatarHash = "0123456789abcdef0123456789abcdef01234567"

// isolateEnv keeps HOME and the XDG dirs inside temp dirs for the whole test.
func isolateEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
}

// avatarJPEG is a recognisable 184x184 picture (diagonal gradient with a bright disc).
func avatarJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 184, 184))
	for y := 0; y < 184; y++ {
		for x := 0; x < 184; x++ {
			c := color.RGBA{uint8(40 + x), uint8(90 + y/2), uint8(220 - x/2), 255}
			if dx, dy := x-92, y-92; dx*dx+dy*dy < 40*40 {
				c = color.RGBA{250, 220, 80, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

// newRenderEnv writes the Steam root under base (created) and starts the fake
// store/avatar server. manifests maps app id -> name (appmanifest in the root).
func newRenderEnv(t *testing.T, base string, accs []fakeAcc, manifests map[string]string) *renderEnv {
	t.Helper()
	root := filepath.Join(base, "Steam")
	ud := filepath.Join(root, "userdata")
	writeFile(t, filepath.Join(ud, "0", "config", "x"), "x")
	var users strings.Builder
	users.WriteString("\"users\" {\n")
	env := &renderEnv{root: root}
	for _, a := range accs {
		n, _ := strconv.ParseUint(a.id, 10, 64)
		fmt.Fprintf(&users, "\t\"%d\" { \"PersonaName\" %q }\n", n+76561197960265728, a.name)
		cfg := fmt.Sprintf("\"UserLocalConfigStore\" { \"friends\" { %q { \"avatar\" %q } } }", a.id, a.avatar)
		writeFile(t, filepath.Join(ud, a.id, "config", "localconfig.vdf"), cfg)
		if a.avatar != "" {
			env.avatars++
		}
		for _, app := range a.apps {
			writeFile(t, filepath.Join(ud, a.id, app, "remote", "save.dat"), "save of "+a.id+"/"+app)
		}
		if len(a.apps) == 0 {
			if err := os.MkdirAll(filepath.Join(ud, a.id), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	users.WriteString("}\n")
	writeFile(t, filepath.Join(root, "config", "loginusers.vdf"), users.String())
	for id, name := range manifests {
		writeFile(t, filepath.Join(root, "steamapps", "appmanifest_"+id+".acf"),
			fmt.Sprintf("\"AppState\"\n{\n\t\"appid\"\t\"%s\"\n\t\"name\"\t%q\n}\n", id, name))
	}

	jpg := avatarJPEG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.URL.Query().Get("appids"); id != "" { // store API: nothing is known
			fmt.Fprintf(w, `{%q:{"success":false}}`, id)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpg)
	}))
	t.Cleanup(srv.Close)
	env.lk = lookup.New(t.TempDir())
	env.lk.StoreURL = srv.URL
	env.lk.AvatarURL = srv.URL + "/"
	return env
}

// pendingID is an app whose name is made "still loading" after the names settle,
// so the default page shows a pending row next to resolved and "App <id>" rows.
const pendingID = "400"

// newAppEnv is the normal fixture:
//
//	Alice (source, with avatar): 16 games, 10 with an appmanifest (one very long name),
//	  999001/999002 resolve to "App <id>", 400 stays pending.
//	Bob (letter tile): already has 620, 730, 413150, 999001 -> those rows overwrite.
//	Carol with a very long name: no games.
func newAppEnv(t *testing.T) *renderEnv {
	manifests := map[string]string{
		"620":     "Portal 2",
		"730":     "Counter-Strike 2",
		"413150":  "Stardew Valley",
		"570":     "Dota 2",
		"440":     "Team Fortress 2",
		"105600":  "Terraria",
		"367520":  "Hollow Knight",
		"1245620": "Elden Ring",
		"292030":  "The Witcher 3: Wild Hunt",
		"1091500": "Cyberpunk 2077 Ultimate Edition with Phantom Liberty and every downloadable content pack ever released",
	}
	accs := []fakeAcc{
		{"1000", "Alice", avatarHash, []string{"620", "730", "413150", "570", "440", "105600", "367520", "1245620", "292030", "1091500", "999001", "999002", "999003", "220", pendingID, "4000"}},
		{"2000", "Bob", "", []string{"620", "730", "413150", "999001"}},
		{"3000", "Carol — a deliberately long display name to test ellipsis", "", nil},
	}
	return newRenderEnv(t, t.TempDir(), accs, manifests)
}

// newHostileEnv has very long account and game names and a very long Steam path.
func newHostileEnv(t *testing.T) *renderEnv {
	long := strings.Repeat("Overlong account name ", 8)
	manifests := map[string]string{
		"620":    strings.Repeat("An absurdly long game title that never ends ", 6),
		"730":    strings.Repeat("W", 140),   // one unbreakable word
		"413150": "Normal Game",              //
		"570":    strings.Repeat("中文游戏", 30), // wide runes, no spaces
	}
	accs := []fakeAcc{
		{"1000", "Alice " + long, avatarHash, []string{"620", "730", "413150", "570", "999001"}},
		{"2000", "Bob " + long, "", []string{"620", "730"}},
	}
	deep := t.TempDir()
	for i := 0; i < 6; i++ {
		deep = filepath.Join(deep, "an-extremely-long-directory-name-number-"+strconv.Itoa(i))
	}
	return newRenderEnv(t, deep, accs, manifests)
}

// newRenderApp loads the env into a fresh App and waits until names and avatars
// have settled, so renders do not depend on goroutine timing.
func newRenderApp(t *testing.T, env *renderEnv) *App {
	t.Helper()
	a := NewApp(nil, &transfer.Fake{}, env.lk)
	a.load(env.root)
	waitApp(t, a, "names and avatars", func() bool {
		return len(a.nameMap) == len(a.rows) && len(a.avatars) == env.avatars
	})
	delete(a.nameMap, pendingID) // render this row as pending
	return a
}

// ---- states ---------------------------------------------------------------------

// chooseBob picks Bob as destination and checks three games (two of them overwrite,
// so the Copy button is enabled and the WARN marks show).
func chooseBob(a *App) {
	a.dst.Selected = 2
	a.onDest()
	for _, r := range a.rows {
		switch r.id {
		case "620", "413150", "105600":
			r.check.Value = true
		}
	}
}

type renderState struct {
	name  string
	setup func(a *App)
	// check verifies the App really is in the state the name claims.
	check func(t *testing.T, a *App)
}

var renderStates = []renderState{
	{"initial", func(a *App) {}, func(t *testing.T, a *App) {
		if a.dst.Selected != 0 || len(a.checkedIDs()) != 0 || a.canCopy() {
			t.Errorf("initial: dst %d, checked %d", a.dst.Selected, len(a.checkedIDs()))
		}
	}},
	{"default", chooseBob, func(t *testing.T, a *App) {
		if !a.canCopy() || a.modal != modalNone || a.overwriteCount() != 2 {
			t.Errorf("default: canCopy %v modal %v overwrites %d", a.canCopy(), a.modal, a.overwriteCount())
		}
	}},
	{"confirm", func(a *App) { chooseBob(a); a.modal = modalConfirm }, func(t *testing.T, a *App) {
		if a.modal != modalConfirm || strings.Contains(a.confirmM.Title, "→ ?") {
			t.Errorf("confirm: modal %v title %q", a.modal, a.confirmM.Title)
		}
	}},
	{"steam-running", func(a *App) { chooseBob(a); a.modal = modalSteam }, func(t *testing.T, a *App) {
		if a.modal != modalSteam {
			t.Errorf("steam-running: modal %v", a.modal)
		}
	}},
	{"path-invalid", func(a *App) { chooseBob(a); a.modal = modalPath; a.pathEd.SetText("/nope") }, func(t *testing.T, a *App) {
		if a.modal != modalPath || a.pathValid {
			t.Errorf("path-invalid: modal %v pathValid %v", a.modal, a.pathValid)
		}
		if txt, _ := a.pathLine(); txt != "No userdata folder here" {
			t.Errorf("path-invalid: live line %q", txt)
		}
	}},
	{"copying", func(a *App) {
		chooseBob(a)
		a.copying, a.progress, a.status = true, 0.67, "Copying Portal 2 (2/3)…"
	}, func(t *testing.T, a *App) {
		if !a.copying || a.canCopy() {
			t.Errorf("copying: copying %v canCopy %v", a.copying, a.canCopy())
		}
	}},
	{"result-error", func(a *App) {
		chooseBob(a)
		a.setMsg("1 of 3 games failed. 620: permission denied", true)
	}, func(t *testing.T, a *App) {
		if txt, _ := a.footerMsg(); txt == "" {
			t.Error("result-error: no footer message")
		}
	}},
	{"dropdown-open", func(a *App) { chooseBob(a); a.dst.open = true }, func(t *testing.T, a *App) {
		if !a.dst.Open() {
			t.Error("dropdown-open: popup closed")
		}
	}},
}

// ---- rendering ------------------------------------------------------------------

// canvasBG is outside the app's palette so any stray pixel stands out.
var canvasBG = color.NRGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff}

const renderMargin = 24 // px around the window box

// renderApp lays a out in a size (dp) box at the given px-per-dp scale, inside a
// canvas with a margin, renders it and returns the box contents as an image. It
// fails the test for every pixel outside the box that differs from the canvas.
func renderApp(t *testing.T, a *App, size image.Point, scale float32) *image.RGBA {
	t.Helper()
	box := image.Pt(int(float32(size.X)*scale), int(float32(size.Y)*scale))
	canvas := image.Pt(box.X+2*renderMargin, box.Y+2*renderMargin)
	win := renderer(t, canvas)
	inner := image.Rect(renderMargin, renderMargin, renderMargin+box.X, renderMargin+box.Y)

	var (
		ops op.Ops
		r   input.Router
	)
	// Three frames: the first applies focus/commands and revalidates the path, the
	// rest settle everything that depends on the previous frame.
	for i := 0; i < 3; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(canvas),
			Metric:      unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Now:         time.Now(),
			Source:      r.Source(),
		}
		Fill(gtx, canvasBG)
		st := op.Offset(inner.Min).Push(&ops)
		in := gtx
		in.Constraints = layout.Constraints{Max: box}
		if d := a.Layout(in); d.Size != box {
			t.Errorf("Layout size = %v, want %v", d.Size, box)
		}
		st.Pop()
		r.Frame(&ops)
	}
	full := shot(t, win, &ops)

	bg := rgba(canvasBG)
	bad, first := 0, image.Point{}
	for y := 0; y < canvas.Y; y++ {
		for x := 0; x < canvas.X; x++ {
			if image.Pt(x, y).In(inner) {
				continue
			}
			if full.RGBAAt(x, y) != bg {
				if bad == 0 {
					first = image.Pt(x, y)
				}
				bad++
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d pixels painted outside the %v window box (first at %v: %v)", bad, inner, first, full.RGBAAt(first.X, first.Y))
	}
	// And the app must cover its whole box (it starts with a PANEL fill).
	for _, p := range []image.Point{inner.Min, inner.Max.Sub(image.Pt(1, 1))} {
		if full.RGBAAt(p.X, p.Y) == bg {
			t.Errorf("box corner %v is still canvas background: the app did not fill its window", p)
		}
	}

	out := image.NewRGBA(image.Rectangle{Max: box})
	draw.Draw(out, out.Bounds(), full, inner.Min, draw.Src)
	return out
}

// writePNG stores img as <dir>/<name>.png when RENDER_OUT is set.
func writePNG(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := os.Getenv("RENDER_OUT")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sizeName(prefix string, size image.Point, scale float32) string {
	n := fmt.Sprintf("%s-%dx%d", prefix, size.X, size.Y)
	if scale != 1 {
		n += fmt.Sprintf("@%gx", scale)
	}
	return n
}

// TestRender renders every state at 520x760 and 960x600, the breakpoint pair and
// one 2x render, and checks each against the pane-overflow rule.
func TestRender(t *testing.T) {
	isolateEnv(t)
	env := newAppEnv(t)

	type rcase struct {
		state renderState
		size  image.Point
		scale float32
		wide  bool
	}
	narrow, wide := image.Pt(520, 760), image.Pt(960, 600)
	var cases []rcase
	for _, st := range renderStates {
		cases = append(cases, rcase{st, narrow, 1, false}, rcase{st, wide, 1, true})
	}
	def := renderStates[1]
	cases = append(cases,
		rcase{def, image.Pt(760, 760), 1, true},  // the breakpoint: two columns
		rcase{def, image.Pt(759, 760), 1, false}, // one dp below: one column
		rcase{def, narrow, 2, false},
		rcase{renderStates[2], narrow, 2, false}, // confirm at 2x
	)

	seen := map[string]string{} // image hash -> case name, to catch states that render alike
	for _, c := range cases {
		name := sizeName(c.state.name, c.size, c.scale)
		t.Run(name, func(t *testing.T) {
			a := newRenderApp(t, env)
			c.state.setup(a)
			img := renderApp(t, a, c.size, c.scale)
			c.state.check(t, a)
			if a.wide != c.wide {
				t.Errorf("wide layout = %v, want %v", a.wide, c.wide)
			}
			writePNG(t, name, img)

			sum := fmt.Sprintf("%x", sha256.Sum256(img.Pix))
			if prev, dup := seen[sum]; dup {
				t.Errorf("renders identical to %s: the state did not show", prev)
			}
			seen[sum] = name
		})
	}
}

// TestRenderHostile renders very long names and a very long path at 520 wide (and
// at 960, two columns), where ellipsis and clipping have to hold.
func TestRenderHostile(t *testing.T) {
	isolateEnv(t)
	env := newHostileEnv(t)
	for _, size := range []image.Point{{520, 760}, {960, 600}, {360, 640}} {
		for _, st := range renderStates {
			switch st.name {
			case "default", "confirm", "copying", "result-error", "dropdown-open", "steam-running":
			default:
				continue
			}
			name := sizeName("hostile-"+st.name, size, 1)
			t.Run(name, func(t *testing.T) {
				a := newRenderApp(t, env)
				// Same flow as chooseBob, on the hostile accounts: Bob is the 2nd account.
				a.dst.Selected = 2
				a.onDest()
				for _, r := range a.rows {
					r.check.Value = r.id != "999001"
				}
				switch st.name {
				case "confirm":
					a.modal = modalConfirm
				case "steam-running":
					a.modal = modalSteam
				case "copying":
					a.copying, a.progress = true, 0.5
					a.status = "Copying " + strings.Repeat("An absurdly long game title ", 4) + " (2/4)…"
				case "result-error":
					a.setMsg("2 of 4 games failed. 620: "+strings.Repeat("permission denied on a very long path ", 6), true)
				case "dropdown-open":
					a.dst.open = true
				}
				img := renderApp(t, a, size, 1)
				writePNG(t, name, img)
			})
		}
	}
}
