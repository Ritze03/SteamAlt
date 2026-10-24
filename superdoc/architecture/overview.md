# Architecture Overview

The start-here page. It links out to sibling docs rather than duplicating them. This grows as the project does.

## Big picture

SteamAlt copies Steam game data (saves/settings) between Steam accounts on one machine. It is being rewritten from Python/Tkinter (`SteamUserdataMoverGui.py`) to Go + Gio (`gioui.org`). No Go code exists yet.

## Data flow

(empty until code lands)

## Module map

(empty until code lands)

## Where to look for X

- Showing a game's name for an app id (API call, local appmanifest lookup, caching, fallback): [Steam AppID to name](../knowledge/steam-appid-to-name.md)
- Colours, layout, controls, any UI work: [Styling Guide (Graphite)](../ui/STYLING-GUIDE.md)
- Project vocabulary: [Terminology](../meta/TERMINOLOGY.md)
