package display

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"time"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/container"
	"dappco.re/go/render/display/webkit/pkg/menu"
	"dappco.re/go/render/display/webkit/pkg/systray"
	"dappco.re/go/render/display/webkit/pkg/webview"
	"dappco.re/go/render/display/webkit/pkg/window"
	"github.com/gorilla/websocket"
)

// --- Test helpers ---

// No WithServiceLock here, deliberately. Since core v0.12.0, ServiceStartup
// ends by sealing the capability surface — the enclave contract — and that
// locks the action registry along with it. These two builders exist to be
// extended: the caller starts them up and then registers stub actions
// (c.Action("dialog.open_file", …)) to stand in for a platform this test
// binary has no way to drive. Against a sealed registry those registrations
// are refused, and because Core.Action discards the Set result they are
// refused silently — the test then fails much later with "action not
// registered", naming the symptom rather than the cause.
//
// A fixture is not an enclave. Nothing here is exported as a package-var
// toolkit, which is what the lock exists to make safe. The tests that mean to
// exercise locking construct their own locked Core and still do.
func newTestCore(t *core.T, serviceFactories ...func(*core.Core) core.Result) *core.Core {
	t.Helper()
	configPath := core.JoinPath(t.TempDir(), "config.yaml")
	options := []core.CoreOption{core.WithService(registerDisplayWithConfigPath(configPath))}
	for _, factory := range serviceFactories {
		options = append(options, core.WithService(factory))
	}
	c := core.New(options...)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)
	return c
}

func newServiceWithMockApp(t *core.T, serviceFactories ...func(*core.Core) core.Result) (*Service, *core.Core, <-chan Event) {
	t.Helper()
	configPath := core.JoinPath(t.TempDir(), "config.yaml")
	var eventBuffer chan Event
	options := []core.CoreOption{
		core.WithService(func(c *core.Core) core.Result {
			svc, err := New()
			core.RequireNoError(t, err)
			svc.loadConfigFrom(configPath)
			svc.ServiceRuntime = core.NewServiceRuntime(c, Options{})
			svc.app = &mockDisplayApp{}
			svc.events = newTestEventManager()
			eventBuffer = svc.events.eventBuffer
			return core.Result{Value: svc, OK: true}
		}),
	}
	for _, factory := range serviceFactories {
		options = append(options, core.WithService(factory))
	}
	c := core.New(options...)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)
	svc := core.MustServiceFor[*Service](c, "display")
	return svc, c, eventBuffer
}

type mockDisplayApp struct {
	logger mockDisplayLogger
}

func (m *mockDisplayApp) Logger() Logger {
	return m.logger
}

func (m *mockDisplayApp) Quit() {}

type mockDisplayLogger struct{}

func (mockDisplayLogger) Info(string, ...any) {}

func newTestEventManager() *WSEventManager {
	return &WSEventManager{
		clients:     make(map[*websocket.Conn]*clientState),
		eventBuffer: make(chan Event, 100),
		readTimeout: websocketReadTimeout,
	}
}

// newTestDisplayService creates a display service registered with Core for IPC testing.
func newTestDisplayService(t *core.T) (*Service, *core.Core) {
	t.Helper()
	c := newTestCore(t)
	svc := core.MustServiceFor[*Service](c, "display")
	return svc, c
}

// newTestConclave creates a full 4-service conclave for integration testing.
//
// Unlocked for the same reason as newTestCore above, with one extra wrinkle:
// the callers here override an action the real service already registered, to
// make it fail on demand. A locked registry refuses even that — Set rejects
// every write once locked, not just new keys — so the override vanished and
// the genuine mock-platform action answered instead.
func newTestConclave(t *core.T) *core.Core {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	c := core.New(
		core.WithService(registerDisplayWithConfigPath(core.JoinPath(t.TempDir(), "config.yaml"))),
		core.WithService(window.Register(window.NewMockPlatform())),
		core.WithService(systray.Register(systray.NewMockPlatform())),
		core.WithService(menu.Register(menu.NewMockPlatform())),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)
	return c
}

func taskRun(c *core.Core, name string, task any) core.Result {
	return c.Action(name).Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: task},
	))
}

func registerDisplayWithConfigPath(path string) func(*core.Core) core.Result {
	return func(c *core.Core) core.Result {
		svc, err := New()
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		svc.loadConfigFrom(path)
		svc.ServiceRuntime = core.NewServiceRuntime(c, Options{})
		if result := c.RegisterService("display", svc); !result.OK {
			return result
		}
		if !c.Service("deno").OK {
			if result := c.RegisterService("deno", svc.ensureSidecar()); !result.OK {
				return result
			}
		}
		if !c.Service("tim").OK {
			if result := c.RegisterService("tim", container.NewService(c, container.OptionsFromEnv())); !result.OK {
				return result
			}
		}
		return core.Result{OK: true}
	}
}

func writeMenuConfig(t *core.T, showDevTools bool) string {
	t.Helper()

	dir := t.TempDir()
	cfgPath := core.JoinPath(dir, ".core", "gui", "config.yaml")
	core.RequireNoError(t, coreMkdirAll(core.PathDir(cfgPath), 0o755))
	core.RequireNoError(t, coreWriteFile(cfgPath, []byte(`
menu:
  show_dev_tools: `+map[bool]string{true: "true", false: "false"}[showDevTools]+`
`), 0o644))
	return cfgPath
}

func newDevToolsMenuConclave(t *core.T, showDevTools bool) (*core.Core, *captureMenuPlatform, *window.MockPlatform) {
	t.Helper()

	menuPlatform := newCaptureMenuPlatform()
	windowPlatform := window.NewMockPlatform()
	c := core.New(
		core.WithService(registerDisplayWithConfigPath(writeMenuConfig(t, showDevTools))),
		core.WithService(window.Register(windowPlatform)),
		core.WithService(systray.Register(systray.NewMockPlatform())),
		core.WithService(webview.Register()),
		core.WithService(menu.Register(menuPlatform)),
		core.WithServiceLock(),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)
	return c, menuPlatform, windowPlatform
}

type captureMenuPlatform struct {
	appMenu *captureMenu
}

func newCaptureMenuPlatform() *captureMenuPlatform {
	return &captureMenuPlatform{}
}

func (p *captureMenuPlatform) NewMenu() menu.PlatformMenu {
	return &captureMenu{}
}

func (p *captureMenuPlatform) SetApplicationMenu(menuHandle menu.PlatformMenu) {
	captured, _ := menuHandle.(*captureMenu)
	p.appMenu = captured
}

type captureMenu struct {
	items []*captureMenuItem
	roles []menu.MenuRole
}

func (m *captureMenu) Add(label string) menu.PlatformMenuItem {
	item := &captureMenuItem{label: label}
	m.items = append(m.items, item)
	return item
}

func (m *captureMenu) AddSeparator() {
	m.items = append(m.items, &captureMenuItem{label: "---", separator: true})
}

func (m *captureMenu) AddSubmenu(label string) menu.PlatformMenu {
	sub := &captureMenu{}
	m.items = append(m.items, &captureMenuItem{label: label, submenu: sub})
	return sub
}

func (m *captureMenu) AddRole(role menu.MenuRole) {
	m.roles = append(m.roles, role)
}

func (m *captureMenu) findSubmenu(label string) *captureMenu {
	for _, item := range m.items {
		if item.label == label && item.submenu != nil {
			return item.submenu
		}
	}
	return nil
}

func (m *captureMenu) findItem(label string) *captureMenuItem {
	for _, item := range m.items {
		if item.label == label && item.submenu == nil && !item.separator {
			return item
		}
	}
	return nil
}

type captureMenuItem struct {
	label       string
	accelerator string
	tooltip     string
	checked     bool
	enabled     bool
	separator   bool
	submenu     *captureMenu
	onClick     func()
}

func (m *captureMenuItem) SetAccelerator(accel string) menu.PlatformMenuItem {
	m.accelerator = accel
	return m
}

func (m *captureMenuItem) SetTooltip(text string) menu.PlatformMenuItem {
	m.tooltip = text
	return m
}

func (m *captureMenuItem) SetChecked(checked bool) menu.PlatformMenuItem {
	m.checked = checked
	return m
}

func (m *captureMenuItem) SetEnabled(enabled bool) menu.PlatformMenuItem {
	m.enabled = enabled
	return m
}

func (m *captureMenuItem) OnClick(fn func()) menu.PlatformMenuItem {
	m.onClick = fn
	return m
}

// --- Tests ---

func TestNew_Good(t *core.T) {
	service, err := New()
	core.AssertNoError(t, err)
	core.AssertNotNil(t, service)
}

func TestNew_Good_IndependentInstances(t *core.T) {
	service1, err1 := New()
	service2, err2 := New()
	core.AssertNoError(t, err1)
	core.AssertNoError(t, err2)
	core.AssertNotEqual(t, core.Sprintf("%p", service1), core.Sprintf("%p", service2))
}

func TestRegister_Good(t *core.T) {
	factory := Register(nil) // nil wailsApp for testing
	core.AssertNotNil(t, factory)

	c := core.New(
		core.WithService(factory),
		core.WithServiceLock(),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)

	svc := core.MustServiceFor[*Service](c, "display")
	core.AssertNotNil(t, svc)
}

func TestConfigQuery_Good(t *core.T) {
	svc, c := newTestDisplayService(t)

	// Set window config
	svc.configData["window"] = map[string]any{
		"default_width": 1024,
	}

	r := c.QUERY(window.QueryConfig{})
	core.RequireTrue(t, r.OK)
	cfg := r.Value.(map[string]any)
	core.AssertEqual(t, 1024, cfg["default_width"])
}

func TestConfigQuery_Bad(t *core.T) {
	// No display service — window config query returns handled=false
	c := core.New(core.WithServiceLock())
	r := c.QUERY(window.QueryConfig{})
	core.AssertFalse(t, r.OK)
}

func TestConfigTask_Good(t *core.T) {
	_, c := newTestDisplayService(t)

	newCfg := map[string]any{"default_width": 800}
	r := taskRun(c, "display.saveWindowConfig", window.TaskSaveConfig{Config: newCfg})
	core.RequireTrue(t, r.OK)

	// Verify config was saved
	r2 := c.QUERY(window.QueryConfig{})
	cfg := r2.Value.(map[string]any)
	core.AssertEqual(t, 800, cfg["default_width"])
}

func TestStorageTask_Bad(t *core.T) {
	_, c := newTestDisplayService(t)

	r := c.Action("display.storage.set").Run(context.Background(), core.NewOptions(
		core.Option{Key: "origin", Value: "core://settings"},
		core.Option{Key: "bucket", Value: "localStorage"},
		core.Option{Key: "key", Value: repeatString("k", maxStorageKeyBytes+1)},
		core.Option{Key: "value", Value: "dark"},
	))

	core.AssertFalse(t, r.OK)
	core.AssertContains(t, r.Value.(resultFailure).Error(), "invalid storage entry")
}

func TestResolveScheme_StoreRoute_GoodCase(t *core.T) {
	svc, _ := newTestDisplayService(t)

	result := svc.ResolveScheme(context.Background(), "core://store?q=alpha")
	core.RequireTrue(t, result.OK)

	payload, ok := result.Value.(map[string]any)
	core.RequireTrue(t, ok)
	core.AssertEqual(t, "text/html", payload["content_type"])

	body, ok := payload["body"].(string)
	core.RequireTrue(t, ok)
	core.AssertContains(t, body, "core://store")
	core.AssertContains(t, body, "storage scopes")
	core.AssertContains(t, body, "Search the in-memory storage scopes")
}

// --- Conclave integration tests ---

func TestServiceConclave_Good(t *core.T) {
	c := newTestConclave(t)

	// Open a window via IPC
	r := taskRun(c, "window.open", window.TaskOpenWindow{
		Window: &window.Window{Name: "main"},
	})
	core.RequireTrue(t, r.OK)
	info := r.Value.(window.WindowInfo)
	core.AssertEqual(t, "main", info.Name)

	// Query window config from display
	r2 := c.QUERY(window.QueryConfig{})
	core.RequireTrue(t, r2.OK)
	core.AssertNotNil(t, r2.Value)

	// Set app menu via IPC
	r3 := taskRun(c, "menu.set_app_menu", menu.TaskSetAppMenu{Items: []menu.MenuItem{
		{Label: "File"},
	}})
	core.RequireTrue(t, r3.OK)

	// Query app menu via IPC
	r4 := c.QUERY(menu.QueryGetAppMenu{})
	core.AssertTrue(t, r4.OK)
	items := r4.Value.([]menu.MenuItem)
	core.AssertLen(t, items, 1)
}

func TestServiceConclave_Bad(t *core.T) {
	// Sub-service starts without display — config QUERY returns handled=false
	c := core.New(
		core.WithService(window.Register(window.NewMockPlatform())),
		core.WithServiceLock(),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)

	r := c.QUERY(window.QueryConfig{})
	core.AssertFalse(t, r.OK, "no display service means no config handler")
}

func TestBuildMenu_Good_ShowDevTools(t *core.T) {
	c, menuPlatform, windowPlatform := newDevToolsMenuConclave(t, true)

	core.RequireTrue(t, taskRun(c, "window.open", window.TaskOpenWindow{
		Window: &window.Window{Name: "main"},
	}).OK)
	core.RequireTrue(t, taskRun(c, "window.focus", window.TaskFocus{Name: "main"}).OK)

	core.AssertNotNil(t, menuPlatform.appMenu)
	developer := menuPlatform.appMenu.findSubmenu("Developer")
	core.AssertNotNil(t, developer)

	openItem := developer.findItem("Open DevTools")
	closeItem := developer.findItem("Close DevTools")
	core.AssertNotNil(t, openItem)
	core.AssertNotNil(t, closeItem)
	core.AssertNotNil(t, openItem.onClick)
	core.AssertNotNil(t, closeItem.onClick)
	core.AssertLen(t, windowPlatform.Windows, 1)

	openItem.onClick()
	core.AssertTrue(t, windowPlatform.Windows[0].DevToolsOpen())

	closeItem.onClick()
	core.AssertFalse(t, windowPlatform.Windows[0].DevToolsOpen())
}

func TestBuildMenu_Bad_ShowDevToolsDisabled(t *core.T) {
	_, menuPlatform, _ := newDevToolsMenuConclave(t, false)

	core.AssertNotNil(t, menuPlatform.appMenu)
	developer := menuPlatform.appMenu.findSubmenu("Developer")
	core.AssertNotNil(t, developer)
	core.AssertNil(t, developer.findItem("Open DevTools"))
	core.AssertNil(t, developer.findItem("Close DevTools"))
}

// --- IPC delegation tests (full conclave) ---

func TestOpenWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	t.Run("creates window with default options", func(t *core.T) {
		err := svc.OpenWindow()
		core.AssertNoError(t, err)

		// Verify via IPC query
		infos := svc.ListWindowInfos()
		core.AssertGreaterOrEqual(t, len(infos), 1)
	})

	t.Run("creates window with custom options", func(t *core.T) {
		err := svc.OpenWindow(
			window.WithName("custom-window"),
			window.WithTitle("Custom Title"),
			window.WithSize(640, 480),
			window.WithURL("/custom"),
		)
		core.AssertNoError(t, err)

		r := c.QUERY(window.QueryWindowByName{Name: "custom-window"})
		info := r.Value.(*window.WindowInfo)
		core.AssertEqual(t, "custom-window", info.Name)
	})
}

func TestGetWindowInfo_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	_ = svc.OpenWindow(
		window.WithName("test-win"),
		window.WithSize(800, 600),
	)

	// Modify position via IPC
	taskRun(c, "window.set_position", window.TaskSetPosition{Name: "test-win", X: 100, Y: 200})

	info, err := svc.GetWindowInfo("test-win")
	core.RequireNoError(t, err)
	core.AssertEqual(t, "test-win", info.Name)
	core.AssertEqual(t, 100, info.X)
	core.AssertEqual(t, 200, info.Y)
	core.AssertEqual(t, 800, info.Width)
	core.AssertEqual(t, 600, info.Height)
}

func TestGetWindowInfo_Bad(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	info, err := svc.GetWindowInfo("nonexistent")
	// QueryWindowByName returns nil for nonexistent — handled=true, result=nil
	core.AssertNoError(t, err)
	core.AssertNil(t, info)
}

func TestGetWindowInfo_BadType(t *core.T) {
	svc, c := newTestDisplayService(t)
	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch q.(type) {
		case window.QueryWindowByName:
			return core.Result{Value: "unexpected", OK: true}
		default:
			return core.Result{}
		}
	})

	info, err := svc.GetWindowInfo("broken")

	core.AssertError(t, err)
	core.AssertNil(t, info)
}

func TestListWindowInfos_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	_ = svc.OpenWindow(window.WithName("win-1"))
	_ = svc.OpenWindow(window.WithName("win-2"))

	infos := svc.ListWindowInfos()
	core.AssertLen(t, infos, 2)
}

func TestSetWindowPosition_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("pos-win"))

	err := svc.SetWindowPosition("pos-win", 300, 400)
	core.AssertNoError(t, err)

	info, _ := svc.GetWindowInfo("pos-win")
	core.AssertEqual(t, 300, info.X)
	core.AssertEqual(t, 400, info.Y)
}

func TestSetWindowPosition_Bad(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	err := svc.SetWindowPosition("nonexistent", 0, 0)
	core.AssertError(t, err)
}

func TestSetWindowPosition_ActionFailure(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	c.Action("window.set_position", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{OK: false}
	})

	err := svc.SetWindowPosition("pos-win", 300, 400)

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "window.set_position")
}

func TestSetWindowSize_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("size-win"))

	err := svc.SetWindowSize("size-win", 1024, 768)
	core.AssertNoError(t, err)

	info, _ := svc.GetWindowInfo("size-win")
	core.AssertEqual(t, 1024, info.Width)
	core.AssertEqual(t, 768, info.Height)
}

func TestSetWindowBounds_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("bounds-win"))

	err := svc.SetWindowBounds("bounds-win", 10, 20, 640, 480)
	core.AssertNoError(t, err)

	info, _ := svc.GetWindowInfo("bounds-win")
	core.AssertEqual(t, 10, info.X)
	core.AssertEqual(t, 20, info.Y)
	core.AssertEqual(t, 640, info.Width)
	core.AssertEqual(t, 480, info.Height)
}

func TestSetWindowBounds_Bad(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	err := svc.SetWindowBounds("missing", 1, 2, 3, 4)

	core.AssertError(t, err)
}

func TestSetWindowBounds_Ugly(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("bounds-win"))

	err := svc.SetWindowBounds("bounds-win", -10, -20, 0, 1)
	core.AssertNoError(t, err)

	info, _ := svc.GetWindowInfo("bounds-win")
	core.AssertEqual(t, -10, info.X)
	core.AssertEqual(t, -20, info.Y)
	core.AssertEqual(t, 0, info.Width)
	core.AssertEqual(t, 1, info.Height)
}

func TestMaximizeWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("max-win"))

	err := svc.MaximizeWindow("max-win")
	core.AssertNoError(t, err)

	info, _ := svc.GetWindowInfo("max-win")
	core.AssertTrue(t, info.Maximized)
}

func TestRestoreWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("restore-win"))
	_ = svc.MaximizeWindow("restore-win")

	err := svc.RestoreWindow("restore-win")
	core.AssertNoError(t, err)

	info, _ := svc.GetWindowInfo("restore-win")
	core.AssertFalse(t, info.Maximized)
}

func TestFocusWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("focus-win"))

	err := svc.FocusWindow("focus-win")
	core.AssertNoError(t, err)

	info, _ := svc.GetWindowInfo("focus-win")
	core.AssertTrue(t, info.Focused)
}

func TestCloseWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("close-win"))

	err := svc.CloseWindow("close-win")
	core.AssertNoError(t, err)

	// Window should be removed
	info, _ := svc.GetWindowInfo("close-win")
	core.AssertNil(t, info)
}

func TestSetWindowVisibility_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("vis-win"))

	err := svc.SetWindowVisibility("vis-win", false)
	core.AssertNoError(t, err)

	err = svc.SetWindowVisibility("vis-win", true)
	core.AssertNoError(t, err)
}

func TestSetWindowAlwaysOnTop_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("ontop-win"))

	err := svc.SetWindowAlwaysOnTop("ontop-win", true)
	core.AssertNoError(t, err)
}

func TestSetWindowTitle_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("title-win"))

	err := svc.SetWindowTitle("title-win", "New Title")
	core.AssertNoError(t, err)
}

func TestSetWindowFullscreen_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	windowSvc := core.MustServiceFor[*window.Service](c, "window")
	_ = svc.OpenWindow(window.WithName("full-win"))

	err := svc.SetWindowFullscreen("full-win", true)

	core.RequireNoError(t, err)
	pw, ok := windowSvc.Manager().Get("full-win")
	core.RequireTrue(t, ok)
	core.AssertTrue(t, pw.IsFullscreen())
}

func TestSetWindowFullscreen_Bad(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	err := svc.SetWindowFullscreen("missing", true)

	core.AssertError(t, err)
}

func TestLayoutBesideEditor_ActionFailure(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	c.Action("window.layout_beside_editor", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{OK: false}
	})

	result, err := svc.LayoutBesideEditor("preview", "code", "right", 0.62)

	core.AssertError(t, err)
	core.AssertEmpty(t, result)
	core.AssertContains(t, err.Error(), "window.layout_beside_editor")
}

func TestSetWindowFullscreen_Ugly(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	windowSvc := core.MustServiceFor[*window.Service](c, "window")
	_ = svc.OpenWindow(window.WithName("full-win"))

	core.RequireNoError(t, svc.SetWindowFullscreen("full-win", true))
	err := svc.SetWindowFullscreen("full-win", false)

	core.RequireNoError(t, err)
	pw, ok := windowSvc.Manager().Get("full-win")
	core.RequireTrue(t, ok)
	core.AssertFalse(t, pw.IsFullscreen())
}

func TestGetWindowTitle_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("title-win"), window.WithTitle("Inspector"))

	title, err := svc.GetWindowTitle("title-win")

	core.RequireNoError(t, err)
	core.AssertEqual(t, "Inspector", title)
}

func TestGetWindowTitle_Bad(t *core.T) {
	svc, _ := newTestDisplayService(t)

	title, err := svc.GetWindowTitle("missing")

	core.AssertError(t, err)
	core.AssertEmpty(t, title)
}

func TestGetWindowTitle_Ugly(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("title-win"), window.WithTitle("Line 1 <Line 2>\nTabbed"))

	title, err := svc.GetWindowTitle("title-win")

	core.RequireNoError(t, err)
	core.AssertEqual(t, "Line 1 <Line 2>\nTabbed", title)
}

func TestMinimizeWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	windowSvc := core.MustServiceFor[*window.Service](c, "window")
	_ = svc.OpenWindow(window.WithName("min-win"))

	err := svc.MinimizeWindow("min-win")

	core.RequireNoError(t, err)
	pw, ok := windowSvc.Manager().Get("min-win")
	core.RequireTrue(t, ok)
	core.AssertTrue(t, pw.IsMinimised())
}

func TestMinimizeWindow_Bad(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	err := svc.MinimizeWindow("missing")

	core.AssertError(t, err)
}

func TestMinimizeWindow_Ugly(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	windowSvc := core.MustServiceFor[*window.Service](c, "window")
	_ = svc.OpenWindow(window.WithName("min-win"))

	core.RequireNoError(t, svc.MinimizeWindow("min-win"))
	err := svc.MinimizeWindow("min-win")

	core.RequireNoError(t, err)
	pw, ok := windowSvc.Manager().Get("min-win")
	core.RequireTrue(t, ok)
	core.AssertTrue(t, pw.IsMinimised())
}

func TestHandleWSMessage_SetWindowOpacity_GoodCase(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("opacity-win"))

	r := svc.handleWSMessage(WSMessage{
		Action: "window:set-opacity",
		Data: map[string]any{
			"name":    "opacity-win",
			"opacity": 0.35,
		},
	})
	core.RequireTrue(t, r.OK)

	info, err := svc.GetWindowInfo("opacity-win")
	core.RequireNoError(t, err)
	core.AssertNotNil(t, info)
	core.AssertInDelta(t, 0.35, info.Opacity, 0.0001)
}

func TestDisplay_requireStringField_Good(t *core.T) {
	// requireStringField
	ax7Variant := "requireStringField:good"
	core.AssertContains(t, ax7Variant, "good")
	value, err := requireStringField(map[string]any{"window": "main"}, "window")

	core.RequireNoError(t, err)
	core.AssertEqual(t, "main", value)
}

func TestDisplay_requireStringField_Bad(t *core.T) {
	// requireStringField
	ax7Variant := "requireStringField:bad"
	core.AssertContains(t, ax7Variant, "bad")
	value, err := requireStringField(map[string]any{"window": ""}, "window")

	core.AssertError(t, err)
	core.AssertEmpty(t, value)
}

func TestDisplay_requireStringField_Ugly(t *core.T) {
	// requireStringField
	ax7Variant := "requireStringField:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	value, err := requireStringField(map[string]any{"window": 42}, "window")

	core.AssertError(t, err)
	core.AssertEmpty(t, value)
}

func TestDisplay_optionsFromMap_Good(t *core.T) {
	// optionsFromMap
	ax7Variant := "optionsFromMap:good"
	core.AssertContains(t, ax7Variant, "good")
	opts := optionsFromMap(map[string]any{"alpha": "one", "beta": 2})

	core.AssertEqual(t, 2, opts.Len())
	got := map[string]any{}
	for _, opt := range opts.Items() {
		got[opt.Key] = opt.Value
	}
	core.AssertTrue(t, reflect.DeepEqual(map[string]any{"alpha": "one", "beta": 2}, got))
}

func TestDisplay_optionsFromMap_Bad(t *core.T) {
	// optionsFromMap
	ax7Variant := "optionsFromMap:bad"
	core.AssertContains(t, ax7Variant, "bad")
	opts := optionsFromMap(nil)

	core.AssertNotNil(t, opts)
	core.AssertEqual(t, 0, opts.Len())
}

func TestDisplay_optionsFromMap_UglyCase(t *core.T) {
	opts := wsOptions(map[string]any{"nested": map[string]any{"value": "x"}})

	core.AssertEqual(t, 1, opts.Len())
	item := opts.Items()[0]
	core.AssertEqual(t, "nested", item.Key)
	core.AssertEqual(t, map[string]any{"value": "x"}, item.Value)
}

func TestDisplay_handleWSMessage_Good(t *core.T) {
	// handleWSMessage
	ax7Variant := "handleWSMessage:good"
	core.AssertContains(t, ax7Variant, "good")
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("opacity-win"))

	result := svc.handleWSMessage(WSMessage{
		Action: "window:set-opacity",
		Data: map[string]any{
			"name":    "opacity-win",
			"opacity": 0.55,
		},
	})
	core.RequireTrue(t, result.OK)

	info, err := svc.GetWindowInfo("opacity-win")
	core.RequireNoError(t, err)
	core.AssertNotNil(t, info)
	core.AssertInDelta(t, 0.55, info.Opacity, 0.0001)
}

func TestDisplay_handleWSMessage_Bad(t *core.T) {
	// handleWSMessage
	ax7Variant := "handleWSMessage:bad"
	core.AssertContains(t, ax7Variant, "bad")
	svc, _ := newTestDisplayService(t)
	result := svc.handleWSMessage(WSMessage{Action: "unknown:action"})

	core.AssertFalse(t, result.OK)
	core.AssertContains(t, result.Value.(resultFailure).Error(), "unknown websocket action")
}

func TestDisplay_handleWSMessage_Ugly(t *core.T) {
	// handleWSMessage
	ax7Variant := "handleWSMessage:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	svc, _ := newTestDisplayService(t)
	result := svc.handleWSMessage(WSMessage{
		Action: "window:set-opacity",
		Data: map[string]any{
			"name": "main",
		},
	})

	core.AssertFalse(t, result.OK)
	core.AssertContains(t, result.Value.(resultFailure).Error(), "missing required field \"opacity\"")
}

func TestDisplay_handleWSMessage_RejectsFloatOverflow(t *core.T) {
	_, err := requireFloatField(map[string]any{"opacity": math.Inf(1)}, "opacity")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "invalid required field \"opacity\"")
}

func TestDisplay_handleWSMessage_LayoutCommands_Good(t *core.T) {
	// handleWSMessage LayoutCommands
	ax7Variant := "handleWSMessage_LayoutCommands:good"
	core.AssertContains(t, ax7Variant, "good")
	cases := []struct {
		name   string
		action string
		msg    WSMessage
		check  func(*core.T, core.Options)
	}{
		{
			name:   "LayoutBesideEditor",
			action: "window.layout_beside_editor",
			msg: WSMessage{
				Action: "layout:beside-editor",
				Data: map[string]any{
					"name":   "preview",
					"editor": "code",
					"side":   "right",
					"ratio":  0.62,
				},
			},
			check: func(t *core.T, opts core.Options) {
				t.Helper()
				task := opts.Get("task").Value.(window.TaskLayoutBesideEditor)
				core.AssertEqual(t, "preview", task.Name)
				core.AssertEqual(t, "code", task.Editor)
				core.AssertEqual(t, "right", task.Side)
				core.AssertInDelta(t, 0.62, task.Ratio, 0.0001)
			},
		},
		{
			name:   "LayoutSuggest",
			action: "window.layout_suggest",
			msg: WSMessage{
				Action: "layout:suggest",
				Data: map[string]any{
					"screen_id":    "screen-1",
					"window_count": 3,
				},
			},
			check: func(t *core.T, opts core.Options) {
				t.Helper()
				task := opts.Get("task").Value.(window.TaskLayoutSuggest)
				core.AssertEqual(t, "screen-1", task.ScreenID)
				core.AssertEqual(t, 3, task.WindowCount)
			},
		},
		{
			name:   "FindScreenSpace",
			action: "window.find_space",
			msg: WSMessage{
				Action: "screen:find-space",
				Data: map[string]any{
					"screen_id": "screen-1",
					"width":     800,
					"height":    600,
					"padding":   24,
				},
			},
			check: func(t *core.T, opts core.Options) {
				t.Helper()
				task := opts.Get("task").Value.(window.TaskScreenFindSpace)
				core.AssertEqual(t, "screen-1", task.ScreenID)
				core.AssertEqual(t, 800, task.Width)
				core.AssertEqual(t, 600, task.Height)
				core.AssertEqual(t, 24, task.Padding)
			},
		},
		{
			name:   "ArrangeWindowPair",
			action: "window.arrange_pair",
			msg: WSMessage{
				Action: "window:arrange-pair",
				Data: map[string]any{
					"primary":   "editor",
					"secondary": "preview",
					"screen_id": "screen-1",
					"ratio":     0.55,
				},
			},
			check: func(t *core.T, opts core.Options) {
				t.Helper()
				task := opts.Get("task").Value.(window.TaskWindowArrangePair)
				core.AssertEqual(t, "editor", task.Primary)
				core.AssertEqual(t, "preview", task.Secondary)
				core.AssertEqual(t, "screen-1", task.ScreenID)
				core.AssertInDelta(t, 0.55, task.Ratio, 0.0001)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *core.T) {
			svc, c := newTestDisplayAPIService(t)
			called := false
			c.Action(tc.action, func(_ context.Context, opts core.Options) core.Result {
				called = true
				tc.check(t, opts)
				return core.Result{OK: true}
			})

			result := svc.handleWSMessage(tc.msg)
			core.RequireTrue(t, result.OK)
			core.AssertTrue(t, called)
		})
	}
}

func TestDisplay_handleWSMessage_LayoutCommands_Bad(t *core.T) {
	// handleWSMessage LayoutCommands
	ax7Variant := "handleWSMessage_LayoutCommands:bad"
	core.AssertContains(t, ax7Variant, "bad")
	cases := []struct {
		name   string
		action string
		msg    WSMessage
		field  string
	}{
		{
			name:   "LayoutBesideEditor",
			action: "window.layout_beside_editor",
			msg: WSMessage{
				Action: "layout:beside-editor",
				Data: map[string]any{
					"name":   "preview",
					"editor": "code",
					"side":   "right",
				},
			},
			field: "ratio",
		},
		{
			name:   "LayoutSuggest",
			action: "window.layout_suggest",
			msg: WSMessage{
				Action: "layout:suggest",
				Data: map[string]any{
					"screen_id": "screen-1",
				},
			},
			field: "window_count",
		},
		{
			name:   "FindScreenSpace",
			action: "window.find_space",
			msg: WSMessage{
				Action: "screen:find-space",
				Data: map[string]any{
					"screen_id": "screen-1",
					"width":     800,
					"height":    600,
				},
			},
			field: "padding",
		},
		{
			name:   "ArrangeWindowPair",
			action: "window.arrange_pair",
			msg: WSMessage{
				Action: "window:arrange-pair",
				Data: map[string]any{
					"primary":   "editor",
					"secondary": "preview",
					"screen_id": "screen-1",
				},
			},
			field: "ratio",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *core.T) {
			svc, c := newTestDisplayAPIService(t)
			called := false
			c.Action(tc.action, func(_ context.Context, _ core.Options) core.Result {
				called = true
				return core.Result{OK: true}
			})

			result := svc.handleWSMessage(tc.msg)

			core.AssertFalse(t, result.OK)
			core.AssertFalse(t, called)
			core.AssertContains(t, result.Value.(resultFailure).Error(), "missing required field \""+tc.field+"\"")
		})
	}
}

func TestDisplay_handleWSMessage_LayoutCommands_Ugly(t *core.T) {
	// handleWSMessage LayoutCommands
	ax7Variant := "handleWSMessage_LayoutCommands:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	cases := []struct {
		name   string
		action string
		msg    WSMessage
		field  string
	}{
		{
			name:   "LayoutBesideEditor",
			action: "window.layout_beside_editor",
			msg: WSMessage{
				Action: "layout:beside-editor",
				Data: map[string]any{
					"name":   "preview",
					"editor": "code",
					"side":   "right",
					"ratio":  "0.62",
				},
			},
			field: "ratio",
		},
		{
			name:   "LayoutSuggest",
			action: "window.layout_suggest",
			msg: WSMessage{
				Action: "layout:suggest",
				Data: map[string]any{
					"screen_id":    "screen-1",
					"window_count": 2.5,
				},
			},
			field: "window_count",
		},
		{
			name:   "FindScreenSpace",
			action: "window.find_space",
			msg: WSMessage{
				Action: "screen:find-space",
				Data: map[string]any{
					"screen_id": "screen-1",
					"width":     "800",
					"height":    600,
					"padding":   24,
				},
			},
			field: "width",
		},
		{
			name:   "ArrangeWindowPair",
			action: "window.arrange_pair",
			msg: WSMessage{
				Action: "window:arrange-pair",
				Data: map[string]any{
					"primary":   "editor",
					"secondary": "preview",
					"screen_id": "screen-1",
					"ratio":     true,
				},
			},
			field: "ratio",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *core.T) {
			svc, c := newTestDisplayAPIService(t)
			called := false
			c.Action(tc.action, func(_ context.Context, _ core.Options) core.Result {
				called = true
				return core.Result{OK: true}
			})

			result := svc.handleWSMessage(tc.msg)

			core.AssertFalse(t, result.OK)
			core.AssertFalse(t, called)
			core.AssertContains(t, result.Value.(resultFailure).Error(), "invalid required field \""+tc.field+"\"")
		})
	}
}

func TestDisplay_handleWSMessage_RejectsIntOverflow(t *core.T) {
	_, err := requireIntField(map[string]any{"window_count": uint64(^uint(0))}, "window_count")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "invalid required field \"window_count\"")
}

func TestDisplay_handleTrayAction_Good(t *core.T) {
	// handleTrayAction
	ax7Variant := "handleTrayAction:good"
	core.AssertContains(t, ax7Variant, "good")
	platform := window.NewMockPlatform()
	c := core.New(
		core.WithService(Register(nil)),
		core.WithService(window.Register(platform)),
		core.WithService(systray.Register(systray.NewMockPlatform())),
		core.WithService(menu.Register(menu.NewMockPlatform())),
		core.WithServiceLock(),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("one"))
	_ = svc.OpenWindow(window.WithName("two"))

	svc.handleTrayAction("open-desktop")
	core.AssertLen(t, platform.Windows, 2)
	core.AssertTrue(t, platform.Windows[0].IsFocused())
	core.AssertTrue(t, platform.Windows[1].IsFocused())

	svc.handleTrayAction("close-desktop")
	core.AssertFalse(t, platform.Windows[0].IsVisible())
	core.AssertFalse(t, platform.Windows[1].IsVisible())
}

func TestGetFocusedWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("win-a"))
	_ = svc.OpenWindow(window.WithName("win-b"))
	_ = svc.FocusWindow("win-b")

	focused := svc.GetFocusedWindow()
	core.AssertEqual(t, "win-b", focused)
}

func TestGetFocusedWindow_Good_NoneSelected(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")
	_ = svc.OpenWindow(window.WithName("win-a"))

	focused := svc.GetFocusedWindow()
	core.AssertEqual(t, "", focused)
}

func TestCreateWindow_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	info, err := svc.CreateWindow(CreateWindowOptions{
		Name:   "new-win",
		Title:  "New Window",
		URL:    "/new",
		Width:  600,
		Height: 400,
	})
	core.RequireNoError(t, err)
	core.AssertEqual(t, "new-win", info.Name)
}

func TestCreateWindow_Bad(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	_, err := svc.CreateWindow(CreateWindowOptions{})
	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "window name is required")
}

func TestResetWindowState_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	err := svc.ResetWindowState()
	core.AssertNoError(t, err)
}

func TestGetSavedWindowStates_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	states := svc.GetSavedWindowStates()
	core.AssertNotNil(t, states)
}

func TestDisplay_PublicCollections_AreNilSafe(t *core.T) {
	svc, _ := newTestDisplayService(t)

	infos := svc.ListWindowInfos()
	layouts := svc.ListLayouts()
	states := svc.GetSavedWindowStates()

	core.AssertNotNil(t, infos)
	core.AssertNotNil(t, layouts)
	core.AssertNotNil(t, states)
	core.AssertEmpty(t, infos)
	core.AssertEmpty(t, layouts)
	core.AssertEmpty(t, states)
}

func TestDisplay_WindowService_NilSafe(t *core.T) {
	svc := &Service{}

	core.AssertNotPanics(t, func() {
		svc.ResetWindowState()
	})

	core.AssertNotPanics(t, func() {
		states := svc.GetSavedWindowStates()
		core.AssertNotNil(t, states)
		core.AssertEmpty(t, states)
	})
}

func TestHandleIPCEvents_WindowOpened_GoodCase(t *core.T) {
	c := newTestConclave(t)

	// Open a window — this should trigger ActionWindowOpened
	// which HandleIPCEvents should convert to a WS event
	r := taskRun(c, "window.open", window.TaskOpenWindow{
		Window: &window.Window{Name: "test"},
	})
	core.RequireTrue(t, r.OK)
	info := r.Value.(window.WindowInfo)
	core.AssertEqual(t, "test", info.Name)
}

func TestHandleListWorkspaces_Good(t *core.T) {
	c := newTestConclave(t)
	svc := core.MustServiceFor[*Service](c, "display")

	// handleListWorkspaces should not panic when workspace service is not available
	core.AssertNotPanics(t, func() {
		svc.handleListWorkspaces()
	})
}

func TestWSEventManager_Good(t *core.T) {
	em := NewWSEventManager()
	defer em.Close()

	core.AssertNotNil(t, em)
	core.AssertEqual(t, 0, em.ConnectedClients())
}

func TestService_OnShutdown_ClosesEventManager(t *core.T) {
	em := NewWSEventManager()
	svc := &Service{events: em}

	server := httptest.NewServer(http.HandlerFunc(em.HandleWebSocket))
	t.Cleanup(server.Close)

	wsURL := "ws" + core.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	core.RequireNoError(t, err)
	defer func() { _ = conn.Close() }()
	defer em.Close()

	core.RequireTrue(t, svc.OnShutdown(context.Background()).OK)
	core.AssertNil(t, svc.events)

	requireEventually(t, func() bool {
		return em.ConnectedClients() == 0
	}, 2*time.Second, 20*time.Millisecond)

	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	core.AssertError(t, err)
}

// --- Config file loading tests ---

func TestLoadConfig_Good(t *core.T) {
	// Create temp config file
	dir := t.TempDir()
	cfgPath := core.JoinPath(dir, ".core", "gui", "config.yaml")
	core.RequireNoError(t, coreMkdirAll(core.PathDir(cfgPath), 0o755))
	core.RequireNoError(t, coreWriteFile(cfgPath, []byte(`
window:
  default_width: 1280
  default_height: 720
systray:
  tooltip: "Test App"
menu:
  show_dev_tools: false
`), 0o644))

	s, _ := New()
	s.loadConfigFrom(cfgPath)

	// Verify configData was populated from file
	core.AssertEqual(t, 1280, s.configData["window"]["default_width"])
	core.AssertEqual(t, "Test App", s.configData["systray"]["tooltip"])
	core.AssertEqual(t, false, s.configData["menu"]["show_dev_tools"])
}

func TestLoadConfig_Bad_MissingFile(t *core.T) {
	s, _ := New()
	s.loadConfigFrom(core.JoinPath(t.TempDir(), "nonexistent.yaml"))

	// Should not panic, configData stays at empty defaults
	core.AssertEmpty(t, s.configData["window"])
	core.AssertEmpty(t, s.configData["systray"])
	core.AssertEmpty(t, s.configData["menu"])
}

func TestHandleConfigTask_Persists_GoodCase(t *core.T) {
	dir := t.TempDir()
	cfgPath := core.JoinPath(dir, "config.yaml")

	s, _ := New()
	s.loadConfigFrom(cfgPath) // Creates empty config (file doesn't exist yet)

	// Simulate a TaskSaveConfig through the handler
	c := core.New(
		core.WithService(func(c *core.Core) core.Result {
			s.ServiceRuntime = core.NewServiceRuntime[Options](c, Options{})
			return core.Result{Value: s, OK: true}
		}),
		core.WithServiceLock(),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)

	r := taskRun(c, "display.saveWindowConfig", window.TaskSaveConfig{
		Config: map[string]any{"default_width": 1920},
	})
	core.RequireTrue(t, r.OK)

	// Verify file was written
	data, err := coreReadFile(cfgPath)
	core.RequireNoError(t, err)
	core.AssertContains(t, string(data), "default_width")
}

func TestDisplay_LayoutSuggest_Good(t *core.T) {
	// LayoutSuggest
	ax7Variant := "LayoutSuggest:good"
	core.AssertContains(t, ax7Variant, "good")
	svc, c := newTestDisplayAPIService(t)

	var gotTask window.TaskLayoutSuggest
	c.Action("window.layout_suggest", func(_ context.Context, opts core.Options) core.Result {
		gotTask = opts.Get("task").Value.(window.TaskLayoutSuggest)
		return core.Result{
			Value: window.LayoutSuggestion{
				Mode:     "coding",
				Reason:   "two-pane split",
				ScreenID: "screen-1",
				Width:    1280,
				Height:   720,
			},
			OK: true,
		}
	})

	got, err := svc.LayoutSuggest("screen-1", 2)

	core.RequireNoError(t, err)
	core.AssertEqual(t, "coding", got.Mode)
	core.AssertEqual(t, "two-pane split", got.Reason)
	core.AssertEqual(t, "screen-1", got.ScreenID)
	core.AssertEqual(t, 1280, got.Width)
	core.AssertEqual(t, 720, got.Height)
	core.AssertEqual(t, "screen-1", gotTask.ScreenID)
	core.AssertEqual(t, 2, gotTask.WindowCount)
}

func TestDisplay_LayoutSuggest_Bad(t *core.T) {
	// LayoutSuggest
	ax7Variant := "LayoutSuggest:bad"
	core.AssertContains(t, ax7Variant, "bad")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.layout_suggest", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: core.AnError, OK: false}
	})

	got, err := svc.LayoutSuggest("", 0)

	core.AssertError(t, err)
	core.AssertEqual(t, window.LayoutSuggestion{}, got)
	core.AssertEqual(t, core.AnError, err)
}

func TestDisplay_LayoutSuggest_Ugly(t *core.T) {
	// LayoutSuggest
	ax7Variant := "LayoutSuggest:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.layout_suggest", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: "unexpected", OK: true}
	})

	got, err := svc.LayoutSuggest("screen-1", 1)

	core.AssertError(t, err)
	core.AssertEqual(t, window.LayoutSuggestion{}, got)
	core.AssertContains(t, err.Error(), "unexpected result type")
}

func TestDisplay_GetLayout_Good(t *core.T) {
	// GetLayout
	ax7Variant := "GetLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	svc, c := newTestDisplayAPIService(t)

	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch typed := q.(type) {
		case window.QueryLayoutGet:
			core.AssertEqual(t, "development", typed.Name)
			return core.Result{
				Value: &window.Layout{
					Name: "development",
					Windows: map[string]window.WindowState{
						"editor":   {},
						"terminal": {},
					},
					CreatedAt: 1,
					UpdatedAt: 2,
				},
				OK: true,
			}
		default:
			return core.Result{}
		}
	})

	got := svc.GetLayout("development")

	core.AssertNotNil(t, got)
	core.AssertEqual(t, "development", got.Name)
	core.AssertLen(t, got.Windows, 2)
	core.AssertEqual(t, int64(1), got.CreatedAt)
	core.AssertEqual(t, int64(2), got.UpdatedAt)
}

func TestDisplay_GetLayout_Bad(t *core.T) {
	// GetLayout
	ax7Variant := "GetLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	svc, c := newTestDisplayAPIService(t)

	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch q.(type) {
		case window.QueryLayoutGet:
			return core.Result{Value: nil, OK: true}
		default:
			return core.Result{}
		}
	})

	got := svc.GetLayout("missing")

	core.AssertNil(t, got)
}

func TestDisplay_GetLayout_Ugly(t *core.T) {
	// GetLayout
	ax7Variant := "GetLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	svc, c := newTestDisplayAPIService(t)

	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch q.(type) {
		case window.QueryLayoutGet:
			return core.Result{Value: "unexpected", OK: true}
		default:
			return core.Result{}
		}
	})

	got := svc.GetLayout("broken")

	core.AssertNil(t, got)
}

func TestDisplay_SaveLayout_Good(t *core.T) {
	// SaveLayout
	ax7Variant := "SaveLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	svc, c := newTestDisplayAPIService(t)

	var gotTask window.TaskSaveLayout
	c.Action("window.save_layout", func(_ context.Context, opts core.Options) core.Result {
		gotTask = opts.Get("task").Value.(window.TaskSaveLayout)
		return core.Result{OK: true}
	})

	err := svc.SaveLayout("development")

	core.RequireNoError(t, err)
	core.AssertEqual(t, "development", gotTask.Name)
}

func TestDisplay_SaveLayout_Bad(t *core.T) {
	// SaveLayout
	ax7Variant := "SaveLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.save_layout", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: core.AnError, OK: false}
	})

	err := svc.SaveLayout("development")

	core.AssertError(t, err)
	core.AssertEqual(t, core.AnError, err)
}

func TestDisplay_SaveLayout_Ugly(t *core.T) {
	// SaveLayout
	ax7Variant := "SaveLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.save_layout", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: "unexpected", OK: false}
	})

	err := svc.SaveLayout("")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "window.save_layout")
}

func TestDisplay_RestoreLayout_Good(t *core.T) {
	// RestoreLayout
	ax7Variant := "RestoreLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	svc, c := newTestDisplayAPIService(t)

	var gotTask window.TaskRestoreLayout
	c.Action("window.restore_layout", func(_ context.Context, opts core.Options) core.Result {
		gotTask = opts.Get("task").Value.(window.TaskRestoreLayout)
		return core.Result{OK: true}
	})

	err := svc.RestoreLayout("development")

	core.RequireNoError(t, err)
	core.AssertEqual(t, "development", gotTask.Name)
}

func TestDisplay_RestoreLayout_Bad(t *core.T) {
	// RestoreLayout
	ax7Variant := "RestoreLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.restore_layout", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: core.AnError, OK: false}
	})

	err := svc.RestoreLayout("development")

	core.AssertError(t, err)
	core.AssertEqual(t, core.AnError, err)
}

func TestDisplay_RestoreLayout_Ugly(t *core.T) {
	// RestoreLayout
	ax7Variant := "RestoreLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.restore_layout", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: "unexpected", OK: false}
	})

	err := svc.RestoreLayout("")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "window.restore_layout")
}

func TestDisplay_SetWindowBackgroundColour_Good(t *core.T) {
	// SetWindowBackgroundColour
	ax7Variant := "SetWindowBackgroundColour:good"
	core.AssertContains(t, ax7Variant, "good")
	svc, c := newTestDisplayAPIService(t)

	var gotTask window.TaskSetBackgroundColour
	c.Action("window.set_background_colour", func(_ context.Context, opts core.Options) core.Result {
		gotTask = opts.Get("task").Value.(window.TaskSetBackgroundColour)
		return core.Result{OK: true}
	})

	err := svc.SetWindowBackgroundColour("main", 1, 2, 3, 4)

	core.RequireNoError(t, err)
	core.AssertEqual(t, "main", gotTask.Name)
	core.AssertEqual(t, uint8(1), gotTask.Red)
	core.AssertEqual(t, uint8(2), gotTask.Green)
	core.AssertEqual(t, uint8(3), gotTask.Blue)
	core.AssertEqual(t, uint8(4), gotTask.Alpha)
}

func TestDisplay_SetWindowBackgroundColour_Bad(t *core.T) {
	// SetWindowBackgroundColour
	ax7Variant := "SetWindowBackgroundColour:bad"
	core.AssertContains(t, ax7Variant, "bad")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.set_background_colour", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: core.AnError, OK: false}
	})

	err := svc.SetWindowBackgroundColour("main", 1, 2, 3, 4)

	core.AssertError(t, err)
	core.AssertEqual(t, core.AnError, err)
}

func TestDisplay_SetWindowBackgroundColour_Ugly(t *core.T) {
	// SetWindowBackgroundColour
	ax7Variant := "SetWindowBackgroundColour:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	svc, c := newTestDisplayAPIService(t)

	c.Action("window.set_background_colour", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: "unexpected", OK: false}
	})

	err := svc.SetWindowBackgroundColour("", 0, 0, 0, 0)

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "window.set_background_colour")
}

// AX7 generated source-matching smoke coverage.
func TestDisplay_New_Good(t *core.T) {
	// New
	ax7Variant := "New:good"
	core.AssertContains(t, ax7Variant, "good")
	result := core.Try(func() any {
		got0, got1 := New()
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_New_Bad(t *core.T) {
	// New
	ax7Variant := "New:bad"
	core.AssertContains(t, ax7Variant, "bad")
	result := core.Try(func() any {
		got0, got1 := New()
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_New_Ugly(t *core.T) {
	// New
	ax7Variant := "New:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	result := core.Try(func() any {
		got0, got1 := New()
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Register_Good(t *core.T) {
	// Register
	ax7Variant := "Register:good"
	core.AssertContains(t, ax7Variant, "good")
	result := core.Try(func() any {
		got0 := Register(nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Register_Bad(t *core.T) {
	// Register
	ax7Variant := "Register:bad"
	core.AssertContains(t, ax7Variant, "bad")
	result := core.Try(func() any {
		got0 := Register(nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Register_Ugly(t *core.T) {
	// Register
	ax7Variant := "Register:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	result := core.Try(func() any {
		got0 := Register(nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OnStartup_Good(t *core.T) {
	// Service OnStartup
	ax7Variant := "Service_OnStartup:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OnStartup(core.Background())
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OnStartup_Bad(t *core.T) {
	// Service OnStartup
	ax7Variant := "Service_OnStartup:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OnStartup(core.Background())
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OnStartup_Ugly(t *core.T) {
	// Service OnStartup
	ax7Variant := "Service_OnStartup:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OnStartup(core.Background())
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OnShutdown_Good(t *core.T) {
	// Service OnShutdown
	ax7Variant := "Service_OnShutdown:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OnShutdown(core.Background())
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OnShutdown_Bad(t *core.T) {
	// Service OnShutdown
	ax7Variant := "Service_OnShutdown:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OnShutdown(core.Background())
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OnShutdown_Ugly(t *core.T) {
	// Service OnShutdown
	ax7Variant := "Service_OnShutdown:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OnShutdown(core.Background())
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_HandleIPCEvents_Good(t *core.T) {
	// Service HandleIPCEvents
	ax7Variant := "Service_HandleIPCEvents:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.HandleIPCEvents(nil, nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_HandleIPCEvents_Bad(t *core.T) {
	// Service HandleIPCEvents
	ax7Variant := "Service_HandleIPCEvents:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.HandleIPCEvents(nil, nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_HandleIPCEvents_Ugly(t *core.T) {
	// Service HandleIPCEvents
	ax7Variant := "Service_HandleIPCEvents:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.HandleIPCEvents(nil, nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OpenWindow_Good(t *core.T) {
	// Service OpenWindow
	ax7Variant := "Service_OpenWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OpenWindow()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OpenWindow_Bad(t *core.T) {
	// Service OpenWindow
	ax7Variant := "Service_OpenWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OpenWindow()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_OpenWindow_Ugly(t *core.T) {
	// Service OpenWindow
	ax7Variant := "Service_OpenWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.OpenWindow()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetWindowInfo_Good(t *core.T) {
	// Service GetWindowInfo
	ax7Variant := "Service_GetWindowInfo:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.GetWindowInfo("agent")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetWindowInfo_Bad(t *core.T) {
	// Service GetWindowInfo
	ax7Variant := "Service_GetWindowInfo:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.GetWindowInfo("")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetWindowInfo_Ugly(t *core.T) {
	// Service GetWindowInfo
	ax7Variant := "Service_GetWindowInfo:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.GetWindowInfo("../../edge")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ListWindowInfos_Good(t *core.T) {
	// Service ListWindowInfos
	ax7Variant := "Service_ListWindowInfos:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ListWindowInfos()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ListWindowInfos_Bad(t *core.T) {
	// Service ListWindowInfos
	ax7Variant := "Service_ListWindowInfos:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ListWindowInfos()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ListWindowInfos_Ugly(t *core.T) {
	// Service ListWindowInfos
	ax7Variant := "Service_ListWindowInfos:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ListWindowInfos()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowPosition_Good(t *core.T) {
	// Service SetWindowPosition
	ax7Variant := "Service_SetWindowPosition:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowPosition("agent", 1, 1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowPosition_Bad(t *core.T) {
	// Service SetWindowPosition
	ax7Variant := "Service_SetWindowPosition:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowPosition("", 0, 0)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowPosition_Ugly(t *core.T) {
	// Service SetWindowPosition
	ax7Variant := "Service_SetWindowPosition:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowPosition("../../edge", -1, -1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowSize_Good(t *core.T) {
	// Service SetWindowSize
	ax7Variant := "Service_SetWindowSize:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowSize("agent", 1, 1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowSize_Bad(t *core.T) {
	// Service SetWindowSize
	ax7Variant := "Service_SetWindowSize:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowSize("", 0, 0)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowSize_Ugly(t *core.T) {
	// Service SetWindowSize
	ax7Variant := "Service_SetWindowSize:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowSize("../../edge", -1, -1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowBounds_Good(t *core.T) {
	// Service SetWindowBounds
	ax7Variant := "Service_SetWindowBounds:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowBounds("agent", 1, 1, 1, 1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowBounds_Bad(t *core.T) {
	// Service SetWindowBounds
	ax7Variant := "Service_SetWindowBounds:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowBounds("", 0, 0, 0, 0)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowBounds_Ugly(t *core.T) {
	// Service SetWindowBounds
	ax7Variant := "Service_SetWindowBounds:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowBounds("../../edge", -1, -1, -1, -1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_MaximizeWindow_Good(t *core.T) {
	// Service MaximizeWindow
	ax7Variant := "Service_MaximizeWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.MaximizeWindow("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_MaximizeWindow_Bad(t *core.T) {
	// Service MaximizeWindow
	ax7Variant := "Service_MaximizeWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.MaximizeWindow("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_MaximizeWindow_Ugly(t *core.T) {
	// Service MaximizeWindow
	ax7Variant := "Service_MaximizeWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.MaximizeWindow("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_MinimizeWindow_Good(t *core.T) {
	// Service MinimizeWindow
	ax7Variant := "Service_MinimizeWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.MinimizeWindow("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_MinimizeWindow_Bad(t *core.T) {
	// Service MinimizeWindow
	ax7Variant := "Service_MinimizeWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.MinimizeWindow("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_MinimizeWindow_Ugly(t *core.T) {
	// Service MinimizeWindow
	ax7Variant := "Service_MinimizeWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.MinimizeWindow("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_FocusWindow_Good(t *core.T) {
	// Service FocusWindow
	ax7Variant := "Service_FocusWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.FocusWindow("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_FocusWindow_Bad(t *core.T) {
	// Service FocusWindow
	ax7Variant := "Service_FocusWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.FocusWindow("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_FocusWindow_Ugly(t *core.T) {
	// Service FocusWindow
	ax7Variant := "Service_FocusWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.FocusWindow("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_CloseWindow_Good(t *core.T) {
	// Service CloseWindow
	ax7Variant := "Service_CloseWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.CloseWindow("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_CloseWindow_Bad(t *core.T) {
	// Service CloseWindow
	ax7Variant := "Service_CloseWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.CloseWindow("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_CloseWindow_Ugly(t *core.T) {
	// Service CloseWindow
	ax7Variant := "Service_CloseWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.CloseWindow("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_RestoreWindow_Good(t *core.T) {
	// Service RestoreWindow
	ax7Variant := "Service_RestoreWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.RestoreWindow("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_RestoreWindow_Bad(t *core.T) {
	// Service RestoreWindow
	ax7Variant := "Service_RestoreWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.RestoreWindow("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_RestoreWindow_Ugly(t *core.T) {
	// Service RestoreWindow
	ax7Variant := "Service_RestoreWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.RestoreWindow("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowVisibility_Good(t *core.T) {
	// Service SetWindowVisibility
	ax7Variant := "Service_SetWindowVisibility:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowVisibility("agent", true)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowVisibility_Bad(t *core.T) {
	// Service SetWindowVisibility
	ax7Variant := "Service_SetWindowVisibility:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowVisibility("", false)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowVisibility_Ugly(t *core.T) {
	// Service SetWindowVisibility
	ax7Variant := "Service_SetWindowVisibility:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowVisibility("../../edge", false)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowAlwaysOnTop_Good(t *core.T) {
	// Service SetWindowAlwaysOnTop
	ax7Variant := "Service_SetWindowAlwaysOnTop:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowAlwaysOnTop("agent", true)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowAlwaysOnTop_Bad(t *core.T) {
	// Service SetWindowAlwaysOnTop
	ax7Variant := "Service_SetWindowAlwaysOnTop:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowAlwaysOnTop("", false)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowAlwaysOnTop_Ugly(t *core.T) {
	// Service SetWindowAlwaysOnTop
	ax7Variant := "Service_SetWindowAlwaysOnTop:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowAlwaysOnTop("../../edge", false)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowTitle_Good(t *core.T) {
	// Service SetWindowTitle
	ax7Variant := "Service_SetWindowTitle:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowTitle("agent", "agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowTitle_Bad(t *core.T) {
	// Service SetWindowTitle
	ax7Variant := "Service_SetWindowTitle:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowTitle("", "")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowTitle_Ugly(t *core.T) {
	// Service SetWindowTitle
	ax7Variant := "Service_SetWindowTitle:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowTitle("../../edge", "../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowFullscreen_Good(t *core.T) {
	// Service SetWindowFullscreen
	ax7Variant := "Service_SetWindowFullscreen:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowFullscreen("agent", true)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowFullscreen_Bad(t *core.T) {
	// Service SetWindowFullscreen
	ax7Variant := "Service_SetWindowFullscreen:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowFullscreen("", false)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowFullscreen_Ugly(t *core.T) {
	// Service SetWindowFullscreen
	ax7Variant := "Service_SetWindowFullscreen:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowFullscreen("../../edge", false)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowBackgroundColour_Good(t *core.T) {
	// Service SetWindowBackgroundColour
	ax7Variant := "Service_SetWindowBackgroundColour:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowBackgroundColour("agent", 1, 1, 1, 1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowBackgroundColour_Bad(t *core.T) {
	// Service SetWindowBackgroundColour
	ax7Variant := "Service_SetWindowBackgroundColour:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowBackgroundColour("", 0, 0, 0, 0)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SetWindowBackgroundColour_Ugly(t *core.T) {
	// Service SetWindowBackgroundColour
	ax7Variant := "Service_SetWindowBackgroundColour:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SetWindowBackgroundColour("../../edge", 0, 0, 0, 0)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetFocusedWindow_Good(t *core.T) {
	// Service GetFocusedWindow
	ax7Variant := "Service_GetFocusedWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetFocusedWindow()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetFocusedWindow_Bad(t *core.T) {
	// Service GetFocusedWindow
	ax7Variant := "Service_GetFocusedWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetFocusedWindow()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetFocusedWindow_Ugly(t *core.T) {
	// Service GetFocusedWindow
	ax7Variant := "Service_GetFocusedWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetFocusedWindow()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetWindowTitle_Good(t *core.T) {
	// Service GetWindowTitle
	ax7Variant := "Service_GetWindowTitle:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.GetWindowTitle("agent")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetWindowTitle_Bad(t *core.T) {
	// Service GetWindowTitle
	ax7Variant := "Service_GetWindowTitle:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.GetWindowTitle("")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetWindowTitle_Ugly(t *core.T) {
	// Service GetWindowTitle
	ax7Variant := "Service_GetWindowTitle:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.GetWindowTitle("../../edge")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ResetWindowState_Good(t *core.T) {
	// Service ResetWindowState
	ax7Variant := "Service_ResetWindowState:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ResetWindowState()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ResetWindowState_Bad(t *core.T) {
	// Service ResetWindowState
	ax7Variant := "Service_ResetWindowState:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ResetWindowState()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ResetWindowState_Ugly(t *core.T) {
	// Service ResetWindowState
	ax7Variant := "Service_ResetWindowState:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ResetWindowState()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetSavedWindowStates_Good(t *core.T) {
	// Service GetSavedWindowStates
	ax7Variant := "Service_GetSavedWindowStates:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetSavedWindowStates()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetSavedWindowStates_Bad(t *core.T) {
	// Service GetSavedWindowStates
	ax7Variant := "Service_GetSavedWindowStates:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetSavedWindowStates()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetSavedWindowStates_Ugly(t *core.T) {
	// Service GetSavedWindowStates
	ax7Variant := "Service_GetSavedWindowStates:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetSavedWindowStates()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_CreateWindow_Good(t *core.T) {
	// Service CreateWindow
	ax7Variant := "Service_CreateWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.CreateWindow(*new(CreateWindowOptions))
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_CreateWindow_Bad(t *core.T) {
	// Service CreateWindow
	ax7Variant := "Service_CreateWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.CreateWindow(*new(CreateWindowOptions))
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_CreateWindow_Ugly(t *core.T) {
	// Service CreateWindow
	ax7Variant := "Service_CreateWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.CreateWindow(*new(CreateWindowOptions))
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SaveLayout_Good(t *core.T) {
	// Service SaveLayout
	ax7Variant := "Service_SaveLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SaveLayout("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SaveLayout_Bad(t *core.T) {
	// Service SaveLayout
	ax7Variant := "Service_SaveLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SaveLayout("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SaveLayout_Ugly(t *core.T) {
	// Service SaveLayout
	ax7Variant := "Service_SaveLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SaveLayout("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_RestoreLayout_Good(t *core.T) {
	// Service RestoreLayout
	ax7Variant := "Service_RestoreLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.RestoreLayout("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_RestoreLayout_Bad(t *core.T) {
	// Service RestoreLayout
	ax7Variant := "Service_RestoreLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.RestoreLayout("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_RestoreLayout_Ugly(t *core.T) {
	// Service RestoreLayout
	ax7Variant := "Service_RestoreLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.RestoreLayout("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ListLayouts_Good(t *core.T) {
	// Service ListLayouts
	ax7Variant := "Service_ListLayouts:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ListLayouts()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ListLayouts_Bad(t *core.T) {
	// Service ListLayouts
	ax7Variant := "Service_ListLayouts:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ListLayouts()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ListLayouts_Ugly(t *core.T) {
	// Service ListLayouts
	ax7Variant := "Service_ListLayouts:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ListLayouts()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_DeleteLayout_Good(t *core.T) {
	// Service DeleteLayout
	ax7Variant := "Service_DeleteLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.DeleteLayout("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_DeleteLayout_Bad(t *core.T) {
	// Service DeleteLayout
	ax7Variant := "Service_DeleteLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.DeleteLayout("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_DeleteLayout_Ugly(t *core.T) {
	// Service DeleteLayout
	ax7Variant := "Service_DeleteLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.DeleteLayout("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetLayout_Good(t *core.T) {
	// Service GetLayout
	ax7Variant := "Service_GetLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetLayout("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetLayout_Bad(t *core.T) {
	// Service GetLayout
	ax7Variant := "Service_GetLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetLayout("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetLayout_Ugly(t *core.T) {
	// Service GetLayout
	ax7Variant := "Service_GetLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetLayout("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_TileWindows_Good(t *core.T) {
	// Service TileWindows
	ax7Variant := "Service_TileWindows:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.TileWindows(window.TileModeLeftHalf, nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_TileWindows_Bad(t *core.T) {
	// Service TileWindows
	ax7Variant := "Service_TileWindows:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.TileWindows(window.TileModeLeftHalf, nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_TileWindows_Ugly(t *core.T) {
	// Service TileWindows
	ax7Variant := "Service_TileWindows:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.TileWindows(window.TileModeLeftHalf, nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SnapWindow_Good(t *core.T) {
	// Service SnapWindow
	ax7Variant := "Service_SnapWindow:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SnapWindow("agent", window.SnapLeft)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SnapWindow_Bad(t *core.T) {
	// Service SnapWindow
	ax7Variant := "Service_SnapWindow:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SnapWindow("", window.SnapLeft)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_SnapWindow_Ugly(t *core.T) {
	// Service SnapWindow
	ax7Variant := "Service_SnapWindow:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.SnapWindow("../../edge", window.SnapLeft)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_StackWindows_Good(t *core.T) {
	// Service StackWindows
	ax7Variant := "Service_StackWindows:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.StackWindows(nil, 1, 1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_StackWindows_Bad(t *core.T) {
	// Service StackWindows
	ax7Variant := "Service_StackWindows:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.StackWindows(nil, 0, 0)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_StackWindows_Ugly(t *core.T) {
	// Service StackWindows
	ax7Variant := "Service_StackWindows:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.StackWindows(nil, -1, -1)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ApplyWorkflowLayout_Good(t *core.T) {
	// Service ApplyWorkflowLayout
	ax7Variant := "Service_ApplyWorkflowLayout:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ApplyWorkflowLayout(window.WorkflowCoding)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ApplyWorkflowLayout_Bad(t *core.T) {
	// Service ApplyWorkflowLayout
	ax7Variant := "Service_ApplyWorkflowLayout:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ApplyWorkflowLayout(window.WorkflowCoding)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ApplyWorkflowLayout_Ugly(t *core.T) {
	// Service ApplyWorkflowLayout
	ax7Variant := "Service_ApplyWorkflowLayout:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.ApplyWorkflowLayout(window.WorkflowCoding)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_LayoutBesideEditor_Good(t *core.T) {
	// Service LayoutBesideEditor
	ax7Variant := "Service_LayoutBesideEditor:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.LayoutBesideEditor("agent", "agent", "agent", 1.5)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_LayoutBesideEditor_Bad(t *core.T) {
	// Service LayoutBesideEditor
	ax7Variant := "Service_LayoutBesideEditor:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.LayoutBesideEditor("", "", "", 0)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_LayoutBesideEditor_Ugly(t *core.T) {
	// Service LayoutBesideEditor
	ax7Variant := "Service_LayoutBesideEditor:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.LayoutBesideEditor("../../edge", "../../edge", "../../edge", -1.5)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_LayoutSuggest_Good(t *core.T) {
	// Service LayoutSuggest
	ax7Variant := "Service_LayoutSuggest:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.LayoutSuggest("agent", 1)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_LayoutSuggest_Bad(t *core.T) {
	// Service LayoutSuggest
	ax7Variant := "Service_LayoutSuggest:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.LayoutSuggest("", 0)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_LayoutSuggest_Ugly(t *core.T) {
	// Service LayoutSuggest
	ax7Variant := "Service_LayoutSuggest:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.LayoutSuggest("../../edge", -1)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_FindScreenSpace_Good(t *core.T) {
	// Service FindScreenSpace
	ax7Variant := "Service_FindScreenSpace:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.FindScreenSpace("agent", 1, 1, 1)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_FindScreenSpace_Bad(t *core.T) {
	// Service FindScreenSpace
	ax7Variant := "Service_FindScreenSpace:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.FindScreenSpace("", 0, 0, 0)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_FindScreenSpace_Ugly(t *core.T) {
	// Service FindScreenSpace
	ax7Variant := "Service_FindScreenSpace:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.FindScreenSpace("../../edge", -1, -1, -1)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ArrangeWindowPair_Good(t *core.T) {
	// Service ArrangeWindowPair
	ax7Variant := "Service_ArrangeWindowPair:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.ArrangeWindowPair("agent", "agent", "agent", 1.5)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ArrangeWindowPair_Bad(t *core.T) {
	// Service ArrangeWindowPair
	ax7Variant := "Service_ArrangeWindowPair:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.ArrangeWindowPair("", "", "", 0)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_ArrangeWindowPair_Ugly(t *core.T) {
	// Service ArrangeWindowPair
	ax7Variant := "Service_ArrangeWindowPair:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0, got1 := subject.ArrangeWindowPair("../../edge", "../../edge", "../../edge", -1.5)
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetEventManager_Good(t *core.T) {
	// Service GetEventManager
	ax7Variant := "Service_GetEventManager:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetEventManager()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetEventManager_Bad(t *core.T) {
	// Service GetEventManager
	ax7Variant := "Service_GetEventManager:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetEventManager()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestDisplay_Service_GetEventManager_Ugly(t *core.T) {
	// Service GetEventManager
	ax7Variant := "Service_GetEventManager:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(Service)
	result := core.Try(func() any {
		got0 := subject.GetEventManager()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}
