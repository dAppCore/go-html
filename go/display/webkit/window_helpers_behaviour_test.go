package webkit

import (
	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/window"
)

// NewService builds a gui Service over the supplied config without starting
// the Wails event loop; App() is nil until OnStartup runs and OnShutdown is a
// safe no-op.
//
//	r := webkit.NewService(webkit.GuiConfig{Name: "demo"})(c)
func TestWindowHelpersBehaviour_NewService_Good(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := NewService(GuiConfig{Name: "demo"})(c)
	core.RequireTrue(t, r.OK)

	svc, ok := r.Value.(*Service)
	core.RequireTrue(t, ok)
	core.AssertNil(t, svc.App())
	core.AssertTrue(t, svc.OnShutdown(core.Background()).OK)
}

// A nil *Service receiver is safe: App returns nil, OnStartup/OnShutdown Ok.
func TestWindowHelpersBehaviour_NilService_Ugly(t *core.T) {
	var svc *Service
	core.AssertNil(t, svc.App())
	core.AssertTrue(t, svc.OnStartup(core.Background()).OK)
	core.AssertTrue(t, svc.OnShutdown(core.Background()).OK)
}

// The window-helper guards reject a nil Core and an empty name without
// dispatching anything.
func TestWindowHelpersBehaviour_Guards_Bad(t *core.T) {
	core.AssertFalse(t, OpenWindow(nil, "chat"))
	core.AssertFalse(t, OpenWindow(core.New(core.WithServiceLock()), ""))

	core.AssertFalse(t, HideWindow(nil, "chat"))
	core.AssertFalse(t, HideWindow(core.New(core.WithServiceLock()), ""))

	core.AssertFalse(t, WindowExists(nil, "chat"))
	core.AssertFalse(t, WindowExists(core.New(core.WithServiceLock()), ""))

	core.AssertNil(t, lookupWindow(nil, "chat"))
	core.AssertNil(t, lookupWindow(core.New(core.WithServiceLock()), ""))

	core.AssertFalse(t, OpenAdhocWindow(nil, &window.Window{Name: "x"}))
	core.AssertFalse(t, OpenAdhocWindow(core.New(core.WithServiceLock()), nil))
	core.AssertFalse(t, OpenAdhocWindow(core.New(core.WithServiceLock()), &window.Window{Name: ""}))
}

// lookupWindow walks GuiConfig.WindowRegistry and returns the matching
// descriptor, or nil when the gui service is absent / the name is unknown.
func TestWindowHelpersBehaviour_lookupWindow_Good(t *core.T) {
	chat := &window.Window{Name: "chat", Title: "Chat"}
	// WithName("gui", …) is the documented registration, and the helpers look
	// the service up under that name. WithService instead derives the name
	// from the instance's package path, which has been "webkit" since the
	// package was renamed — so the service registered fine and every lookup
	// for "gui" missed it.
	c := core.New(
		core.WithName("gui", NewService(GuiConfig{
			WindowRegistry: []*window.Window{chat, nil},
		})),
		core.WithServiceLock(),
	)

	got := lookupWindow(c, "chat")
	core.RequireTrue(t, got != nil, "expected chat window descriptor")
	core.AssertEqual(t, "Chat", got.Title)

	core.AssertNil(t, lookupWindow(c, "missing"))
}

// WindowExists / HideWindow report false when no window service answers the
// QueryWindowByName query (gui service present but window sub-service absent).
func TestWindowHelpersBehaviour_WindowExists_Bad(t *core.T) {
	c := core.New(
		core.WithName("gui", NewService(GuiConfig{})),
		core.WithServiceLock(),
	)
	core.AssertFalse(t, WindowExists(c, "chat"))
	core.AssertFalse(t, HideWindow(c, "chat"))
}

// A window service that answers the query but misses returns a TYPED nil
// *WindowInfo inside the Result — which an interface nil-check passes.
// WindowExists must unwrap the value, or every name "exists" and callers
// take warm-window paths against windows that were never created.
func TestWindowHelpersBehaviour_WindowExists_Ugly(t *core.T) {
	c := core.New(core.WithServiceLock())
	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		if _, ok := q.(window.QueryWindowByName); ok {
			return core.Result{Value: (*window.WindowInfo)(nil), OK: true}
		}
		return core.Result{}
	})

	core.AssertFalse(t, WindowExists(c, "never-created"))
}
