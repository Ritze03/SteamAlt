// Package ui is the SteamAlt Gio UI: the "Graphite" theme (theme.go: role
// tokens, fonts, text helpers) and the widget kit built on it (widgets.go).
// See superdoc/ui/STYLING-GUIDE.md.
//
// Layout rule (nested panes): every widget sizes itself from the constraints
// it is given and paints only inside them. Widgets that draw a box clip their
// content to it. The exceptions are Dropdown's popup and Modal, which draw
// through op.Defer on purpose.
//
// Event order: widget.Clickable, widget.Bool and widget.Editor drain their
// events inside Layout, so poll them (Clicked, Update, Submitted, Modal.Update,
// Dropdown.Changed) before laying the widget out in the same frame.
package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"

	"github.com/Ritze03/SteamAlt/assets"
)

// Role tokens. Call sites never use literal colours, only these.
var (
	ACCENT       = color.NRGBA{0x5B, 0x8B, 0xF0, 0xFF} // brand indigo: selection, primary button, card titles, checked boxes
	HEAD         = color.NRGBA{0x14, 0x17, 0x1A, 0xFF} // title bar
	PANEL        = color.NRGBA{0x1E, 0x21, 0x25, 0xFF} // main panel / window background
	PANEL2       = color.NRGBA{0x19, 0x1C, 0x1F, 0xFF} // footer / status band
	FIELD        = color.NRGBA{0x15, 0x18, 0x1B, 0xFF} // inputs, list wells, open dropdowns
	BORDER       = color.NRGBA{0x2C, 0x30, 0x36, 0xFF} // hairline borders and dividers
	BTN          = color.NRGBA{0x26, 0x2A, 0x30, 0xFF} // secondary button fill
	BTNBD        = color.NRGBA{0x34, 0x39, 0x41, 0xFF} // button border
	TEXT         = color.NRGBA{0xE7, 0xE9, 0xEC, 0xFF} // primary text
	DIM          = color.NRGBA{0x96, 0x9C, 0xA6, 0xFF} // secondary text, placeholders
	FAINT        = color.NRGBA{0x64, 0x6A, 0x73, 0xFF} // tertiary text
	PRIMARY_TEXT = color.NRGBA{0x0B, 0x12, 0x22, 0xFF} // text on an accent button
	DANGER       = color.NRGBA{0xE1, 0x55, 0x54, 0xFF} // delete, error
	GOOD         = color.NRGBA{0x6C, 0xC5, 0x51, 0xFF} // positive
	WARN         = color.NRGBA{0xD8, 0xB4, 0x55, 0xFF} // caution

	// Steel scale: blue-tinted neutrals.
	TEXT_DIM    = steel(150) // muted values, unchecked outline
	TEXT_FAINT  = steel(105) // hints, captions
	STROKE_MID  = steel(80)
	STROKE_DIM  = steel(50)
	TRACK       = steel(40) // track behind bars and sliders
	WELL        = steel(24) // recessed wells (avatar tile)
	CHECK_FRAME = steel(150)

	// WHITE is the check mark and radio dot on an accent fill.
	WHITE = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}

	// Tints: accent and white at fixed alpha.
	SEL   = color.NRGBA{0x5B, 0x8B, 0xF0, 0x29} // selection fill (~16 %)
	SELBD = color.NRGBA{0x5B, 0x8B, 0xF0, 0x6B} // selection border, pressed (~42 %)
	HOV   = color.NRGBA{0xFF, 0xFF, 0xFF, 0x0D} // hover wash (~5 %)

	// SCRIM dims the window behind a modal. Not part of the guide's palette; matches the mockup.
	SCRIM = color.NRGBA{0x00, 0x00, 0x00, 0x8C}
)

// steel returns the blue-tinted neutral for luminance lum: r = 0.9*lum, g = lum, b = min(255, 1.17*lum).
func steel(lum uint8) color.NRGBA {
	b := uint16(lum) + uint16(lum)/6
	if b > 255 {
		b = 255
	}
	return color.NRGBA{lum - lum/10, lum, uint8(b), 0xFF}
}

// Type sizes (sp), from the guide.
const (
	SizeHeading unit.Sp = 18   // modal titles
	SizeBody    unit.Sp = 13   // body text, buttons
	SizeSmall   unit.Sp = 11   // small marks
	SizeMono    unit.Sp = 12   // monospace slot: ids, paths
	SizeCard    unit.Sp = 12   // card title (uppercase bold)
	SizeWord    unit.Sp = 16   // wordmark
	SizePill    unit.Sp = 12.5 // page pill
)

// Metrics (dp), from the guide.
const (
	Radius      unit.Dp = 7  // widgets
	RadiusCheck unit.Dp = 5  // checkbox
	ControlH    unit.Dp = 22 // minimum control height
	Gap         unit.Dp = 8  // between cards, columns, header items
	RowGap      unit.Dp = 4  // between rows inside a card
	CardPad     unit.Dp = 6  // card inner margin
)

// Face selects one of the embedded faces.
type Face uint8

const (
	Regular Face = iota // Geist Regular, the default proportional face
	Medium              // Geist Medium
	Bold                // Geist Bold (Gio does not synthesise bold)
	Mono                // Geist Mono Regular: ids, paths, aligned numbers only
)

// Nerd Font icon codepoints used by the widgets.
const (
	IconCheck   = ''
	IconChevron = ''
	IconWarning = ''
)

// Theme owns the text shaper (fonts) and the helpers that paint with the role tokens.
// One Theme per window; it must not be used from several goroutines.
type Theme struct {
	// Shaper holds Geist, Geist Mono and the Nerd Font fallback.
	Shaper *text.Shaper
	mat    *material.Theme // only for its scrollbar/list implementation
}

// Font stacks. Gio resolves a comma separated Typeface list per rune: the first
// family that has the glyph wins, so text comes from the clean Geist TTFs and
// only icon codepoints fall through to the Nerd Font.
const (
	famSans = "Geist"
	famMono = "GeistMono"
	famNerd = "GeistNerd"
)

// NewTheme builds the shaper from the embedded fonts (never system fonts).
func NewTheme() *Theme {
	mk := func(src []byte, fam string, w font.Weight) font.FontFace {
		f, err := opentype.Parse(src)
		if err != nil {
			panic("ui: bad embedded font " + fam + ": " + err.Error())
		}
		return font.FontFace{Font: font.Font{Typeface: font.Typeface(fam), Weight: w}, Face: f}
	}
	sh := text.NewShaper(
		text.NoSystemFonts(),
		text.WithCollection([]font.FontFace{
			mk(assets.GeistRegular, famSans, font.Normal),
			mk(assets.GeistMedium, famSans, font.Medium),
			mk(assets.GeistBold, famSans, font.Bold),
			mk(assets.GeistMonoRegular, famMono, font.Normal),
			mk(assets.NerdFont, famNerd, font.Normal),
		}),
	)
	m := material.NewTheme()
	m.Shaper = sh
	return &Theme{Shaper: sh, mat: m}
}

func (th *Theme) font(f Face) font.Font {
	switch f {
	case Medium:
		return font.Font{Typeface: famSans + "," + famNerd, Weight: font.Medium}
	case Bold:
		return font.Font{Typeface: famSans + "," + famNerd, Weight: font.Bold}
	case Mono:
		return font.Font{Typeface: famMono + "," + famNerd}
	}
	return font.Font{Typeface: famSans + "," + famNerd}
}

// Label draws wrapping text in the given face, size and colour. It takes the
// width of the constraints and wraps inside it.
func (th *Theme) Label(gtx layout.Context, f Face, size unit.Sp, col color.NRGBA, s string) layout.Dimensions {
	return widget.Label{}.Layout(gtx, th.Shaper, th.font(f), size, s, colorOp(gtx, col))
}

// Line draws single-line text truncated with an ellipsis at the constraint width.
func (th *Theme) Line(gtx layout.Context, f Face, size unit.Sp, col color.NRGBA, s string) layout.Dimensions {
	return widget.Label{MaxLines: 1}.Layout(gtx, th.Shaper, th.font(f), size, s, colorOp(gtx, col))
}

// Icon draws a Nerd Font glyph so that its ink (not its advance box) is centred
// in a square of side size. Use layout.Center around it to place that square.
func (th *Theme) Icon(gtx layout.Context, glyph rune, size unit.Sp, col color.NRGBA) layout.Dimensions {
	px := gtx.Sp(size)
	side := image.Pt(px, px)
	th.Shaper.LayoutString(text.Parameters{
		Font:     font.Font{Typeface: famNerd},
		PxPerEm:  fixed.I(px),
		MaxWidth: 1 << 20,
	}, string(glyph))
	g, ok := th.Shaper.NextGlyph()
	if !ok {
		return layout.Dimensions{Size: side}
	}
	// Ink rectangle in document coordinates (dot at x=g.X on the baseline y=0).
	x0, x1 := fx(g.X+g.Bounds.Min.X), fx(g.X+g.Bounds.Max.X)
	y0, y1 := fx(g.Bounds.Min.Y), fx(g.Bounds.Max.Y)
	cx, cy := (x0+x1)/2, (y0+y1)/2
	off := f32.Pt(float32(px)/2-cx, float32(px)/2-cy)
	defer clip.Rect(image.Rectangle{Max: side}).Push(gtx.Ops).Pop()
	defer op.Affine(f32.Affine2D{}.Offset(off)).Push(gtx.Ops).Pop()
	defer clip.Outline{Path: th.Shaper.Shape([]text.Glyph{g})}.Op().Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: side}
}

func fx(v fixed.Int26_6) float32 { return float32(v) / 64 }

// ---- paint helpers (unexported) ---------------------------------------------

// colorOp records a solid colour as a paint material for text.
func colorOp(gtx layout.Context, c color.NRGBA) op.CallOp {
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	return m.Stop()
}

// fade scales a colour's alpha; used for disabled controls.
func fade(c color.NRGBA, k float32) color.NRGBA {
	c.A = uint8(float32(c.A) * k)
	return c
}

func fillRect(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}

func fillRRect(gtx layout.Context, r image.Rectangle, rad int, c color.NRGBA) {
	paint.FillShape(gtx.Ops, c, clip.UniformRRect(r, rad).Op(gtx.Ops))
}

// strokeRRect draws a border of width w px entirely inside r (nothing leaks outside r).
func strokeRRect(gtx layout.Context, r image.Rectangle, rad int, w float32, c color.NRGBA) {
	h := w / 2
	rr := max(0, float32(rad)-h)
	path := rrectPath(gtx.Ops, float32(r.Min.X)+h, float32(r.Min.Y)+h, float32(r.Max.X)-h, float32(r.Max.Y)-h, rr)
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: path, Width: w}.Op())
}

// rrectPath builds a rounded rectangle path with float coordinates.
func rrectPath(ops *op.Ops, w, n, e, s, rad float32) clip.PathSpec {
	const q = float32(0.5522847498) // 4*(sqrt2-1)/3
	iq := 1 - q
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(w+rad, n))
	p.LineTo(f32.Pt(e-rad, n))
	p.CubeTo(f32.Pt(e-rad*iq, n), f32.Pt(e, n+rad*iq), f32.Pt(e, n+rad))
	p.LineTo(f32.Pt(e, s-rad))
	p.CubeTo(f32.Pt(e, s-rad*iq), f32.Pt(e-rad*iq, s), f32.Pt(e-rad, s))
	p.LineTo(f32.Pt(w+rad, s))
	p.CubeTo(f32.Pt(w+rad*iq, s), f32.Pt(w, s-rad*iq), f32.Pt(w, s-rad))
	p.LineTo(f32.Pt(w, n+rad))
	p.CubeTo(f32.Pt(w, n+rad*iq), f32.Pt(w+rad*iq, n), f32.Pt(w+rad, n))
	p.Close()
	return p.End()
}

// measure lays w out into a macro and returns it with its size, so the caller
// can place or clip it afterwards.
func measure(gtx layout.Context, w layout.Widget) (op.CallOp, layout.Dimensions) {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	return m.Stop(), d
}

// upper is strings.ToUpper, named for the card-title call site.
func upper(s string) string { return strings.ToUpper(s) }

// list returns a scrolling-list style with the thin Graphite scrollbar
// (6 dp indicator in BTNBD, STROKE_MID on hover, no track fill).
func (th *Theme) list(state *widget.List) material.ListStyle {
	ls := material.List(th.mat, state)
	ls.Track.Color = color.NRGBA{}
	ls.Track.MajorPadding, ls.Track.MinorPadding = 1, 1
	ls.Indicator.MinorWidth = 6
	ls.Indicator.MajorMinLen = 24
	ls.Indicator.CornerRadius = 3
	ls.Indicator.Color, ls.Indicator.HoverColor = BTNBD, STROKE_MID
	return ls
}
