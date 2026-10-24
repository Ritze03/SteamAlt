# Package `ui` and `assets`

Gio UI. Read the [Styling Guide](../ui/STYLING-GUIDE.md) first for look and layout rules. Code: `ui/theme.go`, `widgets.go`, `app.go`; the top comment block of `app.go` lists the state fields a render test can set.

- **`theme.go`**: role tokens (Graphite) and the font collection. Proportional Geist Regular/Medium/Bold is the default; Geist Mono only for ids, paths and aligned numbers; the Geist Mono Nerd font is the icon fallback. Gio v0.10.3 fallback works through comma-separated `font.Typeface` lists such as `"Geist,GeistNerd"`, matched per rune.
- **`widgets.go`**: the components.
- **`app.go`**: the `App`. All state lives on the UI goroutine; background goroutines `post` closures that are drained at the start of a frame. Two columns at 760 dp and wider, one column below; the window opens at 520x760.
  - Copy flow: confirm modal, then the Steam-running modal (close Steam and wait up to 30 s), then the copy with progress, then a result message and a refresh.
  - The path modal (D12) opens from the footer "Change" button, or by itself when no Steam is found.
  - The destination dropdown has a "Select account" placeholder and defaults to none. *Why:* the copy is destructive.
    - The placeholder is `Items[0]` with `Dropdown.Placeholder = true`, which draws it DIM in the list and in the closed control (the styling guide says placeholders are DIM). *Why a flag and not a separate field:* account i stays at dst index i+1, so tests and the render test can keep setting `dst.Selected = i+1`; index 0 means no destination.
  - Names: `nameMap` holds what a row shows, `fallback` flags ids whose entry is only the `App <id>` fallback. A fallback is final to look at (not DIM/pending) but unknown to `setSource`, which asks again for every id that is missing or flagged. `setName` never lets a fallback overwrite a real name, and a real name clears the flag. *Why (B1):* `lookup.Names` used to report `App <id>` for ids it was cancelled on (a source switch mid-lookup); the UI stored it as resolved and never asked again, so the names stayed `App 367520` for the whole session. Now cancelled ids stay unknown, and offline or 429 fallbacks get a retry on the next source change.
  - Closing the window calls `(*App).Shutdown()` (from `main.go` on `DestroyEvent`): it cancels the avatar, name and copy contexts and waits up to 5 s for the copy goroutine to return. *Why:* `main.go` used to `os.Exit(0)` straight away, which left a full-size `.steamalt-tmp-<id>` in the destination; `transfer.Copy` removes its tmp folder when its context is cancelled, but only if it gets to run. Gio cannot veto a window close, so a confirm prompt is not possible; cancel and wait is the substitute.
  - Dimming uses linear-light alphas: Gio blends in linear light, so the mockup's CSS opacities look too faint. The copying wash is `fade(PANEL, .72)` (content ends at about 55 %), the modal backdrop is `scrim` in `widgets.go` (alpha 0xD4, not `SCRIM`'s 0x8C). *Why:* measured on the renders; white text under the old values stayed at 77 % / 70 % brightness.
  - Long text is bounded: account names are cut to 24 runes in the confirm title (`ellipsize`), the footer message is capped at 2 lines, and the copy-error line names the game (display name), not the app id.
- **Gio gotcha:** `Clickable`/`Bool`/`Editor` drain their events inside Layout, so poll `Clicked`/`Update`/`Submitted` before laying them out. Popups and modals use `op.Defer`.

## `main.go` and `assets/`

`main.go` creates the window (520x760, with a min size), the `lookup` cache dir, then `ui.NewApp(w.Invalidate, transfer.System(), lookup.New(cache))` and the event loop.

`assets/` embeds Geist R/M/B, Geist Mono R, the GeistMono Nerd OTF and the icon, unmodified. *Why unmodified and not subset:* OFL 1.1 section 2 only needs the notice and licence to travel with each copy; subsetting would make it a "Modified Version" with naming rules. Licences: `assets/fonts/Geist-OFL.txt`, `assets/fonts/NerdFonts-LICENSE.txt`. D11: a credit line in the README is required, not a credits screen.
