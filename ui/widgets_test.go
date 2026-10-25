package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

// newCtx returns a layout context for a size x size canvas at 1 px per dp/sp
// with a live input router, so Update/Clicked/Focused work as in a window.
func newCtx(ops *op.Ops, r *input.Router, size image.Point) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(size),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
		Source:      r.Source(),
	}
}

// renderer renders op lists headlessly. A skipped test means no GPU is available.
func renderer(t *testing.T, size image.Point) *headless.Window {
	t.Helper()
	w, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless GPU unavailable: %v", err)
	}
	t.Cleanup(w.Release)
	return w
}

func shot(t *testing.T, w *headless.Window, ops *op.Ops) *image.RGBA {
	t.Helper()
	if err := w.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: w.Size()})
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

// save writes img to $HEADLESS_OUT/name.png when that variable is set.
func save(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := os.Getenv("HEADLESS_OUT")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func gradient(n int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / n), uint8(y * 255 / n), 200, 255})
		}
	}
	return img
}

func rgba(c color.NRGBA) color.RGBA { return color.RGBA{c.R, c.G, c.B, c.A} }

// TestPaintsInsideConstraints lays each widget out in a fixed box inside a
// bigger canvas and requires every pixel outside the box to stay background.
func TestPaintsInsideConstraints(t *testing.T) {
	const margin = 30
	th := NewTheme()
	longName := strings.Repeat("A very long game title ", 6)

	var (
		clk       [3]widget.Clickable
		chk       = widget.Bool{Value: true}
		chk2      widget.Bool
		ed        widget.Editor
		ed2       widget.Editor
		dd        = Dropdown{Items: []DropdownItem{{longName, "48211937"}, {"alt", "1"}}}
		list      widget.List
		avImg     Avatar
		avLetter  Avatar
		tb        = TitleBar{Wordmark: "SteamAlt", Pill: "Userdata copier", Logo: gradient(32)}
		cardState widget.List
	)
	ed.SetText(longName)
	ed2.SetText("")

	cases := []struct {
		name string
		size image.Point // constraint box
		w    layout.Widget
	}{
		{"button-primary-narrow", image.Pt(60, 40), func(gtx layout.Context) layout.Dimensions {
			return PrimaryButton(gtx, th, &clk[0], "Copy 12 games now", true)
		}},
		{"button-secondary", image.Pt(200, 40), func(gtx layout.Context) layout.Dimensions {
			return SecondaryButton(gtx, th, &clk[1], "Change", true)
		}},
		{"button-danger-disabled", image.Pt(90, 40), func(gtx layout.Context) layout.Dimensions {
			return DangerButton(gtx, th, &clk[2], "Overwrite & copy", false)
		}},
		{"card", image.Pt(260, 140), func(gtx layout.Context) layout.Dimensions {
			return Card(gtx, th, "Destination", func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, Regular, SizeBody, TEXT, longName)
			})
		}},
		{"check-row-long", image.Pt(220, 60), func(gtx layout.Context) layout.Dimensions {
			return CheckRow(gtx, th, &chk, CheckItem{Name: longName, ID: "2938475610", Warn: "overwrites"})
		}},
		{"check-row-narrow-off", image.Pt(90, 60), func(gtx layout.Context) layout.Dimensions {
			return CheckRow(gtx, th, &chk2, CheckItem{Name: "Portal 2", ID: "620", Warn: "overwrites"})
		}},
		{"text-field-long", image.Pt(150, 50), func(gtx layout.Context) layout.Dimensions {
			return TextField(gtx, th, &ed, "Filter", true)
		}},
		{"text-field-placeholder", image.Pt(150, 50), func(gtx layout.Context) layout.Dimensions {
			return TextField(gtx, th, &ed2, "A placeholder that is far too long to fit in here", false)
		}},
		{"dropdown-closed", image.Pt(160, 60), func(gtx layout.Context) layout.Dimensions {
			return dd.Layout(gtx, th)
		}},
		{"progress", image.Pt(200, 30), func(gtx layout.Context) layout.Dimensions {
			return ProgressBar(gtx, th, 0.42)
		}},
		{"avatar-image", image.Pt(80, 80), func(gtx layout.Context) layout.Dimensions {
			return avImg.Layout(gtx, th, gradient(100), "M", 64)
		}},
		{"avatar-letter", image.Pt(80, 80), func(gtx layout.Context) layout.Dimensions {
			return avLetter.Layout(gtx, th, nil, "m", 64)
		}},
		{"listbox", image.Pt(200, 110), func(gtx layout.Context) layout.Dimensions {
			return ListBox(gtx, th, &list, 30, "empty", func(gtx layout.Context, i int) layout.Dimensions {
				return th.Line(gtx, Regular, SizeBody, TEXT, fmt.Sprintf("Row number %d of a long list", i))
			})
		}},
		{"listbox-empty", image.Pt(200, 60), func(gtx layout.Context) layout.Dimensions {
			return ListBox(gtx, th, &cardState, 0, "No games match", nil)
		}},
		{"titlebar", image.Pt(300, 60), func(gtx layout.Context) layout.Dimensions {
			return tb.Layout(gtx, th)
		}},
		{"statusbar", image.Pt(300, 90), func(gtx layout.Context) layout.Dimensions {
			return StatusBar(gtx, th, func(gtx layout.Context) layout.Dimensions {
				return th.Line(gtx, Mono, SizeMono, DIM, "~/.local/share/Steam/userdata/a/very/long/path")
			})
		}},
		{"icon", image.Pt(24, 24), func(gtx layout.Context) layout.Dimensions {
			return th.Icon(gtx, IconWarning, 16, WARN)
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			canvas := image.Pt(c.size.X+2*margin, c.size.Y+2*margin)
			win := renderer(t, canvas)
			var (
				ops op.Ops
				r   input.Router
			)
			box := image.Rect(margin, margin, margin+c.size.X, margin+c.size.Y)
			gtx := newCtx(&ops, &r, canvas)
			Fill(gtx, PANEL)
			st := op.Offset(box.Min).Push(&ops)
			in := gtx
			in.Constraints = layout.Constraints{Max: c.size}
			c.w(in)
			st.Pop()
			img := shot(t, win, &ops)
			save(t, "smoke-"+c.name, img)

			bg := rgba(PANEL)
			bad := 0
			var first image.Point
			for y := 0; y < canvas.Y; y++ {
				for x := 0; x < canvas.X; x++ {
					if image.Pt(x, y).In(box) {
						continue
					}
					if img.RGBAAt(x, y) != bg {
						if bad == 0 {
							first = image.Pt(x, y)
						}
						bad++
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d pixels painted outside box %v (first at %v: %v, want %v)", bad, box, first, img.RGBAAt(first.X, first.Y), bg)
			}
			inside := 0
			for y := box.Min.Y; y < box.Max.Y; y++ {
				for x := box.Min.X; x < box.Max.X; x++ {
					if img.RGBAAt(x, y) != bg {
						inside++
					}
				}
			}
			if inside == 0 {
				t.Errorf("nothing painted inside the box")
			}
		})
	}
}

// frame runs one layout pass and returns the context's ops for rendering.
type scene struct {
	th    *Theme
	ops   op.Ops
	r     input.Router
	size  image.Point
	build func(gtx layout.Context)
}

func (s *scene) frame() *op.Ops {
	s.ops.Reset()
	gtx := newCtx(&s.ops, &s.r, s.size)
	s.build(gtx)
	s.r.Frame(&s.ops)
	return &s.ops
}

// TestComposite renders the kit the way the app will use it: a 520 px wide page
// with cards, dropdown (open), check rows, buttons, field, progress, and a modal.
func TestComposite(t *testing.T) {
	size := image.Pt(520, 760)
	th := NewTheme()
	win := renderer(t, size)

	var (
		bSave, bCancel, bDanger, bDis, bAll, bNone, bChange widget.Clickable
		filter, pathEd                                      widget.Editor
		games                                               [7]widget.Bool
		list                                                widget.List
		src                                                 = Dropdown{Items: []DropdownItem{{"main", "48211937"}, {"alt", "90533104"}, {"friends-and-family-account", "1000000001"}}}
		dst                                                 = Dropdown{Items: src.Items, Selected: 1}
		avS, avD                                            Avatar
		tb                                                  = TitleBar{Wordmark: "SteamAlt", Pill: "Userdata copier", Logo: gradient(32)}
		modal                                               Modal
	)
	pathEd.SetText("~/Games/Steam")
	items := []CheckItem{
		{"Portal 2", "620", "overwrites", false},
		{"Hollow Knight", "367520", "", false},
		{"Stardew Valley", "413150", "overwrites", false},
		{"Celeste", "504230", "", false},
		{"Proton Experimental", "1493710", "", false},
		{"App 2938475610", "2938475610", "", false},
		{"resolving…", "1145360", "", true},
	}
	games[0].Value, games[1].Value, games[2].Value = true, true, true

	// 1) The components stacked, as in the brief.
	stack := &scene{th: th, size: size}
	stack.build = func(gtx layout.Context) {
		Fill(gtx, PANEL)
		layout.UniformInset(Gap).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceEnd}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return Card(gtx, th, "Source", func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return avS.Layout(gtx, th, gradient(120), "M", 64)
							}),
							layout.Rigid(layout.Spacer{Width: Gap}.Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return src.Layout(gtx, th) }),
						)
					})
				}),
				layout.Rigid(layout.Spacer{Height: Gap}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return Card(gtx, th, "Games", func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return TextField(gtx, th, &filter, "Filter", false) }),
									layout.Rigid(layout.Spacer{Width: Gap}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return SecondaryButton(gtx, th, &bAll, "All", true)
									}),
									layout.Rigid(layout.Spacer{Width: Gap}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return SecondaryButton(gtx, th, &bNone, "None", true)
									}),
								)
							}),
							layout.Rigid(layout.Spacer{Height: RowGap}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Max.Y = gtx.Dp(180)
								return ListBox(gtx, th, &list, len(items), "No games match", func(gtx layout.Context, i int) layout.Dimensions {
									return CheckRow(gtx, th, &games[i], items[i])
								})
							}),
							layout.Rigid(layout.Spacer{Height: Gap}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return ProgressBar(gtx, th, 0.6)
							}),
							layout.Rigid(layout.Spacer{Height: Gap}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return TextField(gtx, th, &pathEd, "", true)
							}),
						)
					})
				}),
				layout.Rigid(layout.Spacer{Height: Gap}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return PrimaryButton(gtx, th, &bSave, "Copy 3 games", true)
						}),
						layout.Rigid(layout.Spacer{Width: Gap}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SecondaryButton(gtx, th, &bCancel, "Cancel", true) }),
						layout.Rigid(layout.Spacer{Width: Gap}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return DangerButton(gtx, th, &bDanger, "Overwrite & copy", true)
						}),
						layout.Rigid(layout.Spacer{Width: Gap}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return PrimaryButton(gtx, th, &bDis, "Copy 0 games", false)
						}),
					)
				}),
			)
		})
	}
	// Open the source dropdown with a real click, then re-render: the popup must overlay the card below.
	stack.frame()
	clickAt(&stack.r, image.Pt(150, 66))
	img := shot(t, win, stack.frame())
	if !src.Open() {
		t.Fatalf("click on the dropdown head did not open it")
	}
	save(t, "composite", img)
	saveScratch(t, "composite", img)

	// Pick the third item with a click and check Selected / Changed.
	stack.frame()
	clickAt(&stack.r, image.Pt(150, 155))
	stack.frame()
	if src.Selected != 2 || !src.Changed() || src.Open() {
		t.Errorf("pick failed: selected=%d open=%v", src.Selected, src.Open())
	}
	save(t, "composite-picked", shot(t, win, stack.frame()))

	// 2) The app page from the mockup, with the confirm modal.
	page := &scene{th: th, size: size}
	showModal := false
	page.build = func(gtx layout.Context) {
		Fill(gtx, PANEL)
		layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return tb.Layout(gtx, th) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(Gap).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return Card(gtx, th, "Source", func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return avS.Layout(gtx, th, nil, "m", 64) }),
									layout.Rigid(layout.Spacer{Width: Gap}.Layout),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return src.Layout(gtx, th) }),
								)
							})
						}),
						layout.Rigid(layout.Spacer{Height: Gap}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return Card(gtx, th, "Destination", func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return avD.Layout(gtx, th, nil, "a", 64) }),
									layout.Rigid(layout.Spacer{Width: Gap}.Layout),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return dst.Layout(gtx, th) }),
								)
							})
						}),
						layout.Rigid(layout.Spacer{Height: Gap}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return Card(gtx, th, "Games", func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return TextField(gtx, th, &filter, "Filter", false) }),
											layout.Rigid(layout.Spacer{Width: Gap}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SecondaryButton(gtx, th, &bAll, "All", true) }),
											layout.Rigid(layout.Spacer{Width: Gap}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SecondaryButton(gtx, th, &bNone, "None", true) }),
										)
									}),
									layout.Rigid(layout.Spacer{Height: RowGap}.Layout),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return ListBox(gtx, th, &list, len(items), "No games match", func(gtx layout.Context, i int) layout.Dimensions {
											return CheckRow(gtx, th, &games[i], items[i])
										})
									}),
								)
							})
						}),
					)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return StatusBar(gtx, th, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.Line(gtx, Mono, SizeMono, DIM, "~/.local/share/Steam/userdata")
						}),
						layout.Rigid(layout.Spacer{Width: Gap}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SecondaryButton(gtx, th, &bChange, "Change", true) }),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return PrimaryButton(gtx, th, &bSave, "Copy 3 games", true)
						}),
					)
				})
			}),
		)
		if showModal {
			modal.Title = "Copy 3 games from main to alt?"
			modal.ConfirmLabel, modal.CancelLabel, modal.Danger = "Overwrite & copy", "Cancel", true
			modal.Update(gtx)
			modal.Layout(gtx, th, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, Regular, SizeBody, DIM, "Existing data for 2 of them in alt will be overwritten.")
			})
		}
	}
	src.Selected = 0
	save(t, "app", shot(t, win, page.frame()))
	saveScratch(t, "app", shot(t, win, page.frame()))
	showModal = true
	img = shot(t, win, page.frame())
	save(t, "app-modal", img)
	saveScratch(t, "app-modal", img)

	// The path modal: a text input, a live status line, Save disabled.
	showModal = false
	pm := &scene{th: th, size: size}
	var pmod Modal
	pm.build = func(gtx layout.Context) {
		Fill(gtx, PANEL)
		pmod.Title, pmod.ConfirmLabel, pmod.CancelLabel, pmod.ConfirmDisabled = "Steam folder", "Save", "Cancel", true
		pmod.Update(gtx)
		pmod.Layout(gtx, th, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TextField(gtx, th, &pathEd, "", true) }),
				layout.Rigid(layout.Spacer{Height: Gap}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, Regular, SizeBody, DANGER, "No userdata folder here")
				}),
			)
		})
	}
	img = shot(t, win, pm.frame())
	save(t, "path-modal", img)
	saveScratch(t, "path-modal", img)
}

// saveScratch also copies a PNG to $HEADLESS_OUT2 (the agent's scratch dir) when set.
func saveScratch(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := os.Getenv("HEADLESS_OUT2")
	if dir == "" {
		return
	}
	old := os.Getenv("HEADLESS_OUT")
	t.Setenv("HEADLESS_OUT", dir)
	save(t, name, img)
	t.Setenv("HEADLESS_OUT", old)
}

// clickAt injects a left click at p into the router (press and release).
func clickAt(r *input.Router, p image.Point) {
	pos := f32.Pt(float32(p.X), float32(p.Y))
	r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos},
	)
}

// TestModalKeys: Enter confirms, Esc cancels, a disabled confirm ignores Enter.
func TestModalKeys(t *testing.T) {
	th := NewTheme()
	size := image.Pt(400, 300)
	var m Modal
	s := &scene{th: th, size: size}
	var confirmed, cancelled bool
	s.build = func(gtx layout.Context) {
		m.Title, m.ConfirmLabel, m.CancelLabel = "T", "OK", "Cancel"
		c, x := m.Update(gtx)
		confirmed, cancelled = confirmed || c, cancelled || x
		m.Layout(gtx, th, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, Regular, SizeBody, DIM, "body") })
	}
	s.frame()
	s.r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	s.frame()
	s.frame()
	if !confirmed || cancelled {
		t.Fatalf("Enter: confirmed=%v cancelled=%v", confirmed, cancelled)
	}
	confirmed = false
	s.r.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	s.frame()
	s.frame()
	if confirmed || !cancelled {
		t.Fatalf("Esc: confirmed=%v cancelled=%v", confirmed, cancelled)
	}
	cancelled = false
	m.ConfirmDisabled = true
	s.build = func(gtx layout.Context) {
		m.Title, m.ConfirmLabel, m.CancelLabel, m.ConfirmDisabled = "T", "OK", "Cancel", true
		c, x := m.Update(gtx)
		confirmed, cancelled = confirmed || c, cancelled || x
		m.Layout(gtx, th, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} })
	}
	s.frame()
	s.r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	s.frame()
	s.frame()
	if confirmed {
		t.Fatalf("disabled confirm accepted Enter")
	}
}

// TestTextFieldFocus: a click in the field's padding (outside the text line) focuses it.
func TestTextFieldFocus(t *testing.T) {
	th := NewTheme()
	var ed widget.Editor
	focused := false
	s := &scene{th: th, size: image.Pt(300, 60)}
	s.build = func(gtx layout.Context) {
		focused = gtx.Focused(&ed)
		gtx.Constraints.Max = image.Pt(300, 60)
		TextField(gtx, th, &ed, "hint", false)
	}
	s.frame()
	clickAt(&s.r, image.Pt(150, 2))
	s.frame()
	s.frame()
	if !focused {
		t.Fatal("click in field padding did not focus the editor")
	}
}

// TestTitleBarCentred renders the TitleBar alone and requires the ink of the
// logo, wordmark, divider and pill to share the HEAD fill's vertical centre.
func TestTitleBarCentred(t *testing.T) {
	th := NewTheme()
	logo := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range logo.Pix {
		logo.Pix[i] = 0xFF // opaque white: its ink is the whole 20 dp box
	}
	for _, scale := range []float32{1, 1.25, 1.5, 2} {
		size := image.Pt(int(400*scale), int(40*scale))
		tb := TitleBar{Wordmark: "SteamAlt", Pill: "Userdata copier", Logo: logo}
		ops := new(op.Ops)
		gtx := newCtx(ops, new(input.Router), size)
		gtx.Metric = unit.Metric{PxPerDp: scale, PxPerSp: scale}
		tb.Layout(gtx, th)
		img := shot(t, renderer(t, size), ops)
		save(t, fmt.Sprintf("titlebar-%gx", scale), img)

		head := rgba(HEAD)
		bot := size.Y - 1 - max(1, gtx.Dp(1)) // last HEAD row
		ink := func(x, y int) bool {
			c := img.RGBAAt(x, y)
			d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
			return d(c.R, head.R)+d(c.G, head.G)+d(c.B, head.B) > 12
		}
		// Split the bar into elements by runs of inked columns.
		type seg struct{ x0, x1 int }
		var segs []seg
		for x := 0; x < size.X; x++ {
			hit := false
			for y := 0; y <= bot && !hit; y++ {
				hit = ink(x, y)
			}
			if !hit {
				continue
			}
			if n := len(segs); n > 0 && x-segs[n-1].x1 <= gtx.Dp(4) {
				segs[n-1].x1 = x
			} else {
				segs = append(segs, seg{x, x})
			}
		}
		if len(segs) != 4 {
			t.Fatalf("%gx: want 4 elements (logo, wordmark, divider, pill), got %d: %v", scale, len(segs), segs)
		}
		want := float32(bot) / 2
		for i, s := range segs {
			top, bottom := -1, -1
			for y := 0; y <= bot; y++ {
				for x := s.x0; x <= s.x1; x++ {
					if ink(x, y) {
						if top < 0 {
							top = y
						}
						bottom = y
						break
					}
				}
			}
			got := float32(top+bottom) / 2
			if d := got - want; d > scale || d < -scale {
				t.Errorf("%gx: element %d (x %d..%d) ink centre %.1f, bar centre %.1f", scale, i, s.x0, s.x1, got, want)
			}
		}
	}
}
