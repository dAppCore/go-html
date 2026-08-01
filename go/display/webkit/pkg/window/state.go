// pkg/window/state.go
package window

import (
	"maps"
	"sync"
	"time"

	core "dappco.re/go"
)

const windowStateFileEnv = "WINDOW_STATE_FILE"

// WindowState holds the persisted position/size of a window.
// JSON tags match existing window_state.json format for backward compat.
type WindowState struct {
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Maximized bool   `json:"maximized,omitempty"`
	Screen    string `json:"screen,omitempty"`
	URL       string `json:"url,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// StateManager persists window positions to the configured window state file.
type StateManager struct {
	configDir string
	statePath string
	states    map[string]WindowState
	mu        sync.RWMutex
	saveTimer *time.Timer
}

// NewStateManager creates a StateManager loading from the configured default path.
// WINDOW_STATE_FILE overrides the directory-based default when present.
func NewStateManager() *StateManager {
	if stateFile := core.Env(windowStateFileEnv); stateFile != "" {
		return NewStateManagerWithPath(stateFile)
	}
	sm := &StateManager{
		states: make(map[string]WindowState),
	}
	if configDir := core.Env("DIR_CONFIG"); configDir != "" {
		sm.configDir = core.JoinPath(configDir, "Core")
	}
	sm.load()
	return sm
}

// NewStateManagerWithDir creates a StateManager loading from a custom config directory.
// Useful for testing or when the default config directory is not appropriate.
func NewStateManagerWithDir(configDir string) *StateManager {
	sm := &StateManager{
		configDir: configDir,
		states:    make(map[string]WindowState),
	}
	sm.load()
	return sm
}

// NewStateManagerWithPath creates a StateManager loading from a custom state file.
// Useful for tests or restricted runtimes that need an explicit writable target.
func NewStateManagerWithPath(path string) *StateManager {
	sm := &StateManager{
		statePath: path,
		states:    make(map[string]WindowState),
	}
	sm.load()
	return sm
}

func (sm *StateManager) filePath() string {
	if sm.statePath != "" {
		return sm.statePath
	}
	return core.JoinPath(sm.configDir, "window_state.json")
}

func (sm *StateManager) dataDir() string {
	if sm.statePath != "" {
		return core.PathDir(sm.statePath)
	}
	return sm.configDir
}

func (sm *StateManager) SetPath(path string) {
	if path == "" {
		return
	}
	sm.mu.Lock()
	sm.stopSaveTimerLocked()
	sm.statePath = path
	sm.states = make(map[string]WindowState)
	sm.mu.Unlock()
	sm.load()
}

func (sm *StateManager) load() {
	if sm.configDir == "" && sm.statePath == "" {
		return
	}
	content, err := coreReadFile(sm.filePath())
	if err != nil {
		if core.IsNotExist(err) {
			return
		}
		core.Error(
			"window state load failed",
			"file_path", sm.filePath(),
			"err", core.E("window.StateManager.load", "failed to read window state", err),
		)
		return
	}
	loaded := make(map[string]WindowState)
	result := core.JSONUnmarshal(content, &loaded)
	if !result.OK {
		if decodeErr, ok := result.Value.(error); ok {
			core.Error(
				"window state load failed",
				"file_path", sm.filePath(),
				"err", core.E("window.StateManager.load", "failed to decode window state", decodeErr),
			)
		}
		return
	}
	sm.mu.Lock()
	sm.states = loaded
	sm.mu.Unlock()
}

func (sm *StateManager) save() resultFailure {
	if sm.configDir == "" && sm.statePath == "" {
		return nil
	}
	sm.mu.RLock()
	filePath := sm.filePath()
	states := make(map[string]WindowState, len(sm.states))
	maps.Copy(states, sm.states)
	sm.mu.RUnlock()
	result := core.JSONMarshal(states)
	if !result.OK {
		marshalErr, _ := result.Value.(error)
		core.Error(
			"window state save failed",
			"file_path", filePath,
			"err", core.E("window.StateManager.save", "failed to encode window state", marshalErr),
		)
		return core.E("window.StateManager.save", "failed to encode window state", marshalErr)
	}
	data := result.Value.([]byte)
	if dir := core.PathDir(filePath); dir != "" {
		if err := coreMkdirAll(dir, 0o755); err != nil {
			core.Error(
				"window state save failed",
				"file_path", filePath,
				"err", core.E("window.StateManager.save", "failed to create window state directory", err),
			)
			return core.E("window.StateManager.save", "failed to create window state directory", err)
		}
	}
	if err := coreWriteFile(filePath, data, 0o644); err != nil {
		core.Error(
			"window state save failed",
			"file_path", filePath,
			"err", core.E("window.StateManager.save", "failed to write window state", err),
		)
		return core.E("window.StateManager.save", "failed to write window state", err)
	}
	return nil
}

func (sm *StateManager) scheduleSave() {
	sm.mu.Lock()
	sm.stopSaveTimerLocked()
	sm.saveTimer = time.AfterFunc(500*time.Millisecond, func() {
		if err := sm.save(); err != nil {
			return
		}
	})
	sm.mu.Unlock()
}

// GetState returns the saved state for a window name.
func (sm *StateManager) GetState(name string) (WindowState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.states[name]
	return s, ok
}

// SetState saves state for a window name (debounced disk write).
func (sm *StateManager) SetState(name string, state WindowState) {
	state.UpdatedAt = time.Now().UnixMilli()
	sm.mu.Lock()
	sm.states[name] = state
	sm.mu.Unlock()
	sm.scheduleSave()
}

// UpdatePosition updates only the position fields.
func (sm *StateManager) UpdatePosition(name string, x, y int) {
	sm.mu.Lock()
	s := sm.states[name]
	s.X = x
	s.Y = y
	s.UpdatedAt = time.Now().UnixMilli()
	sm.states[name] = s
	sm.mu.Unlock()
	sm.scheduleSave()
}

// UpdateSize updates only the size fields.
func (sm *StateManager) UpdateSize(name string, width, height int) {
	sm.mu.Lock()
	s := sm.states[name]
	s.Width = width
	s.Height = height
	s.UpdatedAt = time.Now().UnixMilli()
	sm.states[name] = s
	sm.mu.Unlock()
	sm.scheduleSave()
}

// UpdateMaximized updates the maximized flag.
func (sm *StateManager) UpdateMaximized(name string, maximized bool) {
	sm.mu.Lock()
	s := sm.states[name]
	s.Maximized = maximized
	s.UpdatedAt = time.Now().UnixMilli()
	sm.states[name] = s
	sm.mu.Unlock()
	sm.scheduleSave()
}

// CaptureState snapshots the current state from a PlatformWindow.
func (sm *StateManager) CaptureState(pw PlatformWindow) {
	x, y := pw.Position()
	w, h := pw.Size()
	sm.SetState(pw.Name(), WindowState{
		X: x, Y: y, Width: w, Height: h,
		Maximized: pw.IsMaximised(),
	})
}

// ApplyState restores saved position/size to a Window descriptor.
func (sm *StateManager) ApplyState(w *Window) {
	s, ok := sm.GetState(w.Name)
	if !ok {
		return
	}
	if s.Width > 0 {
		w.Width = s.Width
	}
	if s.Height > 0 {
		w.Height = s.Height
	}
	w.X = s.X
	w.Y = s.Y
}

// ListStates returns all stored window names.
func (sm *StateManager) ListStates() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	names := make([]string, 0, len(sm.states))
	for name := range sm.states {
		names = append(names, name)
	}
	return names
}

// Clear removes all stored states.
func (sm *StateManager) Clear() {
	sm.mu.Lock()
	sm.states = make(map[string]WindowState)
	sm.mu.Unlock()
	sm.scheduleSave()
}

func (sm *StateManager) stopSaveTimerLocked() {
	if sm.saveTimer == nil {
		return
	}
	sm.saveTimer.Stop()
	sm.saveTimer = nil
}

// ForceSync writes state to disk immediately.
func (sm *StateManager) ForceSync() resultFailure {
	sm.mu.Lock()
	sm.stopSaveTimerLocked()
	sm.mu.Unlock()
	return sm.save()
}
