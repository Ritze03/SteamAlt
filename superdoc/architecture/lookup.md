# Package `lookup`

Network and cache: game names and avatars. Code: `lookup/`. Run `go doc -all ./lookup`. Name strategy in detail: [Steam AppID to name](../knowledge/steam-appid-to-name.md).

`Client{CacheDir, StoreURL, AvatarURL, HTTP}`, built with `New(cacheDir)`; `DefaultCacheDir()` is `os.UserCacheDir()/steamalt`. The URL fields exist so tests can point at an `httptest` server.

- `Names` resolves in order: the local map (from `steam.InstalledNames`), the `names.json` cache, `appdetails` (one id per request, at most 4 concurrent, 5 s `http.Client.Timeout`), then `FallbackName` (`App <id>`).
  - A definitive `success:false` is cached permanently; transient errors (timeout, 5xx, 429) are not. A 429 stops further requests for that call.
  - *Why:* roughly 200 requests / 5 min / IP rate limit, and offline-first behaviour.
- `Avatar(hash)` fetches `https://avatars.cloudflare.steamstatic.com/<hash>_full.jpg`, caches to `avatars/<hash>.jpg` under the cache dir, 2 MB cap. The hash is validated as `^[0-9a-f]{40}$`. *Why:* it is a trust boundary for both the URL and the filename.
