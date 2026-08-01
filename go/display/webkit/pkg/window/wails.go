// pkg/window/wails.go
package window

import (
	"reflect"
	"unsafe"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/preload"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// WailsPlatform implements Platform using Wails v3.
type WailsPlatform struct {
	app *application.App
}

// NewWailsPlatform creates a Wails-backed Platform.
func NewWailsPlatform(app *application.App) *WailsPlatform {
	return &WailsPlatform{app: app}
}

// BindEvalReply satisfies EvalReplyBinder. Registers an
// app.Event.On listener for EvalJSEventName so JS-side replies
// emitted via the @wailsio/runtime Events bus reach the Service's
// CompleteEval router.
//
// Wails delivers the JS payload as ev.Data — for our
// {reqId, result, err} shape it deserialises to map[string]any.
// Extract the three fields and forward; unknown shapes are
// silently ignored so a third-party event with the same name
// can't poison the eval channel.
func (wp *WailsPlatform) BindEvalReply(cb func(reqID string, result any, errStr string)) {
	if wp == nil || wp.app == nil || cb == nil {
		return
	}
	wp.app.Event.On(EvalJSEventName, func(ev *application.CustomEvent) {
		if ev == nil {
			return
		}
		data, ok := ev.Data.(map[string]any)
		if !ok {
			return
		}
		reqID, _ := data["reqId"].(string)
		errStr, _ := data["err"].(string)
		result := data["result"]
		if reqID == "" {
			return
		}
		cb(reqID, result, errStr)
	})
}

// BindCustomEvent satisfies CustomEventBinder. Registers an
// app.Event.On listener for an arbitrary event name. The callback
// receives ev.Data verbatim — consumers are responsible for the
// type-assertion to whatever shape their JS-side emit produced.
// Nil callback / nil app / empty name are guarded as no-ops so
// boot-time wiring against an unconfigured platform stays quiet.
func (wp *WailsPlatform) BindCustomEvent(name string, cb func(data any)) {
	if wp == nil || wp.app == nil || cb == nil || name == "" {
		return
	}
	wp.app.Event.On(name, func(ev *application.CustomEvent) {
		if ev == nil {
			return
		}
		cb(ev.Data)
	})
}

func (wp *WailsPlatform) CreateWindow(options PlatformWindowOptions) PlatformWindow {
	if wp == nil || wp.app == nil || wp.app.Window == nil {
		return nil
	}
	// wails3 alpha.91 defaults InitialPosition=WindowCentered, which
	// IGNORES the X/Y options entirely and centres on the primary
	// screen. To honour saved positions (and any explicit spec X/Y)
	// we must set WindowXY when either coordinate is non-zero. The
	// (0,0) case still centres — first-launch / no-state windows
	// keep the platform-default placement.
	initialPos := application.WindowCentered
	if options.X != 0 || options.Y != 0 {
		initialPos = application.WindowXY
	}
	wOpts := application.WebviewWindowOptions{
		Name:                       options.Name,
		Title:                      options.Title,
		URL:                        options.URL,
		HTML:                       options.HTML,
		JS:                         options.JS,
		CSS:                        options.CSS,
		Width:                      options.Width,
		Height:                     options.Height,
		InitialPosition:            initialPos,
		X:                          options.X,
		Y:                          options.Y,
		MinWidth:                   options.MinWidth,
		MinHeight:                  options.MinHeight,
		MaxWidth:                   options.MaxWidth,
		MaxHeight:                  options.MaxHeight,
		Frameless:                  options.Frameless,
		Hidden:                     options.Hidden,
		AlwaysOnTop:                options.AlwaysOnTop,
		DisableResize:              options.DisableResize,
		EnableFileDrop:             options.EnableFileDrop,
		HideOnEscape:               options.HideOnEscape,
		HideOnFocusLost:            options.HideOnFocusLost,
		DefaultContextMenuDisabled: options.DefaultContextMenuDisabled,
		StartState:                 application.WindowState(options.StartState),
		BackgroundType:             application.BackgroundType(options.BackgroundType),
		Zoom:                       options.Zoom,
		ZoomControlEnabled:         options.ZoomControlEnabled,
		Permissions:                translatePermissions(options.Permissions),
		OpenInspectorOnStartup:     options.OpenInspectorOnStartup,
		MinimiseButtonState:        application.ButtonState(options.MinimiseButtonState),
		MaximiseButtonState:        application.ButtonState(options.MaximiseButtonState),
		CloseButtonState:           application.ButtonState(options.CloseButtonState),
		FullscreenButtonState:      application.ButtonState(options.FullscreenButtonState),
		DevToolsEnabled:            options.DevToolsEnabled,
		IgnoreMouseEvents:          options.IgnoreMouseEvents,
		ContentProtectionEnabled:   options.ContentProtection,
		UseApplicationMenu:         options.UseApplicationMenu,
		BackgroundColour:           application.NewRGBA(options.BackgroundColour[0], options.BackgroundColour[1], options.BackgroundColour[2], options.BackgroundColour[3]),
		Mac: application.MacWindow{
			WindowLevel:                  application.MacWindowLevel(options.Mac.WindowLevel),
			CollectionBehavior:           application.MacWindowCollectionBehavior(options.Mac.CollectionBehavior),
			InvisibleTitleBarHeight:      options.Mac.InvisibleTitleBarHeight,
			Backdrop:                     application.MacBackdrop(options.Mac.Backdrop),
			DisableShadow:                options.Mac.DisableShadow,
			TabbingMode:                  application.MacWindowTabbingMode(options.Mac.TabbingMode),
			DisableEscapeExitsFullscreen: options.Mac.DisableEscapeExitsFullscreen,
			LiquidGlass: application.MacLiquidGlass{
				Style:        application.MacLiquidGlassStyle(options.Mac.LiquidGlassStyle),
				Material:     application.NSVisualEffectMaterial(options.Mac.LiquidGlassMaterial),
				CornerRadius: options.Mac.LiquidGlassCornerRadius,
				GroupID:      options.Mac.LiquidGlassGroupID,
				GroupSpacing: options.Mac.LiquidGlassGroupSpacing,
			},
		},
		Linux: application.LinuxWindow{
			Icon: options.Linux.Icon,
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar:            options.Windows.HiddenOnTaskbar,
			DisableMenu:                options.Windows.DisableMenu,
			Theme:                      application.Theme(options.Windows.Theme),
			NonClientRegionSupport:     options.Windows.NonClientRegionSupport,
			WebView2CompositionHosting: options.Windows.WebView2CompositionHosting,
			WindowDidMoveDebounceMS:    options.Windows.WindowDidMoveDebounceMS,
		},
	}
	if options.ScreenID != "" && wp.app.Screen != nil {
		wOpts.Screen = wp.app.Screen.GetByID(options.ScreenID)
	}
	if options.Mac.DisableBackForwardNav {
		// alpha2 moved the preference to an internal optional.Bool — the
		// field's Set method is the public construction path.
		wOpts.Mac.WebviewPreferences.AllowsBackForwardNavigationGestures.Set(false)
	}
	if options.Mac.EnableAutoplayWithoutUserAction {
		wOpts.Mac.WebviewPreferences.EnableAutoplayWithoutUserAction.Set(true)
	}
	if tint := options.Mac.LiquidGlassTintColour; tint != nil {
		colour := application.NewRGBA((*tint)[0], (*tint)[1], (*tint)[2], (*tint)[3])
		wOpts.Mac.LiquidGlass.TintColor = &colour
	}
	var windowHandle *application.WebviewWindow
	preloadHook := func(origin string, target preload.Webview) {
		if target == nil {
			target = windowHandle
		}
		if target == nil {
			return
		}
		// Wails 3 gates ExecJS on a runtimeLoaded flag that only flips when
		// the page emits 'wails:runtime:ready' via its injected runtime —
		// foreign pages (anthropic.com, wikipedia.org, plugin shells served
		// from /plugin/<code>/, etc.) never ship that runtime, so every
		// ExecJS for those windows queues forever.
		//
		// Flipping the flag here unblocks ExecJS for any origin, making
		// every webview agent-addressable (same dispatch as our own
		// index.html). Reflection is the only path because runtimeLoaded
		// is unexported on Wails's WebviewWindow; we set it once per page
		// load and any pending or future ExecJS goes straight through to
		// the platform.
		unblockWailsRuntime(windowHandle)
		if err := preload.InjectPreload(target, origin); err != nil {
			return
		}
		if extra := postPageLoadWindowJS(options.JS); core.Trim(extra) != "" {
			target.ExecJS(extra)
		}
	}
	preloadHookOrigin := options.URL
	hasOnPageLoad := wirePreloadOnPageLoad(&wOpts, preloadHookOrigin, preloadHook)
	if hasOnPageLoad {
		wOpts.JS = ""
	}
	w := wp.app.Window.NewWithOptions(wOpts)
	windowHandle = w
	// Wails 3 alpha.83 doesn't expose OnPageLoad on WebviewWindowOptions
	// (so wirePreloadOnPageLoad returns false), but it does emit a per-
	// window navigation-finished event we can subscribe to. Wire that as
	// the fallback path so plugin shells / foreign URLs still get the
	// runtime gate flipped after the page lands.
	if !hasOnPageLoad {
		subscribeToNavigationFinished(w, preloadHookOrigin, preloadHook)
	}
	return &wailsWindow{w: w, app: wp.app, title: options.Title, opacity: 1.0, alwaysOnTop: options.AlwaysOnTop}
}

func translatePermissions(input map[uint8]uint8) map[application.PermissionType]application.Permission {
	if len(input) == 0 {
		return nil
	}
	output := make(map[application.PermissionType]application.Permission, len(input))
	for kind, policy := range input {
		output[application.PermissionType(kind)] = application.Permission(policy)
	}
	return output
}

// subscribeToNavigationFinished wires a handler that runs on each page-load
// completion via Wails's per-window event system. Used when OnPageLoad isn't
// available on WebviewWindowOptions (Wails 3 alpha.83). Picks the right
// platform event by trying common candidates; the unknown ones are no-ops
// because Wails ignores unregistered event types.
func subscribeToNavigationFinished(window *application.WebviewWindow, origin string, hook func(origin string, target preload.Webview)) {
	if window == nil || hook == nil {
		return
	}
	handler := func(_ *application.WindowEvent) {
		hook(origin, window)
	}
	// Try every known event that signals "page finished loading" across
	// platforms — only one will actually fire on the running OS, the rest
	// stay dormant. Belt-and-braces.
	for _, evt := range []events.WindowEventType{
		events.Mac.WebViewDidFinishNavigation,
		events.Windows.WebViewNavigationCompleted,
		events.Linux.WindowLoadFinished,
	} {
		window.OnWindowEvent(evt, handler)
	}
}

// unblockWailsRuntime force-flips Wails's per-window runtimeLoaded flag and
// drains any queued ExecJS calls. Called from OnPageLoad so foreign pages
// don't sit forever waiting for a wails:runtime:ready signal that they were
// never going to send. No-op on builds where the field isn't accessible
// (future Wails refactors) — degraded behaviour is just the existing queue.
func unblockWailsRuntime(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	value := reflect.ValueOf(window)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return
	}
	struc := value.Elem()
	if struc.Kind() != reflect.Struct {
		return
	}
	flagField := struc.FieldByName("runtimeLoaded")
	if !flagField.IsValid() {
		return
	}
	// Already set — nothing to do.
	if flagField.Kind() == reflect.Bool {
		flagAddr := unsafe.Pointer(flagField.UnsafeAddr())
		// safe: flag is bool, single byte
		flagWritable := reflect.NewAt(flagField.Type(), flagAddr).Elem()
		if flagWritable.Bool() {
			return
		}
		flagWritable.SetBool(true)
	}

	// Drain any ExecJS calls that queued before the flag flipped — they
	// were waiting for runtimeLoaded; now they need to actually run.
	pendingField := struc.FieldByName("pendingJS")
	if !pendingField.IsValid() || pendingField.Kind() != reflect.Slice {
		return
	}
	pendingAddr := unsafe.Pointer(pendingField.UnsafeAddr())
	pendingWritable := reflect.NewAt(pendingField.Type(), pendingAddr).Elem()
	pending := make([]string, pendingWritable.Len())
	for i := 0; i < pendingWritable.Len(); i++ {
		pending[i] = pendingWritable.Index(i).String()
	}
	pendingWritable.SetLen(0)
	for _, js := range pending {
		window.ExecJS(js)
	}
}

func wirePreloadOnPageLoad(options *application.WebviewWindowOptions, fallbackOrigin string, inject func(origin string, target preload.Webview)) bool {
	if options == nil || inject == nil {
		return false
	}

	value := reflect.ValueOf(options)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return false
	}
	structValue := value.Elem()
	if structValue.Kind() != reflect.Struct {
		return false
	}

	field := structValue.FieldByName("OnPageLoad")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Func {
		return false
	}

	fnType := field.Type()
	field.Set(reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		inject(extractPageLoadOrigin(args, fallbackOrigin), extractPageLoadWebview(args))
		return zeroReturnValues(fnType)
	}))
	return true
}

func extractPageLoadOrigin(args []reflect.Value, fallback string) string {
	for _, arg := range args {
		if !arg.IsValid() {
			continue
		}
		if arg.Kind() == reflect.Pointer {
			if arg.IsNil() {
				continue
			}
			arg = arg.Elem()
		}
		switch arg.Kind() {
		case reflect.String:
			if value := core.Trim(arg.String()); value != "" {
				return value
			}
		case reflect.Struct:
			for _, name := range []string{"URL", "Url", "Origin", "Location"} {
				field := arg.FieldByName(name)
				if field.IsValid() && field.Kind() == reflect.String {
					if value := core.Trim(field.String()); value != "" {
						return value
					}
				}
			}
		}
	}
	return fallback
}

func extractPageLoadWebview(args []reflect.Value) preload.Webview {
	for _, arg := range args {
		if !arg.IsValid() || !arg.CanInterface() {
			continue
		}
		if target, ok := arg.Interface().(preload.Webview); ok {
			return target
		}
	}
	return nil
}

func zeroReturnValues(fnType reflect.Type) []reflect.Value {
	if fnType.NumOut() == 0 {
		return nil
	}
	out := make([]reflect.Value, 0, fnType.NumOut())
	for out0 := range fnType.Outs() {
		out = append(out, reflect.Zero(out0))
	}
	return out
}

func postPageLoadWindowJS(raw string) string {
	if looksLikeLegacyDisplayPreload(raw) {
		return ""
	}
	return raw
}

func looksLikeLegacyDisplayPreload(raw string) bool {
	trimmed := core.Trim(raw)
	if trimmed == "" {
		return false
	}
	return core.Contains(trimmed, "const __corePageURL =") &&
		core.Contains(trimmed, "globalThis.core.ml") &&
		core.Contains(trimmed, "Document.prototype, 'cookie'")
}

func (wp *WailsPlatform) GetWindows() []PlatformWindow {
	if wp == nil || wp.app == nil || wp.app.Window == nil {
		return nil
	}
	all := wp.app.Window.GetAll()
	out := make([]PlatformWindow, 0, len(all))
	for _, w := range all {
		if wv, ok := w.(*application.WebviewWindow); ok {
			out = append(out, &wailsWindow{w: wv, app: wp.app})
		}
	}
	return out
}

// wailsWindow wraps *application.WebviewWindow to implement PlatformWindow.
// It stores the title and opacity locally because Wails v3 does not expose getters for both.
// The app reference enables behaviours that span the application (e.g. CloseBehaviorQuit).
type wailsWindow struct {
	w           *application.WebviewWindow
	app         *application.App
	title       string
	opacity     float64
	alwaysOnTop bool
}

func (ww *wailsWindow) Name() string         { return ww.w.Name() }
func (ww *wailsWindow) Title() string        { return ww.title }
func (ww *wailsWindow) Position() (int, int) { return ww.w.Position() }
func (ww *wailsWindow) Size() (int, int)     { return ww.w.Size() }
func (ww *wailsWindow) IsMaximised() bool    { return ww.w.IsMaximised() }
func (ww *wailsWindow) IsFocused() bool      { return ww.w.IsFocused() }
func (ww *wailsWindow) IsVisible() bool      { return ww.w.IsVisible() }
func (ww *wailsWindow) IsFullscreen() bool   { return ww.w.IsFullscreen() }
func (ww *wailsWindow) IsMinimised() bool    { return ww.w.IsMinimised() }
func (ww *wailsWindow) IsAlwaysOnTop() bool  { return ww.alwaysOnTop }
func (ww *wailsWindow) GetBounds() (int, int, int, int) {
	r := ww.w.Bounds()
	return r.X, r.Y, r.Width, r.Height
}
func (ww *wailsWindow) GetZoom() float64          { return ww.w.GetZoom() }
func (ww *wailsWindow) GetOpacity() float64       { return ww.opacity }
func (ww *wailsWindow) SetTitle(title string)     { ww.title = title; ww.w.SetTitle(title) }
func (ww *wailsWindow) SetPosition(x, y int)      { ww.w.SetPosition(x, y) }
func (ww *wailsWindow) SetSize(width, height int) { ww.w.SetSize(width, height) }
func (ww *wailsWindow) SetBackgroundColour(r, g, b, a uint8) {
	ww.w.SetBackgroundColour(application.NewRGBA(r, g, b, a))
}
func (ww *wailsWindow) SetVisibility(visible bool) {
	if visible {
		ww.w.Show()
	} else {
		ww.w.Hide()
	}
}
func (ww *wailsWindow) SetAlwaysOnTop(alwaysOnTop bool) {
	ww.alwaysOnTop = alwaysOnTop
	ww.w.SetAlwaysOnTop(alwaysOnTop)
}
func (ww *wailsWindow) SetOpacity(opacity float64) {
	ww.opacity = opacity
	if setter, ok := any(ww.w).(interface{ SetOpacity(float64) }); ok {
		setter.SetOpacity(opacity)
	}
}
func (ww *wailsWindow) SetBounds(x, y, width, height int) {
	ww.w.SetBounds(application.Rect{X: x, Y: y, Width: width, Height: height})
}
func (ww *wailsWindow) SetURL(url string)             { ww.w.SetURL(url) }
func (ww *wailsWindow) SetHTML(html string)           { ww.w.SetHTML(html) }
func (ww *wailsWindow) SetZoom(magnification float64) { ww.w.SetZoom(magnification) }
func (ww *wailsWindow) SetContentProtection(protection bool) {
	ww.w.SetContentProtection(protection)
}
func (ww *wailsWindow) Maximise()            { ww.w.Maximise() }
func (ww *wailsWindow) Restore()             { ww.w.Restore() }
func (ww *wailsWindow) Minimise()            { ww.w.Minimise() }
func (ww *wailsWindow) Focus()               { ww.w.Focus() }
func (ww *wailsWindow) Close()               { ww.w.Close() }
func (ww *wailsWindow) Show()                { ww.w.Show() }
func (ww *wailsWindow) Hide()                { ww.w.Hide() }
func (ww *wailsWindow) Fullscreen()          { ww.w.Fullscreen() }
func (ww *wailsWindow) UnFullscreen()        { ww.w.UnFullscreen() }
func (ww *wailsWindow) ToggleFullscreen()    { ww.w.ToggleFullscreen() }
func (ww *wailsWindow) ToggleMaximise()      { ww.w.ToggleMaximise() }
func (ww *wailsWindow) ExecJS(js string)     { ww.w.ExecJS(js) }
func (ww *wailsWindow) Flash(enabled bool)   { ww.w.Flash(enabled) }
func (ww *wailsWindow) Print() resultFailure { return ww.w.Print() }
func (ww *wailsWindow) OpenDevTools()        { ww.w.OpenDevTools() }
func (ww *wailsWindow) CloseDevTools() {
	if closer, ok := any(ww.w).(interface{ CloseDevTools() }); ok {
		closer.CloseDevTools()
	}
}

func (ww *wailsWindow) OnWindowEvent(handler func(event WindowEvent)) {
	name := ww.w.Name()

	// Map common Wails window events to our WindowEvent type.
	eventMap := map[events.WindowEventType]string{
		events.Common.WindowFocus:                "focus",
		events.Common.WindowLostFocus:            "blur",
		events.Common.WindowDidMove:              "move",
		events.Common.WindowDidResize:            "resize",
		events.Common.WindowClosing:              "close",
		events.Common.WindowHide:                 "hide",
		events.Common.WindowShow:                 "show",
		events.Common.WindowMinimise:             "minimise",
		events.Common.WindowUnMinimise:           "unminimise",
		events.Common.WindowMaximise:             "maximise",
		events.Common.WindowUnMaximise:           "unmaximise",
		events.Common.WindowFullscreen:           "fullscreen",
		events.Common.WindowUnFullscreen:         "unfullscreen",
		events.Common.WindowRuntimeReady:         "ready",
		events.Windows.WindowNonClientHit:        "nonclient-hit",
		events.Windows.WindowNonClientMouseDown:  "nonclient-mouse-down",
		events.Windows.WindowNonClientMouseUp:    "nonclient-mouse-up",
		events.Windows.WindowNonClientMouseMove:  "nonclient-mouse-move",
		events.Windows.WindowNonClientMouseLeave: "nonclient-mouse-leave",
	}

	for eventType, eventName := range eventMap {
		typeName := eventName // capture for closure
		ww.w.OnWindowEvent(eventType, func(event *application.WindowEvent) {
			data := make(map[string]any)
			switch typeName {
			case "move":
				x, y := ww.w.Position()
				data["x"] = x
				data["y"] = y
			case "resize":
				w, h := ww.w.Size()
				data["width"] = w
				data["height"] = h
			}
			handler(WindowEvent{
				Type: typeName,
				Name: name,
				Data: data,
			})
		})
	}
}

func (ww *wailsWindow) OnFileDrop(handler func(paths []string, target *DropTarget)) {
	ww.w.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		details := event.Context().DropTargetDetails()
		var target *DropTarget
		if details != nil {
			target = &DropTarget{
				ID:         details.ElementID,
				X:          details.X,
				Y:          details.Y,
				ClassList:  details.ClassList,
				Attributes: details.Attributes,
			}
		}
		handler(files, target)
	})
}

// SetCloseBehavior wires a RegisterHook on the wails close event
// implementing the requested behaviour. Default (Destroy) detaches
// any prior hook so the close proceeds naturally.
func (ww *wailsWindow) SetCloseBehavior(behavior CloseBehavior) {
	if ww == nil || ww.w == nil {
		return
	}
	switch behavior {
	case CloseBehaviorHide:
		ww.w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			ww.w.Hide()
			e.Cancel()
		})
	case CloseBehaviorQuit:
		ww.w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			if ww.app != nil {
				ww.app.Quit()
			}
		})
	case CloseBehaviorDestroy, "":
		// Default — no hook; the wails default close path runs.
		// Wails alpha.91 has no public Unhook API, so once a hook is
		// installed the consumer can only replace it, not remove it.
		// Calling with Destroy after Hide installs a pass-through
		// hook that simply returns without cancelling.
		ww.w.RegisterHook(events.Common.WindowClosing, func(_ *application.WindowEvent) {})
	}
}

// Ensure wailsWindow satisfies PlatformWindow at compile time.
var _ PlatformWindow = (*wailsWindow)(nil)

// Ensure WailsPlatform satisfies Platform at compile time.
var _ Platform = (*WailsPlatform)(nil)
