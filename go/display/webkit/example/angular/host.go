// SPDX-Licence-Identifier: EUPL-1.2

package angular

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	core "dappco.re/go"
	webkit "dappco.re/go/render/display/webkit"
	"dappco.re/go/render/display/webkit/pkg/window"
)

// Ports the example uses. They mirror the topology a real hosted
// Angular app runs: the framework dev server on one port, the wails
// asset/WebSocket transport on another, the binding transport on a
// third. Each one that the renderer talks to must appear in the CSP.
const (
	// DevServerPort is where `npm start` in ui/ serves from.
	DevServerPort = 9245
	// TransportPort is the wails asset + WebSocket transport.
	TransportPort = 9099
	// BindingPort is the wails binding transport.
	BindingPort = 9199
)

// DevServerOrigin is the origin the framework dev server listens on.
const DevServerOrigin = "http://localhost:9245"

// BuiltAssetDir is where `npm run build` in ui/ leaves the bundle.
// Angular's application builder writes dist/<project>/browser — the
// browser/ suffix is the part people miss, and pointing the asset
// handler one level too high yields a window that loads nothing with no
// error anywhere.
const BuiltAssetDir = "ui/dist/webkit-angular-example/browser"

// HostOptions selects how the example serves its frontend.
type HostOptions struct {
	// Dev serves through the framework dev server instead of the built
	// bundle, so an edit in ui/src reaches the WebView without a
	// rebuild. The CSP is relaxed to match; see webkit.CSPOptions.
	Dev bool

	// AssetDir overrides BuiltAssetDir. Ignored when Dev is set.
	AssetDir string

	// StateDir is where window state and layouts persist. Empty uses
	// the webkit defaults.
	StateDir string
}

// Config assembles the webkit.GuiConfig for the example, wiring the
// asset handler, the CSP and both bound services.
//
// It is deliberately separate from Run: everything below is pure
// configuration, so a headless test can assert the wiring — that the
// policy names every transport, that the asset handler refuses /wails,
// that both receiver shapes are bound — without opening a WebView.
//
//	cfg, err := angular.Config(angular.HostOptions{Dev: true})
func Config(opts HostOptions) (webkit.GuiConfig, error) {
	runner := NewRunnerService()
	stats := NewStatsWailsService(runner)

	assets, err := assetHandler(opts)
	if err != nil {
		return webkit.GuiConfig{}, err
	}

	policy := webkit.CSPOptions{
		Transports: []string{
			"http://localhost:" + itoa(TransportPort),
			"http://localhost:" + itoa(BindingPort),
		},
	}
	if opts.Dev {
		policy.DevServer = DevServerOrigin
	}

	cfg := webkit.GuiConfig{
		Mode:        webkit.ModeSingleWindow,
		Name:        "webkit-angular-example",
		Description: "go-render display/webkit hosting an Angular application",
		Assets: webkit.AssetOptions{
			Handler: assets,
			// CSP wraps the wails carve-out so the runtime keeps its
			// own URL space AND its responses carry the policy.
			Middleware: webkit.CSPMiddleware(policy, webkit.WailsHTTPMiddleware(assets)),
		},
		Bindings: []webkit.Binding{
			webkit.Bind(runner),
			webkit.Bind(stats),
		},
		WindowRegistry: []*window.Window{{
			Name:   "main",
			Title:  "webkit + Angular",
			URL:    "/",
			Width:  1100,
			Height: 760,
		}},
	}

	if opts.StateDir != "" {
		cfg.WindowStatePath = filepath.Join(opts.StateDir, "window_state.json")
		cfg.WindowLayoutPath = filepath.Join(opts.StateDir, "window_layout.json")
	}
	return cfg, nil
}

// Services returns the bound services as the drift gate needs them:
// the same pointers Config binds, so the gate checks what actually
// ships rather than a parallel list that can rot.
func Services() []any {
	runner := NewRunnerService()
	return []any{runner, NewStatsWailsService(runner)}
}

// assetHandler picks the dev-server proxy or the built bundle.
func assetHandler(opts HostOptions) (http.Handler, error) {
	if opts.Dev {
		return webkit.SPAHandler(webkit.SPAOptions{DevServer: DevServerOrigin})
	}

	dir := opts.AssetDir
	if dir == "" {
		dir = BuiltAssetDir
	}
	if result := core.Stat(filepath.Join(dir, webkit.DefaultIndex)); !result.OK {
		return nil, core.E("angular.assetHandler",
			"no built frontend at "+dir+": run `npm install && npm run build` in ui/, or pass HostOptions{Dev: true} to serve from "+DevServerOrigin,
			result.Err())
	}
	return webkit.SPAHandler(webkit.SPAOptions{FS: os.DirFS(dir)})
}

// AssetFS opens the built bundle as a filesystem. Split out so a test
// can serve the real build when one is present.
//
// A release binary would embed the bundle instead, which is a one-liner
// once ui/dist is a build artefact of the release job:
//
//	//go:embed all:ui/dist/webkit-angular-example/browser
//	var dist embed.FS
//	sub, _ := fs.Sub(dist, "ui/dist/webkit-angular-example/browser")
//
// The example reads from disk so the repository stays free of committed
// build output and `go test ./...` needs no npm.
func AssetFS(dir string) (fs.FS, error) {
	if dir == "" {
		dir = BuiltAssetDir
	}
	if result := core.Stat(filepath.Join(dir, webkit.DefaultIndex)); !result.OK {
		return nil, core.E("angular.AssetFS", "no built frontend at "+dir, result.Err())
	}
	return os.DirFS(dir), nil
}

// itoa avoids pulling fmt into a package that only needs digits.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
