// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// directive pulls one directive's sources out of a policy string.
func directive(policy, name string) string {
	for _, part := range strings.Split(policy, "; ") {
		if strings.HasPrefix(part, name+" ") {
			return strings.TrimPrefix(part, name+" ")
		}
	}
	return ""
}

// TestCSP_Good asserts the baseline: a locked-down default with exactly
// the relaxations the stack needs, in a stable sorted order.
func TestCSP_Good(t *testing.T) {
	policy := CSP(CSPOptions{})

	want := map[string]string{
		"default-src":     "'self'",
		"script-src":      "'self'",
		"style-src":       "'self' 'unsafe-inline'",
		"img-src":         "'self' data: blob:",
		"font-src":        "'self' data:",
		"connect-src":     "'self'",
		"object-src":      "'none'",
		"base-uri":        "'self'",
		"frame-ancestors": "'none'",
	}
	for name, sources := range want {
		if got := directive(policy, name); got != sources {
			t.Fatalf("%s = %q, want %q", name, got, sources)
		}
	}
	if !sortedDirectives(policy) {
		t.Fatalf("directives are not in sorted order: %q", policy)
	}
}

// TestCSP_Ugly is the lthn/desktop #93 regression: a transport origin
// must contribute BOTH its http:// and its ws:// form. Permitting only
// the HTTP origin yields a policy that passes every page-load check and
// then silently kills the runtime's event channel.
func TestCSP_Ugly(t *testing.T) {
	policy := CSP(CSPOptions{Transports: []string{"http://localhost:9099", "https://desktop.example:9199"}})
	connect := directive(policy, "connect-src")

	for _, source := range []string{
		"'self'",
		"http://localhost:9099",
		"ws://localhost:9099", // the one that is always forgotten
		"https://desktop.example:9199",
		"wss://desktop.example:9199",
	} {
		if !strings.Contains(connect, source) {
			t.Fatalf("connect-src = %q, missing %q", connect, source)
		}
	}
}

// TestCSP_TransportEndpointIsNormalised asserts a caller who passes the
// full WS endpoint rather than a bare origin still gets a valid policy —
// CSP sources have no path, so an un-normalised value would be silently
// ignored by the browser.
func TestCSP_TransportEndpointIsNormalised(t *testing.T) {
	policy := CSP(CSPOptions{Transports: []string{"http://localhost:9099/wails/ws"}})
	connect := directive(policy, "connect-src")

	if strings.Contains(connect, "/wails/ws") {
		t.Fatalf("connect-src = %q, want the path stripped", connect)
	}
	for _, source := range []string{"http://localhost:9099", "ws://localhost:9099"} {
		if !strings.Contains(connect, source) {
			t.Fatalf("connect-src = %q, missing %q", connect, source)
		}
	}
}

// TestCSP_Bad asserts unusable transport entries are dropped rather than
// emitted as garbage sources — a malformed source can invalidate the
// whole directive in some engines.
func TestCSP_Bad(t *testing.T) {
	policy := CSP(CSPOptions{Transports: []string{"", "   ", "not a url", "localhost:9099", "://broken"}})
	if got := directive(policy, "connect-src"); got != "'self'" {
		t.Fatalf("connect-src = %q, want only 'self' — unusable origins must be dropped", got)
	}
}

// TestCSP_DevServer asserts the dev relaxations land only when a dev
// server is configured, and that production never carries them.
func TestCSP_DevServer(t *testing.T) {
	dev := CSP(CSPOptions{DevServer: "http://localhost:9245"})
	if script := directive(dev, "script-src"); !strings.Contains(script, "'unsafe-eval'") {
		t.Fatalf("dev script-src = %q, want 'unsafe-eval' for unbundled builds", script)
	}
	if connect := directive(dev, "connect-src"); !strings.Contains(connect, "ws://localhost:9245") {
		t.Fatalf("dev connect-src = %q, want the HMR socket", connect)
	}

	prod := CSP(CSPOptions{Transports: []string{"http://localhost:9099"}})
	if script := directive(prod, "script-src"); strings.Contains(script, "'unsafe-eval'") {
		t.Fatalf("production script-src = %q, must not carry the dev relaxation", script)
	}
}

// TestCSP_StyleNonce asserts the strict styling path: with a nonce,
// 'unsafe-inline' is gone. Angular injects component styles as <style>
// elements, so one of the two must always be present or the window
// renders unstyled.
func TestCSP_StyleNonce(t *testing.T) {
	policy := CSP(CSPOptions{StyleNonce: "r4nd0m"})
	style := directive(policy, "style-src")

	if !strings.Contains(style, "'nonce-r4nd0m'") {
		t.Fatalf("style-src = %q, want the nonce", style)
	}
	if strings.Contains(style, "'unsafe-inline'") {
		t.Fatalf("style-src = %q, must drop 'unsafe-inline' once a nonce is set", style)
	}
}

// TestCSP_Directives asserts the escape hatch both overrides and removes.
func TestCSP_Directives(t *testing.T) {
	policy := CSP(CSPOptions{Directives: map[string][]string{
		"img-src":         {"'self'"},
		"frame-ancestors": {},
		"worker-src":      {"'self'", "blob:"},
	}})

	if got := directive(policy, "img-src"); got != "'self'" {
		t.Fatalf("img-src = %q, want the override", got)
	}
	if got := directive(policy, "frame-ancestors"); got != "" {
		t.Fatalf("frame-ancestors = %q, want removal on an empty value", got)
	}
	if got := directive(policy, "worker-src"); got != "'self' blob:" {
		t.Fatalf("worker-src = %q, want the addition", got)
	}
}

// TestCSPMiddleware_Good asserts the header is set and the request still
// reaches the handler underneath.
func TestCSPMiddleware_Good(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("app"))
	})
	opts := CSPOptions{Transports: []string{"http://localhost:9099"}}
	handler := CSPMiddleware(opts)(next)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Body.String() != "app" {
		t.Fatalf("body = %q, want the handler's response", w.Body.String())
	}
	if got := w.Header().Get(CSPHeader); got != CSP(opts) {
		t.Fatalf("%s = %q, want the computed policy", CSPHeader, got)
	}
}

// TestCSPMiddleware_ReportOnly asserts the non-enforcing header is used
// instead of, not as well as, the enforcing one.
func TestCSPMiddleware_ReportOnly(t *testing.T) {
	handler := CSPMiddleware(CSPOptions{ReportOnly: true})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Header().Get(CSPReportOnlyHeader) == "" {
		t.Fatal("report-only header not set")
	}
	if got := w.Header().Get(CSPHeader); got != "" {
		t.Fatalf("%s = %q, want empty in report-only mode", CSPHeader, got)
	}
}

// TestCSPMiddleware_Bad asserts a handler that sets its own policy keeps
// it — a sandboxed preview pane needs a stricter policy than the shell.
func TestCSPMiddleware_Bad(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(CSPHeader, "default-src 'none'")
		w.WriteHeader(http.StatusOK)
	})
	handler := CSPMiddleware(CSPOptions{})(next)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := w.Header().Get(CSPHeader); got != "default-src 'none'" {
		t.Fatalf("%s = %q, want the handler's own policy preserved", CSPHeader, got)
	}
}

// TestCSPMiddleware_Composes asserts the wails carve-out still works
// when wrapped, and that the runtime's responses carry the policy too.
func TestCSPMiddleware_Composes(t *testing.T) {
	assets := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("assets"))
	})
	runtime := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("runtime"))
	})
	handler := CSPMiddleware(CSPOptions{}, WailsHTTPMiddleware(assets))(runtime)

	for _, tc := range []struct{ path, body string }{
		{"/wails/runtime.js", "runtime"},
		{"/index.html", "assets"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Body.String() != tc.body {
				t.Fatalf("body = %q, want %q — the carve-out must survive wrapping", w.Body.String(), tc.body)
			}
			if w.Header().Get(CSPHeader) == "" {
				t.Fatal("policy missing; every response must carry it")
			}
		})
	}
}

// sortedDirectives reports whether the policy's directive names are in
// ascending order, which is what makes the output diffable.
func sortedDirectives(policy string) bool {
	parts := strings.Split(policy, "; ")
	previous := ""
	for _, part := range parts {
		name, _, _ := strings.Cut(part, " ")
		if name < previous {
			return false
		}
		previous = name
	}
	return true
}
