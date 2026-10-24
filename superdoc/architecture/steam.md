# Package `steam`

Pure stdlib, synchronous disk I/O. Reads Steam's on-disk state: root detection, accounts, games, installed names. Code: `steam/vdf.go`, `root.go`, `accounts.go`, `games.go`. Run `go doc -all ./steam` for signatures.

## VDF parser (`vdf.go`)

`ParseVDF` is a hand-written KeyValues parser (decision D20). It handles a BOM, bare (unquoted) tokens, escapes and `//` comments, with a nesting depth cap of 100. `Get`/`Find`/`Str` on `*Node` are case-insensitive and nil-safe, so chained lookups need no nil checks. A malformed file is a whole-file error (no partial tree).

## Config and root (`root.go`)

- `ConfigPath`/`LoadConfig`/`SaveConfig`: JSON `{"steam_path": ...}` at `os.UserConfigDir()/steamalt/config` (D10). *Why JSON:* stdlib `encoding/json`, so no parser to write and no escaping bugs for paths with spaces or `=`. *Why the Steam root and not `userdata`:* the app also needs `steamapps/`.
- `NormalizeRoot` strips a trailing `userdata`.
- `CheckRoot` counts numeric accounts, excluding the dummy `0` folder. *Why:* real installs have a `userdata/0` with no games.
- `DetectRoot` tries, in order: on Windows only `%ProgramFiles(x86)%\Steam`, `C:\Program Files (x86)\Steam`, `~\AppData\Local\Steam`; then everywhere `~/.local/share/Steam`, `~/.steam/steam` and two Flatpak paths.
- `ResolveRoot`: a saved path wins; an invalid saved path gives a warning and falls back to detection; if nothing is found it returns `ErrNotFound` and the UI opens the path modal (D12).

## Accounts (`accounts.go`)

`Accounts` lists `userdata/` accounts. `PersonaName` comes from `config/loginusers.vdf` keyed by SteamID64 (account id + 76561197960265728), falling back to `localconfig.vdf` `UserLocalConfigStore/friends/<own id>/NameHistory/0`, then `UnknownName` (`(unknown)`). *Why:* the old regex took the oldest name and crashed on bad UTF-8. The avatar hash comes from `friends/<own id>/avatar` (fetched by [lookup](lookup.md)).

## Games and names (`games.go`)

- `Games(root, srcID, dstID)` returns the numeric app folders of the source; `Overwrites` is set when the folder already exists in the destination.
- `InstalledNames(root)` reads appmanifests across the libraries in `libraryfolders.vdf`; nonexistent library paths are skipped. Built-ins: 7 "Steam Client" and 760 "Screenshots". This is the local tier of [Steam AppID to name](../knowledge/steam-appid-to-name.md).
