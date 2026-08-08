package window

import "sync"

// The mock is driven from more than one goroutine, so it is guarded like the
// thing it stands in for.
//
// A real platform window is called from wherever the Service happens to be:
// taskEvalJS runs ExecJS on the caller's goroutine while a test — or another
// task — reads the window's state on its own. mockWindow began as a bag of
// plain fields, which made that a data race rather than a design: `go test
// ./display/webkit/pkg/window/ -race` failed in TestTaskEvalJS_Good, one
// goroutine appending to execJSCalls inside ExecJS while the test read the same
// slice.
//
// One mutex per window covers every field rather than only the slice that
// happened to be caught. Guarding one field would leave the same defect latent
// in every other, waiting for the next test that drives the Service
// concurrently — and the cost here is a test double taking an uncontended lock.
type mockPlatform struct {
	mu      sync.Mutex
	windows []*mockWindow
}

func newMockPlatform() *mockPlatform {
	return &mockPlatform{}
}

func (m *mockPlatform) CreateWindow(options PlatformWindowOptions) PlatformWindow {
	w := &mockWindow{
		name: options.Name, title: options.Title, url: options.URL, html: options.HTML,
		width: options.Width, height: options.Height,
		x: options.X, y: options.Y,
		opacity: 1.0,
	}
	if options.JS != "" {
		w.execJSCalls = append(w.execJSCalls, options.JS)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.windows = append(m.windows, w)

	return w
}

func (m *mockPlatform) GetWindows() []PlatformWindow {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]PlatformWindow, len(m.windows))
	for i, w := range m.windows {
		out[i] = w
	}

	return out
}

type mockWindow struct {
	mu                     sync.Mutex
	name, title, url, html string
	width, height, x, y    int
	maximised, focused     bool
	visible, alwaysOnTop   bool
	backgroundColour       [4]uint8
	closed                 bool
	minimised              bool
	fullscreened           bool
	zoom                   float64
	opacity                float64
	contentProtection      bool
	flashed                bool
	devToolsOpen           bool
	execJSCalls            []string
	eventHandlers          []func(WindowEvent)
	fileDropHandlers       []func(paths []string, target *DropTarget)
	closeBehavior          CloseBehavior
}

func (w *mockWindow) Name() string  { w.mu.Lock(); defer w.mu.Unlock(); return w.name }
func (w *mockWindow) Title() string { w.mu.Lock(); defer w.mu.Unlock(); return w.title }
func (w *mockWindow) Position() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.x, w.y
}

func (w *mockWindow) Size() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.width, w.height
}
func (w *mockWindow) IsMaximised() bool   { w.mu.Lock(); defer w.mu.Unlock(); return w.maximised }
func (w *mockWindow) IsFocused() bool     { w.mu.Lock(); defer w.mu.Unlock(); return w.focused }
func (w *mockWindow) IsVisible() bool     { w.mu.Lock(); defer w.mu.Unlock(); return w.visible }
func (w *mockWindow) IsFullscreen() bool  { w.mu.Lock(); defer w.mu.Unlock(); return w.fullscreened }
func (w *mockWindow) IsMinimised() bool   { w.mu.Lock(); defer w.mu.Unlock(); return w.minimised }
func (w *mockWindow) IsAlwaysOnTop() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.alwaysOnTop }
func (w *mockWindow) GetBounds() (int, int, int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.x, w.y, w.width, w.height
}

func (w *mockWindow) GetZoom() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.zoom == 0 {
		return 1.0
	}

	return w.zoom
}
func (w *mockWindow) GetOpacity() float64   { w.mu.Lock(); defer w.mu.Unlock(); return w.opacity }
func (w *mockWindow) SetTitle(title string) { w.mu.Lock(); defer w.mu.Unlock(); w.title = title }
func (w *mockWindow) SetPosition(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.x, w.y = x, y
}

func (w *mockWindow) SetSize(width, height int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.width, w.height = width, height
}

func (w *mockWindow) SetBackgroundColour(r, g, b, a uint8) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.backgroundColour = [4]uint8{r, g, b, a}
}

func (w *mockWindow) SetVisibility(visible bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.visible = visible
}

func (w *mockWindow) SetAlwaysOnTop(alwaysOnTop bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.alwaysOnTop = alwaysOnTop
}

func (w *mockWindow) SetOpacity(opacity float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.opacity = opacity
}

func (w *mockWindow) SetBounds(x, y, width, height int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.x = x
	w.y = y
	w.width = width
	w.height = height
}
func (w *mockWindow) SetURL(url string)   { w.mu.Lock(); defer w.mu.Unlock(); w.url = url }
func (w *mockWindow) SetHTML(html string) { w.mu.Lock(); defer w.mu.Unlock(); w.html = html }
func (w *mockWindow) SetZoom(magnification float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.zoom = magnification
}

func (w *mockWindow) SetContentProtection(protection bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.contentProtection = protection
}
func (w *mockWindow) Maximise()     { w.mu.Lock(); defer w.mu.Unlock(); w.maximised = true }
func (w *mockWindow) Restore()      { w.mu.Lock(); defer w.mu.Unlock(); w.maximised = false }
func (w *mockWindow) Minimise()     { w.mu.Lock(); defer w.mu.Unlock(); w.minimised = true }
func (w *mockWindow) Focus()        { w.mu.Lock(); defer w.mu.Unlock(); w.focused = true }
func (w *mockWindow) Close()        { w.mu.Lock(); defer w.mu.Unlock(); w.closed = true }
func (w *mockWindow) Show()         { w.mu.Lock(); defer w.mu.Unlock(); w.visible = true }
func (w *mockWindow) Hide()         { w.mu.Lock(); defer w.mu.Unlock(); w.visible = false }
func (w *mockWindow) Fullscreen()   { w.mu.Lock(); defer w.mu.Unlock(); w.fullscreened = true }
func (w *mockWindow) UnFullscreen() { w.mu.Lock(); defer w.mu.Unlock(); w.fullscreened = false }
func (w *mockWindow) ToggleFullscreen() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fullscreened = !w.fullscreened
}

func (w *mockWindow) ToggleMaximise() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maximised = !w.maximised
}

func (w *mockWindow) ExecJS(js string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.execJSCalls = append(w.execJSCalls, js)
}
func (w *mockWindow) Flash(enabled bool)   { w.mu.Lock(); defer w.mu.Unlock(); w.flashed = enabled }
func (w *mockWindow) Print() resultFailure { return nil }
func (w *mockWindow) OpenDevTools()        { w.mu.Lock(); defer w.mu.Unlock(); w.devToolsOpen = true }
func (w *mockWindow) CloseDevTools()       { w.mu.Lock(); defer w.mu.Unlock(); w.devToolsOpen = false }
func (w *mockWindow) OnWindowEvent(handler func(WindowEvent)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.eventHandlers = append(w.eventHandlers, handler)
}

func (w *mockWindow) OnFileDrop(handler func(paths []string, target *DropTarget)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fileDropHandlers = append(w.fileDropHandlers, handler)
}

func (w *mockWindow) SetCloseBehavior(behavior CloseBehavior) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closeBehavior = behavior
}

// execJSCallsSnapshot is how a test reads what was executed.
//
// A copy, taken under the lock, because the slice it copies is appended to by
// whichever goroutine the Service ran ExecJS on — reading the field directly is
// the race this file exists to have fixed.
func (w *mockWindow) execJSCallsSnapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]string(nil), w.execJSCalls...)
}

// emit fires a test event to all registered handlers.
//
// The handlers are copied under the lock and called outside it: a handler is
// free to call back into the window it was registered on, and holding the lock
// across the call would deadlock on the first one that does.
func (w *mockWindow) emit(e WindowEvent) {
	w.mu.Lock()
	handlers := make([]func(WindowEvent), len(w.eventHandlers))
	copy(handlers, w.eventHandlers)
	w.mu.Unlock()

	for _, h := range handlers {
		h(e)
	}
}

// emitFileDrop simulates a file drop on the window. Pass nil target
// for legacy zero-context drops, or a DropTarget with the element
// metadata the consumer expects to receive.
//
// Handlers are copied and called outside the lock, for the reason {@see emit}
// gives.
func (w *mockWindow) emitFileDrop(paths []string, target *DropTarget) {
	w.mu.Lock()
	handlers := make([]func(paths []string, target *DropTarget), len(w.fileDropHandlers))
	copy(handlers, w.fileDropHandlers)
	w.mu.Unlock()

	for _, h := range handlers {
		h(paths, target)
	}
}

// recordingBinder is a test Platform that composes mockPlatform's
// window-creation surface AND implements CustomEventBinder by
// recording every (name, cb) pair the Service registers. Tests
// inspect bindings to prove the wiring without depending on
// app.Event.On (Wails-only).
type recordingBinder struct {
	mockPlatform
	bindingsMu sync.Mutex
	bindings   []recordedBinding
}

type recordedBinding struct {
	name string
	cb   func(any)
}

// BindCustomEvent satisfies CustomEventBinder.
func (r *recordingBinder) BindCustomEvent(name string, cb func(data any)) {
	r.bindingsMu.Lock()
	defer r.bindingsMu.Unlock()
	r.bindings = append(r.bindings, recordedBinding{name: name, cb: cb})
}
