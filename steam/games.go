package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Game is one numeric app folder in an account's userdata.
type Game struct {
	AppID      string
	Overwrites bool // the destination account already has a folder with this AppID
}

// numLess orders digit strings numerically (no leading-zero handling needed:
// shorter is smaller, equal lengths compare lexically).
func numLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func sortNumeric(s []string) {
	sort.Slice(s, func(i, j int) bool { return numLess(s[i], s[j]) })
}

// Games lists the numeric app folders of userdata/<srcID>, sorted
// numerically (this includes 7 and 760). Non-numeric folders (config, ...)
// are never listed. If dstID is non-empty, games whose folder already exists
// in userdata/<dstID> are marked Overwrites. srcID and dstID must be numeric.
func Games(root, srcID, dstID string) ([]Game, error) {
	if !isNumeric(srcID) || (dstID != "" && !isNumeric(dstID)) {
		return nil, fmt.Errorf("invalid account id %q / %q", srcID, dstID)
	}
	ids, err := numericDirs(UserdataDir(root, srcID))
	if err != nil {
		return nil, err
	}
	games := make([]Game, len(ids))
	for i, id := range ids {
		games[i] = Game{AppID: id}
		if dstID != "" {
			_, err := os.Stat(filepath.Join(UserdataDir(root, dstID), id))
			games[i].Overwrites = err == nil
		}
	}
	return games, nil
}

// builtinNames names numeric userdata folders that are not games, so nobody
// has to look them up.
var builtinNames = map[string]string{
	"7":   "Steam Client",
	"760": "Screenshots",
}

// InstalledNames maps app id to game name using the appmanifest_*.acf files
// of the Steam root and every library in libraryfolders.vdf, plus built-in
// names for 7 and 760. It is best-effort: missing libraries and unreadable
// or invalid manifests are skipped, and it never fails.
func InstalledNames(root string) map[string]string {
	names := map[string]string{}
	for _, lib := range libraries(root) {
		dir := filepath.Join(lib, "steamapps")
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			n := e.Name()
			if !strings.HasPrefix(n, "appmanifest_") || !strings.HasSuffix(n, ".acf") {
				continue
			}
			m, err := readVDF(filepath.Join(dir, n))
			if err != nil {
				continue
			}
			id := m.Str("AppState", "appid")
			if !isNumeric(id) {
				id = strings.TrimSuffix(strings.TrimPrefix(n, "appmanifest_"), ".acf")
			}
			if name := m.Str("AppState", "name"); name != "" && isNumeric(id) {
				names[id] = name
			}
		}
	}
	for id, name := range builtinNames {
		if _, ok := names[id]; !ok {
			names[id] = name
		}
	}
	return names
}

// libraries returns the Steam root plus every library path from
// libraryfolders.vdf (new "path" blocks and the old "1" "/path" form),
// de-duplicated. Paths are not checked for existence.
func libraries(root string) []string {
	libs := []string{root}
	seen := map[string]bool{filepath.Clean(root): true}
	for _, f := range []string{
		filepath.Join(root, "steamapps", "libraryfolders.vdf"),
		filepath.Join(root, "config", "libraryfolders.vdf"),
	} {
		n, err := readVDF(f)
		if err != nil {
			continue
		}
		for _, e := range n.Get("libraryfolders").children() {
			if !isNumeric(e.Key) { // skips TimeNextStatsReport, ContentStatsID, ...
				continue
			}
			p := e.Value
			if e.IsBlock() {
				p = e.Str("path")
			}
			if p = filepath.Clean(p); p != "." && !seen[p] {
				seen[p] = true
				libs = append(libs, p)
			}
		}
		break // first readable file wins; both hold the same list
	}
	return libs
}
