# Package `ui` and `assets`

Gio UI. Read the [Styling Guide](../ui/STYLING-GUIDE.md) first for look and layout rules. Code: `ui/theme.go`, `widgets.go`, `app.go`; the top comment block of `app.go` lists the state fields a render test can set.

- **`theme.go`**: role tokens (Graphite) and the font collection. Proportional Geist Regular/Medium/Bold is the default; Geist Mono only for ids, paths and aligned numbers; the Geist Mono Nerd font is the icon fallback. Gio v0.10.3 fallback works through comma-separated `font.Typeface` lists such as `"Geist,GeistNerd"`, matched per rune.
- **`widgets.go`**: the components.
- **`app.go`**: the `App`. All state lives on the UI goroutine; background goroutines `post` closures that are drained at the start of a frame. Two columns at 760 dp and wider, one column below; the window opens at 520x760.
  - Copy flow: confirm modal, then the Steam-running modal (close Steam and wait up to 30 s), then the copy with progress, then a result message and a refresh.
  - The path modal (D12) opens from the footer "Change" button, or by itself when no Steam is found.
  - The destination dropdown has a "Select account" placeholder and defaults to none. *Why:* the copy is destructive.
- **Gio gotcha:** `Clickable`/`Bool`/`Editor` drain their events inside Layout, so poll `Clicked`/`Update`/`Submitted` before laying them out. Popups and modals use `op.Defer`.

## `main.go` and `assets/`

`main.go` creates the window (520x760, with a min size), the `lookup` cache dir, then `ui.NewApp(w.Invalidate, transfer.System(), lookup.New(cache))` and the event loop.

`assets/` embeds Geist R/M/B, Geist Mono R, the GeistMono Nerd OTF and the icon, unmodified. *Why unmodified and not subset:* OFL 1.1 section 2 only needs the notice and licence to travel with each copy; subsetting would make it a "Modified Version" with naming rules. Licences: `assets/fonts/Geist-OFL.txt`, `assets/fonts/NerdFonts-LICENSE.txt`. D11: a credit line in the README is required, not a credits screen.
