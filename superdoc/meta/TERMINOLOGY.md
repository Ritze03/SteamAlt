# Terminology

Project-specific vocabulary. When the user uses a term defined here, use the same
meaning.

**If the user uses a non-standard term that is not defined here, ask what they mean by
it before acting on it.** Once its meaning is clear, add it to this file. Keep this
file up to date automatically: whenever the user introduces or clarifies a term, add or
amend the entry here.

## Terms

- **userdata** — the `<SteamRoot>/userdata/` folder; holds one subfolder per Steam account.
- **SteamID3 / account id** — the numeric folder name under `userdata/`; the 32-bit Steam account ID (the numeric part of a SteamID3 such as `[U:1:<id>]`). SteamID64 = account id + 76561197960265728; `loginusers.vdf` is keyed by SteamID64.
- **app id** — Steam's numeric id of a game/app; resolved to a name via [Steam AppID to name](../knowledge/steam-appid-to-name.md).
- **appmanifest** — `steamapps/appmanifest_<APPID>.acf`, the VDF text file Steam keeps for each installed game (has a `"name"` field).
- **pane** — a clipped layout region (page column, card, row half) that sizes content only within its own width; see [Styling Guide](../ui/STYLING-GUIDE.md).
- **role token** — a named theme colour (`ACCENT`, `PANEL`, ...) used instead of a literal hex at call sites.
- **sandbox** — a throwaway copy of a Steam root (`/tmp/claude-1000/steamalt-sandbox/root`) used for every non-unit test, with temp `XDG_CONFIG_HOME`/`XDG_CACHE_HOME`/`HOME`; live Steam is never touched (D16). See [Testing rules](../architecture/testing.md).
- **Steam seam** — the `transfer.Steam` interface (`Running`, `Close`) that hides the real Steam process; production uses `transfer.System()`, tests use `&transfer.Fake{}`. See [transfer](../architecture/transfer.md).
