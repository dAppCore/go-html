// pkg/window/layout.go
package window

import (
	"maps"
	"sync"
	"time"

	core "dappco.re/go"
)

const layoutFileEnv = "WINDOW_LAYOUT_FILE"

// Layout is a named window arrangement.
type Layout struct {
	Name      string                 `json:"name"`
	Windows   map[string]WindowState `json:"windows"`
	CreatedAt int64                  `json:"createdAt"`
	UpdatedAt int64                  `json:"updatedAt"`
}

// LayoutInfo is a summary of a layout.
type LayoutInfo struct {
	Name        string `json:"name"`
	WindowCount int    `json:"windowCount"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// LayoutManager persists named window arrangements to ~/.config/Core/layouts.json.
type LayoutManager struct {
	configDir  string
	layoutPath string
	layouts    map[string]Layout
	mu         sync.RWMutex
}

// NewLayoutManager creates a LayoutManager loading from the default config directory.
// WINDOW_LAYOUT_FILE overrides the directory-based default when present.
func NewLayoutManager() *LayoutManager {
	if layoutFile := core.Env(layoutFileEnv); layoutFile != "" {
		return NewLayoutManagerWithPath(layoutFile)
	}
	lm := &LayoutManager{
		layouts: make(map[string]Layout),
	}
	if configDir := core.Env("DIR_CONFIG"); configDir != "" {
		lm.configDir = core.JoinPath(configDir, "Core")
	}
	lm.load()
	return lm
}

// NewLayoutManagerWithDir creates a LayoutManager loading from a custom config directory.
// Useful for testing or when the default config directory is not appropriate.
func NewLayoutManagerWithDir(configDir string) *LayoutManager {
	lm := &LayoutManager{
		configDir: configDir,
		layouts:   make(map[string]Layout),
	}
	lm.load()
	return lm
}

// NewLayoutManagerWithPath creates a LayoutManager loading from a custom layout file.
// Useful for tests and restricted runtimes that need an explicit writable target.
func NewLayoutManagerWithPath(path string) *LayoutManager {
	lm := &LayoutManager{
		layoutPath: path,
		layouts:    make(map[string]Layout),
	}
	lm.load()
	return lm
}

func (lm *LayoutManager) filePath() string {
	if lm.layoutPath != "" {
		return lm.layoutPath
	}
	return core.JoinPath(lm.configDir, "layouts.json")
}

func (lm *LayoutManager) dataDir() string {
	if lm.layoutPath != "" {
		return core.PathDir(lm.layoutPath)
	}
	return lm.configDir
}

// SetPath switches the manager to a custom layout file path.
func (lm *LayoutManager) SetPath(path string) {
	if path == "" {
		return
	}
	lm.mu.Lock()
	lm.layoutPath = path
	lm.layouts = make(map[string]Layout)
	lm.mu.Unlock()
	lm.load()
}

func (lm *LayoutManager) load() {
	if lm.configDir == "" && lm.layoutPath == "" {
		return
	}
	content, err := coreReadFile(lm.filePath())
	if err != nil {
		if core.IsNotExist(err) {
			return
		}
		core.Error(
			"window layout load failed",
			"file_path", lm.filePath(),
			"err", core.E("window.LayoutManager.load", "failed to read window layouts", err),
		)
		return
	}
	loaded := make(map[string]Layout)
	if result := core.JSONUnmarshal(content, &loaded); !result.OK {
		if decodeErr, ok := result.Value.(error); ok {
			core.Error(
				"window layout load failed",
				"file_path", lm.filePath(),
				"err", core.E("window.LayoutManager.load", "failed to decode window layouts", decodeErr),
			)
		}
		return
	}
	lm.mu.Lock()
	lm.layouts = loaded
	lm.mu.Unlock()
}

func (lm *LayoutManager) save() resultFailure {
	if lm.configDir == "" && lm.layoutPath == "" {
		return nil
	}
	lm.mu.RLock()
	filePath := lm.filePath()
	layouts := make(map[string]Layout, len(lm.layouts))
	maps.Copy(layouts, lm.layouts)
	result := core.JSONMarshal(layouts)
	lm.mu.RUnlock()
	if !result.OK {
		marshalErr, _ := result.Value.(error)
		core.Error(
			"window layout save failed",
			"file_path", filePath,
			"err", core.E("window.LayoutManager.save", "failed to encode window layouts", marshalErr),
		)
		return core.E("window.LayoutManager.save", "failed to encode window layouts", marshalErr)
	}
	data := result.Value.([]byte)
	if dir := lm.dataDir(); dir != "" {
		if err := coreMkdirAll(dir, 0o755); err != nil {
			core.Error(
				"window layout save failed",
				"file_path", filePath,
				"err", core.E("window.LayoutManager.save", "failed to create window layout directory", err),
			)
			return core.E("window.LayoutManager.save", "failed to create window layout directory", err)
		}
	}
	if err := coreWriteFile(filePath, data, 0o644); err != nil {
		core.Error(
			"window layout save failed",
			"file_path", filePath,
			"err", core.E("window.LayoutManager.save", "failed to write window layouts", err),
		)
		return core.E("window.LayoutManager.save", "failed to write window layouts", err)
	}
	return nil
}

// SaveLayout creates or updates a named layout.
func (lm *LayoutManager) SaveLayout(name string, windowStates map[string]WindowState) resultFailure {
	if name == "" {
		return core.E("window.LayoutManager.SaveLayout", "layout name cannot be empty", nil)
	}
	now := time.Now().UnixMilli()
	lm.mu.Lock()
	existing, exists := lm.layouts[name]
	layout := Layout{
		Name:      name,
		Windows:   windowStates,
		UpdatedAt: now,
	}
	if exists {
		layout.CreatedAt = existing.CreatedAt
	} else {
		layout.CreatedAt = now
	}
	lm.layouts[name] = layout
	lm.mu.Unlock()
	if err := lm.save(); err != nil {
		return err
	}
	return nil
}

// GetLayout returns a layout by name.
func (lm *LayoutManager) GetLayout(name string) (Layout, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	l, ok := lm.layouts[name]
	return l, ok
}

// ListLayouts returns info summaries for all layouts.
func (lm *LayoutManager) ListLayouts() []LayoutInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	infos := make([]LayoutInfo, 0, len(lm.layouts))
	for _, l := range lm.layouts {
		infos = append(infos, LayoutInfo{
			Name: l.Name, WindowCount: len(l.Windows),
			CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
		})
	}
	return infos
}

// DeleteLayout removes a layout by name.
func (lm *LayoutManager) DeleteLayout(name string) {
	lm.mu.Lock()
	delete(lm.layouts, name)
	lm.mu.Unlock()
	if err := lm.save(); err != nil {
		return
	}
}
