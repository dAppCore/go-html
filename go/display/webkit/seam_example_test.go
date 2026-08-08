// SPDX-Licence-Identifier: EUPL-1.2

package webkit_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing/fstest"

	webkit "dappco.re/go/render/display/webkit"
)

// exampleRunner stands in for a consumer's bound domain service.
type exampleRunner struct{}

func (*exampleRunner) Start(name string) error { return nil }
func (*exampleRunner) Status() (string, error) { return "idle", nil }

// ExampleBindingNames prints the call strings an Angular renderer must
// pass to @wailsio/runtime's Call.ByName for a bound service.
func ExampleBindingNames() {
	for _, name := range webkit.BindingNames(&exampleRunner{}) {
		fmt.Println(name)
	}
	// Output:
	// dappco.re/go/render/display/webkit_test.exampleRunner.Start
	// dappco.re/go/render/display/webkit_test.exampleRunner.Status
}

// ExampleBindingName resolves a single method to its call string.
func ExampleBindingName() {
	name, ok := webkit.BindingName(&exampleRunner{}, "Start")
	fmt.Println(name, ok)

	_, ok = webkit.BindingName(&exampleRunner{}, "Renamed")
	fmt.Println("Renamed resolves:", ok)
	// Output:
	// dappco.re/go/render/display/webkit_test.exampleRunner.Start true
	// Renamed resolves: false
}

// ExampleUnresolvedBindingNames is the drift gate: frontend call strings
// checked against the Go services that back them.
func ExampleUnresolvedBindingNames() {
	called := []string{
		"dappco.re/go/render/display/webkit_test.exampleRunner.Start",
		"dappco.re/go/render/display/webkit_test.exampleRunner.Halt",
	}
	fmt.Println(webkit.UnresolvedBindingNames(called, &exampleRunner{}))
	// Output:
	// [dappco.re/go/render/display/webkit_test.exampleRunner.Halt]
}

// ExampleScanCallByName pulls the call strings out of a frontend tree so
// they can be fed to UnresolvedBindingNames.
func ExampleScanCallByName() {
	frontend := fstest.MapFS{
		"app/runner.service.ts": &fstest.MapFile{Data: []byte(
			`import { Call } from '@wailsio/runtime';
			 export const start = () => Call.ByName('pkg/runner.Service.Start');`)},
	}

	sites, err := webkit.ScanCallByName(frontend)
	if err != nil {
		fmt.Println("scan failed:", err)
		return
	}
	fmt.Println(sites.Names)
	fmt.Println(sites.Files["pkg/runner.Service.Start"])
	// Output:
	// [pkg/runner.Service.Start]
	// [app/runner.service.ts]
}

// ExampleCSP prints the policy a hosted Angular app needs. Note that
// each transport origin contributes both its http:// and its ws:// form
// — omitting the socket form is what broke lthn/desktop #93.
func ExampleCSP() {
	fmt.Println(webkit.CSP(webkit.CSPOptions{
		Transports: []string{"http://localhost:9099"},
	}))
	// Output:
	// base-uri 'self'; connect-src 'self' http://localhost:9099 ws://localhost:9099; default-src 'self'; font-src 'self' data:; frame-ancestors 'none'; img-src 'self' data: blob:; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'
}

// ExampleSPAHandler serves an Angular build with the routing a hosted
// WebView needs: deep links fall back to the index, missing bundles 404.
func ExampleSPAHandler() {
	dist := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("<app-root></app-root>")},
		"main-A1B2C3D4.js": &fstest.MapFile{Data: []byte("console.log(1);")},
	}

	handler, err := webkit.SPAHandler(webkit.SPAOptions{FS: dist})
	if err != nil {
		fmt.Println("config error:", err)
		return
	}

	for _, path := range []string{"/settings/profile", "/main-A1B2C3D4.js", "/chunk-MISSING.js"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		fmt.Println(path, w.Code)
	}
	// Output:
	// /settings/profile 200
	// /main-A1B2C3D4.js 200
	// /chunk-MISSING.js 404
}

// ExampleCSPMiddleware wires the policy in front of the wails carve-out,
// which is the composition a hosted app wants: the runtime keeps its own
// URL space and every response still carries the policy.
func ExampleCSPMiddleware() {
	assets, _ := webkit.SPAHandler(webkit.SPAOptions{FS: fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<app-root></app-root>")},
	}})
	runtime := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("runtime"))
	})

	handler := webkit.CSPMiddleware(
		webkit.CSPOptions{Transports: []string{"http://localhost:9099"}},
		webkit.WailsHTTPMiddleware(assets),
	)(runtime)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil))
	fmt.Println(w.Body.String())
	fmt.Println(w.Header().Get(webkit.CSPHeader) != "")
	// Output:
	// runtime
	// true
}
