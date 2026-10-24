package main

import (
	"log"
	"os"
	"path/filepath"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/Ritze03/SteamAlt/lookup"
	"github.com/Ritze03/SteamAlt/transfer"
	"github.com/Ritze03/SteamAlt/ui"
)

func main() {
	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("SteamAlt"),
			app.Size(unit.Dp(520), unit.Dp(760)),
			app.MinSize(unit.Dp(420), unit.Dp(560)),
		)
		if err := run(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	cache, err := lookup.DefaultCacheDir()
	if err != nil { // an empty dir would make the client cache into the working directory
		cache = filepath.Join(os.TempDir(), "steamalt")
	}
	a := ui.NewApp(w.Invalidate, transfer.System(), lookup.New(cache))
	a.Start()
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}
