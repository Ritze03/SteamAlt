package lookup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// collector records onName calls thread-safely.
type collector struct {
	mu    sync.Mutex
	names map[string]string
	calls map[string]int
}

func newCollector() *collector {
	return &collector{names: map[string]string{}, calls: map[string]int{}}
}

func (c *collector) on(id, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names[id] = name
	c.calls[id]++
}

// storeServer serves appdetails: ids in names succeed, others return success:false.
// Hits are counted per request.
func storeServer(t *testing.T, names map[string]string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		id := r.URL.Query().Get("appids")
		if strings.Contains(id, ",") {
			http.Error(w, "batch", http.StatusBadRequest)
			return
		}
		if n, ok := names[id]; ok {
			fmt.Fprintf(w, `{%q:{"success":true,"data":{"name":%q,"type":"game"}}}`, id, n)
			return
		}
		fmt.Fprintf(w, `{%q:{"success":false}}`, id)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newClient(t *testing.T, dir, storeURL string) *Client {
	t.Helper()
	c := New(dir)
	c.StoreURL = storeURL
	c.HTTP.Timeout = 2 * time.Second
	return c
}

func TestLocalTierWins(t *testing.T) {
	var hits atomic.Int32
	srv := storeServer(t, map[string]string{"620": "Portal 2 (API)"}, &hits)
	c := newClient(t, t.TempDir(), srv.URL)
	col := newCollector()
	c.Names(context.Background(), []string{"620"}, map[string]string{"620": "Portal 2 (local)"}, col.on)
	if got := col.names["620"]; got != "Portal 2 (local)" {
		t.Fatalf("got %q", got)
	}
	if hits.Load() != 0 {
		t.Fatalf("expected 0 hits, got %d", hits.Load())
	}
}

func TestAPISuccessPersistedAndCached(t *testing.T) {
	var hits atomic.Int32
	srv := storeServer(t, map[string]string{"620": "Portal 2"}, &hits)
	dir := t.TempDir()
	col := newCollector()
	newClient(t, dir, srv.URL).Names(context.Background(), []string{"620"}, nil, col.on)
	if col.names["620"] != "Portal 2" || hits.Load() != 1 {
		t.Fatalf("name=%q hits=%d", col.names["620"], hits.Load())
	}
	b, err := os.ReadFile(filepath.Join(dir, "names.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]cacheEntry
	if err := json.Unmarshal(b, &m); err != nil || m["620"] != (cacheEntry{Name: "Portal 2", OK: true}) {
		t.Fatalf("names.json = %s (err %v)", b, err)
	}

	col2 := newCollector()
	newClient(t, dir, srv.URL).Names(context.Background(), []string{"620"}, nil, col2.on)
	if col2.names["620"] != "Portal 2" || hits.Load() != 1 {
		t.Fatalf("second run: name=%q hits=%d", col2.names["620"], hits.Load())
	}
}

func TestDefinitiveFailureCached(t *testing.T) {
	var hits atomic.Int32
	srv := storeServer(t, nil, &hits)
	dir := t.TempDir()
	col := newCollector()
	newClient(t, dir, srv.URL).Names(context.Background(), []string{"228980"}, nil, col.on)
	if col.names["228980"] != "App 228980" || hits.Load() != 1 {
		t.Fatalf("name=%q hits=%d", col.names["228980"], hits.Load())
	}
	col2 := newCollector()
	newClient(t, dir, srv.URL).Names(context.Background(), []string{"228980"}, nil, col2.on)
	if col2.names["228980"] != "App 228980" || hits.Load() != 1 {
		t.Fatalf("second run: name=%q hits=%d", col2.names["228980"], hits.Load())
	}
}

func TestDataAsEmptyArrayIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"5":{"success":true,"data":[]}}`)
	}))
	defer srv.Close()
	col := newCollector()
	newClient(t, t.TempDir(), srv.URL).Names(context.Background(), []string{"5"}, nil, col.on)
	if col.names["5"] != "App 5" {
		t.Fatalf("got %q", col.names["5"])
	}
}

func TestTransientErrorsNotCached(t *testing.T) {
	var hits atomic.Int32
	var mode atomic.Int32 // 0 = 500, 1 = hang
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if mode.Load() == 0 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	for name, m := range map[string]int32{"500": 0, "timeout": 1} {
		t.Run(name, func(t *testing.T) {
			mode.Store(m)
			dir := t.TempDir()
			c := newClient(t, dir, srv.URL)
			c.HTTP.Timeout = 150 * time.Millisecond
			before := hits.Load()
			col := newCollector()
			c.Names(context.Background(), []string{"620"}, nil, col.on)
			if col.names["620"] != "App 620" {
				t.Fatalf("got %q", col.names["620"])
			}
			if _, err := os.Stat(filepath.Join(dir, "names.json")); err == nil {
				t.Fatal("transient error must not write a cache")
			}
			c2 := newClient(t, dir, srv.URL)
			c2.HTTP.Timeout = 150 * time.Millisecond
			c2.Names(context.Background(), []string{"620"}, nil, newCollector().on)
			if hits.Load()-before != 2 {
				t.Fatalf("expected server hit again on second run, hits=%d", hits.Load()-before)
			}
		})
	}
}

func TestRateLimitStopsRequests(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(30 * time.Millisecond)
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	ids := make([]string, 30)
	for i := range ids {
		ids[i] = fmt.Sprint(1000 + i)
	}
	dir := t.TempDir()
	col := newCollector()
	newClient(t, dir, srv.URL).Names(context.Background(), ids, nil, col.on)
	if h := hits.Load(); h > maxConcurrent {
		t.Fatalf("expected at most %d requests before stopping, got %d", maxConcurrent, h)
	}
	for _, id := range ids {
		if col.names[id] != "App "+id || col.calls[id] != 1 {
			t.Fatalf("id %s: name=%q calls=%d", id, col.names[id], col.calls[id])
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "names.json")); err == nil {
		t.Fatal("429 must not be cached")
	}
}

func TestConcurrencyCapAndSingleCallback(t *testing.T) {
	var inflight, maxSeen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		id := r.URL.Query().Get("appids")
		fmt.Fprintf(w, `{%q:{"success":true,"data":{"name":"Game %s"}}}`, id, id)
	}))
	defer srv.Close()

	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprint(100 + i)
	}
	ids = append(ids, ids[0]) // duplicate must still produce one callback
	col := newCollector()
	newClient(t, t.TempDir(), srv.URL).Names(context.Background(), ids, nil, col.on)
	if m := maxSeen.Load(); m > 4 || m < 2 {
		t.Fatalf("max in-flight = %d, want 2..4", m)
	}
	if len(col.calls) != 20 {
		t.Fatalf("got %d distinct ids", len(col.calls))
	}
	for id, n := range col.calls {
		if n != 1 || col.names[id] != "Game "+id {
			t.Fatalf("id %s: calls=%d name=%q", id, n, col.names[id])
		}
	}
}

func TestBadIDMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := storeServer(t, nil, &hits)
	col := newCollector()
	bad := []string{"../x", "", "12a", "1,2"}
	newClient(t, t.TempDir(), srv.URL).Names(context.Background(), bad, nil, col.on)
	if hits.Load() != 0 {
		t.Fatalf("hits=%d", hits.Load())
	}
	for _, id := range bad {
		if col.names[id] != "App "+id || col.calls[id] != 1 {
			t.Fatalf("id %q: name=%q calls=%d", id, col.names[id], col.calls[id])
		}
	}
}

func TestCorruptCacheTreatedAsEmpty(t *testing.T) {
	var hits atomic.Int32
	srv := storeServer(t, map[string]string{"620": "Portal 2"}, &hits)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "names.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	col := newCollector()
	newClient(t, dir, srv.URL).Names(context.Background(), []string{"620"}, nil, col.on)
	if col.names["620"] != "Portal 2" || hits.Load() != 1 {
		t.Fatalf("name=%q hits=%d", col.names["620"], hits.Load())
	}
	b, _ := os.ReadFile(filepath.Join(dir, "names.json"))
	var m map[string]cacheEntry
	if err := json.Unmarshal(b, &m); err != nil || m["620"].Name != "Portal 2" {
		t.Fatalf("cache not rewritten: %s", b)
	}
}

func TestCancelledContextGivesNoCallbacks(t *testing.T) {
	var hits atomic.Int32
	srv := storeServer(t, map[string]string{"620": "Portal 2"}, &hits)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	col := newCollector()
	newClient(t, t.TempDir(), srv.URL).Names(ctx, []string{"620", "730"}, nil, col.on)
	if len(col.calls) != 0 || hits.Load() != 0 {
		t.Fatalf("calls=%v hits=%d, want no callbacks and no requests after cancel", col.calls, hits.Load())
	}
}

// A cancel while requests are in flight must not turn them into fallback callbacks.
func TestCancelInFlightGivesNoFallbacks(t *testing.T) {
	started := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done() // hang until the client gives up
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	col := newCollector()
	done := make(chan struct{})
	go func() {
		newClient(t, t.TempDir(), srv.URL).Names(ctx, []string{"1", "2", "3", "4", "5", "6"}, nil, col.on)
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Names did not return after cancel")
	}
	col.mu.Lock()
	defer col.mu.Unlock()
	if len(col.calls) != 0 {
		t.Fatalf("callbacks after cancel: %v", col.calls)
	}
}

// ---- avatars ----

const testHash = "0123456789abcdef0123456789abcdef01234567"

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func avatarServer(t *testing.T, body []byte, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/"+testHash+"_full.jpg" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAvatarFetchAndCache(t *testing.T) {
	var hits atomic.Int32
	srv := avatarServer(t, jpegBytes(t), &hits)
	dir := t.TempDir()
	c := New(dir)
	c.AvatarURL = srv.URL + "/"

	img, err := c.Avatar(context.Background(), strings.ToUpper(testHash)) // lowercased internally
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 8 {
		t.Fatalf("bounds %v", img.Bounds())
	}
	if _, err := os.Stat(filepath.Join(dir, "avatars", testHash+".jpg")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
	if _, err := New(dir).Avatar(context.Background(), testHash); err != nil { // default URL must not be hit
		t.Fatalf("cached read: %v", err)
	}
	if _, err := c.Avatar(context.Background(), testHash); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d, want 1", hits.Load())
	}
}

func TestAvatarInvalidHashNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := avatarServer(t, jpegBytes(t), &hits)
	c := New(t.TempDir())
	c.AvatarURL = srv.URL + "/"
	for _, h := range []string{"", "../../etc/passwd", "abc", strings.Repeat("g", 40), strings.Repeat("0", 40), testHash + "0"} {
		if _, err := c.Avatar(context.Background(), h); err == nil {
			t.Errorf("hash %q: expected error", h)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestAvatarBadBodyNotCached(t *testing.T) {
	var hits atomic.Int32
	srv := avatarServer(t, []byte("not a jpeg"), &hits)
	dir := t.TempDir()
	c := New(dir)
	c.AvatarURL = srv.URL + "/"
	if _, err := c.Avatar(context.Background(), testHash); err == nil {
		t.Fatal("expected decode error")
	}
	if _, err := os.Stat(filepath.Join(dir, "avatars", testHash+".jpg")); err == nil {
		t.Fatal("bad body must not be cached")
	}
}

func TestAvatarCorruptCacheRefetched(t *testing.T) {
	var hits atomic.Int32
	srv := avatarServer(t, jpegBytes(t), &hits)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "avatars"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "avatars", testHash+".jpg"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(dir)
	c.AvatarURL = srv.URL + "/"
	if _, err := c.Avatar(context.Background(), testHash); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestAvatarOversizedRejected(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, maxAvatarPx+1, 8)) // solid: a tiny file, a big image
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, big, nil); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := avatarServer(t, buf.Bytes(), &hits)
	dir := t.TempDir()
	c := New(dir)
	c.AvatarURL = srv.URL + "/"
	if _, err := c.Avatar(context.Background(), testHash); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want a size rejection", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "avatars", testHash+".jpg")); err == nil {
		t.Fatal("oversized avatar must not be cached")
	}
}
