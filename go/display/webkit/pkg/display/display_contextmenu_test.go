package display

import (
	"maps"
	"sync"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/contextmenu"
)

type wsContextMenuPlatform struct {
	mu    sync.Mutex
	menus map[string]contextmenu.ContextMenuDef
}

func newWSContextMenuPlatform() *wsContextMenuPlatform {
	return &wsContextMenuPlatform{
		menus: make(map[string]contextmenu.ContextMenuDef),
	}
}

func (m *wsContextMenuPlatform) Add(name string, menu contextmenu.ContextMenuDef, _ func(string, string, string)) resultFailure {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.menus[name] = menu
	return nil
}

func (m *wsContextMenuPlatform) Remove(name string) resultFailure {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.menus, name)
	return nil
}

func (m *wsContextMenuPlatform) Get(name string) (*contextmenu.ContextMenuDef, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	menu, ok := m.menus[name]
	if !ok {
		return nil, false
	}
	return &menu, true
}

func (m *wsContextMenuPlatform) GetAll() map[string]contextmenu.ContextMenuDef {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]contextmenu.ContextMenuDef, len(m.menus))
	maps.Copy(out, m.menus)
	return out
}

func newDisplayWithContextMenu(t *core.T, platform *wsContextMenuPlatform) (*Service, *core.Core) {
	t.Helper()
	c := newTestCore(t, contextmenu.Register(platform))
	return core.MustServiceFor[*Service](c, "display"), c
}

func TestDisplay_handleWSMessage_ContextMenuAdd_MissingMenu(t *core.T) {
	platform := newWSContextMenuPlatform()
	svc, _ := newDisplayWithContextMenu(t, platform)

	result := svc.handleWSMessage(WSMessage{
		Action: "contextmenu:add",
		Data: map[string]any{
			"name": "menu",
		},
	})

	core.AssertFalse(t, result.OK)
	err, ok := result.Value.(resultFailure)
	core.RequireTrue(t, ok)
	core.AssertContains(t, err.Error(), `missing required field "menu"`)
	_, ok = platform.Get("menu")
	core.AssertFalse(t, ok)
}
