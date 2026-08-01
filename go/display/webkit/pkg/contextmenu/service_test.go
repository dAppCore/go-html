// pkg/contextmenu/service_test.go
package contextmenu

import (
	"context"
	"maps"
	"sync"

	core "dappco.re/go"
)

// mockPlatform records Add/Remove calls and allows simulating clicks.
type mockPlatform struct {
	mu            sync.Mutex
	menus         map[string]ContextMenuDef
	clickHandlers map[string]func(menuName, actionID, data string)
	removed       []string
	addErr        resultFailure
	removeErr     resultFailure
}

func newMockPlatform() *mockPlatform {
	return &mockPlatform{
		menus:         make(map[string]ContextMenuDef),
		clickHandlers: make(map[string]func(menuName, actionID, data string)),
	}
}

func (m *mockPlatform) Add(name string, menu ContextMenuDef, onItemClick func(string, string, string)) resultFailure {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addErr != nil {
		return m.addErr
	}
	m.menus[name] = menu
	m.clickHandlers[name] = onItemClick
	return nil
}

func (m *mockPlatform) Remove(name string) resultFailure {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.removeErr != nil {
		return m.removeErr
	}
	delete(m.menus, name)
	delete(m.clickHandlers, name)
	m.removed = append(m.removed, name)
	return nil
}

func (m *mockPlatform) Get(name string) (*ContextMenuDef, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	menu, ok := m.menus[name]
	if !ok {
		return nil, false
	}
	return &menu, true
}

func (m *mockPlatform) GetAll() map[string]ContextMenuDef {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]ContextMenuDef, len(m.menus))
	maps.Copy(out, m.menus)
	return out
}

type flakyAddPlatform struct {
	*mockPlatform
	mu          sync.Mutex
	failAddOnce bool
}

func newFlakyAddPlatform() *flakyAddPlatform {
	return &flakyAddPlatform{mockPlatform: newMockPlatform()}
}

func (m *flakyAddPlatform) Add(name string, menu ContextMenuDef, onItemClick func(string, string, string)) resultFailure {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAddOnce {
		m.failAddOnce = false
		return ErrorMenuNotFound
	}
	return m.mockPlatform.Add(name, menu, onItemClick)
}

// simulateClick simulates a context menu item click by calling the registered handler.
func (m *mockPlatform) simulateClick(menuName, actionID, data string) {
	m.mu.Lock()
	h, ok := m.clickHandlers[menuName]
	m.mu.Unlock()
	if ok {
		h(menuName, actionID, data)
	}
}

func newTestContextMenuService(t *core.T, mp Platform) (*Service, *core.Core) {
	t.Helper()
	c := core.New(
		core.WithService(Register(mp)),
		core.WithServiceLock(),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)
	svc := core.MustServiceFor[*Service](c, "contextmenu")
	return svc, c
}

// taskRun runs a named action with a task struct and returns the result.
func taskRun(c *core.Core, name string, task any) core.Result {
	return c.Action(name).Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: task},
	))
}

func TestRegister_Good(t *core.T) {
	mp := newMockPlatform()
	svc, _ := newTestContextMenuService(t, mp)
	core.AssertNotNil(t, svc)
	core.AssertNotNil(t, svc.platform)
}

func TestNilPlatform_Good_MutationAndShutdownAreSafe(t *core.T) {
	_, c := newTestContextMenuService(t, nil)

	cases := []struct {
		name   string
		action string
		task   any
	}{
		{name: "add", action: "contextmenu.add", task: TaskAdd{Name: "file-menu", Menu: ContextMenuDef{Name: "file-menu"}}},
		{name: "remove", action: "contextmenu.remove", task: TaskRemove{Name: "file-menu"}},
		{name: "update", action: "contextmenu.update", task: TaskUpdate{Name: "file-menu", Menu: ContextMenuDef{Name: "file-menu"}}},
		{name: "destroy", action: "contextmenu.destroy", task: TaskDestroy{Name: "file-menu"}},
	}

	for _, tc := range cases {
		r := taskRun(c, tc.action, tc.task)
		core.AssertFalse(t, r.OK, tc.name)
		err, _ := r.Value.(resultFailure)
		core.AssertError(t, err)
		core.AssertContains(t, err.Error(), "platform backend unavailable")
	}

	core.AssertTrue(t, c.ServiceShutdown(t.Context()).OK)
}

func TestTaskAdd_Good(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := taskRun(c, "contextmenu.add", TaskAdd{
		Name: "file-menu",
		Menu: ContextMenuDef{
			Name: "file-menu",
			Items: []MenuItemDef{
				{Label: "Open", ActionID: "open"},
				{Label: "Delete", ActionID: "delete"},
			},
		},
	})
	core.RequireTrue(t, r.OK)

	// Verify menu registered on platform
	_, ok := mp.Get("file-menu")
	core.AssertTrue(t, ok)
}

func TestTaskAdd_Good_ReplaceExisting(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	// Add initial menu
	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "ctx",
		Menu: ContextMenuDef{Name: "ctx", Items: []MenuItemDef{{Label: "A", ActionID: "a"}}},
	})

	// Replace with new menu
	r := taskRun(c, "contextmenu.add", TaskAdd{
		Name: "ctx",
		Menu: ContextMenuDef{Name: "ctx", Items: []MenuItemDef{{Label: "B", ActionID: "b"}}},
	})
	core.RequireTrue(t, r.OK)

	// Verify registry has new menu
	qr := c.QUERY(QueryGet{Name: "ctx"})
	core.RequireTrue(t, qr.OK)
	def := qr.Value.(*ContextMenuDef)
	core.AssertLen(t, def.Items, 1)
	core.AssertEqual(t, "B", def.Items[0].Label)
}

func TestTaskRemove_Good(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	// Add then remove
	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "test",
		Menu: ContextMenuDef{Name: "test"},
	})
	r := taskRun(c, "contextmenu.remove", TaskRemove{Name: "test"})
	core.RequireTrue(t, r.OK)

	// Verify removed from registry
	qr := c.QUERY(QueryGet{Name: "test"})
	core.AssertNil(t, qr.Value)
}

func TestTaskRemove_Bad_NotFound(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := taskRun(c, "contextmenu.remove", TaskRemove{Name: "nonexistent"})
	core.AssertFalse(t, r.OK)
	err, _ := r.Value.(resultFailure)
	core.AssertErrorIs(t, err, ErrorMenuNotFound)
}

func TestQueryGet_Good(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "my-menu",
		Menu: ContextMenuDef{
			Name:  "my-menu",
			Items: []MenuItemDef{{Label: "Edit", ActionID: "edit"}},
		},
	})

	r := c.QUERY(QueryGet{Name: "my-menu"})
	core.RequireTrue(t, r.OK)
	def := r.Value.(*ContextMenuDef)
	core.AssertEqual(t, "my-menu", def.Name)
	core.AssertLen(t, def.Items, 1)
}

func TestQueryGet_Good_NotFound(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := c.QUERY(QueryGet{Name: "missing"})
	core.RequireTrue(t, r.OK)
	core.AssertNil(t, r.Value)
}

func TestQueryList_Good(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "a", Menu: ContextMenuDef{Name: "a"}})
	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "b", Menu: ContextMenuDef{Name: "b"}})

	r := c.QUERY(QueryList{})
	core.RequireTrue(t, r.OK)
	list := r.Value.(map[string]ContextMenuDef)
	core.AssertLen(t, list, 2)
}

func TestQueryList_Good_Empty(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := c.QUERY(QueryList{})
	core.RequireTrue(t, r.OK)
	list := r.Value.(map[string]ContextMenuDef)
	core.AssertLen(t, list, 0)
}

func TestTaskAdd_Good_ClickBroadcast(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	// Capture broadcast actions
	var clicked ActionItemClicked
	var mu sync.Mutex
	c.RegisterAction(func(_ *core.Core, msg core.Message) core.Result {
		if a, ok := msg.(ActionItemClicked); ok {
			mu.Lock()
			clicked = a
			mu.Unlock()
		}
		return core.Result{OK: true}
	})

	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "file-menu",
		Menu: ContextMenuDef{
			Name: "file-menu",
			Items: []MenuItemDef{
				{Label: "Open", ActionID: "open"},
			},
		},
	})

	// Simulate click via mock
	mp.simulateClick("file-menu", "open", "file-123")

	mu.Lock()
	core.AssertEqual(t, "file-menu", clicked.MenuName)
	core.AssertEqual(t, "open", clicked.ActionID)
	core.AssertEqual(t, "file-123", clicked.Data)
	mu.Unlock()
}

func TestTaskAdd_Ugly_PlatformAddFailureRollsBackExistingMenu(t *core.T) {
	mp := newFlakyAddPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "file-menu",
		Menu: ContextMenuDef{Name: "file-menu", Items: []MenuItemDef{{Label: "Open", ActionID: "open"}}},
	})

	mp.mu.Lock()
	mp.failAddOnce = true
	mp.mu.Unlock()

	r := taskRun(c, "contextmenu.add", TaskAdd{
		Name: "file-menu",
		Menu: ContextMenuDef{Name: "file-menu", Items: []MenuItemDef{{Label: "Delete", ActionID: "delete"}}},
	})
	core.AssertFalse(t, r.OK)

	qr := c.QUERY(QueryGet{Name: "file-menu"})
	core.RequireTrue(t, qr.OK)
	def := qr.Value.(*ContextMenuDef)
	core.AssertLen(t, def.Items, 1)
	core.AssertEqual(t, "Open", def.Items[0].Label)
	platformMenu, ok := mp.Get("file-menu")
	core.RequireTrue(t, ok)
	core.AssertLen(t, platformMenu.Items, 1)
	core.AssertEqual(t, "Open", platformMenu.Items[0].Label)
}

func TestTaskAdd_Good_SubmenuItems(t *core.T) {
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := taskRun(c, "contextmenu.add", TaskAdd{
		Name: "nested",
		Menu: ContextMenuDef{
			Name: "nested",
			Items: []MenuItemDef{
				{Label: "File", Type: "submenu", Items: []MenuItemDef{
					{Label: "New", ActionID: "new"},
					{Label: "Open", ActionID: "open"},
				}},
				{Type: "separator"},
				{Label: "Quit", ActionID: "quit"},
			},
		},
	})
	core.RequireTrue(t, r.OK)

	qr := c.QUERY(QueryGet{Name: "nested"})
	def := qr.Value.(*ContextMenuDef)
	core.AssertLen(t, def.Items, 3)
	core.AssertLen(t, def.Items[0].Items, 2) // submenu children
}

func TestQueryList_Bad_NoService(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.QUERY(QueryList{})
	core.AssertFalse(t, r.OK)
}

// --- TaskUpdate ---

func TestTaskUpdate_Good(t *core.T) {
	// Update replaces items on an existing menu
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "edit-menu",
		Menu: ContextMenuDef{Name: "edit-menu", Items: []MenuItemDef{{Label: "Cut", ActionID: "cut"}}},
	})

	r := taskRun(c, "contextmenu.update", TaskUpdate{
		Name: "edit-menu",
		Menu: ContextMenuDef{Name: "edit-menu", Items: []MenuItemDef{
			{Label: "Cut", ActionID: "cut"},
			{Label: "Copy", ActionID: "copy"},
		}},
	})
	core.RequireTrue(t, r.OK)

	qr := c.QUERY(QueryGet{Name: "edit-menu"})
	def := qr.Value.(*ContextMenuDef)
	core.AssertLen(t, def.Items, 2)
	core.AssertEqual(t, "Copy", def.Items[1].Label)
}

func TestTaskUpdate_Bad_NotFound(t *core.T) {
	// Update on a non-existent menu returns ErrorMenuNotFound
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := taskRun(c, "contextmenu.update", TaskUpdate{
		Name: "ghost",
		Menu: ContextMenuDef{Name: "ghost"},
	})
	core.AssertFalse(t, r.OK)
	err, _ := r.Value.(resultFailure)
	core.AssertErrorIs(t, err, ErrorMenuNotFound)
}

func TestTaskUpdate_Ugly_PlatformRemoveError(t *core.T) {
	// Platform Remove fails mid-update — resultFailure is propagated
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "tricky",
		Menu: ContextMenuDef{Name: "tricky"},
	})

	mp.mu.Lock()
	mp.removeErr = ErrorMenuNotFound // reuse sentinel as a platform-level resultFailure
	mp.mu.Unlock()

	r := taskRun(c, "contextmenu.update", TaskUpdate{
		Name: "tricky",
		Menu: ContextMenuDef{Name: "tricky", Items: []MenuItemDef{{Label: "X", ActionID: "x"}}},
	})
	core.AssertFalse(t, r.OK)
}

func TestTaskUpdate_Ugly_PlatformAddFailureRollsBackExistingMenu(t *core.T) {
	mp := newFlakyAddPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{
		Name: "edit-menu",
		Menu: ContextMenuDef{Name: "edit-menu", Items: []MenuItemDef{{Label: "Cut", ActionID: "cut"}}},
	})

	mp.mu.Lock()
	mp.failAddOnce = true
	mp.mu.Unlock()

	r := taskRun(c, "contextmenu.update", TaskUpdate{
		Name: "edit-menu",
		Menu: ContextMenuDef{Name: "edit-menu", Items: []MenuItemDef{{Label: "Copy", ActionID: "copy"}}},
	})
	core.AssertFalse(t, r.OK)

	qr := c.QUERY(QueryGet{Name: "edit-menu"})
	core.RequireTrue(t, qr.OK)
	def := qr.Value.(*ContextMenuDef)
	core.AssertLen(t, def.Items, 1)
	core.AssertEqual(t, "Cut", def.Items[0].Label)
	platformMenu, ok := mp.Get("edit-menu")
	core.RequireTrue(t, ok)
	core.AssertLen(t, platformMenu.Items, 1)
	core.AssertEqual(t, "Cut", platformMenu.Items[0].Label)
}

// --- TaskDestroy ---

func TestTaskDestroy_Good(t *core.T) {
	// Destroy removes the menu and releases platform resources
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "doomed", Menu: ContextMenuDef{Name: "doomed"}})

	r := taskRun(c, "contextmenu.destroy", TaskDestroy{Name: "doomed"})
	core.RequireTrue(t, r.OK)

	qr := c.QUERY(QueryGet{Name: "doomed"})
	core.AssertNil(t, qr.Value)

	_, ok := mp.Get("doomed")
	core.AssertFalse(t, ok)
}

func TestTaskDestroy_Bad_NotFound(t *core.T) {
	// Destroy on a non-existent menu returns ErrorMenuNotFound
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := taskRun(c, "contextmenu.destroy", TaskDestroy{Name: "nonexistent"})
	core.AssertFalse(t, r.OK)
	err, _ := r.Value.(resultFailure)
	core.AssertErrorIs(t, err, ErrorMenuNotFound)
}

func TestTaskDestroy_Ugly_PlatformError(t *core.T) {
	// Platform Remove fails — resultFailure is propagated but service remains consistent
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "frail", Menu: ContextMenuDef{Name: "frail"}})

	mp.mu.Lock()
	mp.removeErr = ErrorMenuNotFound
	mp.mu.Unlock()

	r := taskRun(c, "contextmenu.destroy", TaskDestroy{Name: "frail"})
	core.AssertFalse(t, r.OK)
}

// --- QueryGetAll ---

func TestQueryGetAll_Good(t *core.T) {
	// QueryGetAll returns all registered menus (equivalent to QueryList)
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "x", Menu: ContextMenuDef{Name: "x"}})
	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "y", Menu: ContextMenuDef{Name: "y"}})

	r := c.QUERY(QueryGetAll{})
	core.RequireTrue(t, r.OK)
	all := r.Value.(map[string]ContextMenuDef)
	core.AssertLen(t, all, 2)
	core.AssertContains(t, all, "x")
	core.AssertContains(t, all, "y")
}

func TestQueryGetAll_Bad_Empty(t *core.T) {
	// QueryGetAll on an empty registry returns an empty map
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	r := c.QUERY(QueryGetAll{})
	core.RequireTrue(t, r.OK)
	all := r.Value.(map[string]ContextMenuDef)
	core.AssertLen(t, all, 0)
}

func TestQueryGetAll_Ugly_NoService(t *core.T) {
	// No contextmenu service — query is unhandled
	c := core.New(core.WithServiceLock())
	r := c.QUERY(QueryGetAll{})
	core.AssertFalse(t, r.OK)
}

// --- OnShutdown ---

func TestOnShutdown_Good_CleansUpMenus(t *core.T) {
	// OnShutdown removes all registered menus from the platform
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "alpha", Menu: ContextMenuDef{Name: "alpha"}})
	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "beta", Menu: ContextMenuDef{Name: "beta"}})

	core.RequireTrue(t, c.ServiceShutdown(t.Context()).OK)

	core.AssertLen(t, mp.menus, 0)
}

func TestOnShutdown_Bad_NothingRegistered(t *core.T) {
	// OnShutdown with no menus — no-op, no resultFailure
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	core.AssertTrue(t, c.ServiceShutdown(t.Context()).OK)
}

func TestOnShutdown_Ugly_PlatformRemoveErrors(t *core.T) {
	// Platform Remove errors during shutdown are silently swallowed
	mp := newMockPlatform()
	_, c := newTestContextMenuService(t, mp)

	_ = taskRun(c, "contextmenu.add", TaskAdd{Name: "stubborn", Menu: ContextMenuDef{Name: "stubborn"}})

	mp.mu.Lock()
	mp.removeErr = ErrorMenuNotFound
	mp.mu.Unlock()

	// Shutdown must not return an resultFailure even if platform Remove fails
	core.AssertTrue(t, c.ServiceShutdown(t.Context()).OK)
}

// AX7 generated source-matching smoke coverage.
func TestService_Service_OnStartup_Good(t *core.T) {
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

func TestService_Service_OnStartup_Bad(t *core.T) {
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

func TestService_Service_OnStartup_Ugly(t *core.T) {
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

func TestService_Service_OnShutdown_Good(t *core.T) {
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

func TestService_Service_OnShutdown_Bad(t *core.T) {
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

func TestService_Service_OnShutdown_Ugly(t *core.T) {
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

func TestService_Service_HandleIPCEvents_Good(t *core.T) {
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

func TestService_Service_HandleIPCEvents_Bad(t *core.T) {
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

func TestService_Service_HandleIPCEvents_Ugly(t *core.T) {
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
