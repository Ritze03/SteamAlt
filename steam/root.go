package steam

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNotFound is returned by DetectRoot and ResolveRoot when no valid Steam
// install is found; the UI should then ask the user for a path.
var ErrNotFound = errors.New("no Steam installation found")

// Config is the saved SteamAlt settings file.
type Config struct {
	// SteamPath is the Steam root (the parent of userdata/). Empty = auto-detect.
	SteamPath string `json:"steam_path"`
}

// ConfigPath is os.UserConfigDir()/steamalt/config.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "steamalt", "config"), nil
}

// LoadConfig reads the config file. A missing file is not an error and
// yields the zero Config.
func LoadConfig() (Config, error) {
	var c Config
	p, err := ConfigPath()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

// SaveConfig writes the config atomically (temp file in the same dir, then rename).
// Callers should pass SteamPath through NormalizeRoot first.
func SaveConfig(c Config) error {
	p, err := ConfigPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// NormalizeRoot turns a user-supplied path into a Steam root: it cleans the
// path (dropping trailing separators) and strips a final "userdata" element.
// An empty or blank input yields "".
func NormalizeRoot(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if strings.EqualFold(filepath.Base(p), "userdata") {
		p = filepath.Dir(p)
	}
	return p
}

// isNumeric reports whether s is a non-empty string of ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isDir is true for directories, following symlinks.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// numericDirs lists the numeric subdirectory names of dir, sorted numerically.
func numericDirs(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if isNumeric(e.Name()) && (e.IsDir() || isDir(filepath.Join(dir, e.Name()))) {
			out = append(out, e.Name())
		}
	}
	sortNumeric(out)
	return out, nil
}

// UserdataDir is <root>/userdata/<accountID>.
func UserdataDir(root, accountID string) string {
	return filepath.Join(root, "userdata", accountID)
}

// CheckRoot validates root as a Steam root: <root>/userdata must exist and
// hold at least one numeric account folder other than "0". It returns the
// number of such folders.
func CheckRoot(root string) (accounts int, err error) {
	ids, err := numericDirs(filepath.Join(root, "userdata"))
	if err != nil {
		return 0, fmt.Errorf("no userdata folder in %q: %w", root, err)
	}
	for _, id := range ids {
		if id != "0" {
			accounts++
		}
	}
	if accounts == 0 {
		return 0, fmt.Errorf("%q has no Steam accounts in userdata", root)
	}
	return accounts, nil
}

// candidates lists the Steam roots to try, in priority order.
func candidates() []string {
	var c []string
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
			c = append(c, filepath.Join(pf, "Steam"))
		}
		c = append(c, `C:\Program Files (x86)\Steam`)
	}
	if home != "" {
		if runtime.GOOS == "windows" {
			c = append(c, filepath.Join(home, "AppData", "Local", "Steam"))
		}
		c = append(c,
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
		)
	}
	return c
}

// DetectRoot returns the first candidate that passes CheckRoot, else ErrNotFound.
func DetectRoot() (string, error) {
	for _, c := range candidates() {
		if _, err := CheckRoot(c); err == nil {
			return c, nil
		}
	}
	return "", ErrNotFound
}

// ResolveRoot picks the Steam root: a valid saved path wins; an invalid or
// unreadable one yields a human-readable warning and falls back to
// DetectRoot. If nothing is found the error is ErrNotFound (any warning is
// still returned) and the UI should ask the user for a path.
func ResolveRoot() (root, warning string, err error) {
	cfg, cerr := LoadConfig()
	switch {
	case cerr != nil:
		warning = fmt.Sprintf("Could not read saved settings (%v); using auto-detection.", cerr)
	case cfg.SteamPath != "":
		p := NormalizeRoot(cfg.SteamPath)
		if _, err := CheckRoot(p); err == nil {
			return p, "", nil
		} else {
			warning = fmt.Sprintf("Saved Steam path is not valid (%v); using auto-detection.", err)
		}
	}
	root, err = DetectRoot()
	if err != nil {
		return "", warning, err
	}
	return root, warning, nil
}
