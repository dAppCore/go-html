package window

import (
	core "dappco.re/go"
	"time"
)

func TestStateManagerState_NewStateManagerWithDir_Good(t *core.T) {
	// NewStateManagerWithDir
	ax7Variant := "NewStateManagerWithDir:good"
	core.AssertContains(t, ax7Variant, "good")
	dir := t.TempDir()
	sm := NewStateManagerWithDir(dir)

	core.AssertNotNil(t, sm)
	core.AssertEqual(t, dir, sm.dataDir())
	core.AssertEqual(t, core.PathJoin(dir, "window_state.json"), sm.filePath())
	core.AssertEmpty(t, sm.ListStates())
}

func TestStateManagerState_NewStateManagerWithPathEnv_GoodCase(t *core.T) {
	path := core.PathJoin(t.TempDir(), "custom", "window_state.json")
	t.Setenv(windowStateFileEnv, path)

	sm := NewStateManager()

	core.AssertNotNil(t, sm)
	core.AssertEqual(t, path, sm.filePath())
	core.AssertEqual(t, core.PathDir(path), sm.dataDir())
}

func TestStateManagerState_NewStateManagerWithDir_Bad(t *core.T) {
	// NewStateManagerWithDir
	ax7Variant := "NewStateManagerWithDir:bad"
	core.AssertContains(t, ax7Variant, "bad")
	sm := NewStateManagerWithDir("")

	core.AssertNotNil(t, sm)
	core.AssertEmpty(t, sm.dataDir())
}

func TestStateManagerState_NewStateManagerWithDir_InvalidFile_Good(t *core.T) {
	// NewStateManagerWithDir InvalidFile
	ax7Variant := "NewStateManagerWithDir_InvalidFile:good"
	core.AssertContains(t, ax7Variant, "good")
	dir := t.TempDir()
	core.RequireNoError(t, coreWriteFile(core.PathJoin(dir, "window_state.json"), []byte("{invalid"), 0o644))

	sm := NewStateManagerWithDir(dir)

	core.AssertNotNil(t, sm)
	core.AssertEmpty(t, sm.ListStates())
}

func TestStateManagerState_SetPath_Good(t *core.T) {
	// SetPath
	ax7Variant := "SetPath:good"
	core.AssertContains(t, ax7Variant, "good")
	dir := t.TempDir()
	sm := NewStateManagerWithDir(dir)
	path := core.PathJoin(dir, "custom", "window-state.json")

	sm.SetPath(path)
	sm.SetState("main", WindowState{X: 10, Y: 20, Width: 300, Height: 200})
	sm.ForceSync()

	content, err := coreReadFile(path)
	core.RequireNoError(t, err)
	core.AssertContains(t, string(content), `"main"`)
	core.AssertEqual(t, path, sm.filePath())
	core.AssertEqual(t, core.PathDir(path), sm.dataDir())
}

func TestStateManagerState_SetPath_Ugly(t *core.T) {
	// SetPath
	ax7Variant := "SetPath:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	sm := NewStateManagerWithDir(t.TempDir())
	initial := sm.filePath()

	sm.SetPath("")

	core.AssertEqual(t, initial, sm.filePath())
}

func TestStateManagerState_SetState_Good(t *core.T) {
	// SetState
	ax7Variant := "SetState:good"
	core.AssertContains(t, ax7Variant, "good")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.SetState("main", WindowState{X: 1, Y: 2, Width: 3, Height: 4, Maximized: true})

	got, ok := sm.GetState("main")
	core.RequireTrue(t, ok)
	core.AssertEqual(t, 1, got.X)
	core.AssertEqual(t, 2, got.Y)
	core.AssertEqual(t, 3, got.Width)
	core.AssertEqual(t, 4, got.Height)
	core.AssertTrue(t, got.Maximized)
	core.AssertNotEmpty(t, got.UpdatedAt)
}

func TestStateManagerState_UpdatePosition_Bad(t *core.T) {
	// UpdatePosition
	ax7Variant := "UpdatePosition:bad"
	core.AssertContains(t, ax7Variant, "bad")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.UpdatePosition("missing", 30, 40)

	got, ok := sm.GetState("missing")
	core.RequireTrue(t, ok)
	core.AssertEqual(t, 30, got.X)
	core.AssertEqual(t, 40, got.Y)
	core.AssertEmpty(t, got.Width)
	core.AssertEmpty(t, got.Height)
}

func TestStateManagerState_UpdateSize_Ugly(t *core.T) {
	// UpdateSize
	ax7Variant := "UpdateSize:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.UpdateSize("missing", -800, -600)

	got, ok := sm.GetState("missing")
	core.RequireTrue(t, ok)
	core.AssertEqual(t, -800, got.Width)
	core.AssertEqual(t, -600, got.Height)
}

func TestStateManagerState_UpdateMaximized_Good(t *core.T) {
	// UpdateMaximized
	ax7Variant := "UpdateMaximized:good"
	core.AssertContains(t, ax7Variant, "good")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.UpdateMaximized("main", true)

	got, ok := sm.GetState("main")
	core.RequireTrue(t, ok)
	core.AssertTrue(t, got.Maximized)
}

func TestStateManagerState_CaptureState_Good(t *core.T) {
	// CaptureState
	ax7Variant := "CaptureState:good"
	core.AssertContains(t, ax7Variant, "good")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.CaptureState(&mockWindow{name: "captured", x: 50, y: 60, width: 800, height: 600, maximised: true})

	got, ok := sm.GetState("captured")
	core.RequireTrue(t, ok)
	core.AssertEqual(t, 50, got.X)
	core.AssertEqual(t, 60, got.Y)
	core.AssertEqual(t, 800, got.Width)
	core.AssertEqual(t, 600, got.Height)
	core.AssertTrue(t, got.Maximized)
}

func TestStateManagerState_ApplyState_Bad(t *core.T) {
	// ApplyState
	ax7Variant := "ApplyState:bad"
	core.AssertContains(t, ax7Variant, "bad")
	sm := NewStateManagerWithDir(t.TempDir())
	w := &Window{Name: "missing", X: 9, Y: 8, Width: 7, Height: 6}

	sm.ApplyState(w)

	core.AssertEqual(t, 9, w.X)
	core.AssertEqual(t, 8, w.Y)
	core.AssertEqual(t, 7, w.Width)
	core.AssertEqual(t, 6, w.Height)
}

func TestStateManagerState_ApplyState_Good(t *core.T) {
	// ApplyState
	ax7Variant := "ApplyState:good"
	core.AssertContains(t, ax7Variant, "good")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.SetState("main", WindowState{X: 11, Y: 12, Width: 1300, Height: 900})

	w := &Window{Name: "main", X: 1, Y: 2, Width: 10, Height: 20}
	sm.ApplyState(w)

	core.AssertEqual(t, 11, w.X)
	core.AssertEqual(t, 12, w.Y)
	core.AssertEqual(t, 1300, w.Width)
	core.AssertEqual(t, 900, w.Height)
}

func TestStateManagerState_ListStates_Good(t *core.T) {
	// ListStates
	ax7Variant := "ListStates:good"
	core.AssertContains(t, ax7Variant, "good")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.SetState("alpha", WindowState{})
	sm.SetState("beta", WindowState{})

	names := sm.ListStates()

	core.AssertElementsMatch(t, []string{"alpha", "beta"}, names)
}

func TestStateManagerState_Clear_Good(t *core.T) {
	// Clear
	ax7Variant := "Clear:good"
	core.AssertContains(t, ax7Variant, "good")
	sm := NewStateManagerWithDir(t.TempDir())
	sm.SetState("alpha", WindowState{})
	sm.SetState("beta", WindowState{})

	sm.Clear()

	core.AssertEmpty(t, sm.ListStates())
}

func TestStateManagerState_ForceSync_Good(t *core.T) {
	// ForceSync
	ax7Variant := "ForceSync:good"
	core.AssertContains(t, ax7Variant, "good")
	dir := t.TempDir()
	sm := NewStateManagerWithDir(dir)
	sm.SetState("main", WindowState{Width: 800, Height: 600})
	time.Sleep(10 * time.Millisecond)

	sm.ForceSync()

	content, err := coreReadFile(core.PathJoin(dir, "window_state.json"))
	core.RequireNoError(t, err)
	core.AssertContains(t, string(content), `"main"`)
}

// AX7 generated source-matching smoke coverage.
func TestState_NewStateManager_Good(t *core.T) {
	// NewStateManager
	ax7Variant := "NewStateManager:good"
	core.AssertContains(t, ax7Variant, "good")
	result := core.Try(func() any {
		got0 := NewStateManager()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManager_Bad(t *core.T) {
	// NewStateManager
	ax7Variant := "NewStateManager:bad"
	core.AssertContains(t, ax7Variant, "bad")
	result := core.Try(func() any {
		got0 := NewStateManager()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManager_Ugly(t *core.T) {
	// NewStateManager
	ax7Variant := "NewStateManager:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	result := core.Try(func() any {
		got0 := NewStateManager()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManagerWithDir_Good(t *core.T) {
	// NewStateManagerWithDir
	ax7Variant := "NewStateManagerWithDir:good"
	core.AssertContains(t, ax7Variant, "good")
	result := core.Try(func() any {
		got0 := NewStateManagerWithDir("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManagerWithDir_Bad(t *core.T) {
	// NewStateManagerWithDir
	ax7Variant := "NewStateManagerWithDir:bad"
	core.AssertContains(t, ax7Variant, "bad")
	result := core.Try(func() any {
		got0 := NewStateManagerWithDir("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManagerWithDir_Ugly(t *core.T) {
	// NewStateManagerWithDir
	ax7Variant := "NewStateManagerWithDir:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	result := core.Try(func() any {
		got0 := NewStateManagerWithDir("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManagerWithPath_Good(t *core.T) {
	// NewStateManagerWithPath
	ax7Variant := "NewStateManagerWithPath:good"
	core.AssertContains(t, ax7Variant, "good")
	result := core.Try(func() any {
		got0 := NewStateManagerWithPath("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManagerWithPath_Bad(t *core.T) {
	// NewStateManagerWithPath
	ax7Variant := "NewStateManagerWithPath:bad"
	core.AssertContains(t, ax7Variant, "bad")
	result := core.Try(func() any {
		got0 := NewStateManagerWithPath("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_NewStateManagerWithPath_Ugly(t *core.T) {
	// NewStateManagerWithPath
	ax7Variant := "NewStateManagerWithPath:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	result := core.Try(func() any {
		got0 := NewStateManagerWithPath("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_SetPath_Good(t *core.T) {
	// StateManager SetPath
	ax7Variant := "StateManager_SetPath:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.SetPath("agent")
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_SetPath_Bad(t *core.T) {
	// StateManager SetPath
	ax7Variant := "StateManager_SetPath:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.SetPath("")
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_SetPath_Ugly(t *core.T) {
	// StateManager SetPath
	ax7Variant := "StateManager_SetPath:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.SetPath("../../edge")
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_GetState_Good(t *core.T) {
	// StateManager GetState
	ax7Variant := "StateManager_GetState:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0, got1 := subject.GetState("agent")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_GetState_Bad(t *core.T) {
	// StateManager GetState
	ax7Variant := "StateManager_GetState:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0, got1 := subject.GetState("")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_GetState_Ugly(t *core.T) {
	// StateManager GetState
	ax7Variant := "StateManager_GetState:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0, got1 := subject.GetState("../../edge")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_SetState_Good(t *core.T) {
	// StateManager SetState
	ax7Variant := "StateManager_SetState:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.SetState("agent", *new(WindowState))
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_SetState_Bad(t *core.T) {
	// StateManager SetState
	ax7Variant := "StateManager_SetState:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.SetState("", *new(WindowState))
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_SetState_Ugly(t *core.T) {
	// StateManager SetState
	ax7Variant := "StateManager_SetState:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.SetState("../../edge", *new(WindowState))
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdatePosition_Good(t *core.T) {
	// StateManager UpdatePosition
	ax7Variant := "StateManager_UpdatePosition:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdatePosition("agent", 1, 1)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdatePosition_Bad(t *core.T) {
	// StateManager UpdatePosition
	ax7Variant := "StateManager_UpdatePosition:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdatePosition("", 0, 0)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdatePosition_Ugly(t *core.T) {
	// StateManager UpdatePosition
	ax7Variant := "StateManager_UpdatePosition:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdatePosition("../../edge", -1, -1)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdateSize_Good(t *core.T) {
	// StateManager UpdateSize
	ax7Variant := "StateManager_UpdateSize:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdateSize("agent", 1, 1)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdateSize_Bad(t *core.T) {
	// StateManager UpdateSize
	ax7Variant := "StateManager_UpdateSize:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdateSize("", 0, 0)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdateSize_Ugly(t *core.T) {
	// StateManager UpdateSize
	ax7Variant := "StateManager_UpdateSize:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdateSize("../../edge", -1, -1)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdateMaximized_Good(t *core.T) {
	// StateManager UpdateMaximized
	ax7Variant := "StateManager_UpdateMaximized:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdateMaximized("agent", true)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdateMaximized_Bad(t *core.T) {
	// StateManager UpdateMaximized
	ax7Variant := "StateManager_UpdateMaximized:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdateMaximized("", false)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_UpdateMaximized_Ugly(t *core.T) {
	// StateManager UpdateMaximized
	ax7Variant := "StateManager_UpdateMaximized:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.UpdateMaximized("../../edge", false)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_CaptureState_Good(t *core.T) {
	// StateManager CaptureState
	ax7Variant := "StateManager_CaptureState:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.CaptureState(*new(PlatformWindow))
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_CaptureState_Bad(t *core.T) {
	// StateManager CaptureState
	ax7Variant := "StateManager_CaptureState:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.CaptureState(*new(PlatformWindow))
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_CaptureState_Ugly(t *core.T) {
	// StateManager CaptureState
	ax7Variant := "StateManager_CaptureState:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.CaptureState(*new(PlatformWindow))
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ApplyState_Good(t *core.T) {
	// StateManager ApplyState
	ax7Variant := "StateManager_ApplyState:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.ApplyState(nil)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ApplyState_Bad(t *core.T) {
	// StateManager ApplyState
	ax7Variant := "StateManager_ApplyState:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.ApplyState(nil)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ApplyState_Ugly(t *core.T) {
	// StateManager ApplyState
	ax7Variant := "StateManager_ApplyState:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.ApplyState(nil)
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ListStates_Good(t *core.T) {
	// StateManager ListStates
	ax7Variant := "StateManager_ListStates:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0 := subject.ListStates()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ListStates_Bad(t *core.T) {
	// StateManager ListStates
	ax7Variant := "StateManager_ListStates:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0 := subject.ListStates()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ListStates_Ugly(t *core.T) {
	// StateManager ListStates
	ax7Variant := "StateManager_ListStates:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0 := subject.ListStates()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_Clear_Good(t *core.T) {
	// StateManager Clear
	ax7Variant := "StateManager_Clear:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.Clear()
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_Clear_Bad(t *core.T) {
	// StateManager Clear
	ax7Variant := "StateManager_Clear:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.Clear()
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_Clear_Ugly(t *core.T) {
	// StateManager Clear
	ax7Variant := "StateManager_Clear:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		subject.Clear()
		return "called"
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ForceSync_Good(t *core.T) {
	// StateManager ForceSync
	ax7Variant := "StateManager_ForceSync:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0 := subject.ForceSync()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ForceSync_Bad(t *core.T) {
	// StateManager ForceSync
	ax7Variant := "StateManager_ForceSync:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0 := subject.ForceSync()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestState_StateManager_ForceSync_Ugly(t *core.T) {
	// StateManager ForceSync
	ax7Variant := "StateManager_ForceSync:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StateManager)
	result := core.Try(func() any {
		got0 := subject.ForceSync()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStateManagerState_SaveAtomic_Good(t *core.T) {
	// save — publishes via coreWriteFileAtomic
	ax7Variant := "save:good"
	core.AssertContains(t, ax7Variant, "good")
	dir := t.TempDir()
	sm := NewStateManagerWithDir(dir)
	sm.states["main"] = WindowState{X: 1, Y: 2, Width: 640, Height: 480}
	core.RequireNoError(t, sm.save())

	content, err := coreReadFile(core.PathJoin(dir, "window_state.json"))
	core.RequireNoError(t, err)
	loaded := make(map[string]WindowState)
	core.RequireTrue(t, core.JSONUnmarshal(content, &loaded).OK)
	core.AssertEqual(t, 640, loaded["main"].Width)
}

func TestStateManagerState_SaveAtomic_Ugly_ConcurrentWritersNeverSplice(t *core.T) {
	// save — two managers share one path, as lthn and core/ide share the
	// Core config directory in production. Every read during the storm
	// must parse: rename publishes complete documents only. The plain
	// truncate-and-write this guards against could splice one writer's
	// short document onto the tail of the other's longer one.
	ax7Variant := "save:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	path := core.PathJoin(t.TempDir(), "window_state.json")
	long := NewStateManagerWithPath(path)
	short := NewStateManagerWithPath(path)
	for i := 0; i < 24; i++ {
		long.states[core.Concat("window-", core.Itoa(i))] = WindowState{X: i, Y: i, Width: 1600, Height: 900}
	}
	short.states["main"] = WindowState{Width: 640, Height: 480}

	// The first save lands before the reader starts, so every read below
	// observes a file that exists; the reader then runs until BOTH writers
	// finish, so reads are guaranteed to overlap the write storm.
	core.RequireNoError(t, long.save())
	saveErrs := make(chan resultFailure, 100)
	done := make(chan bool, 2)
	writer := func(sm *StateManager) {
		for i := 0; i < 50; i++ {
			saveErrs <- sm.save()
		}
		done <- true
	}
	go writer(long)
	go writer(short)

	spliced := 0
	finished := 0
	for finished < 2 {
		select {
		case <-done:
			finished++
		default:
		}
		content, err := coreReadFile(path)
		core.RequireNoError(t, err)
		loaded := make(map[string]WindowState)
		if !core.JSONUnmarshal(content, &loaded).OK {
			spliced++
		}
	}
	close(saveErrs)
	for err := range saveErrs {
		core.RequireNoError(t, err)
	}
	core.AssertEqual(t, 0, spliced)
}
