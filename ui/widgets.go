package ui

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

// Event-order rule for every widget that wraps a widget.Clickable, widget.Bool
// or widget.Editor: their Layout drains pending events, so poll the state
// (Clicked / Update / Submitted) BEFORE laying the widget out in the same frame.

// ---- Buttons ----------------------------------------------------------------

type buttonKind uint8

const (
	kindPrimary buttonKind = iota
	kindSecondary
	kindDanger
)

// PrimaryButton is a solid accent button with dark bold text. A disabled button
// is drawn at 40 % opacity and ignores input. Poll c.Clicked(gtx) before calling.
func PrimaryButton(gtx layout.Context, th *Theme, c *widget.Clickable, label string, enabled bool) layout.Dimensions {
	return button(gtx, th, kindPrimary, c, label, enabled)
}

// SecondaryButton is the neutral BTN/BTNBD button.
func SecondaryButton(gtx layout.Context, th *Theme, c *widget.Clickable, label string, enabled bool) layout.Dimensions {
	return button(gtx, th, kindSecondary, c, label, enabled)
}

// DangerButton is transparent with red bold text and a faint red border.
func DangerButton(gtx layout.Context, th *Theme, c *widget.Clickable, label string, enabled bool) layout.Dimensions {
	return button(gtx, th, kindDanger, c, label, enabled)
}

func button(gtx layout.Context, th *Theme, kind buttonKind, c *widget.Clickable, label string, enabled bool) layout.Dimensions {
	if !enabled {
		gtx = gtx.Disabled()
	}
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hov, prs := enabled && c.Hovered(), enabled && c.Pressed()
		var fill, border, txt color.NRGBA
		face := Regular
		switch kind {
		case kindPrimary:
			fill, border, txt, face = ACCENT, ACCENT, PRIMARY_TEXT, Bold
		case kindSecondary:
			fill, border, txt = BTN, BTNBD, TEXT
		case kindDanger:
			border, txt, face = color.NRGBA{DANGER.R, DANGER.G, DANGER.B, 0x73}, DANGER, Bold
		}
		if hov {
			border = ACCENT
		}
		if prs {
			border = SELBD
		}
		if !enabled {
			if kind == kindPrimary {
				txt = TEXT // dark text on the dimmed accent fill is unreadable; light text stays legible
			}
			fill, border, txt = fade(fill, .4), fade(border, .4), fade(txt, .4)
		}
		lab, ld := measure(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: 9, Right: 9, Top: 5, Bottom: 5}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return th.Line(gtx, face, SizeBody, txt, label)
			})
		})
		size := gtx.Constraints.Constrain(image.Pt(ld.Size.X, max(ld.Size.Y, gtx.Dp(ControlH))))
		rect := image.Rectangle{Max: size}
		rad := gtx.Dp(Radius)
		defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
		if enabled {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		fillRect(gtx, rect, fill)
		if hov || prs {
			fillRect(gtx, rect, HOV)
		}
		strokeRRect(gtx, rect, rad, 1, border)
		defer op.Offset(image.Pt((size.X-ld.Size.X)/2, (size.Y-ld.Size.Y)/2)).Push(gtx.Ops).Pop()
		lab.Add(gtx.Ops)
		return layout.Dimensions{Size: size}
	})
}

// ---- Card -------------------------------------------------------------------

// Card is a bordered pane: it fills the constraint width, draws a 1 px BORDER
// outline (radius 7, transparent fill), an UPPERCASE bold ACCENT title, a 4 dp
// gap, then body inside a 6 dp margin. The body is laid out in, and clipped to,
// the card's inner width. If the incoming Min.Y is larger than the content (e.g.
// a Flexed slot) the card grows to it and body gets the remaining height, so a
// body that ends in a Flexed child (a list) fills the card.
func Card(gtx layout.Context, th *Theme, title string, body layout.Widget) layout.Dimensions {
	bw := max(1, gtx.Dp(1))
	pad := bw + gtx.Dp(CardPad)
	w := gtx.Constraints.Max.X
	inner := gtx
	inner.Constraints.Min = image.Pt(max(0, w-2*pad), max(0, gtx.Constraints.Min.Y-2*pad))
	inner.Constraints.Max = image.Pt(inner.Constraints.Min.X, max(0, gtx.Constraints.Max.Y-2*pad))
	content, cd := measure(inner, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.Line(gtx, Bold, SizeCard, ACCENT, upper(title))
			}),
			layout.Rigid(layout.Spacer{Height: RowGap}.Layout),
			layout.Rigid(body),
		)
	})
	size := image.Pt(w, cd.Size.Y+2*pad)
	size = gtx.Constraints.Constrain(size)
	strokeRRect(gtx, image.Rectangle{Max: size}, gtx.Dp(Radius), float32(bw), BORDER)
	st := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	cl := clip.Rect(image.Rectangle{Max: image.Pt(size.X-2*pad, size.Y-2*pad)}).Push(gtx.Ops)
	content.Add(gtx.Ops)
	cl.Pop()
	st.Pop()
	return layout.Dimensions{Size: size}
}

// ---- Check rows -------------------------------------------------------------

// CheckItem is the content of one check row.
type CheckItem struct {
	Name    string // game name, proportional
	ID      string // dim id, shown mono and right-aligned in an 80 dp column
	Warn    string // optional WARN mark text (e.g. "overwrites"); empty = none
	Pending bool   // name still resolving: drawn DIM instead of TEXT
}

// CheckRow is a checkbox row whose whole width is the click target: an 18 dp box
// (ACCENT with a white check when on, steel outline when off), the name, an
// optional WARN mark and the dim mono id. A checked row has SEL fill + SELBD
// border; hover adds the HOV wash. b.Layout toggles b.Value itself; to learn
// about a change call b.Update(gtx) before CheckRow.
func CheckRow(gtx layout.Context, th *Theme, b *widget.Bool, it CheckItem) layout.Dimensions {
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bw := max(1, gtx.Dp(1))
		w := gtx.Constraints.Max.X
		padX, padY := bw+gtx.Dp(8), bw+gtx.Dp(4)
		in := gtx
		in.Constraints.Min = image.Pt(max(0, w-2*padX), 0)
		in.Constraints.Max = image.Pt(in.Constraints.Min.X, gtx.Constraints.Max.Y)
		nameCol := TEXT
		if it.Pending {
			nameCol = DIM
		}
		content, cd := measure(in, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return checkBox(gtx, th, b.Value) }),
				layout.Rigid(layout.Spacer{Width: Gap}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return th.Line(gtx, Regular, SizeBody, nameCol, it.Name)
				}),
			}
			if it.Warn != "" {
				children = append(children,
					layout.Rigid(layout.Spacer{Width: Gap}.Layout), // keeps an ellipsised name off the badge
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return th.Icon(gtx, IconWarning, 12, WARN)
							}),
							layout.Rigid(layout.Spacer{Width: RowGap}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return th.Line(gtx, Regular, SizeSmall, WARN, it.Warn)
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Width: Gap}.Layout),
				)
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(80)
				gtx.Constraints.Max.X = gtx.Dp(80)
				return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return th.Line(gtx, Mono, SizeMono, DIM, it.ID)
				})
			}))
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
		})
		size := gtx.Constraints.Constrain(image.Pt(w, max(cd.Size.Y+2*padY, gtx.Dp(30))))
		rect := image.Rectangle{Max: size}
		rad := gtx.Dp(Radius)
		defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		if b.Value {
			fillRect(gtx, rect, SEL)
			strokeRRect(gtx, rect, rad, float32(bw), SELBD)
		}
		if b.Hovered() {
			fillRect(gtx, rect, HOV)
		}
		defer op.Offset(image.Pt(padX, (size.Y-cd.Size.Y)/2)).Push(gtx.Ops).Pop()
		content.Add(gtx.Ops)
		return layout.Dimensions{Size: size}
	})
}

// checkBox draws the 18 dp mark: ACCENT fill + white check when on, 1.5 px steel outline when off.
func checkBox(gtx layout.Context, th *Theme, on bool) layout.Dimensions {
	side := gtx.Dp(18)
	rect := image.Rect(0, 0, side, side)
	rad := gtx.Dp(RadiusCheck)
	if on {
		fillRRect(gtx, rect, rad, ACCENT)
		cx := image.Pt(side, side)
		gtx.Constraints = layout.Exact(cx)
		layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return th.Icon(gtx, IconCheck, 11, WHITE)
		})
	} else {
		strokeRRect(gtx, rect, rad, 1.5, CHECK_FRAME)
	}
	return layout.Dimensions{Size: image.Pt(side, side)}
}

// ---- Text field -------------------------------------------------------------

// fieldH is the single-line input height (dp), from the mockup.
const fieldH unit.Dp = 28

// TextField is a single-line input: FIELD fill, 1 px BORDER (ACCENT when focused),
// radius 7, 9 dp side padding, DIM placeholder while empty. mono selects Geist
// Mono 12 (paths, ids) instead of Geist 13. It fills the constraint width and
// forces e.SingleLine and e.Submit; use Submitted for Enter.
func TextField(gtx layout.Context, th *Theme, e *widget.Editor, hint string, mono bool) layout.Dimensions {
	e.SingleLine, e.Submit = true, true
	face, size := Regular, SizeBody
	if mono {
		face, size = Mono, SizeMono
	}
	bw := max(1, gtx.Dp(1))
	h := gtx.Dp(fieldH)
	w := gtx.Constraints.Max.X
	rect := image.Rect(0, 0, w, h)
	rad := gtx.Dp(Radius)
	padX := gtx.Dp(9)
	defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
	fillRect(gtx, rect, FIELD)
	bd := BORDER
	if gtx.Focused(e) {
		bd = ACCENT
	}
	strokeRRect(gtx, rect, rad, float32(bw), bd)

	// Natural-height editor, centred vertically. The same tag also gets a pointer
	// area over the whole box so a click in the padding focuses the field too.
	in := gtx
	in.Constraints.Min = image.Pt(w-2*padX, 0)
	in.Constraints.Max = image.Pt(w-2*padX, h)
	ed, ded := measure(in, func(gtx layout.Context) layout.Dimensions {
		return e.Layout(gtx, th.Shaper, th.font(face), size, colorOp(gtx, TEXT), colorOp(gtx, SELBD))
	})
	dy := (h - ded.Size.Y) / 2
	st := op.Offset(image.Pt(padX, dy)).Push(gtx.Ops)
	if e.Len() == 0 && hint != "" {
		th.Line(in, face, size, DIM, hint)
	}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: e, Kinds: pointer.Press}); !ok {
			break
		}
		gtx.Execute(key.FocusCmd{Tag: e})
	}
	area := clip.Rect(image.Rect(-padX, -dy, w-padX, h-dy)).Push(gtx.Ops)
	event.Op(gtx.Ops, e)
	pointer.CursorText.Add(gtx.Ops)
	area.Pop()
	ed.Add(gtx.Ops)
	st.Pop()
	return layout.Dimensions{Size: rect.Size()}
}

// Submitted reports whether Enter was pressed in e since the last call. Call it
// before TextField in the same frame. It also drains the editor's change events,
// so read e.Text() each frame instead of waiting for them.
func Submitted(gtx layout.Context, e *widget.Editor) bool {
	sub := false
	for {
		ev, ok := e.Update(gtx)
		if !ok {
			return sub
		}
		if _, is := ev.(widget.SubmitEvent); is {
			sub = true
		}
	}
}

// ---- Dropdown ---------------------------------------------------------------

// DropdownItem is one account entry: a name and a dim mono id shown as "name (id)".
type DropdownItem struct {
	Name, ID string
}

// Dropdown is a select control with an overlay list. Fill Items, read Selected
// (index, -1 = none) and Changed. It does not own the avatar; lay that out beside it.
//
// The popup is drawn with op.Defer, so it is neither clipped by the enclosing
// Card/pane nor pushed down in layout: it floats over everything laid out
// before it in the frame, below the control, as wide as the control, at most 6
// rows tall (scrolls beyond). A click outside, Esc, or picking a row closes it.
type Dropdown struct {
	Items    []DropdownItem
	Selected int
	// Placeholder marks Items[0] as a "none selected" entry: its text is drawn DIM,
	// in the list and in the closed control, and indexes keep counting from it.
	Placeholder bool

	open    bool
	changed bool
	head    widget.Clickable
	away    widget.Clickable
	rows    []widget.Clickable
	list    widget.List
}

// Changed reports (once) that the user picked a different item since the last call.
func (d *Dropdown) Changed() bool {
	c := d.changed
	d.changed = false
	return c
}

// Open reports whether the popup is showing.
func (d *Dropdown) Open() bool { return d.open }

// Close hides the popup.
func (d *Dropdown) Close() { d.open = false }

const dropdownMaxRows = 6

// Layout draws the closed control (fills the constraint width) and, when open, defers the popup.
func (d *Dropdown) Layout(gtx layout.Context, th *Theme) layout.Dimensions {
	if len(d.rows) != len(d.Items) { // fixed backing array: Clickable tags are their addresses
		d.rows = make([]widget.Clickable, len(d.Items))
	}
	if d.open {
		for {
			ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			if e, is := ev.(key.Event); is && e.State == key.Press {
				d.open = false
			}
		}
	}
	if d.away.Clicked(gtx) {
		d.open = false
	}
	if d.head.Clicked(gtx) {
		d.open = !d.open
	}
	for i := range d.Items {
		if d.rows[i].Clicked(gtx) {
			if d.Selected != i {
				d.Selected, d.changed = i, true
			}
			d.open = false
		}
	}

	w := gtx.Constraints.Max.X
	dims := d.layoutHead(gtx, th, w)
	if d.open {
		m := op.Record(gtx.Ops)
		d.layoutPopup(gtx, th, w, dims.Size.Y)
		op.Defer(gtx.Ops, m.Stop())
	}
	return dims
}

// accountLine lays out "name (id)" on one line: the name truncates, the id never does.
func accountLine(gtx layout.Context, th *Theme, it DropdownItem, nameCol color.NRGBA) layout.Dimensions {
	if it.ID == "" {
		return th.Line(gtx, Regular, SizeBody, nameCol, it.Name)
	}
	idm, idd := measure(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return th.Line(gtx, Mono, SizeMono, DIM, "("+it.ID+")")
	})
	gap := gtx.Dp(RowGap)
	nm := gtx
	nm.Constraints.Min.X = 0
	nm.Constraints.Max.X = max(0, gtx.Constraints.Max.X-idd.Size.X-gap)
	namem, nd := measure(nm, func(gtx layout.Context) layout.Dimensions {
		return th.Line(gtx, Regular, SizeBody, nameCol, it.Name)
	})
	h := max(nd.Size.Y, idd.Size.Y)
	st := op.Offset(image.Pt(0, (h-nd.Size.Y)/2)).Push(gtx.Ops)
	namem.Add(gtx.Ops)
	st.Pop()
	st = op.Offset(image.Pt(nd.Size.X+gap, (h-idd.Size.Y)/2)).Push(gtx.Ops)
	idm.Add(gtx.Ops)
	st.Pop()
	return layout.Dimensions{Size: image.Pt(nd.Size.X+gap+idd.Size.X, h)}
}

func (d *Dropdown) layoutHead(gtx layout.Context, th *Theme, w int) layout.Dimensions {
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	return d.head.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hov, prs := d.head.Hovered(), d.head.Pressed()
		fill, border := BTN, BTNBD
		if d.open {
			fill = FIELD
		}
		if hov {
			border = ACCENT
		}
		if prs {
			border = SELBD
		}
		padX, padY := gtx.Dp(9), gtx.Dp(5)
		chev := gtx.Dp(14)
		in := gtx
		in.Constraints.Min = image.Point{}
		in.Constraints.Max.X = max(0, w-2*padX-chev-gtx.Dp(Gap))
		var it DropdownItem
		if d.Selected >= 0 && d.Selected < len(d.Items) {
			it = d.Items[d.Selected]
		}
		col := TEXT
		if d.Placeholder && d.Selected == 0 {
			col = DIM
		}
		txt, td := measure(in, func(gtx layout.Context) layout.Dimensions { return accountLine(gtx, th, it, col) })
		h := max(td.Size.Y+2*padY, gtx.Dp(ControlH))
		size := image.Pt(w, h)
		rect := image.Rectangle{Max: size}
		rad := gtx.Dp(Radius)
		defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		fillRect(gtx, rect, fill)
		if hov {
			fillRect(gtx, rect, HOV)
		}
		strokeRRect(gtx, rect, rad, 1, border)
		st := op.Offset(image.Pt(padX, (h-td.Size.Y)/2)).Push(gtx.Ops)
		txt.Add(gtx.Ops)
		st.Pop()
		st = op.Offset(image.Pt(w-padX-chev, (h-chev)/2)).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(chev, chev))
		th.Icon(cg, IconChevron, 10, DIM)
		st.Pop()
		return layout.Dimensions{Size: size}
	})
}

// layoutPopup draws the click-away catcher and the list below the head. It runs
// inside a deferred macro, at the transform the control was laid out with.
func (d *Dropdown) layoutPopup(gtx layout.Context, th *Theme, w, headH int) {
	// Click-away catcher: a huge transparent area that swallows clicks outside the list.
	const big = 1 << 14
	cst := op.Offset(image.Pt(-big/2, -big/2)).Push(gtx.Ops)
	d.away.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(big, big)}
	})
	cst.Pop()

	bw := max(1, gtx.Dp(1))
	pad := gtx.Dp(RowGap)
	rowH := gtx.Dp(28)
	n := min(len(d.Items), dropdownMaxRows)
	h := n*rowH + 2*pad + 2*bw
	rect := image.Rect(0, 0, w, h)
	rad := gtx.Dp(Radius)
	st := op.Offset(image.Pt(0, headH+gtx.Dp(2))).Push(gtx.Ops)
	defer st.Pop()
	defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
	fillRect(gtx, rect, FIELD)
	strokeRRect(gtx, rect, rad, float32(bw), BORDER)
	in := gtx
	in.Constraints = layout.Exact(image.Pt(w-2*(pad+bw), h-2*(pad+bw)))
	ist := op.Offset(image.Pt(pad+bw, pad+bw)).Push(gtx.Ops)
	defer ist.Pop()
	d.list.Axis = layout.Vertical
	th.list(&d.list).Layout(in, len(d.Items), func(gtx layout.Context, i int) layout.Dimensions {
		return d.layoutRow(gtx, th, i, rowH)
	})
}

func (d *Dropdown) layoutRow(gtx layout.Context, th *Theme, i, rowH int) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return d.rows[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Constraints.Max.X, rowH)
		rect := image.Rectangle{Max: size}
		defer clip.UniformRRect(rect, gtx.Dp(5)).Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		if i == d.Selected {
			fillRect(gtx, rect, SEL)
		}
		if d.rows[i].Hovered() {
			fillRect(gtx, rect, HOV)
		}
		padX := gtx.Dp(Gap)
		in := gtx
		in.Constraints.Min = image.Point{}
		in.Constraints.Max.X = max(0, size.X-2*padX)
		col := TEXT
		if d.Placeholder && i == 0 {
			col = DIM
		}
		m, md := measure(in, func(gtx layout.Context) layout.Dimensions { return accountLine(gtx, th, d.Items[i], col) })
		st := op.Offset(image.Pt(padX, (rowH-md.Size.Y)/2)).Push(gtx.Ops)
		m.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: size}
	})
}

// ---- List -------------------------------------------------------------------

// ListBox is a vertical scrolling list inside a FIELD well (1 px BORDER, radius
// 7, 4 dp padding, 4 dp between rows) with a thin scrollbar. It fills the
// constraints (use a Flexed slot to give it the card's remaining height). When
// n == 0 it shows empty in DIM. row lays out item i at the list width.
func ListBox(gtx layout.Context, th *Theme, state *widget.List, n int, empty string, row func(gtx layout.Context, i int) layout.Dimensions) layout.Dimensions {
	bw := max(1, gtx.Dp(1))
	size := gtx.Constraints.Max
	if size.Y >= 1e6 { // unbounded height: do not claim infinity
		size.Y = gtx.Dp(200)
	}
	rect := image.Rectangle{Max: size}
	rad := gtx.Dp(Radius)
	defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
	fillRect(gtx, rect, FIELD)
	strokeRRect(gtx, rect, rad, float32(bw), BORDER)
	pad := bw + gtx.Dp(RowGap)
	in := gtx
	in.Constraints = layout.Exact(image.Pt(max(0, size.X-2*pad), max(0, size.Y-2*pad)))
	defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
	if n == 0 {
		layout.UniformInset(Gap).Layout(in, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, Regular, SizeBody, DIM, empty)
		})
		return layout.Dimensions{Size: size}
	}
	state.Axis = layout.Vertical
	state.Gap = gtx.Dp(RowGap)
	th.list(state).Layout(in, n, row)
	return layout.Dimensions{Size: size}
}

// ---- Progress, avatar, chrome ----------------------------------------------

// ProgressBar draws a 6 dp TRACK bar with an ACCENT fill of frac (0..1) of the constraint width.
func ProgressBar(gtx layout.Context, th *Theme, frac float32) layout.Dimensions {
	frac = max(0, min(1, frac))
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(6))
	rad := size.Y / 2
	fillRRect(gtx, image.Rectangle{Max: size}, rad, TRACK)
	if fw := int(float32(size.X) * frac); fw > 0 {
		r := image.Rect(0, 0, fw, size.Y)
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		fillRRect(gtx, r, min(rad, fw/2), ACCENT)
	}
	return layout.Dimensions{Size: size}
}

// imgCache keeps one paint.ImageOp per source image so the texture is not re-uploaded each frame.
type imgCache struct {
	src image.Image
	op  paint.ImageOp
}

func (c *imgCache) get(img image.Image) paint.ImageOp {
	if img != c.src {
		c.src, c.op = img, paint.NewImageOp(img)
	}
	return c.op
}

// Avatar shows a decoded account image scaled to cover a rounded square, or a
// letter tile (WELL fill, BORDER, DIM bold letter) while img is nil.
type Avatar struct {
	cache imgCache
}

// Layout draws the avatar as a size x size square with radius 7. img may be nil
// (loading or failed); letter is the placeholder glyph (first letter of the name).
func (a *Avatar) Layout(gtx layout.Context, th *Theme, img image.Image, letter string, size unit.Dp) layout.Dimensions {
	px := gtx.Dp(size)
	sq := image.Pt(px, px)
	rect := image.Rectangle{Max: sq}
	rad := gtx.Dp(Radius)
	defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
	fillRect(gtx, rect, WELL)
	if img != nil {
		g := gtx
		g.Constraints = layout.Exact(sq)
		widget.Image{Src: a.cache.get(img), Fit: widget.Cover, Position: layout.Center}.Layout(g)
	} else {
		g := gtx
		g.Constraints = layout.Exact(sq)
		layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
			return th.Line(gtx, Bold, SizeHeading, DIM, upper(letter))
		})
	}
	strokeRRect(gtx, rect, rad, 1, BORDER)
	return layout.Dimensions{Size: sq}
}

// TitleBar is the HEAD band at the top: optional logo, ACCENT bold wordmark, a
// 1x18 divider and the page pill (SEL fill, SELBD border, fully rounded).
type TitleBar struct {
	Logo     image.Image // optional 20 dp logo
	Wordmark string
	Pill     string // optional

	cache imgCache
}

// Layout fills the constraint width, 40 dp tall (including the 1 px BORDER line at the bottom).
func (t *TitleBar) Layout(gtx layout.Context, th *Theme) layout.Dimensions {
	h := gtx.Dp(40)
	w := gtx.Constraints.Max.X
	rect := image.Rect(0, 0, w, h)
	fillRect(gtx, rect, HEAD)
	fillRect(gtx, image.Rect(0, h-max(1, gtx.Dp(1)), w, h), BORDER)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: Gap, Right: Gap, Top: 4, Bottom: 5}.Layout(g, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if t.Logo == nil {
					return layout.Dimensions{}
				}
				s := gtx.Dp(20)
				gtx.Constraints = layout.Exact(image.Pt(s, s))
				d := widget.Image{Src: t.cache.get(t.Logo), Fit: widget.Contain, Position: layout.Center}.Layout(gtx)
				return layout.Dimensions{Size: image.Pt(d.Size.X+gtx.Dp(Gap), d.Size.Y)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.Line(gtx, Bold, SizeWord, ACCENT, t.Wordmark)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if t.Pill == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Left: Gap + 2, Right: Gap + 2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					s := image.Pt(1, gtx.Dp(18))
					fillRect(gtx, image.Rectangle{Max: s}, BORDER)
					return layout.Dimensions{Size: s}
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if t.Pill == "" {
					return layout.Dimensions{}
				}
				return pill(gtx, th, t.Pill)
			}),
		)
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// pill draws text in a fully rounded SEL/SELBD capsule.
func pill(gtx layout.Context, th *Theme, s string) layout.Dimensions {
	m, d := measure(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 10, Right: 10, Top: 3, Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return th.Line(gtx, Regular, SizePill, TEXT, s)
		})
	})
	rect := image.Rectangle{Max: d.Size}
	fillRRect(gtx, rect, d.Size.Y/2, SEL)
	strokeRRect(gtx, rect, d.Size.Y/2, 1, SELBD)
	m.Add(gtx.Ops)
	return d
}

// StatusBar is the PANEL2 footer: 1 px BORDER line on top, 8 dp padding, at
// least 44 dp tall. content is laid out inside the padding at the full inner width.
func StatusBar(gtx layout.Context, th *Theme, content layout.Widget) layout.Dimensions {
	bw := max(1, gtx.Dp(1))
	w := gtx.Constraints.Max.X
	pad := gtx.Dp(Gap)
	in := gtx
	in.Constraints.Min = image.Pt(max(0, w-2*pad), 0)
	in.Constraints.Max = image.Pt(in.Constraints.Min.X, gtx.Constraints.Max.Y)
	m, d := measure(in, content)
	h := max(d.Size.Y+2*pad+bw, gtx.Dp(44))
	fillRect(gtx, image.Rect(0, 0, w, h), PANEL2)
	fillRect(gtx, image.Rect(0, 0, w, bw), BORDER)
	defer op.Offset(image.Pt(pad, bw+(h-bw-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	m.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// Fill paints the whole constraint area with c (use PANEL for the window background).
func Fill(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	fillRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, c)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// ---- Modal ------------------------------------------------------------------

// scrim is the modal backdrop. SCRIM (55 % black) is the mockup's CSS value, but
// Gio blends in linear light, where 55 % black only dims white to about 69 % of
// its sRGB brightness. 0xD4 (83 %) lands on the mockup's 45 % remaining.
var scrim = color.NRGBA{0, 0, 0, 0xD4}

// Modal is a dialog over a dimmed backdrop. Set the exported fields each frame,
// call Update, then (last in the frame) Layout.
//
// Enter confirms, Esc and a backdrop click cancel (a focused TextField takes
// Enter first as its own submit: treat Submitted(...) as confirm). The backdrop swallows all
// pointer input. Layout draws through op.Defer, so it is always on top of what
// was laid out before it and is not clipped by any pane.
type Modal struct {
	Title        string
	ConfirmLabel string
	CancelLabel  string // empty hides the Cancel button
	Danger       bool   // confirm button uses the danger style instead of primary
	// ConfirmDisabled greys out the confirm button and ignores Enter.
	ConfirmDisabled bool
	// Width in dp; 0 means 400. Clamped to the window minus a 16 dp margin.
	Width unit.Dp

	confirm  widget.Clickable
	cancel   widget.Clickable
	backdrop widget.Clickable
	sink     widget.Clickable
}

// Update polls input. Call once per frame, before Layout.
func (m *Modal) Update(gtx layout.Context) (confirmed, cancelled bool) {
	if m.confirm.Clicked(gtx) && !m.ConfirmDisabled {
		confirmed = true
	}
	if m.cancel.Clicked(gtx) || m.backdrop.Clicked(gtx) {
		cancelled = true
	}
	m.sink.Clicked(gtx) // drained so presses on the panel never reach the backdrop
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameReturn},
			key.Filter{Name: key.NameEnter},
			key.Filter{Name: key.NameEscape},
		)
		if !ok {
			break
		}
		e, is := ev.(key.Event)
		if !is || e.State != key.Press {
			continue
		}
		switch e.Name {
		case key.NameReturn, key.NameEnter:
			if !m.ConfirmDisabled {
				confirmed = true
			}
		case key.NameEscape:
			cancelled = true
		}
	}
	return
}

// Layout draws the backdrop and the centred panel with title, body and the button row.
// Call it last, at window level, with the window-sized constraints.
func (m *Modal) Layout(gtx layout.Context, th *Theme, body layout.Widget) layout.Dimensions {
	win := gtx.Constraints.Max
	rec := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(win)
	m.backdrop.Layout(g, func(gtx layout.Context) layout.Dimensions {
		fillRect(gtx, image.Rectangle{Max: win}, scrim)
		return layout.Dimensions{Size: win}
	})
	layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions { return m.panel(gtx, th, body) })
	op.Defer(gtx.Ops, rec.Stop())
	return layout.Dimensions{Size: win}
}

func (m *Modal) panel(gtx layout.Context, th *Theme, body layout.Widget) layout.Dimensions {
	wdp := m.Width
	if wdp == 0 {
		wdp = 400
	}
	w := min(gtx.Dp(wdp), gtx.Constraints.Max.X-2*gtx.Dp(16))
	bw := max(1, gtx.Dp(1))
	pad := bw + gtx.Dp(16)
	in := gtx
	in.Constraints = layout.Constraints{Min: image.Pt(w-2*pad, 0), Max: image.Pt(w-2*pad, max(0, gtx.Constraints.Max.Y-2*pad))}
	content, cd := measure(in, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, Bold, SizeHeading, TEXT, m.Title)
			}),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(body),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Spacing: layout.SpaceStart, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if m.CancelLabel == "" {
							return layout.Dimensions{}
						}
						return SecondaryButton(gtx, th, &m.cancel, m.CancelLabel, true)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if m.CancelLabel == "" {
							return layout.Dimensions{}
						}
						return layout.Spacer{Width: Gap}.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if m.Danger {
							return DangerButton(gtx, th, &m.confirm, m.ConfirmLabel, !m.ConfirmDisabled)
						}
						return PrimaryButton(gtx, th, &m.confirm, m.ConfirmLabel, !m.ConfirmDisabled)
					}),
				)
			}),
		)
	})
	size := image.Pt(w, cd.Size.Y+2*pad)
	return m.sink.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rect := image.Rectangle{Max: size}
		rad := gtx.Dp(Radius)
		defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
		fillRect(gtx, rect, PANEL)
		strokeRRect(gtx, rect, rad, float32(bw), BORDER)
		defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
		content.Add(gtx.Ops)
		return layout.Dimensions{Size: size}
	})
}
