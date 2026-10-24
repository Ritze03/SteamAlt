# Styling Guide: Graphite

"Graphite": a dark UI theme and layout rules, reusable in any desktop/web project. Cool, blue-tinted dark greys with one indigo accent, compact type, settings laid out as cards of two-column rows inside nested, clipped panes.

Source: Obsidian note `01 - Projects/02 - AI Coding/Agent Instructions/Graphite Theme.md`.

- **Origin:** "ritz", a game launcher built for tinkering and simplicity, refined in ForzaTelemetryV3.
- **Reference implementation (Rust / egui):** `~/Schinken/Programming/Local/ForzaTelemetryV3/src/theme.rs`, rules in `docs/ui/STYLING-GUIDE.md` there.
- **This project uses Go + Gio**; see "Applying it in Gio" at the end.

## Principles

- **Role tokens, not hex.** Every chrome colour is a named role (`ACCENT`, `PANEL`, `TEXT_DIM`...) defined in one theme file. Call sites never hard-code a colour, so the whole app re-themes from one place.
- **Chrome vs. data colours.** Tokens are for chrome only. Colours that carry meaning in the data (temperatures, input bars, good/bad readings) stay at their call sites.
- **Steel, not grey.** Neutrals are slightly blue-tinted so the UI leans toward the accent.
- **Nested panes** (see below): every region is its own pane, and content is sized only inside its pane.

## Nested panes (layout rule)

**Every page column is its own pane, every category card is its own pane, and every row inside a card is laid out in its card's width. Nothing may draw outside its pane.** Dynamic sizing happens only *inside* a pane, using stack / row layouts. Nothing is placed freely in one big area with absolute coordinates.

- **Page to columns:** split a page into equal columns with fixed gaps. Each column is exactly `1/n` of the width, clipped to its slot.
- **Column to cards:** a card fills its column's width exactly and clips its body to its inner width.
- **Card to rows:** rows size from the available width, never fixed pixel widths. When narrow they **shrink, wrap or stack**: slider tracks shrink, labels wrap, groups of options wrap onto further lines.
- **Row to halves:** a label | control row is itself two panes.
- **Custom-drawn widgets** take their size from the available width (a max cap is fine) and paint only inside their own rect.
- **Free-placed tiles** (dashboard widgets) are panes too: fit the content to the cell (scale fonts down together, truncate titles), no minimum size.
- The clip is the safety net, not the layout: if something is cut at a pane edge, fix the row so it fits; don't widen the pane.

*Why:* nesting makes formatting easy and predictable. Each pane only has to care about its own width, one too-wide element can't push its card over the neighbouring column, and resizing the window just reflows each pane on its own. (Learned the hard way in egui, where a too-wide widget silently grows every enclosing container.) Back it with a headless render test at a few widths that asserts nothing paints across a pane.

## Palette

| Token | Hex | Role |
| --- | --- | --- |
| `ACCENT` | `#5B8BF0` | Brand indigo: selection, primary button, section titles, links, checked boxes |
| `HEAD` | `#14171A` | Title bar / top panel (darker than panels) |
| `PANEL` | `#1E2125` | Main panel, window background |
| `PANEL2` | `#191C1F` | Footer / status band |
| `FIELD` | `#15181B` | Inputs, code blocks, open dropdowns |
| `BORDER` | `#2C3036` | All hairline borders / dividers |
| `BTN` | `#262A30` | Secondary button background |
| `BTNBD` | `#343941` | Button border |
| `TEXT` | `#E7E9EC` | Primary text |
| `DIM` | `#969CA6` | Secondary text, placeholders |
| `FAINT` | `#646A73` | Tertiary text / labels |
| `PRIMARY_TEXT` | `#0B1222` | Text on an accent button |
| `DANGER` | `#E15554` | Delete, stop, disconnect |
| `GOOD` | `#6CC551` | Positive / connected |
| `WARN` | `#D8B455` | Muted amber, in-between state |

### Steel scale

`steel(lum)` gives `rgb(0.9·lum, lum, min(255, 1.17·lum))`

| Token | lum | Role |
| --- | --- | --- |
| `TEXT_DIM` | 150 | Unit labels, legends, muted values, unchecked outline |
| `TEXT_FAINT` | 105 | Hints, captions |
| `STROKE_MID` | 80 | Gauge rims & rings |
| `STROKE_DIM` | 50 | Gridlines, crosshairs |
| `TRACK` | 40 | Track behind bars & sliders |
| `WELL` | 24 | Recessed wells (gauge backgrounds, inset frames) |

### Tints

- `SEL`: accent at ~16 % alpha: selection fill.
- `SELBD`: accent at ~42 % alpha: selection border, pressed state.
- `HOV`: white at ~5 % alpha: hover wash.

## Fonts

| Font | Use | File (source) | License file |
| --- | --- | --- | --- |
| **Geist Mono** Regular (TTF) | All UI text, proportional *and* monospace slots | `GeistMono/GeistMono-Regular.ttf` | `GeistMono/Geist-OFL.txt` (SIL OFL 1.1) |
| **Geist Mono Nerd Font** Regular (OTF) | Icon glyphs only (fallback after Geist Mono) | `GeistMonoNerdFont/GeistMonoNerdFont-Regular.otf` | `GeistMonoNerdFont/NerdFonts-LICENSE.txt` (Nerd Fonts, OFL 1.1 / MIT) |

- Source folder (Obsidian vault, outside the repo): `01 - Projects/02 - AI Coding/Agent Instructions/assets/`. The font files are **not** in the repo yet; they will be embedded at build time.
- Font stack: Geist Mono first, the Nerd Font appended as fallback, so text renders from the clean TTF and only icon codepoints (`\uf...`) come from the patched OTF.
- Monospace everywhere so numbers and readouts line up in columns.
- Icons are centred on their **ink**, not their advance box (Nerd glyphs sit off-centre).

## Type & metrics

- Sizes: Heading 18 · Body 13 · Button 13 · Small 11 · Mono 12 · page pill 12.5 · segmented control 14.
- Corner radius 7 on widgets, 5 on checkboxes, full pill for segmented controls.
- Button padding 9 × 5, standard control height 22.
- Spacing: **8 px** everywhere between cards and between columns; the outer panel margin (8 px) is the top gap. 4 px between rows inside a card.

## Titles & text colours

| What | Colour | Style |
| --- | --- | --- |
| Card / category title | `ACCENT` | UPPERCASE, 12, bold |
| Dashboard tile title | `ACCENT` | same as card title, truncates with ... (never wraps) |
| App wordmark (top bar) | `ACCENT` | 16, bold |
| Body text | `TEXT` | global text colour override |
| Secondary / muted values | `TEXT_DIM` / `DIM` | |
| Captions, hints | `TEXT_FAINT` / `FAINT` | |
| Text-field placeholder | `DIM` | set explicitly, otherwise the global override paints it like real text |
| Hyperlinks | `ACCENT` | |
| Text selection | `SEL` fill, `SELBD` border | |

## App chrome

- **Top bar:** `HEAD` fill, 4 px margin (5 px bottom), 30 px tall tab buttons, fill only (no outline), radius 4, hand cursor. Selected tab = `SEL` fill with the icon in `SELBD`; hover = `BTN` fill. Icon-only tabs are 30 × 30.
- **Top bar layout ("Modern"):** wordmark left, then a 1 × 18 px `BORDER` divider, then the current-page pill (full radius, `SEL` fill, `SELBD` border, `TEXT`; reserves the widest title's width so the tabs don't jump), then icon tabs centred, settings/help tabs right-aligned.
- **Status bar:** `PANEL2` fill at the bottom. State shown by colour (`GOOD` connected, `DANGER` disconnected, `WARN` connecting), so it reads in icon-only mode too.
- **Content area:** `PANEL`, 8 px margin on all sides.

## Components

- **Buttons:** *primary* = solid accent, dark bold text · *secondary* = `BTN` fill, `BTNBD` border · *danger* = transparent, red text, faint red border.
- **Widget states:** resting `BTN`/`BTNBD` · hover: accent border + `HOV` wash · pressed: `SELBD` border · open (menus): `FIELD`.
- **Card / category:** transparent fill (the panel shows through), 1 px `BORDER` stroke, radius 7, 6 px inner margin; accent title, 4 px gap, 4 px between rows. Groups related controls.
- **Checkbox:** 18 px rounded box, accent-filled with a white check when on, 1.5 px steel outline when off. The whole row (box + label) is the click target, with a hover wash. Disabled = everything one step dimmer.
- **Radio:** same family, a circle with a white centre dot. Groups wrap onto further lines when narrow.
- **Segmented control** (one of a few short options): 32 px tall pill container (`FIELD`, `BORDER`, 3 px inset), equal-width segments, the selected one a pill with `SEL` fill + `SELBD` border, the others `TEXT_DIM` with a hover wash.
- **Slider row:** label in the left half; in the right half a slider plus a **fixed-width value field (72 px)** pinned right. The slider shrinks, the value field never jumps when digits change. Percentages always show one decimal.
- **Modals:** dim backdrop that swallows clicks + centred window; primary (or danger) + Cancel. Enter confirms, Esc / backdrop click cancels.

## Content conventions

- Settings rows: label left half, control right half, so every control edge lines up.
- Labels are vertically centred on their control's row.
- No trailing colons on labels.
- No helper text under options: explanations go in tooltips, and only where the label isn't self-explanatory. Warnings and live status stay visible.
- A single-column settings page doesn't reserve an empty second column: cap it at half the width (max ~520 px) instead.
- Text inputs fill the available width; a trailing button is pinned right.

## Applying it in Gio

Guidance for the upcoming Go + Gio (`gioui.org`) rewrite; nothing here exists in code yet.

- Tokens live in one Go file (planned: a `theme` package / `theme.go`) as `color.NRGBA` values. Call sites never use literal colours.
- Units: the spec's px values are meant as dp (sizes) and sp (text).
- Clip per pane with `clip.Rect` / `clip.RRect` ops, scoped with `.Push(gtx.Ops).Pop()`. Width comes from `gtx.Constraints`; build layouts with `layout.Flex` / `layout.Stack` / `layout.Inset`. Do NOT position with absolute coordinates (`op.Offset` exists, but use `layout.Flex`/`Stack`/`Inset` and constraints instead). *Why:* absolute placement defeats the nested-pane width rule.
- Geist Mono is embedded via `go:embed` and loaded with `gioui.org/font/opentype` into the shaper's collection (see Fonts for the files). Gio's loader needs static TTF/OTF, which these are.
- Keep the headless render test at a few widths; Gio can render headless via `gioui.org/gpu/headless`.
