# Terminology

Project-specific vocabulary. When the user uses a term defined here, use the same
meaning.

**If the user uses a non-standard term that is not defined here, ask what they mean by
it before acting on it.** Once its meaning is clear, add it to this file. Keep this
file up to date automatically: whenever the user introduces or clarifies a term, add or
amend the entry here.

## Terms

- **userdata** — the `<SteamRoot>/userdata/` folder; holds one subfolder per Steam account.
- **SteamID3 / account id** — the numeric folder name under `userdata/`; the 32-bit Steam account ID.
- **app id** — Steam's numeric id of a game/app; resolved to a name via [Steam AppID to name](../knowledge/steam-appid-to-name.md).
- **appmanifest** — `steamapps/appmanifest_<APPID>.acf`, the VDF text file Steam keeps for each installed game (has a `"name"` field).
- **pane** — a clipped layout region (page column, card, row half) that sizes content only within its own width; see [Styling Guide](../ui/STYLING-GUIDE.md).
- **role token** — a named theme colour (`ACCENT`, `PANEL`, ...) used instead of a literal hex at call sites.
