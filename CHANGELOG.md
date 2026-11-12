# Changelog

All notable user-facing changes, newest first. Categories:

**Added** (new features),
**Fixed** (bug and behaviour fixes),
**Removed** (things taken out),
**Info** (notes worth knowing).

## 2026-10-25

### Added
- **Linux install script**: `./install.sh --install` builds SteamAlt from source and
  checks the build requirements, listing any missing packages; `--remove` uninstalls it.
  It also works through ritz-get.

### Fixed
- **Title bar**: the logo, name and status pill are now vertically centred in the bar.

## 2026-10-24

### Added
- **Rewritten in Go**: SteamAlt is now a native Go app built with Gio, replacing the
  Python/Tkinter version, with a responsive layout and a dark "Graphite" look.
- **Real game names**: games are listed with their names, looked up from their Steam app
  id and cached on disk.
- **Account avatars**: avatars are fetched and cached as visual confirmation of the
  account.
- **Steam folder setting**: a dialog lets you pick the Steam folder, and the choice is
  saved between runs.
- **Windows build**: the app ships with Windows resources and an icon.

### Fixed
- **Safer copy**: if a copy fails or is cancelled, the destination's existing data is
  restored; leftovers are cleaned up and symlinked game folders are handled.
- **Closing Steam**: works with Flatpak Steam, and closing the window cancels a running
  copy.
- **Stuck game names**: names that failed to load are retried instead of staying as a
  placeholder.

### Removed
- **Python version**: the Python/Tkinter app and its `requirements.txt` are gone.

## 2025-12-09

### Fixed
- **Project page**: text encoding problems on the project web page are fixed.

## 2025-04-04

### Added
- **First release**: a simple GUI tool, written in Python, that copies game data (saves,
  settings and so on) between Steam accounts on the same computer.
