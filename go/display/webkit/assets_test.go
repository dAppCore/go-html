// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// angularDist mimics the shape `ng build` leaves in dist/<project>/browser:
// a hash-named entry bundle, a stylesheet, a static asset and the index
// that names them.
func angularDist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":          &fstest.MapFile{Data: []byte(`<!doctype html><app-root></app-root>`)},
		"main-A1B2C3D4.js":    &fstest.MapFile{Data: []byte(`console.log('bundle');`)},
		"styles-E5F6A7B8.css": &fstest.MapFile{Data: []byte(`:root{}`)},
		"assets/logo.svg":     &fstest.MapFile{Data: []byte(`<svg/>`)},
		"favicon.ico":         &fstest.MapFile{Data: []byte{0x00}},
	}
}

// TestSPAHandler_Good covers the routing table an Angular build needs:
// real files served as themselves, the document root and every unknown
// route falling back to index.html so a deep link survives a reload.
func TestSPAHandler_Good(t *testing.T) {
	handler, err := SPAHandler(SPAOptions{FS: angularDist()})
	if err != nil {
		t.Fatalf("SPAHandler: %v", err)
	}

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"root", "/", http.StatusOK, `<!doctype html><app-root></app-root>`},
		{"entry_bundle", "/main-A1B2C3D4.js", http.StatusOK, `console.log('bundle');`},
		{"stylesheet", "/styles-E5F6A7B8.css", http.StatusOK, `:root{}`},
		{"nested_asset", "/assets/logo.svg", http.StatusOK, `<svg/>`},
		{"deep_link", "/settings/profile", http.StatusOK, `<!doctype html><app-root></app-root>`},
		{"hash_route_base", "/index.html", http.StatusOK, `<!doctype html><app-root></app-root>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if got := w.Body.String(); got != tc.wantBody {
				t.Fatalf("body = %q, want %q", got, tc.wantBody)
			}
		})
	}
}

// TestSPAHandler_Bad covers the configuration mistakes that would
// otherwise present as a blank window at run time rather than an error
// at start-up.
func TestSPAHandler_Bad(t *testing.T) {
	cases := []struct {
		name string
		opts SPAOptions
	}{
		{"no_source", SPAOptions{}},
		{"both_sources", SPAOptions{FS: angularDist(), DevServer: "http://localhost:9245"}},
		{"dev_server_not_a_url", SPAOptions{DevServer: "://nope"}},
		{"dev_server_no_scheme", SPAOptions{DevServer: "localhost:9245"}},
		{"fs_without_index", SPAOptions{FS: fstest.MapFS{"main.js": &fstest.MapFile{Data: []byte("x")}}}},
		{"fs_wrong_root", SPAOptions{FS: fstest.MapFS{"browser/index.html": &fstest.MapFile{Data: []byte("x")}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := SPAHandler(tc.opts)
			if err == nil {
				t.Fatal("expected a start-up error, got none")
			}
			if handler != nil {
				t.Fatal("expected no handler alongside the error")
			}
		})
	}
}

// TestSPAHandler_Ugly pins the two fallbacks that must NOT happen. Both
// return HTML where the caller expects something else, and both surface
// far from their cause: a missing chunk becomes "Unexpected token '<'"
// on the next page load, and an index served under /wails hands HTML to
// the script tag loading the runtime.
func TestSPAHandler_Ugly(t *testing.T) {
	handler, err := SPAHandler(SPAOptions{FS: angularDist()})
	if err != nil {
		t.Fatalf("SPAHandler: %v", err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"missing_chunk", "/chunk-DEADBEEF.js"},
		{"missing_stylesheet", "/styles-00000000.css"},
		{"missing_asset", "/assets/absent.png"},
		{"wails_runtime", "/wails/runtime.js"},
		{"wails_websocket", "/wails/ws"},
		{"wails_root", "/wails"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 — never fall back to index.html here", w.Code)
			}
			if body := w.Body.String(); body == `<!doctype html><app-root></app-root>` {
				t.Fatal("served the index document; a script/style request must 404 instead")
			}
		})
	}

	// A path that merely looks like a directory is still a route.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/wailsy/route", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/wailsy/route = %d, want 200 — only /wails and /wails/* are reserved", w.Code)
	}
}

// TestSPAHandler_IndexIsNotCached asserts the fallback document carries
// no-store. The index names the hash-versioned bundles, so a cached copy
// pins the WebView to a build that no longer exists on disk — the
// "app is stale after upgrade until you wipe the WebView data" bug.
func TestSPAHandler_IndexIsNotCached(t *testing.T) {
	handler, err := SPAHandler(SPAOptions{FS: angularDist()})
	if err != nil {
		t.Fatalf("SPAHandler: %v", err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings", nil))

	if got := w.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
}

// TestSPAHandler_DevServer_Good asserts the dev arm proxies verbatim —
// including the routes the embedded arm would rewrite, because the dev
// server owns its own fallback and rewriting would break HMR.
func TestSPAHandler_DevServer_Good(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("vite:" + r.URL.Path))
	}))
	defer upstream.Close()

	handler, err := SPAHandler(SPAOptions{DevServer: upstream.URL})
	if err != nil {
		t.Fatalf("SPAHandler: %v", err)
	}

	for _, path := range []string{"/", "/settings/profile", "/@vite/client", "/main.ts"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if want := "vite:" + path; w.Body.String() != want {
				t.Fatalf("body = %q, want %q", w.Body.String(), want)
			}
		})
	}
}

// TestSPAHandler_DevServer_Ugly asserts the dev arm keeps the same
// /wails refusal as the embedded arm, so the two modes cannot disagree
// about who owns the runtime's URL space.
func TestSPAHandler_DevServer_Ugly(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()

	handler, err := SPAHandler(SPAOptions{DevServer: upstream.URL})
	if err != nil {
		t.Fatalf("SPAHandler: %v", err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — the dev proxy must not answer for the runtime", w.Code)
	}
}

// TestSPAHandler_WithWailsMiddleware asserts the intended composition:
// with WailsHTTPMiddleware in front, the runtime's requests reach the
// runtime and everything else reaches the SPA. The handler's own /wails
// refusal is a backstop for a MISSING middleware, not a replacement.
func TestSPAHandler_WithWailsMiddleware(t *testing.T) {
	assets, err := SPAHandler(SPAOptions{FS: angularDist()})
	if err != nil {
		t.Fatalf("SPAHandler: %v", err)
	}
	runtime := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("runtime"))
	})
	wired := WailsHTTPMiddleware(assets)(runtime)

	cases := []struct{ path, body string }{
		{"/wails/runtime.js", "runtime"},
		{"/main-A1B2C3D4.js", "console.log('bundle');"},
		{"/settings", `<!doctype html><app-root></app-root>`},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			wired.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if w.Body.String() != tc.body {
				t.Fatalf("body = %q, want %q", w.Body.String(), tc.body)
			}
		})
	}
}
