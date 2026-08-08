// SPDX-Licence-Identifier: EUPL-1.2

package angular

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webkit "dappco.re/go/render/display/webkit"
)

// frontendDir is the Angular source tree this example hosts.
const frontendDir = "ui/src"

// TestSeam_FrontendCallsResolve is the drift gate, and the reason this
// example exists in-tree.
//
// Every Call.ByName literal in the Angular sources is resolved against
// the Go services the host actually binds. Rename RunnerService, drop a
// method, or typo a call string, and this fails at `go test` time —
// instead of at click time in a WebView, as a rejected promise with no
// indication of which side moved.
func TestSeam_FrontendCallsResolve(t *testing.T) {
	sites, err := webkit.ScanCallByName(os.DirFS(frontendDir))
	if err != nil {
		t.Fatalf("scan %s: %v", frontendDir, err)
	}
	if len(sites.Names) == 0 {
		t.Fatalf("no Call.ByName literals found under %s — the gate is not actually checking anything", frontendDir)
	}

	missing := webkit.UnresolvedBindingNames(sites.Names, Services()...)
	for _, name := range missing {
		t.Errorf("frontend calls %q, which no bound service exposes (called from %v)", name, sites.Files[name])
	}
	if len(missing) > 0 {
		t.Fatalf("%d/%d call strings do not resolve", len(missing), len(sites.Names))
	}
	t.Logf("%d Call.ByName literals resolve against %d bound services", len(sites.Names), len(Services()))
}

// TestSeam_BothReceiverShapesAreCalled asserts the example keeps
// exercising BOTH receiver names found in the wild. If a refactor
// collapses them to one, the gate above would still pass while quietly
// losing the coverage that made it worth having.
func TestSeam_BothReceiverShapesAreCalled(t *testing.T) {
	sites, err := webkit.ScanCallByName(os.DirFS(frontendDir))
	if err != nil {
		t.Fatalf("scan %s: %v", frontendDir, err)
	}

	for _, receiver := range []string{".RunnerService.", ".StatsWailsService."} {
		found := false
		for _, name := range sites.Names {
			if strings.Contains(name, receiver) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no frontend call uses the %q receiver shape", receiver)
		}
	}
}

// TestSeam_NoUnverifiableCalls keeps the gate honest. A call whose name
// is assembled at runtime cannot be checked, so the example must not
// contain one — otherwise the pass above covers less than it appears to.
func TestSeam_NoUnverifiableCalls(t *testing.T) {
	sites, err := webkit.ScanCallByName(os.DirFS(frontendDir))
	if err != nil {
		t.Fatalf("scan %s: %v", frontendDir, err)
	}
	for _, file := range sites.Dynamic {
		t.Errorf("%s builds a binding name at runtime; the gate cannot verify it — use a full string literal", file)
	}
}

// TestSeam_EveryBindingIsReachable is the converse direction: a Go
// method the frontend never calls is either dead or a gap in the
// example. Failing here forces the choice to be explicit.
func TestSeam_EveryBindingIsReachable(t *testing.T) {
	sites, err := webkit.ScanCallByName(os.DirFS(frontendDir))
	if err != nil {
		t.Fatalf("scan %s: %v", frontendDir, err)
	}
	called := make(map[string]bool, len(sites.Names))
	for _, name := range sites.Names {
		called[name] = true
	}

	for _, service := range Services() {
		for _, name := range webkit.BindingNames(service) {
			if !called[name] {
				t.Errorf("%s is bound but never called from the frontend — exercise it or unbind it", name)
			}
		}
	}
}

// TestSeam_ConfigWiring asserts the host config binds both services,
// registers the window and installs an asset handler + middleware. This
// is the part of start-up that can be checked without a WebView.
func TestSeam_ConfigWiring(t *testing.T) {
	cfg, err := Config(HostOptions{Dev: true})
	if err != nil {
		t.Fatalf("Config: %v", err)
	}

	if len(cfg.Bindings) != 2 {
		t.Fatalf("Bindings = %d, want 2 (both receiver shapes)", len(cfg.Bindings))
	}
	if cfg.Assets.Handler == nil {
		t.Fatal("no asset handler configured")
	}
	if cfg.Assets.Middleware == nil {
		t.Fatal("no middleware configured — the CSP and the wails carve-out would both be missing")
	}
	if len(cfg.WindowRegistry) != 1 || cfg.WindowRegistry[0].Name != "main" {
		t.Fatalf("WindowRegistry = %+v, want one window named main", cfg.WindowRegistry)
	}
	if cfg.Mode != webkit.ModeSingleWindow {
		t.Fatalf("Mode = %v, want ModeSingleWindow", cfg.Mode)
	}
}

// TestSeam_WindowStateIsAppScoped asserts an explicit state directory
// reaches both persistence paths.
//
// The default is a shared location, and two apps writing one
// window_state.json is what corrupted it before go/v0.20.3 made the
// saves atomic. Atomic writes stop the file being torn; they do not stop
// two apps disagreeing about its contents. Scoping the path per app is
// the other half, so the example does it and pins it here.
func TestSeam_WindowStateIsAppScoped(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Config(HostOptions{Dev: true, StateDir: dir})
	if err != nil {
		t.Fatalf("Config: %v", err)
	}

	if want := filepath.Join(dir, "window_state.json"); cfg.WindowStatePath != want {
		t.Fatalf("WindowStatePath = %q, want %q", cfg.WindowStatePath, want)
	}
	if want := filepath.Join(dir, "window_layout.json"); cfg.WindowLayoutPath != want {
		t.Fatalf("WindowLayoutPath = %q, want %q", cfg.WindowLayoutPath, want)
	}
	if cfg.WindowStatePath == cfg.WindowLayoutPath {
		t.Fatal("state and layout share a path; they are separate stores")
	}
}

// TestSeam_CSPPermitsEveryTransport asserts the policy names every port
// the renderer talks to, in BOTH its http:// and ws:// form. Omitting
// the socket form yields a policy that passes page load and then kills
// the event channel — lthn/desktop #93.
func TestSeam_CSPPermitsEveryTransport(t *testing.T) {
	policy := webkit.CSP(webkit.CSPOptions{
		Transports: []string{
			"http://localhost:" + itoa(TransportPort),
			"http://localhost:" + itoa(BindingPort),
		},
	})

	for _, source := range []string{
		"http://localhost:9099", "ws://localhost:9099",
		"http://localhost:9199", "ws://localhost:9199",
	} {
		if !strings.Contains(policy, source) {
			t.Errorf("policy omits %q\npolicy: %s", source, policy)
		}
	}
	if strings.Contains(policy, "'unsafe-eval'") {
		t.Errorf("production policy carries the dev relaxation:\n%s", policy)
	}
}

// TestSeam_CSPDevAddsOnlyDevRelaxations asserts the dev policy is a
// superset of production plus exactly the dev server and 'unsafe-eval',
// so a dev-only allowance cannot leak into a release build unnoticed.
func TestSeam_CSPDevAddsOnlyDevRelaxations(t *testing.T) {
	prod, err := Config(HostOptions{Dev: false, AssetDir: writeFakeBuild(t)})
	if err != nil {
		t.Fatalf("Config(prod): %v", err)
	}
	dev, err := Config(HostOptions{Dev: true})
	if err != nil {
		t.Fatalf("Config(dev): %v", err)
	}

	prodPolicy := policyFrom(t, prod)
	devPolicy := policyFrom(t, dev)

	if strings.Contains(prodPolicy, DevServerOrigin) {
		t.Errorf("production policy names the dev server:\n%s", prodPolicy)
	}
	if !strings.Contains(devPolicy, DevServerOrigin) {
		t.Errorf("dev policy omits the dev server:\n%s", devPolicy)
	}
	if !strings.Contains(devPolicy, "ws://localhost:9245") {
		t.Errorf("dev policy omits the HMR socket:\n%s", devPolicy)
	}
	if !strings.Contains(devPolicy, "'unsafe-eval'") {
		t.Errorf("dev policy omits 'unsafe-eval'; an unbundled build will not boot:\n%s", devPolicy)
	}
}

// TestSeam_AssetRouting asserts the routing an Angular build needs, over
// the real handler the host installs: the shell for deep links, a 404
// for a missing bundle, and no answer at all inside /wails.
func TestSeam_AssetRouting(t *testing.T) {
	cfg, err := Config(HostOptions{AssetDir: writeFakeBuild(t)})
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	handler := cfg.Assets.Handler

	cases := []struct {
		name   string
		path   string
		status int
	}{
		{"root", "/", http.StatusOK},
		{"hash_route_entry", "/index.html", http.StatusOK},
		{"path_deep_link", "/about", http.StatusOK},
		{"nested_deep_link", "/jobs/build", http.StatusOK},
		{"real_bundle", "/main-ABCD1234.js", http.StatusOK},
		{"missing_bundle", "/chunk-DEADBEEF.js", http.StatusNotFound},
		{"wails_runtime", "/wails/runtime.js", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("%s = %d, want %d", tc.path, w.Code, tc.status)
			}
		})
	}
}

// TestSeam_MiddlewareServesRuntimeAndPolicy asserts the assembled
// middleware chain does both jobs at once: the runtime keeps its URL
// space, and every response carries the policy.
func TestSeam_MiddlewareServesRuntimeAndPolicy(t *testing.T) {
	cfg, err := Config(HostOptions{AssetDir: writeFakeBuild(t)})
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	runtime := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("runtime"))
	})
	handler := cfg.Assets.Middleware(runtime)

	t.Run("runtime_reaches_wails", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil))
		if w.Body.String() != "runtime" {
			t.Fatalf("body = %q, want the runtime's response", w.Body.String())
		}
		if w.Header().Get(webkit.CSPHeader) == "" {
			t.Fatal("no policy on the runtime response")
		}
	})

	t.Run("app_reaches_assets", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/about", nil))
		if !strings.Contains(w.Body.String(), "app-root") {
			t.Fatalf("body = %q, want the application shell", w.Body.String())
		}
		if w.Header().Get(webkit.CSPHeader) == "" {
			t.Fatal("no policy on the asset response")
		}
	})
}

// TestSeam_MissingBuildIsAStartupError asserts an unbuilt frontend fails
// at start-up with an actionable message, rather than opening a window
// that renders nothing.
func TestSeam_MissingBuildIsAStartupError(t *testing.T) {
	_, err := Config(HostOptions{AssetDir: filepath.Join(t.TempDir(), "never-built")})
	if err == nil {
		t.Fatal("expected a start-up error for a missing build")
	}
	for _, hint := range []string{"npm run build", DevServerOrigin} {
		if !strings.Contains(err.Error(), hint) {
			t.Errorf("error message omits %q — it should say how to fix it: %v", hint, err)
		}
	}
}

// writeFakeBuild lays out a directory shaped like `ng build` output, so
// the asset tests run without npm.
func writeFakeBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"index.html":          `<!doctype html><html><body><app-root></app-root></body></html>`,
		"main-ABCD1234.js":    `console.log('bundle');`,
		"styles-EF012345.css": `:root{}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// policyFrom extracts the policy the config's middleware sets.
func policyFrom(t *testing.T, cfg webkit.GuiConfig) string {
	t.Helper()
	handler := cfg.Assets.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil))

	policy := w.Header().Get(webkit.CSPHeader)
	if policy == "" {
		t.Fatal("middleware set no policy")
	}
	return policy
}
