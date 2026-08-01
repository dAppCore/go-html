// pkg/contextmenu/service.go
package contextmenu

import (
	"context"
	"maps"
	"sync"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/internal/coreutil"
)

type Options struct{}

type Service struct {
	*core.ServiceRuntime[Options]
	platform        Platform
	mu              sync.RWMutex
	registeredMenus map[string]ContextMenuDef
}

func platformUnavailableError(op string) resultFailure {
	return core.E("contextmenu."+op, "platform backend unavailable", nil)
}

func (s *Service) OnStartup(_ context.Context) core.Result {
	s.Core().RegisterQuery(s.handleQuery)
	s.Core().Action("contextmenu.add", func(_ context.Context, opts core.Options) core.Result {
		t, ok := opts.Get("task").Value.(TaskAdd)
		if !ok {
			return invalidTaskResult("add")
		}
		return core.Result{Value: nil, OK: true}.New(s.taskAdd(t))
	})
	s.Core().Action("contextmenu.remove", func(_ context.Context, opts core.Options) core.Result {
		t, ok := opts.Get("task").Value.(TaskRemove)
		if !ok {
			return invalidTaskResult("remove")
		}
		return core.Result{Value: nil, OK: true}.New(s.taskRemove(t))
	})
	s.Core().Action("contextmenu.update", func(_ context.Context, opts core.Options) core.Result {
		t, ok := opts.Get("task").Value.(TaskUpdate)
		if !ok {
			return invalidTaskResult("update")
		}
		return core.Result{Value: nil, OK: true}.New(s.taskUpdate(t))
	})
	s.Core().Action("contextmenu.destroy", func(_ context.Context, opts core.Options) core.Result {
		t, ok := opts.Get("task").Value.(TaskDestroy)
		if !ok {
			return invalidTaskResult("destroy")
		}
		return core.Result{Value: nil, OK: true}.New(s.taskDestroy(t))
	})
	return core.Result{OK: true}
}

func invalidTaskResult(op string) core.Result {
	return core.Result{
		Value: core.E("contextmenu."+op, "invalid task payload", nil),
		OK:    false,
	}
}

func (s *Service) OnShutdown(_ context.Context) core.Result {
	// Destroy all registered menus on shutdown to release platform resources
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.platform == nil {
		s.registeredMenus = make(map[string]ContextMenuDef)
		return core.Result{OK: true}
	}
	for name := range s.registeredMenus {
		if err := s.platform.Remove(name); err != nil {
			continue
		}
	}
	s.registeredMenus = make(map[string]ContextMenuDef)
	return core.Result{OK: true}
}

func (s *Service) HandleIPCEvents(_ *core.Core, _ core.Message) core.Result {
	return core.Result{OK: true}
}

// --- Query Handlers ---

func (s *Service) handleQuery(_ *core.Core, q core.Query) core.Result {
	switch q := q.(type) {
	case QueryGet:
		return core.Result{Value: s.queryGet(q), OK: true}
	case QueryList:
		return core.Result{Value: s.queryList(), OK: true}
	case QueryGetAll:
		return core.Result{Value: s.queryList(), OK: true}
	default:
		return core.Result{}
	}
}

func (s *Service) queryGet(q QueryGet) *ContextMenuDef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	menu, ok := s.registeredMenus[q.Name]
	if !ok {
		return nil
	}
	return &menu
}

func (s *Service) queryList() map[string]ContextMenuDef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]ContextMenuDef, len(s.registeredMenus))
	maps.Copy(result, s.registeredMenus)
	return result
}

func (s *Service) taskAdd(t TaskAdd) resultFailure {
	if s.platform == nil {
		return platformUnavailableError("taskAdd")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// If menu already exists, remove it first (replace semantics).
	oldMenu, existed := s.registeredMenus[t.Name]
	if existed {
		if err := s.platform.Remove(t.Name); err != nil {
			return core.E("contextmenu.taskAdd", "platform remove failed", err)
		}
		delete(s.registeredMenus, t.Name)
	}

	// Register on platform with a callback that broadcasts ActionItemClicked
	err := s.platform.Add(t.Name, t.Menu, s.menuCallback())
	if err != nil {
		if existed {
			s.tryRestoreMenu(t.Name, oldMenu)
		}
		return core.E("contextmenu.taskAdd", "platform add failed", err)
	}

	s.registeredMenus[t.Name] = t.Menu
	return nil
}

func (s *Service) taskRemove(t TaskRemove) resultFailure {
	if s.platform == nil {
		return platformUnavailableError("taskRemove")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.registeredMenus[t.Name]; !exists {
		return ErrorMenuNotFound
	}

	err := s.platform.Remove(t.Name)
	if err != nil {
		return core.E("contextmenu.taskRemove", "platform remove failed", err)
	}

	delete(s.registeredMenus, t.Name)
	return nil
}

func (s *Service) taskUpdate(t TaskUpdate) resultFailure {
	if s.platform == nil {
		return platformUnavailableError("taskUpdate")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldMenu, exists := s.registeredMenus[t.Name]
	if !exists {
		return ErrorMenuNotFound
	}

	// Re-register with updated definition — remove then add
	if err := s.platform.Remove(t.Name); err != nil {
		return core.E("contextmenu.taskUpdate", "platform remove failed", err)
	}

	err := s.platform.Add(t.Name, t.Menu, s.menuCallback())
	if err != nil {
		s.tryRestoreMenu(t.Name, oldMenu)
		return core.E("contextmenu.taskUpdate", "platform add failed", err)
	}

	s.registeredMenus[t.Name] = t.Menu
	return nil
}

func (s *Service) taskDestroy(t TaskDestroy) resultFailure {
	if s.platform == nil {
		return platformUnavailableError("taskDestroy")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.registeredMenus[t.Name]; !exists {
		return ErrorMenuNotFound
	}

	if err := s.platform.Remove(t.Name); err != nil {
		return core.E("contextmenu.taskDestroy", "platform remove failed", err)
	}

	delete(s.registeredMenus, t.Name)
	return nil
}

func (s *Service) tryRestoreMenu(name string, menu ContextMenuDef) {
	if restoreErr := s.platform.Add(name, menu, s.menuCallback()); restoreErr == nil {
		s.registeredMenus[name] = menu
	}
}

func (s *Service) menuCallback() func(string, string, string) {
	return func(menuName, actionID, data string) {
		coreutil.DispatchAction(s.Core(), "contextmenu.itemClicked", ActionItemClicked{
			MenuName: menuName,
			ActionID: actionID,
			Data:     data,
		})
	}
}
