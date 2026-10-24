# Package `transfer`

The copy and the Steam-process seam. Code: `transfer/`. Run `go doc -all ./transfer`.

## Copy

`Copy(ctx, srcDir, dstDir, ids, progress)` returns `map[string]error` (per-id failures). Each selected id is copied to `dst/.steamalt-tmp-<id>`, then `RemoveAll(dst/<id>)` and `Rename` into place. *Why:* the end result equals the old rmtree+copytree, but a failed or cancelled copy no longer destroys the destination's existing data. Residual window: a `Rename` failure after `RemoveAll`. Mode and mtime are preserved, symlinks stay symlinks, only numeric ids are accepted.

## The Steam seam

`type Steam interface{ Running; Close }`, with `System()` (the real one), `&Fake{}` (tests) and `WaitClosed(ctx, s, poll)`.

- Linux `Running`: `/proc/*/comm` is exactly `steam` or `steam.exe`, so `steamwebhelper` does not match. Linux `Close`: `steam -shutdown`.
- Windows: `tasklist` / `taskkill /F` with a hidden console window.

*Why a seam:* D16. No test may ever signal the user's real Steam; tests and the sandbox use `Fake`. See [Testing](testing.md).
