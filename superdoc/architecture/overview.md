# Architecture Overview

The start-here page. It links out to sibling docs rather than duplicating them.

## Big picture

SteamAlt copies Steam game data (saves/settings) between Steam accounts on one machine. It was rewritten from Python/Tkinter to Go + Gio (`gioui.org`). The user picks a source account, individual games and a destination account; the app shows real game names and avatars, closes a running Steam, and copies safely.

## Data flow

1. `main.go` builds the window and `ui.NewApp(invalidate, transfer.System(), lookup.New(cache))`.
2. `steam.ResolveRoot` finds the Steam root (saved config, else detection; otherwise the path modal opens).
3. `steam.Accounts` lists accounts; picking a source runs `steam.Games`.
4. Background goroutines resolve names (`steam.InstalledNames`, then `lookup.Names`) and avatars (`lookup.Avatar`) and post the results back to the UI goroutine.
5. Copy: confirm, check/close Steam via `transfer.Steam`, `transfer.Copy` with progress, result message, refresh.

## Module map

| Package | Role | Doc |
|---|---|---|
| `steam/` | VDF parser, config, root detection, accounts, games, installed names | [steam](steam.md) |
| `transfer/` | safe copy, Steam-process seam | [transfer](transfer.md) |
| `lookup/` | game names (API + cache), avatars | [lookup](lookup.md) |
| `ui/` | Gio theme, widgets, App | [ui and assets](ui.md) |
| `assets/` | embedded fonts and icon | [ui and assets](ui.md) |
| `main.go` | window and event loop | [ui and assets](ui.md) |

Building: `./build.sh` writes `dist/SteamAlt-Linux` and `dist/SteamAlt.exe`. Tests: [Testing rules](testing.md).

## Where to look for X

- Showing a game's name for an app id (API call, local appmanifest lookup, caching, fallback): [Steam AppID to name](../knowledge/steam-appid-to-name.md)
- VDF parser, root detection, accounts, games: [steam](steam.md)
- The copy, or detecting/closing Steam: [transfer](transfer.md)
- Names and avatars over the network, and their caches: [lookup](lookup.md)
- Window, modals, fonts, licences: [ui and assets](ui.md)
- Colours, layout, controls, any UI work: [Styling Guide (Graphite)](../ui/STYLING-GUIDE.md)
- Running tests safely (headless sway, sandbox, ccorral): [Testing rules](testing.md)
- Project vocabulary: [Terminology](../meta/TERMINOLOGY.md)
