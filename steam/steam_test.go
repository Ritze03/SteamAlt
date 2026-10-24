package steam

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// ---- VDF ----

func TestParseVDF(t *testing.T) {
	tests := []struct {
		name, in string
		path     []string
		want     string
	}{
		{"loginusers", `"users" { "76561197960265729" { "AccountName" "x" "PersonaName" "Alice" } }`,
			[]string{"users", "76561197960265729", "PersonaName"}, "Alice"},
		{"localconfig", `"UserLocalConfigStore" { "friends" { "1" { "name" "n" "avatar" "ab12" "NameHistory" { "0" "Old Name" "1" "Older" } } } }`,
			[]string{"UserLocalConfigStore", "friends", "1", "NameHistory", "0"}, "Old Name"},
		{"appmanifest", "\"AppState\"\n{\n\t\"appid\"\t\t\"620\"\n\t\"name\"\t\t\"Portal 2\"\n}", []string{"AppState", "name"}, "Portal 2"},
		{"libraryfolders new", `"libraryfolders" { "0" { "path" "/x/Steam" "apps" { "620" "1" } } "1" { "path" "/y" } }`,
			[]string{"libraryfolders", "1", "path"}, "/y"},
		{"libraryfolders old", `"LibraryFolders" { "TimeNextStatsReport" "1" "1" "/old/lib" }`,
			[]string{"LibraryFolders", "1"}, "/old/lib"},
		{"escapes", `"a" { "b" "say \"hi\"\tC:\\dir\n" }`, []string{"a", "b"}, "say \"hi\"\tC:\\dir\n"},
		{"unknown escape kept", `"a" { "b" "x\qy" }`, []string{"a", "b"}, `x\qy`},
		{"comments", "// head\n\"a\" { // c\n \"b\" \"v//notcomment\" // tail\n}\n", []string{"a", "b"}, "v//notcomment"},
		{"BOM", "\xef\xbb\xbf\"a\" { \"b\" \"v\" }", []string{"a", "b"}, "v"},
		{"bare tokens", "a { b v }", []string{"a", "b"}, "v"},
		{"case-insensitive", `"A" { "PERSONANAME" "Bob" }`, []string{"a", "personaname"}, "Bob"},
		{"multiline string", "\"a\" { \"b\" \"l1\nl2\" }", []string{"a", "b"}, "l1\nl2"},
		{"empty block", `"a" { }`, []string{"a"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := ParseVDF([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if got := n.Str(tc.path...); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseVDFEmptyBlockIsBlock(t *testing.T) {
	n, err := ParseVDF([]byte(`"a" { }`))
	if err != nil {
		t.Fatal(err)
	}
	if a := n.Get("a"); a == nil || !a.IsBlock() {
		t.Errorf("empty block not a block: %+v", a)
	}
	if n.Find("missing", "deeper") != nil || n.Str("missing") != "" {
		t.Error("missing path should be nil / empty")
	}
}

func TestParseVDFErrors(t *testing.T) {
	bad := map[string]string{
		"invalid utf8":       "\"a\" { \"b\" \"\xff\xfe\" }",
		"unterminated str":   `"a" { "b" "v`,
		"missing close":      `"a" { "b" "v"`,
		"stray close":        `"a" "b" }`,
		"key without value":  `"a" { "b" }`,
		"open where key":     `{ "a" "b" }`,
		"truncated mid-pair": `"a"`,
		"too deep":           strings.Repeat(`"a" {`, 500),
	}
	for name, in := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseVDF([]byte(in)); err == nil {
				t.Error("want error")
			}
		})
	}
}

// ---- Root / config ----

func fakeRoot(t *testing.T, base string, ids ...string) string {
	t.Helper()
	for _, id := range ids {
		mkdirs(t, filepath.Join(base, "userdata", id))
	}
	return base
}

func TestNormalizeRoot(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/a/Steam", "/a/Steam"},
		{"/a/Steam/", "/a/Steam"},
		{"/a/Steam///", "/a/Steam"},
		{"/a/Steam/userdata", "/a/Steam"},
		{"/a/Steam/userdata/", "/a/Steam"},
		{"/a/Steam/UserData", "/a/Steam"},
		{"  /a/Steam/userdata  ", "/a/Steam"},
		{"/a/Steam/userdata/123", "/a/Steam/userdata/123"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range tests {
		if got := NormalizeRoot(tc.in); got != tc.want {
			t.Errorf("NormalizeRoot(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCheckRoot(t *testing.T) {
	tmp := t.TempDir()
	if _, err := CheckRoot(filepath.Join(tmp, "none")); err == nil {
		t.Error("missing root should fail")
	}
	onlyZero := fakeRoot(t, filepath.Join(tmp, "zero"), "0")
	if _, err := CheckRoot(onlyZero); err == nil {
		t.Error("userdata with only 0 should fail")
	}
	nonNum := fakeRoot(t, filepath.Join(tmp, "nonnum"), "config", "abc")
	if _, err := CheckRoot(nonNum); err == nil {
		t.Error("userdata without numeric dirs should fail")
	}
	// A numeric *file* is not an account.
	fileRoot := filepath.Join(tmp, "file")
	write(t, filepath.Join(fileRoot, "userdata", "55"), "x")
	if _, err := CheckRoot(fileRoot); err == nil {
		t.Error("numeric file should not count")
	}
	ok := fakeRoot(t, filepath.Join(tmp, "ok"), "0", "123", "456", "config")
	n, err := CheckRoot(ok)
	if err != nil || n != 2 {
		t.Errorf("CheckRoot = %d, %v; want 2, nil", n, err)
	}
}

func setHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ProgramFiles(x86)", "")
}

func TestDetectRoot(t *testing.T) {
	for _, rel := range []string{
		".local/share/Steam",
		".steam/steam",
		".var/app/com.valvesoftware.Steam/.local/share/Steam",
		".var/app/com.valvesoftware.Steam/data/Steam",
	} {
		t.Run(rel, func(t *testing.T) {
			home := t.TempDir()
			setHome(t, home)
			want := fakeRoot(t, filepath.Join(home, filepath.FromSlash(rel)), "123")
			got, err := DetectRoot()
			if err != nil || got != want {
				t.Errorf("DetectRoot = %q, %v; want %q", got, err, want)
			}
		})
	}

	t.Run("first valid wins, invalid skipped", func(t *testing.T) {
		home := t.TempDir()
		setHome(t, home)
		fakeRoot(t, filepath.Join(home, ".local/share/Steam"), "0") // invalid
		want := fakeRoot(t, filepath.Join(home, ".steam/steam"), "9")
		if got, err := DetectRoot(); err != nil || got != want {
			t.Errorf("DetectRoot = %q, %v; want %q", got, err, want)
		}
	})

	t.Run("none", func(t *testing.T) {
		setHome(t, t.TempDir())
		if _, err := DetectRoot(); err != ErrNotFound {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func setConfigHome(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", d)
	t.Setenv("APPDATA", d)
}

func TestConfigRoundTrip(t *testing.T) {
	setConfigHome(t)
	c, err := LoadConfig()
	if err != nil || c != (Config{}) {
		t.Fatalf("missing file: %+v, %v", c, err)
	}
	want := Config{SteamPath: "/some/Steam"}
	if err := SaveConfig(want); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(want); err != nil { // overwrite works
		t.Fatal(err)
	}
	got, err := LoadConfig()
	if err != nil || got != want {
		t.Fatalf("LoadConfig = %+v, %v; want %+v", got, err, want)
	}
	p, _ := ConfigPath()
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), `"steam_path": "/some/Steam"`) {
		t.Errorf("unexpected JSON: %s", raw)
	}
	if ents, _ := os.ReadDir(filepath.Dir(p)); len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
}

func TestLoadConfigCorrupt(t *testing.T) {
	setConfigHome(t)
	p, _ := ConfigPath()
	write(t, p, "{not json")
	if _, err := LoadConfig(); err == nil {
		t.Error("want error for corrupt config")
	}
}

func TestResolveRoot(t *testing.T) {
	t.Run("saved valid wins, userdata normalised", func(t *testing.T) {
		setConfigHome(t)
		setHome(t, t.TempDir())
		saved := fakeRoot(t, filepath.Join(t.TempDir(), "MySteam"), "42")
		if err := SaveConfig(Config{SteamPath: filepath.Join(saved, "userdata") + string(filepath.Separator)}); err != nil {
			t.Fatal(err)
		}
		root, warn, err := ResolveRoot()
		if err != nil || warn != "" || root != saved {
			t.Errorf("got %q, %q, %v; want %q", root, warn, err, saved)
		}
	})

	t.Run("invalid saved warns and falls back", func(t *testing.T) {
		setConfigHome(t)
		home := t.TempDir()
		setHome(t, home)
		det := fakeRoot(t, filepath.Join(home, ".local", "share", "Steam"), "123")
		if err := SaveConfig(Config{SteamPath: filepath.Join(home, "nope")}); err != nil {
			t.Fatal(err)
		}
		root, warn, err := ResolveRoot()
		if err != nil || root != det || warn == "" {
			t.Errorf("got %q, %q, %v; want %q + warning", root, warn, err, det)
		}
	})

	t.Run("no saved, detect", func(t *testing.T) {
		setConfigHome(t)
		home := t.TempDir()
		setHome(t, home)
		det := fakeRoot(t, filepath.Join(home, ".steam", "steam"), "5")
		root, warn, err := ResolveRoot()
		if err != nil || root != det || warn != "" {
			t.Errorf("got %q, %q, %v", root, warn, err)
		}
	})

	t.Run("nothing found", func(t *testing.T) {
		setConfigHome(t)
		setHome(t, t.TempDir())
		_ = SaveConfig(Config{SteamPath: "/definitely/not/here"})
		root, warn, err := ResolveRoot()
		if err != ErrNotFound || root != "" || warn == "" {
			t.Errorf("got %q, %q, %v", root, warn, err)
		}
	})
}

// ---- Accounts ----

const (
	id1 = "1000" // in loginusers
	id2 = "2000" // fallback to localconfig
	id3 = "3000" // unknown
	id4 = "4000" // invalid-UTF-8 localconfig, not in loginusers
	hex = "0123456789abcdef0123456789abcdef01234567"
)

func id64(id string) string {
	// 76561197960265728 + id, kept literal for the fixture.
	return map[string]string{
		"1000": "76561197960266728",
		"2000": "76561197960267728",
		"3000": "76561197960268728",
		"4000": "76561197960269728",
	}[id]
}

func localconfig(id, avatar, name string) string {
	s := `"UserLocalConfigStore" { "friends" { "` + id + `" { "name" "ignored" `
	if avatar != "" {
		s += `"avatar" "` + avatar + `" `
	}
	if name != "" {
		s += `"NameHistory" { "0" "` + name + `" "1" "other" } `
	}
	return s + `} "99999" { "avatar" "ffff" } } }`
}

func accountsFixture(t *testing.T) string {
	root := t.TempDir()
	write(t, filepath.Join(root, "config", "loginusers.vdf"), "\xef\xbb\xbf"+`"users" {
		"`+id64(id1)+`" { "AccountName" "a1" "personaname" "zed" }
		"`+id64(id2)+`" { "AccountName" "a2" }
	}`)
	mkdirs(t, filepath.Join(root, "userdata", "0", "config"), filepath.Join(root, "userdata", "abc"))
	write(t, filepath.Join(root, "userdata", "0", "config", "localconfig.vdf"), localconfig("0", hex, "Zero"))
	write(t, filepath.Join(root, "userdata", id1, "config", "localconfig.vdf"), localconfig(id1, hex, "OldOne"))
	write(t, filepath.Join(root, "userdata", id2, "config", "localconfig.vdf"), localconfig(id2, "0000000000000000000000000000000000000000", "alice"))
	mkdirs(t, filepath.Join(root, "userdata", id3))
	write(t, filepath.Join(root, "userdata", id4, "config", "localconfig.vdf"), "\"a\" { \"b\" \"\xff\" }")
	return root
}

func TestAccounts(t *testing.T) {
	root := accountsFixture(t)
	got, err := Accounts(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Account{
		{ID: id2, Name: "alice", AvatarHash: ""}, // NameHistory fallback; all-zero avatar dropped
		{ID: id3, Name: UnknownName},             // no files at all
		{ID: id4, Name: UnknownName},             // invalid UTF-8
		{ID: id1, Name: "zed", AvatarHash: hex},  // loginusers wins over NameHistory
	}
	// Sorted case-insensitively by name: "(unknown)" sorts before letters.
	want = []Account{want[1], want[2], want[0], want[3]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestAccountsNoUserdata(t *testing.T) {
	if _, err := Accounts(t.TempDir()); err == nil {
		t.Error("want error")
	}
}

func TestAccountsBadLoginusers(t *testing.T) {
	root := accountsFixture(t)
	write(t, filepath.Join(root, "config", "loginusers.vdf"), "\"users\" { \"x\" \"\xff\" }")
	got, err := Accounts(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.ID == id1 && a.Name != "OldOne" {
			t.Errorf("id1 name = %q, want localconfig fallback", a.Name)
		}
	}
}

func TestAccountsSortCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	for id, n := range map[string]string{"10": "bob", "9": "Alice", "8": "alice", "7": "Carl"} {
		write(t, filepath.Join(root, "userdata", id, "config", "localconfig.vdf"), localconfig(id, "", n))
	}
	got, _ := Accounts(root)
	var order []string
	for _, a := range got {
		order = append(order, a.ID)
	}
	// alice(8) and Alice(9) tie case-insensitively -> by numeric ID.
	if want := []string{"8", "9", "10", "7"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestCleanAvatar(t *testing.T) {
	for in, want := range map[string]string{
		"":         "",
		"0000":     "",
		"ABCDEF01": "abcdef01",
		"zz12":     "",
		"ab/../cd": "",
		hex:        hex,
	} {
		if got := cleanAvatar(in); got != want {
			t.Errorf("cleanAvatar(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- Games ----

func TestGames(t *testing.T) {
	root := t.TempDir()
	mkdirs(t,
		filepath.Join(root, "userdata", "1", "620"), filepath.Join(root, "userdata", "1", "7"),
		filepath.Join(root, "userdata", "1", "760"), filepath.Join(root, "userdata", "1", "1000"),
		filepath.Join(root, "userdata", "1", "99"), filepath.Join(root, "userdata", "1", "config"),
		filepath.Join(root, "userdata", "1", "ugcmsgcache"),
		filepath.Join(root, "userdata", "2", "620"), filepath.Join(root, "userdata", "2", "1000"),
		filepath.Join(root, "userdata", "2", "555"),
	)
	write(t, filepath.Join(root, "userdata", "1", "12345"), "file, not a dir")

	got, err := Games(root, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	want := []Game{
		{"7", false}, {"99", false}, {"620", true}, {"760", false}, {"1000", true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	got, err = Games(root, "1", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range got {
		if g.Overwrites {
			t.Errorf("dst empty but %s marked", g.AppID)
		}
	}
	if len(got) != 5 {
		t.Errorf("len = %d, want 5", len(got))
	}

	// Destination without any folders: nothing overwrites.
	got, _ = Games(root, "1", "3")
	for _, g := range got {
		if g.Overwrites {
			t.Errorf("missing dst but %s marked", g.AppID)
		}
	}
}

func TestGamesErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := Games(root, "1", ""); err == nil {
		t.Error("missing src should fail")
	}
	for _, c := range [][2]string{{"../x", ""}, {"", ""}, {"1", "../x"}, {"a", "b"}} {
		if _, err := Games(root, c[0], c[1]); err == nil {
			t.Errorf("Games(%q, %q) should fail", c[0], c[1])
		}
	}
}

// ---- InstalledNames ----

func manifest(id, name string) string {
	return "\"AppState\"\n{\n\t\"appid\"\t\t\"" + id + "\"\n\t\"name\"\t\t\"" + name + "\"\n\t\"StateFlags\"\t\t\"4\"\n}\n"
}

func TestInstalledNames(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Steam")
	libB := filepath.Join(tmp, "libB")
	missing := filepath.Join(tmp, "gone")

	write(t, filepath.Join(root, "steamapps", "libraryfolders.vdf"),
		`"libraryfolders" {
			"0" { "path" "`+strings.ReplaceAll(root, `\`, `\\`)+`" "apps" { "620" "1" } }
			"1" { "path" "`+strings.ReplaceAll(libB, `\`, `\\`)+`" }
			"2" { "path" "`+strings.ReplaceAll(missing, `\`, `\\`)+`" }
		}`)
	write(t, filepath.Join(root, "steamapps", "appmanifest_620.acf"), manifest("620", `Portal \"2\"`))
	write(t, filepath.Join(libB, "steamapps", "appmanifest_70.acf"), manifest("70", "Half-Life"))
	write(t, filepath.Join(libB, "steamapps", "appmanifest_71.acf"), "\"AppState\" { \"appid\" \"71\" \"name\" \"\xff\" }")
	write(t, filepath.Join(libB, "steamapps", "appmanifest_72.acf"), `"AppState" { "appid" "72" `) // truncated
	write(t, filepath.Join(libB, "steamapps", "appmanifest_73.acf"), `"AppState" { "name" "No Appid" }`)
	write(t, filepath.Join(libB, "steamapps", "notamanifest.acf"), manifest("80", "Nope"))

	got := InstalledNames(root)
	want := map[string]string{
		"620": `Portal "2"`,
		"70":  "Half-Life",
		"73":  "No Appid", // appid taken from the file name
		"7":   "Steam Client",
		"760": "Screenshots",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestInstalledNamesOldLibraryFormat(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Steam")
	lib := filepath.Join(tmp, "old")
	write(t, filepath.Join(root, "steamapps", "libraryfolders.vdf"),
		`"LibraryFolders" { "TimeNextStatsReport" "123" "ContentStatsID" "-5" "1" "`+strings.ReplaceAll(lib, `\`, `\\`)+`" }`)
	write(t, filepath.Join(lib, "steamapps", "appmanifest_10.acf"), manifest("10", "Counter-Strike"))

	got := InstalledNames(root)
	if got["10"] != "Counter-Strike" || len(got) != 3 {
		t.Errorf("got %v", got)
	}
}

func TestInstalledNamesNothing(t *testing.T) {
	got := InstalledNames(filepath.Join(t.TempDir(), "nope"))
	if want := map[string]string{"7": "Steam Client", "760": "Screenshots"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}
