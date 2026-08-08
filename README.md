[![Go Reference](https://pkg.go.dev/badge/dappco.re/go/core/html.svg)](https://pkg.go.dev/dappco.re/go/core/html)
[![License: EUPL-1.2](https://img.shields.io/badge/License-EUPL--1.2-blue.svg)](LICENSE.md)
[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&logo=go)](go.mod)

# go-html

HLCRF DOM compositor with grammar pipeline integration for server-side HTML generation and optional WASM client rendering. Provides a type-safe node tree (El, Text, Raw, If, Each, Switch, Entitled, AriaLabel, AltText, TabIndex, AutoFocus, Role), a five-slot Header/Left/Content/Right/Footer layout compositor with deterministic `data-block` path IDs and ARIA roles, a responsive multi-variant wrapper, a server-side grammar pipeline (StripTags, GrammarImprint via go-i18n reversal, CompareVariants), a build-time Web Component codegen CLI with optional TypeScript declarations, and a WASM module (2.90 MB raw, 842 KB gzip) exposing `renderToString()`.

**Module**: `dappco.re/go/render`
**Licence**: EUPL-1.2
**Language**: Go 1.26

## Quick Start

```go
import html "dappco.re/go/core/html"

page := html.NewLayout("HCF").
    H(html.El("nav", html.Text("i18n.label.navigation"))).
    C(html.El("main",
        html.El("h1", html.Text("i18n.label.welcome")),
        html.Each(items, func(item Item) html.Node {
            return html.El("li", html.Text(item.Name))
        }),
    )).
    F(html.El("footer", html.Text("i18n.label.copyright")))

rendered := page.Render(html.NewContext("en-GB"))
```

## Terminal renderer

The same node trees render styled ANSI for a terminal — go-html is the
application layer, and HTML output is one renderer of it. `RenderTerm` walks
El/Text/If/Each/Switch/Entitled with identical i18n and entitlement semantics,
and `Layout.RenderTerm` composes the HLCRF frame for a CLI: `H` as a top band,
`L | C | R` side by side (stacking below 80 columns), `F` as a status band.
Headings, lists, tables, code blocks, progress bars, links (OSC 8 hyperlinks),
and class tokens (`ok`, `warn`, `error`, `muted`, `accent`, `card`) style
through a dark-first adaptive `TermTheme`. Server/CLI only (`//go:build !js`) —
the WASM module never links it.

```go
out := page.RenderTerm(html.NewContext("en-GB"), html.TermOptions{Width: 120})
```

Try it: `cd go && go run ./cmd/termdemo/ -w 110`

## WebView host (`display/webkit`)

`display/webkit` is the adaptation seam over wails3 — the surface a desktop app
uses to host a web frontend without importing wails directly. Three helpers cover
what a hosted single-page app actually needs:

```go
assets, err := webkit.SPAHandler(webkit.SPAOptions{FS: dist})   // or DevServer: "http://localhost:9245"

cfg := webkit.GuiConfig{
    Assets: webkit.AssetOptions{
        Handler: assets,
        Middleware: webkit.CSPMiddleware(
            webkit.CSPOptions{Transports: []string{"http://localhost:9099"}},
            webkit.WailsHTTPMiddleware(assets),
        ),
    },
    Bindings: []webkit.Binding{webkit.Bind(runnerSvc)},
}
```

- **`SPAHandler`** — embedded build or dev-server proxy. Deep links serve the app
  shell; a *missing* bundle 404s rather than receiving HTML (the cause of
  `Unexpected token '<'` a page-load later); `/wails/*` is refused.
- **`CSPMiddleware`** — each transport origin contributes **both** its `http://`
  and `ws://` form. Allowing only the first yields a policy that passes page load
  and then silently kills the runtime's event channel.
- **`BindingNames` / `ScanCallByName` / `UnresolvedBindingNames`** — the drift
  gate. wails resolves `Call.ByName` through an exact-match map on
  `<pkg path>.<receiver type>.<method>`, so a Go struct rename invalidates every
  hardcoded call string in the frontend with no build-time signal. These turn that
  into a failing test.

### Angular example

[`go/display/webkit/example/angular`](go/display/webkit/example/angular) is a
minimal Angular application hosted by the seam, carrying both receiver shapes
(`.Service` and `.WailsService`) found in the wild. Its `seam_test.go` runs in
`go test ./...` with no `npm install` and no WebView — it resolves every
`Call.ByName` literal in the Angular sources against the bound Go services, and
asserts the CSP, asset routing and window-state wiring. See its
[README](go/display/webkit/example/angular/README.md) for what needs a real
WebView.

## Documentation

- [Architecture](docs/architecture.md) — node interface, HLCRF layout, responsive compositor, grammar pipeline, WASM module, codegen CLI
- [Development Guide](docs/development.md) — building, testing, WASM build, server/client split rules
- [Project History](docs/history.md) — completed phases and known limitations

## Build & Test

```bash
go test ./...
go test -bench . ./...
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o gohtml.wasm ./cmd/wasm/
go build ./...
```

## Licence

European Union Public Licence 1.2 — see [LICENCE](LICENCE) for details.
