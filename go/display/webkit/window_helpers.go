// SPDX-License-Identifier: EUPL-1.2

package webkit

import (
	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/window"
)

// OpenWindow shows + focuses a registered window by name. Composes the
// three actions a "tray click opens chat" / "Cmd+J toggles popover"
// dispatch normally fires:
//
//  1. window.restore — undoes a minimised state
//  2. window.set_visibility{Visible: true} — un-hides
//  3. window.focus — raises + activates
//
// Plus, when the registered Window has ShowDockIcon set, fires
// dock.show_icon BEFORE the show sequence so the Dock / taskbar
// presence is in place by the time the window draws.
//
// Returns false (and is a no-op) if the named window is not registered.
// Returns true on success. Callers that need the wrapped Result types
// can call the underlying actions directly.
//
//	if !webkit.OpenWindow(c, "chat") {
//	    // window not in registry — caller chooses fallback
//	}
//
// Consumer-side this replaces a ~3-action dispatch sequence per call
// site. The lthn-side openWindow() helper that did exactly this is the
// canonical example.
func OpenWindow(c *core.Core, name string) bool {
	if c == nil || name == "" {
		return false
	}
	info := lookupWindow(c, name)
	if info == nil {
		return false
	}
	ctx := core.Background()
	if info.ShowDockIcon {
		c.Action("dock.show_icon").Run(ctx, core.NewOptions())
	}
	c.Action("window.restore").Run(ctx, core.NewOptions(
		core.Option{Key: "task", Value: window.TaskRestore{Name: name}},
	))
	c.Action("window.set_visibility").Run(ctx, core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetVisibility{Name: name, Visible: true}},
	))
	c.Action("window.focus").Run(ctx, core.NewOptions(
		core.Option{Key: "task", Value: window.TaskFocus{Name: name}},
	))
	return true
}

// WindowSpec returns the registered Window descriptor by name from the
// webkit.Service's WindowRegistry, or nil + false if not registered.
//
// The window service's QueryWindowByName returns runtime WindowInfo
// (position/size/focus state) rather than the original descriptor, so
// callers that need the *declaration* — close behaviour, content
// protection flag, dock-icon elevation, etc. — walk the registry via
// this helper. The slice is small fixed-size (single-digit entries in
// practice) so the O(N) scan runs at human latency.
//
//	if spec, ok := webkit.WindowSpec(c, "app"); ok {
//	    // …use spec.URL / spec.HideOnClose / spec.ShowDockIcon
//	}
func WindowSpec(c *core.Core, name string) (*window.Window, bool) {
	if c == nil || name == "" {
		return nil, false
	}
	svc, ok := core.ServiceFor[*Service](c, "gui")
	if !ok || svc == nil {
		return nil, false
	}
	for _, w := range svc.Options().WindowRegistry {
		if w != nil && w.Name == name {
			return w, true
		}
	}
	return nil, false
}

// lookupWindow is the internal alias preserved so existing call sites
// in this package keep their nil-only return contract.
func lookupWindow(c *core.Core, name string) *window.Window {
	w, _ := WindowSpec(c, name)
	return w
}

// OpenAdhocWindow opens a runtime-created window that is NOT in the
// boot registry. Use for windows whose existence depends on user
// action (per-plugin views, dynamic detail panes) rather than the
// app's known catalogue. The Window descriptor is consumed for its
// initial open; subsequent open-by-name re-shows go through OpenWindow.
//
// Sequence:
//  1. window.open with the full descriptor (Hidden=true for pre-create
//     — set spec.Hidden=false if you want the window visible immediately)
//  2. window.set_close_behavior if HideOnClose is set
//  3. window.set_content_protection if ContentProtection is set
//  4. window.set_visibility{true} to show
//  5. window.focus
//
// Returns false on any step's Result failure, true on success.
//
//	webkit.OpenAdhocWindow(c, &window.Window{
//	    Name:  "plugin-mychat",
//	    Title: "My Chat Plugin",
//	    URL:   "/?surface=plugin&code=mychat",
//	    Width: 800, Height: 600,
//	    HideOnClose: true,
//	})
func OpenAdhocWindow(c *core.Core, spec *window.Window) bool {
	if c == nil || spec == nil || spec.Name == "" {
		return false
	}
	ctx := core.Background()
	// Clone so the caller's spec isn't mutated by the Hidden=true
	// pre-create convention. Most callers want the window visible
	// immediately, but the open action needs a fresh struct anyway.
	open := *spec
	if r := c.Action("window.open").Run(ctx, core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{Window: &open}},
	)); !r.OK {
		return false
	}
	if spec.HideOnClose {
		c.Action("window.set_close_behavior").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskSetCloseBehavior{
				Name:     spec.Name,
				Behavior: window.CloseBehaviorHide,
			}},
		))
	}
	if spec.ContentProtection {
		c.Action("window.set_content_protection").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskSetContentProtection{
				Name:       spec.Name,
				Protection: true,
			}},
		))
	}
	if spec.ShowDockIcon {
		c.Action("dock.show_icon").Run(ctx, core.NewOptions())
	}
	if !spec.Hidden {
		c.Action("window.set_visibility").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskSetVisibility{Name: spec.Name, Visible: true}},
		))
		c.Action("window.focus").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskFocus{Name: spec.Name}},
		))
	}
	return true
}

// HideWindow hides a registered window. Composition of
// window.set_visibility{Visible: false}. Returns false if the name
// isn't in the registry, true on dispatch success.
//
//	webkit.HideWindow(c, "chat")
func HideWindow(c *core.Core, name string) bool {
	if c == nil || name == "" {
		return false
	}
	if !WindowExists(c, name) {
		return false
	}
	r := c.Action("window.set_visibility").Run(core.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetVisibility{Name: name, Visible: false}},
	))
	return r.OK
}

// WindowExists reports whether a window with the given name has been
// registered (via GuiConfig.WindowRegistry or an ad-hoc window.open
// call). Wraps the QueryWindowByName query.
//
//	if webkit.WindowExists(c, "chat") { … }
func WindowExists(c *core.Core, name string) bool {
	if c == nil || name == "" {
		return false
	}
	r := c.QUERY(window.QueryWindowByName{Name: name})
	if !r.OK {
		return false
	}
	// The query answers a miss with a typed-nil *WindowInfo inside the
	// Result, and a typed nil still passes an interface nil-check — so
	// trusting r.Value != nil made every name exist, and callers took
	// warm-window paths against windows that were never created.
	info, ok := r.Value.(*window.WindowInfo)
	return ok && info != nil
}
