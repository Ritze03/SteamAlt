# Package `transfer`

The copy and the Steam-process seam. Code: `transfer/`. Run `go doc -all ./transfer`.

## Copy

`Copy(ctx, srcDir, dstDir, ids, progress)` returns `map[string]error` (per-id failures). Mode and mtime are preserved, symlinks inside the tree stay symlinks, only numeric ids are accepted.

**Swap.** Each selected id is copied to `dst/.steamalt-tmp-<id>`. Once complete: `dst/<id>` is renamed to `dst/.steamalt-old-<id>`, the tmp is renamed to `dst/<id>`, then the old folder is removed (best effort). If the second rename fails, the old folder is renamed back; if that also fails, both folders are kept and the error names their paths. The tmp copy is never deleted once the swap has begun. *Why:* the end result equals the old rmtree+copytree, but a failed or cancelled copy must never destroy the destination's existing data. The earlier `RemoveAll(final)` then `Rename` left a window where a failure lost both old and new data; renames within one directory are atomic. The renames go through the package var `rename` so tests can inject failures.

**Sweep.** At the start of `Copy` (after `MkdirAll(dst)`), `dst` is swept for leftovers of an interrupted run: every `.steamalt-tmp-*` is removed; a `.steamalt-old-<id>` is removed if `<id>` exists, otherwise restored as `<id>` (a crash between the two swap renames). Only these exact prefixes are touched. *Why:* closing the window mid-copy leaves a full-size tmp dir in the user's `userdata/<acct>/`, which was previously cleaned only if the same id was copied again; the restore case keeps the old data from being deleted when the crash fell inside the swap.

**Symlinked app folder.** `srcDir/<id>` is resolved with `os.Stat`/`EvalSymlinks`, so a symlink to a directory is copied as a real folder `dst/<id>` (links inside the tree are still recreated as links). *Why:* `steam` lists such a link as an app folder, and refusing it ("not a directory") contradicted what the user sees.

## The Steam seam

`type Steam interface{ Running; Close }`, with `System()` (the real one), `&Fake{}` (tests) and `WaitClosed(ctx, s, poll)`.

- Linux `Running`: `/proc/*/comm` is exactly `steam` or `steam.exe`, so `steamwebhelper` does not match. Linux `Close`: `steam -shutdown`; if `steam` is not on PATH but `flatpak` is, `flatpak run com.valvesoftware.Steam -shutdown`; if neither, an error saying to close Steam manually. The choice is made by the unexported `shutdownCmd(lookPath)`, which builds the command without running it so tests can assert it. *Why:* Flatpak Steam has no `steam` binary on the host PATH; the sandboxed process is still named `steam`, so the `/proc` match works unchanged.
- Windows: `tasklist` / `taskkill /F` with a hidden console window.

*Why a seam:* D16. No test may ever signal the user's real Steam; tests and the sandbox use `Fake`. See [Testing](testing.md).
