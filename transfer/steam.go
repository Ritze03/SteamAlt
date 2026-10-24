package transfer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Steam is the seam between SteamAlt and the real Steam client. Tests and
// render tests use Fake; production code uses System.
type Steam interface {
	// Running reports whether a Steam client process exists.
	Running() (bool, error)
	// Close asks Steam to quit. It does not wait for it to exit; use WaitClosed.
	Close() error
}

// System returns the real Steam controller for the current OS. Never call its
// Close from tests.
func System() Steam {
	switch runtime.GOOS {
	case "linux":
		return linuxSteam{procDir: "/proc"}
	case "windows":
		return windowsSteam{}
	}
	return unsupportedSteam{}
}

// Fake is an in-memory Steam for tests. Not safe for concurrent use.
type Fake struct {
	IsRunning bool
	CloseErr  error // returned by Close; state is then left unchanged
	Closes    int   // number of Close calls
}

func (f *Fake) Running() (bool, error) { return f.IsRunning, nil }

func (f *Fake) Close() error {
	f.Closes++
	if f.CloseErr != nil {
		return f.CloseErr
	}
	f.IsRunning = false
	return nil
}

// WaitClosed polls s.Running every poll (default 500ms if <= 0) until Steam is
// not running (nil), Running errors (that error), or ctx is done (ctx.Err()).
func WaitClosed(ctx context.Context, s Steam, poll time.Duration) error {
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		running, err := s.Running()
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

type linuxSteam struct{ procDir string }

func (l linuxSteam) Running() (bool, error) { return runningIn(l.procDir), nil }

// Close runs `steam -shutdown` (or the Flatpak equivalent); Steam handles it
// asynchronously.
func (linuxSteam) Close() error {
	cmd, err := shutdownCmd(exec.LookPath)
	if err != nil {
		return err
	}
	hide(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap the child
	return nil
}

// shutdownCmd picks the command that asks Steam to quit, without running it:
// `steam` if on PATH, else Flatpak Steam via `flatpak run`. lookPath is
// exec.LookPath in production.
func shutdownCmd(lookPath func(string) (string, error)) (*exec.Cmd, error) {
	if _, err := lookPath("steam"); err == nil {
		return exec.Command("steam", "-shutdown"), nil
	}
	if _, err := lookPath("flatpak"); err == nil {
		return exec.Command("flatpak", "run", "com.valvesoftware.Steam", "-shutdown"), nil
	}
	return nil, errors.New("could not find steam or flatpak on PATH: close Steam manually")
}

// runningIn scans procDir/*/comm for an exact `steam` or `steam.exe`. comm is
// truncated to 15 chars by the kernel, so `steamwebhelper` is a different name
// and does not match.
func runningIn(procDir string) bool {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || !isNumeric(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(procDir, e.Name(), "comm"))
		if err != nil {
			continue // process exited meanwhile, or not readable
		}
		switch strings.TrimSpace(string(b)) {
		case "steam", "steam.exe":
			return true
		}
	}
	return false
}

type windowsSteam struct{}

func (windowsSteam) Running() (bool, error) {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq steam.exe", "/NH")
	hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(string(out)), "steam.exe"), nil
}

func (windowsSteam) Close() error {
	cmd := exec.Command("taskkill", "/F", "/IM", "steam.exe")
	hide(cmd)
	return cmd.Run()
}

type unsupportedSteam struct{}

func (unsupportedSteam) Running() (bool, error) { return false, nil }
func (unsupportedSteam) Close() error {
	return errors.New("closing Steam is not supported on " + runtime.GOOS)
}
