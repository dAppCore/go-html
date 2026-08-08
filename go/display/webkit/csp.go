// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// CSPHeader is the enforcing Content-Security-Policy header name.
const CSPHeader = "Content-Security-Policy"

// CSPReportOnlyHeader is the non-enforcing variant: violations are
// reported to the console but nothing is blocked. Use it to find what a
// tightened policy would break before it breaks for a user.
const CSPReportOnlyHeader = "Content-Security-Policy-Report-Only"

// CSPOptions describes the Content-Security-Policy a hosted WebView
// needs: strict enough to be worth setting, permissive enough that the
// wails transport and an Angular application both still work.
type CSPOptions struct {
	// Transports are the origins the wails runtime talks to beyond the
	// document's own — the asset/WS transport and the binding
	// transport, e.g. "http://localhost:9099" and
	// "http://localhost:9199".
	//
	// Each entry contributes BOTH its http(s):// and its ws(s)://
	// form to connect-src. That pairing is the point: allowing the
	// HTTP origin while omitting the WebSocket one produces a policy
	// that passes every page-load check and then silently kills the
	// runtime's event channel — the failure lthn/desktop shipped as
	// issue #93.
	Transports []string

	// DevServer is the framework dev server origin, e.g.
	// "http://localhost:9245". When set, the policy additionally
	// permits that origin (and its WebSocket form, for HMR) and adds
	// 'unsafe-eval' to script-src, which unbundled dev builds require.
	// Leave empty for production — the release policy must not carry
	// the dev relaxations.
	DevServer string

	// StyleNonce is the nonce Angular was configured with via
	// ngCspNonce. When set, style-src carries 'nonce-<value>' instead
	// of 'unsafe-inline', which is the only way to keep runtime-injected
	// component styles working under a strict policy. Empty falls back
	// to 'unsafe-inline', because Angular injects component styles as
	// plain <style> elements and a policy without either is a blank
	// unstyled window.
	StyleNonce string

	// Directives overrides or adds whole directives. A key present here
	// replaces the computed value entirely, so it is the escape hatch
	// for policies this helper does not model. An empty slice value
	// removes the directive.
	Directives map[string][]string

	// ReportOnly emits the Report-Only header instead of the enforcing
	// one.
	ReportOnly bool
}

// CSP builds the policy string for the given options.
//
// The baseline is default-src 'self' with object-src 'none', base-uri
// 'self' and frame-ancestors 'none' — a hosted WebView has no reason to
// load plugins, rewrite its base URL, or be framed. On top of that it
// permits exactly what the stack needs: data:/blob: images (Angular
// inlines small assets and builds blob URLs for downloads), data: fonts,
// inline styles or a nonce, and every transport origin in both its HTTP
// and WebSocket form.
//
//	policy := webkit.CSP(webkit.CSPOptions{
//	    Transports: []string{"http://localhost:9099", "http://localhost:9199"},
//	})
//
// Directives are emitted in a stable, sorted order so the output is
// diffable and testable.
func CSP(opts CSPOptions) string {
	connect := []string{"'self'"}
	script := []string{"'self'"}

	for _, origin := range opts.Transports {
		connect = appendOrigins(connect, origin)
	}
	if dev := strings.TrimSpace(opts.DevServer); dev != "" {
		connect = appendOrigins(connect, dev)
		script = appendUnique(script, normaliseOrigin(dev))
		// Unbundled dev builds and the HMR client evaluate code at
		// runtime; without this the dev window never boots.
		script = appendUnique(script, "'unsafe-eval'")
	}

	style := []string{"'self'"}
	if nonce := strings.TrimSpace(opts.StyleNonce); nonce != "" {
		style = append(style, "'nonce-"+nonce+"'")
	} else {
		style = append(style, "'unsafe-inline'")
	}

	directives := map[string][]string{
		"default-src":     {"'self'"},
		"script-src":      script,
		"style-src":       style,
		"img-src":         {"'self'", "data:", "blob:"},
		"font-src":        {"'self'", "data:"},
		"connect-src":     connect,
		"object-src":      {"'none'"},
		"base-uri":        {"'self'"},
		"frame-ancestors": {"'none'"},
	}
	for name, values := range opts.Directives {
		directives[name] = values
	}

	names := make([]string, 0, len(directives))
	for name := range directives {
		if len(directives[name]) == 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+" "+strings.Join(directives[name], " "))
	}
	return strings.Join(parts, "; ")
}

// CSPMiddleware sets the policy on every response that does not already
// carry one, then delegates.
//
// It composes with WailsHTTPMiddleware — wrap the wails carve-out so the
// runtime's own responses are covered too:
//
//	Middleware: webkit.CSPMiddleware(cspOpts, webkit.WailsHTTPMiddleware(assets))
//
// An existing header is never overwritten, so a handler that needs a
// per-route policy (a sandboxed preview pane, say) keeps control.
func CSPMiddleware(opts CSPOptions, inner ...MiddlewareFunc) MiddlewareFunc {
	policy := CSP(opts)
	header := CSPHeader
	if opts.ReportOnly {
		header = CSPReportOnlyHeader
	}

	return func(next http.Handler) http.Handler {
		for i := len(inner) - 1; i >= 0; i-- {
			if inner[i] != nil {
				next = inner[i](next)
			}
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if w.Header().Get(CSPHeader) == "" && w.Header().Get(CSPReportOnlyHeader) == "" {
				w.Header().Set(header, policy)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// appendOrigins adds an origin in both its HTTP and WebSocket form. The
// pairing is the whole reason this helper exists: the two are distinct
// CSP sources, and permitting only the first is a policy that looks
// correct and breaks the event channel.
func appendOrigins(list []string, origin string) []string {
	normalised := normaliseOrigin(origin)
	if normalised == "" {
		return list
	}
	list = appendUnique(list, normalised)
	if socket := socketOrigin(normalised); socket != "" {
		list = appendUnique(list, socket)
	}
	return list
}

// normaliseOrigin reduces a URL to its scheme://host[:port] form, so a
// caller passing a full endpoint ("http://localhost:9099/wails/ws")
// still yields a valid CSP source. Returns "" for unparseable input.
func normaliseOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// socketOrigin maps an HTTP origin onto its WebSocket sibling. Anything
// already ws:// or wss://, or on a scheme with no socket equivalent,
// yields "".
func socketOrigin(origin string) string {
	switch {
	case strings.HasPrefix(origin, "https://"):
		return "wss://" + strings.TrimPrefix(origin, "https://")
	case strings.HasPrefix(origin, "http://"):
		return "ws://" + strings.TrimPrefix(origin, "http://")
	default:
		return ""
	}
}
