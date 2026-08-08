// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	core "dappco.re/go"
)

// DefaultIndex is the document an SPA handler falls back to for a
// route the build produced no file for.
const DefaultIndex = "index.html"

// wailsPrefix is the URL space the wails runtime owns. An SPA handler
// must never answer inside it — see SPAOptions for why.
const wailsPrefix = "/wails"

// SPAOptions configures SPAHandler.
//
// Exactly one source must be set: FS for a production build compiled
// into the binary, or DevServer for a live framework dev server. Both
// or neither is a configuration error, because silently preferring one
// is how a release binary ends up serving from a dev server that is not
// running.
type SPAOptions struct {
	// FS is the built frontend, rooted at the directory holding
	// index.html. For an Angular application builder that is
	// dist/<project>/browser — NOT dist/<project>. Pair with
	// fs.Sub(embedded, "ui/dist/app/browser").
	FS fs.FS

	// DevServer is the origin of a running framework dev server, e.g.
	// "http://localhost:9245" for an `ng serve`. When set, every request
	// the handler owns is reverse-proxied there, so HMR, source maps and
	// unbundled ES modules all work. Ignored when FS is set.
	DevServer string

	// Index is the fallback document name. Empty means DefaultIndex.
	Index string
}

// SPAHandler serves a single-page application — an Angular build in the
// house case — from either an embedded filesystem or a live dev server,
// with the routing behaviour a hosted WebView actually needs.
//
// Three behaviours distinguish it from http.FileServer, and each one is
// a bug that has already been paid for:
//
//  1. A request for a path the build produced no file for falls back to
//     index.html with HTTP 200, so a path-routed deep link survives a
//     reload. Hash routing (#/settings) never sends the fragment to the
//     server and so works either way — but an app that later drops the
//     hash does not silently break.
//
//  2. A request that LOOKS like a build asset — anything with a file
//     extension, /main-A1B2C3.js, /styles.css, /icon.svg — gets a plain
//     404 when it is missing, never the index fallback. Returning HTML
//     for a missing chunk is what produces the notorious
//     "Uncaught SyntaxError: Unexpected token '<'" a whole page-load
//     later, with nothing pointing at the real cause.
//
//  3. Requests under /wails/* are refused outright. That space belongs
//     to the runtime; serving index.html there would hand HTML to the
//     script tag loading the runtime. Wire WailsHTTPMiddleware so the
//     runtime sees those requests first — this refusal is the backstop
//     that makes a missing middleware loud instead of baffling.
//
//     assets, err := webkit.SPAHandler(webkit.SPAOptions{FS: dist})
//     cfg := webkit.GuiConfig{Assets: webkit.AssetOptions{
//     Handler:    assets,
//     Middleware: webkit.WailsHTTPMiddleware(assets),
//     }}
//
// Returns an error when neither or both sources are configured, when
// DevServer is not a parseable absolute URL, or when FS carries no
// index document — all of which are start-up-time mistakes that would
// otherwise present as a blank window.
func SPAHandler(opts SPAOptions) (http.Handler, error) {
	index := opts.Index
	if index == "" {
		index = DefaultIndex
	}

	hasFS := opts.FS != nil
	hasDev := strings.TrimSpace(opts.DevServer) != ""
	switch {
	case hasFS && hasDev:
		return nil, core.E("webkit.SPAHandler", "both FS and DevServer set: pick the embedded build or the dev server, not both", nil)
	case !hasFS && !hasDev:
		return nil, core.E("webkit.SPAHandler", "neither FS nor DevServer set: nothing to serve", nil)
	case hasDev:
		return devServerHandler(opts.DevServer)
	}

	if _, err := fs.Stat(opts.FS, index); err != nil {
		return nil, core.E("webkit.SPAHandler", "no "+index+" in the asset filesystem: point FS at the directory containing index.html (Angular: dist/<project>/browser)", err)
	}
	return &spaFS{fsys: opts.FS, index: index, files: http.FileServer(http.FS(opts.FS))}, nil
}

// spaFS serves an embedded build with SPA fallback semantics.
type spaFS struct {
	fsys  fs.FS
	index string
	files http.Handler
}

func (h *spaFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWailsPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	// The root and the index document are both answered directly.
	// http.FileServer would 301 /index.html to ./ instead, which costs a
	// round-trip on every boot of a WebView pointed at the index — and
	// makes the "is the app loading?" trace harder to read than it needs
	// to be.
	if name == "" || name == h.index {
		h.serveIndex(w, r)
		return
	}

	if info, err := fs.Stat(h.fsys, name); err == nil && !info.IsDir() {
		h.files.ServeHTTP(w, r)
		return
	}

	// A missing asset is a 404, never the index document — see
	// SPAHandler's contract.
	if path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}
	h.serveIndex(w, r)
}

// serveIndex writes the fallback document with HTTP 200. The index is
// never cached: it names the hashed bundles, so a stale copy pins the
// WebView to a build that no longer exists on disk.
func (h *spaFS) serveIndex(w http.ResponseWriter, r *http.Request) {
	file, err := h.fsys.Open(h.index)
	if err != nil {
		http.Error(w, "index unavailable", http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, file)
}

// devServerHandler reverse-proxies to a framework dev server so HMR,
// websockets and unbundled modules pass through untouched.
func devServerHandler(origin string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimSpace(origin))
	if err != nil {
		return nil, core.E("webkit.SPAHandler", "DevServer is not a valid URL: "+origin, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, core.E("webkit.SPAHandler", "DevServer needs a scheme and host, e.g. http://localhost:9245, got: "+origin, nil)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// The dev server's own fallback handles unknown routes, so no SPA
	// rewriting happens here — proxying it verbatim is what keeps HMR
	// and the vite client working.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWailsPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

// isWailsPath reports whether a URL path falls inside the runtime's
// reserved space. Matches /wails and /wails/... but not /wailsfoo.
func isWailsPath(urlPath string) bool {
	return urlPath == wailsPrefix || strings.HasPrefix(urlPath, wailsPrefix+"/")
}
