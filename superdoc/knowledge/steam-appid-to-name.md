# Steam AppID to game name

How to turn a Steam app id (the number seen in `userdata/<id>/<appid>/`) into a display name. Two sources: the offline local appmanifest (free, try first) and the public Store API.

Source: Obsidian note `01 - Projects/02 - AI Coding/Agent Knowledge/Steam API - AppID to Game Name.md` (verified: 2026-10-24). The first section below is from that note; the local-manifest section is Steam-client knowledge added separately, not from the note.

## Local lookup (offline, try first)

Steam-client knowledge, not from the source note.

- Installed games have `<SteamRoot>/steamapps/appmanifest_<APPID>.acf`, a KeyValues/VDF text file with a `"name"` field.
- Steam libraries on other drives are listed in `<SteamRoot>/steamapps/libraryfolders.vdf`; each library has its own `steamapps/` with manifests.
- `<SteamRoot>` is the parent of `userdata/`.

*Why:* it needs no network, has no rate limit and no timeout, so it is a free first lookup before hitting the API. It only covers installed apps, so fall back to the API for the rest.

Installed tools (Proton, Steam Linux Runtime, etc.) DO have appmanifests, so the local tier also covers some ids the API answers with `success:false`.

## How the app does it

`lookup.Names` ([lookup](../architecture/lookup.md)) resolves: the local map (`steam.InstalledNames`, see [steam](../architecture/steam.md)), then the `names.json` cache, then the API (one id per request, at most 4 concurrent, one 5 s total timeout each), then `App <id>`. A definitive `success:false` is cached permanently; timeouts, 5xx and 429 are not cached, and a 429 stops further requests for that call.

## Store API (public, no API key, no auth)

```
GET https://store.steampowered.com/api/appdetails?appids=<APPID>&filters=basic
```

- `filters=basic` trims the payload (no screenshots/movies/etc.). Still includes long HTML descriptions.
- Optional: `l=<language>` (e.g. `l=german`) for localized name/text, `cc=<country>` for regional store.

### Response

Success:

```json
{"570": {"success": true, "data": {"type": "game", "name": "Dota 2", "steam_appid": 570, ...}}}
```

Name is at `.<APPID>.data.name`. Quick check:

```
curl -s 'https://store.steampowered.com/api/appdetails?appids=620&filters=basic' | jq '."620".data.name'
```

Unknown / delisted / region-locked id:

```json
{"999999999": {"success": false}}
```

Always check `success` before reading `data`.

### Gotchas

- **One appid per request.** `appids=1,570&filters=basic` gives HTTP 400. Batching only works with `filters=price_overview`, which returns no name.
- **Rate limit:** unofficial, roughly ~200 requests / 5 min per IP; exceeding it gives HTTP 429. Cache results. *Why:* a game list easily exceeds the limit, and names rarely change.
- **Non-Steam shortcuts / tools** (Proton, Steam Linux Runtime, non-Steam games with generated ids) return `success: false`, so a fallback name is needed (e.g. show the raw app id). *Why:* these ids are not in the store.
- Undocumented store endpoint: can change without notice. Treat as best-effort: one 5 s total timeout per request (`http.Client.Timeout`) and nil/empty on any failure. *Why:* the UI must never hang or crash on a name lookup.
- `ISteamApps/GetAppList/v2` (full id to name dump) now returns **404**, so it is dead. Its replacement `IStoreService/GetAppList/v1` needs a Web API key.

### Go sketch

Illustrative single-request sketch, stdlib only; the real code is in `lookup/` (see above).

```go
var client = &http.Client{Timeout: 5 * time.Second}

func steamName(appid string) (string, bool) {
	resp, err := client.Get("https://store.steampowered.com/api/appdetails?appids=" + appid + "&filters=basic")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var m map[string]struct {
		Success bool
		Data    struct{ Name string }
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&m) != nil {
		return "", false
	}
	e := m[appid]
	return e.Data.Name, e.Success && e.Data.Name != ""
}
```
