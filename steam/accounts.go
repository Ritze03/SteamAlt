package steam

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// UnknownName is the display name used when no name could be read.
const UnknownName = "(unknown)"

// steamID64Base is added to a 32-bit account id to get the SteamID64.
const steamID64Base = 76561197960265728

// Account is one Steam account found under userdata/.
type Account struct {
	ID         string // folder name (SteamID3 / account id)
	Name       string // PersonaName, else localconfig NameHistory["0"], else UnknownName
	AvatarHash string // 40-hex avatar hash, "" if none or all zeros
}

// Accounts lists the accounts under <root>/userdata (the "0" folder is not an
// account), sorted by Name case-insensitively, then by ID. Unreadable or
// invalid VDF files only degrade names/avatars; an error is returned only if
// userdata/ itself cannot be read.
func Accounts(root string) ([]Account, error) {
	ids, err := numericDirs(filepath.Join(root, "userdata"))
	if err != nil {
		return nil, err
	}
	// SteamID64 -> PersonaName. Failure just leaves the map empty.
	personas := map[string]string{}
	if n, err := readVDF(filepath.Join(root, "config", "loginusers.vdf")); err == nil {
		for _, u := range n.Get("users").children() {
			if name := u.Str("PersonaName"); name != "" {
				personas[u.Key] = name
			}
		}
	}

	var out []Account
	for _, id := range ids {
		if id == "0" {
			continue
		}
		a := Account{ID: id}
		var friend *Node
		if n, err := readVDF(filepath.Join(UserdataDir(root, id), "config", "localconfig.vdf")); err == nil {
			friend = n.Find("UserLocalConfigStore", "friends", id)
		}
		if n, err := strconv.ParseUint(id, 10, 64); err == nil {
			a.Name = personas[strconv.FormatUint(n+steamID64Base, 10)]
		}
		if a.Name == "" {
			a.Name = friend.Str("NameHistory", "0")
		}
		if a.Name == "" {
			a.Name = UnknownName
		}
		a.AvatarHash = cleanAvatar(friend.Str("avatar"))
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return numLess(out[i].ID, out[j].ID)
	})
	return out, nil
}

// children returns n's children; nil-safe.
func (n *Node) children() []*Node {
	if n == nil {
		return nil
	}
	return n.Children
}

// cleanAvatar returns h lower-cased if it is hex and not all zeros, else "".
// (Hex-only so it is safe to put into an avatar URL.)
func cleanAvatar(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	nonZero := false
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
		if c != '0' {
			nonZero = true
		}
	}
	if !nonZero {
		return ""
	}
	return h
}
