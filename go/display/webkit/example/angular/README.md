# Angular on `display/webkit`

A minimal Angular application hosted by go-render's `display/webkit` seam.

It exists so the seam a real hosted Angular app depends on can be **reproduced and
fixed in this repository**, without pulling a consumer in. Everything below that can
be checked headless *is* checked — `seam_test.go` runs in `go test ./...` with no
`npm install` and no WebView.

## Layout

```
example/angular/
├── bindings.go            RunnerService (.Service shape) + StatsWailsService (.WailsService shape)
├── host.go                Config() — asset handler, CSP, bindings, window registry
├── seam_test.go           the headless gate (see below)
├── cmd/webkit-angular/    the runnable host
└── ui/                    the Angular application
    └── src/app/wails.service.ts   every Call.ByName literal lives here
```

## Running it

```bash
cd go/display/webkit/example/angular/ui
npm install

# built bundle
npm run build
cd ../../../../.. && go run ./display/webkit/example/angular/cmd/webkit-angular

# or live dev server, in two shells
npm start                                    # ng serve on :9245
WEBKIT_ANGULAR_DEV=1 go run ./display/webkit/example/angular/cmd/webkit-angular
```

`WEBKIT_ANGULAR_STATE=<dir>` scopes window state and layouts to a directory of your
choosing.

## What the headless gate covers

`go test ./display/webkit/example/angular/` — no npm, no WebView:

| Test | What it pins |
|---|---|
| `TestSeam_FrontendCallsResolve` | every `Call.ByName` literal in `ui/src` resolves against a bound Go service |
| `TestSeam_BothReceiverShapesAreCalled` | both `.RunnerService.` and `.StatsWailsService.` stay exercised |
| `TestSeam_NoUnverifiableCalls` | no call name is assembled at runtime, so the gate's pass means what it says |
| `TestSeam_EveryBindingIsReachable` | no bound Go method is unreachable from the frontend |
| `TestSeam_ConfigWiring` | both services bound, asset handler + middleware installed, window registered |
| `TestSeam_WindowStateIsAppScoped` | state and layout persist to separate, app-scoped paths |
| `TestSeam_CSPPermitsEveryTransport` | every transport origin appears in **both** `http://` and `ws://` form |
| `TestSeam_CSPDevAddsOnlyDevRelaxations` | `'unsafe-eval'` and the dev server never leak into the production policy |
| `TestSeam_AssetRouting` | deep links serve the shell; a missing bundle 404s; `/wails/*` is refused |
| `TestSeam_MiddlewareServesRuntimeAndPolicy` | the runtime keeps its URL space *and* every response carries the policy |
| `TestSeam_MissingBuildIsAStartupError` | an unbuilt frontend fails at start-up with an actionable message |

### The drift gate

`Call.ByName` resolves through an **exact-match map** on
`<Go package path>.<receiver type>.<method>` (wails v3 `pkg/application/bindings.go` —
`:250` builds the FQN, `:173` looks it up). Aliases exist only on the numeric
`Call.ByID` path, so a renamed receiver **cannot** be papered over at runtime.

That makes the Go struct name part of the wire contract, and nothing in either build
couples the two sides. The `.Service` → `.WailsService` rename seen in the wild
invalidates every hardcoded call string, and the only symptom is a promise that
rejects when a user clicks something.

`webkit.ScanCallByName` + `webkit.UnresolvedBindingNames` turn that into a failing
test. This example carries **both** receiver shapes so the gate is proven against
either.

## What still needs a real WebView

These are *not* covered by the headless gate — run the app to check them:

- **Bindings actually resolving over the transport.** The gate proves the names
  match; only a running WebView proves the transport carries them. Click **probe**
  on the Jobs page: it calls `Echo` and reports resolved / unavailable, which
  separates "wrong binding name" from "transport down".
- **Events.** The Go host emits `webkit-angular:tick` every second; the header shows
  the latest. If the value never updates, the `NgZone.run` in `wails.service.ts` is
  the first place to look — events arrive outside Angular's zone, and a signal
  written outside it updates without repainting.
- **Hash-route deep link + reload.** Navigate to `#/about`, reload. The host answers
  `/` with the shell and the router restores the route from the fragment.
- **CSP under the real runtime.** The policy is asserted as a string headlessly; only
  a real WebView proves nothing is blocked. Any violation prints to the console
  naming the directive.
- **Window state persistence.** Move and resize the window, quit, relaunch.

## Notes worth keeping

- **Angular's output path.** `ng build` writes `dist/<project>/browser`. Pointing the
  asset handler at `dist/<project>/` yields a window that loads nothing, with no
  error anywhere. `BuiltAssetDir` carries the `browser/` suffix.
- **CSP lives in Go, not in `index.html`.** A `<meta http-equiv>` policy is
  *intersected* with the header, which is how a policy that looks correct in one
  place still blocks the transport. `index.html` deliberately carries none.
- **`window_state.json`.** `go/v0.20.3` made the saves atomic, which stops the file
  being torn. It does not stop two apps disagreeing about its contents — scope the
  path per app, as `HostOptions.StateDir` does.
- **Generated bindings are not required.** `wails3 generate bindings` produces typed
  TS under a gitignored directory. This example calls `Call.ByName` with explicit
  literals instead, because that is the shape consumers actually have in the wild and
  the shape the drift gate checks. Generated bindings are strictly better where you
  can use them.
- **Browser tab vs WebView.** Loaded in a plain browser, wails3 declares a "Browser
  Environment" and bindings are unavailable. `probe()` returns `false` rather than
  hanging, so a surface can degrade deliberately instead of appearing to hang.

## Known upstream defect

`application.NewBindings(...)` and `Bindings.Add(...)` are both exported, but `Add`
dereferences the package-global `globalApplication` to log
(`pkg/application/bindings.go:156` → `application.go:539`). With no `application.New`
having run, that is a nil-pointer panic, so wails' own binding registry cannot be
unit-tested through its exported API. Pinned at `v3.0.0-alpha2.121`.

This is why `webkit.BindingNames` mirrors wails' reflection rules rather than
delegating to them, and why `internalBindingMethods` has a test that fails loudly if
the two ever drift.
