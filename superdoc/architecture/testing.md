# Testing rules

- **GUI testing always in a headless sway session**, never on the user's desktop; pass `WAYLAND_DISPLAY` explicitly. Start it with:
  ```
  env -u DISPLAY -u WAYLAND_DISPLAY -u SWAYSOCK WLR_BACKENDS=headless WLR_LIBINPUT_NO_DEVICES=1 WLR_RENDERER=pixman sway -c /dev/null &
  ```
  Use the new `/run/user/1000/wayland-N` socket. For a real window: `swaymsg create_output`, then `grim`. Kill sway afterwards.
- **Never test against live Steam** (D16). Use the sandbox copy at `/tmp/claude-1000/steamalt-sandbox/root` via a D10 config in a temp `XDG_CONFIG_HOME`, plus temp `XDG_CACHE_HOME` and `HOME`. The sandbox test is `go test -tags sandbox .` (`sandbox_test.go`). Render PNGs with `RENDER_OUT=<dir> go test -run Render ./ui/` (`ui/render_test.go`).
- Unit tests use synthetic fixtures only; Steam control goes through `transfer.Fake` (see [transfer](transfer.md)).
- **Build and test commands go through `ccorral -- CMD`** (the user's global rule for agents), e.g. `ccorral -- go test ./...`.
