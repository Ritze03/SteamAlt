// Package lookup resolves Steam app ids to game names and fetches account
// avatars. Both jobs are best-effort: nothing here is fatal, and the caller
// shows fallbacks (the "App <id>" name, the letter tile) on failure.
//
// Names are resolved in tiers (D6): a caller-supplied local map (appmanifest
// names), then the disk cache, then the Steam store appdetails endpoint, then
// the "App <id>" fallback. Avatars (D19) come from the Steam CDN and are cached
// on disk. Everything is stdlib only.
package lookup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.Decode
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultStoreURL  = "https://store.steampowered.com/api/appdetails"
	defaultAvatarURL = "https://avatars.cloudflare.steamstatic.com/"
	defaultTimeout   = 5 * time.Second
	maxConcurrent    = 4
	maxNameBody      = 4 << 20 // cap on an appdetails response
	maxAvatarBody    = 2 << 20 // cap on an avatar download
	namesFile        = "names.json"
	zeroHash         = "0000000000000000000000000000000000000000"
)

var (
	appIDRe   = regexp.MustCompile(`^[0-9]+$`)
	avatarRe  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	errRate   = errors.New("lookup: rate limited (HTTP 429)")
	errNoName = errors.New("lookup: no name")
)

// DefaultCacheDir returns os.UserCacheDir()/steamalt.
func DefaultCacheDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "steamalt"), nil
}

// FallbackName is the name used for an id that could not be resolved.
func FallbackName(id string) string { return "App " + id }

// Client resolves names and avatars. Set the exported fields before first use;
// a Client is safe for concurrent use afterwards.
type Client struct {
	CacheDir  string       // names.json and avatars/ live here
	StoreURL  string       // default "https://store.steampowered.com/api/appdetails"
	AvatarURL string       // default "https://avatars.cloudflare.steamstatic.com/" (hash + "_full.jpg" appended)
	HTTP      *http.Client // default timeout 5s

	mu sync.Mutex // guards names.json read-modify-write within Names
}

// New returns a Client with default endpoints and a 5 s timeout.
func New(cacheDir string) *Client {
	return &Client{
		CacheDir:  cacheDir,
		StoreURL:  defaultStoreURL,
		AvatarURL: defaultAvatarURL,
		HTTP:      &http.Client{Timeout: defaultTimeout},
	}
}

type cacheEntry struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}

func (c *Client) namesPath() string { return filepath.Join(c.CacheDir, namesFile) }

// loadCache reads names.json; any problem yields an empty cache.
func (c *Client) loadCache() map[string]cacheEntry {
	m := map[string]cacheEntry{}
	b, err := os.ReadFile(c.namesPath())
	if err != nil || json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]cacheEntry{}
	}
	return m
}

func (c *Client) saveCache(m map[string]cacheEntry) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(c.namesPath(), b)
}

// writeFileAtomic writes data to a temp file in the same dir, then renames.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp)
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}

// Names resolves app ids to names. local is tier 1 (appID -> name from
// appmanifests). onName(id, name) is called exactly once per distinct id, with
// "App <id>" for anything unresolved (including non-numeric ids).
//
// Local and cache hits are reported synchronously, before any network
// request, from the calling goroutine. Network results are reported from
// worker goroutines (up to 4 at once), so onName must be safe for concurrent
// use. Names blocks until every id is done, so run it in its own goroutine.
// The cache is written once at the end.
//
// Definitive "no such app" answers (success:false) are cached permanently;
// transient failures (timeout, network error, 5xx, 429) are not. After a 429
// no further requests are sent during this call.
func (c *Client) Names(ctx context.Context, ids []string, local map[string]string, onName func(id, name string)) {
	c.mu.Lock()
	cache := c.loadCache()
	c.mu.Unlock()

	var pending []string
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		switch {
		case !appIDRe.MatchString(id):
			onName(id, FallbackName(id))
		case strings.TrimSpace(local[id]) != "":
			onName(id, strings.TrimSpace(local[id]))
		default:
			if e, ok := cache[id]; ok && (!e.OK || e.Name != "") {
				if e.OK {
					onName(id, e.Name)
				} else {
					onName(id, FallbackName(id))
				}
				continue
			}
			pending = append(pending, id)
		}
	}
	if len(pending) == 0 {
		return
	}

	var (
		wg          sync.WaitGroup
		resMu       sync.Mutex // guards cache writes below
		fresh       = map[string]cacheEntry{}
		rateLimited atomic.Bool
		sem         = make(chan struct{}, maxConcurrent)
	)
	for _, id := range pending {
		if rateLimited.Load() || ctx.Err() != nil {
			onName(id, FallbackName(id))
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			onName(id, FallbackName(id))
			continue
		}
		if rateLimited.Load() { // set while we waited for a slot
			<-sem
			onName(id, FallbackName(id))
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			name, err := c.fetchName(ctx, id)
			switch {
			case err == nil:
				resMu.Lock()
				fresh[id] = cacheEntry{Name: name, OK: true}
				resMu.Unlock()
				onName(id, name)
			case errors.Is(err, errNoName):
				resMu.Lock()
				fresh[id] = cacheEntry{OK: false}
				resMu.Unlock()
				onName(id, FallbackName(id))
			default:
				if errors.Is(err, errRate) {
					rateLimited.Store(true)
				}
				onName(id, FallbackName(id))
			}
		}(id)
	}
	wg.Wait()

	if len(fresh) > 0 {
		// Reload so concurrent Names calls don't clobber each other's entries.
		c.mu.Lock()
		merged := c.loadCache()
		for id, e := range fresh {
			merged[id] = e
		}
		_ = c.saveCache(merged) // best-effort
		c.mu.Unlock()
	}
}

// fetchName asks the store for one id. nil error means a name; errNoName is
// a definitive failure; anything else is transient.
func (c *Client) fetchName(ctx context.Context, id string) (string, error) {
	u, err := url.Parse(c.StoreURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("appids", id)
	q.Set("filters", "basic")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errRate
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lookup: appdetails HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxNameBody))
	if err != nil {
		return "", err
	}
	var parsed map[string]struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"` // sometimes [] instead of an object
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	entry, ok := parsed[id]
	if !ok {
		return "", fmt.Errorf("lookup: appdetails response lacks id %s", id)
	}
	if !entry.Success {
		return "", errNoName
	}
	var data struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(entry.Data, &data) != nil || strings.TrimSpace(data.Name) == "" {
		return "", errNoName
	}
	return strings.TrimSpace(data.Name), nil
}

// Avatar returns the decoded avatar for a 40-hex hash, from
// <CacheDir>/avatars/<hash>.jpg or from the network (cached on success). An
// invalid or all-zero hash returns an error without any request. The caller
// shows the letter tile on error.
func (c *Client) Avatar(ctx context.Context, hash string) (image.Image, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if !avatarRe.MatchString(hash) || hash == zeroHash {
		return nil, fmt.Errorf("lookup: invalid avatar hash %q", hash)
	}
	path := filepath.Join(c.CacheDir, "avatars", hash+".jpg")

	if b, err := os.ReadFile(path); err == nil {
		if img, _, err := image.Decode(bytes.NewReader(b)); err == nil {
			return img, nil
		}
		os.Remove(path) // corrupt cache entry; refetch
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.AvatarURL+hash+"_full.jpg", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lookup: avatar HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBody))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	_ = writeFileAtomic(path, b) // best-effort
	return img, nil
}
