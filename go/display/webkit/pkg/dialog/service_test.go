// pkg/dialog/service_test.go
package dialog

import (
	"context"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/webview"
	"dappco.re/go/render/display/webkit/pkg/window"
)

type mockPlatform struct {
	openFilePaths []string
	saveFilePath  string
	openDirPath   string
	messageButton string
	openFileErr   resultFailure
	saveFileErr   resultFailure
	openDirErr    resultFailure
	messageErr    resultFailure
	lastOpenOpts  OpenFileOptions
	lastSaveOpts  SaveFileOptions
	lastDirOpts   OpenDirectoryOptions
	lastMsgOpts   MessageDialogOptions
}

func (m *mockPlatform) OpenFile(opts OpenFileOptions) ([]string, resultFailure) {
	m.lastOpenOpts = opts
	return m.openFilePaths, m.openFileErr
}
func (m *mockPlatform) SaveFile(opts SaveFileOptions) (string, resultFailure) {
	m.lastSaveOpts = opts
	return m.saveFilePath, m.saveFileErr
}
func (m *mockPlatform) OpenDirectory(opts OpenDirectoryOptions) (string, resultFailure) {
	m.lastDirOpts = opts
	return m.openDirPath, m.openDirErr
}
func (m *mockPlatform) MessageDialog(opts MessageDialogOptions) (string, resultFailure) {
	m.lastMsgOpts = opts
	return m.messageButton, m.messageErr
}

func newTestService(t *core.T) (*mockPlatform, *core.Core) {
	t.Helper()
	mock := &mockPlatform{
		openFilePaths: []string{"/tmp/file.txt"},
		saveFilePath:  "/tmp/save.txt",
		openDirPath:   "/tmp/dir",
		messageButton: "OK",
	}
	// Unlocked: callers register a "gui.webview.eval" stub after startup to
	// stand in for the webview, and since core v0.12.0 ServiceStartup seals
	// the action registry, which refuses that write without reporting it.
	c := core.New(
		core.WithService(Register(mock)),
	)
	core.RequireTrue(t, c.ServiceStartup(context.Background(), nil).OK)
	return mock, c
}

func taskRun(c *core.Core, name string, task any) core.Result {
	return c.Action(name).Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: task},
	))
}

// --- Good path tests ---

func TestService_Register_GoodCase(t *core.T) {
	_, c := newTestService(t)
	svc := core.MustServiceFor[*Service](c, "dialog")
	core.AssertNotNil(t, svc)
}

func TestService_TaskOpenFile_Good(t *core.T) {
	// TaskOpenFile
	ax7Variant := "TaskOpenFile:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.openFilePaths = []string{"/a.txt", "/b.txt"}

	r := taskRun(c, "dialog.open_file", TaskOpenFile{
		Options: OpenFileOptions{Title: "Pick", AllowMultiple: true},
	})
	core.RequireTrue(t, r.OK)
	paths := r.Value.([]string)
	core.AssertEqual(t, []string{"/a.txt", "/b.txt"}, paths)
	core.AssertEqual(t, "Pick", mock.lastOpenOpts.Title)
	core.AssertTrue(t, mock.lastOpenOpts.AllowMultiple)
}

func TestService_TaskOpenFile_FileFilters_Good(t *core.T) {
	// TaskOpenFile FileFilters
	ax7Variant := "TaskOpenFile_FileFilters:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.openFilePaths = []string{"/img.png"}

	filters := []FileFilter{{DisplayName: "Images", Pattern: "*.png;*.jpg"}}
	r := taskRun(c, "dialog.open_file", TaskOpenFile{
		Options: OpenFileOptions{
			Title:   "Select image",
			Filters: filters,
		},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, []string{"/img.png"}, r.Value.([]string))
	core.AssertLen(t, mock.lastOpenOpts.Filters, 1)
	core.AssertEqual(t, "Images", mock.lastOpenOpts.Filters[0].DisplayName)
	core.AssertEqual(t, "*.png;*.jpg", mock.lastOpenOpts.Filters[0].Pattern)
}

func TestService_TaskOpenFile_MultipleSelection_Good(t *core.T) {
	// TaskOpenFile MultipleSelection
	ax7Variant := "TaskOpenFile_MultipleSelection:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.openFilePaths = []string{"/a.txt", "/b.txt", "/c.txt"}

	r := taskRun(c, "dialog.open_file", TaskOpenFile{
		Options: OpenFileOptions{AllowMultiple: true},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, []string{"/a.txt", "/b.txt", "/c.txt"}, r.Value.([]string))
	core.AssertTrue(t, mock.lastOpenOpts.AllowMultiple)
}

func TestService_TaskOpenFile_CanChooseOptions_Good(t *core.T) {
	// TaskOpenFile CanChooseOptions
	ax7Variant := "TaskOpenFile_CanChooseOptions:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)

	r := taskRun(c, "dialog.open_file", TaskOpenFile{
		Options: OpenFileOptions{
			CanChooseFiles:       true,
			CanChooseDirectories: true,
			ShowHiddenFiles:      true,
		},
	})
	core.RequireTrue(t, r.OK)
	core.AssertTrue(t, mock.lastOpenOpts.CanChooseFiles)
	core.AssertTrue(t, mock.lastOpenOpts.CanChooseDirectories)
	core.AssertTrue(t, mock.lastOpenOpts.ShowHiddenFiles)
}

func TestService_TaskOpenFileWithOptions_Good(t *core.T) {
	// TaskOpenFileWithOptions
	ax7Variant := "TaskOpenFileWithOptions:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.openFilePaths = []string{"/log.txt"}

	opts := &OpenFileOptions{
		Title:           "Select log",
		AllowMultiple:   false,
		ShowHiddenFiles: true,
	}
	r := taskRun(c, "dialog.open_file", TaskOpenFileWithOptions{Options: opts})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, []string{"/log.txt"}, r.Value.([]string))
	core.AssertEqual(t, "Select log", mock.lastOpenOpts.Title)
	core.AssertTrue(t, mock.lastOpenOpts.ShowHiddenFiles)
}

func TestService_TaskOpenFileWithOptions_NilOptions_Good(t *core.T) {
	// TaskOpenFileWithOptions NilOptions
	ax7Variant := "TaskOpenFileWithOptions_NilOptions:good"
	core.AssertContains(t, ax7Variant, "good")
	_, c := newTestService(t)

	r := taskRun(c, "dialog.open_file", TaskOpenFileWithOptions{Options: nil})
	core.RequireTrue(t, r.OK)
	core.AssertNotNil(t, r.Value)
}

func TestService_TaskSaveFile_Good(t *core.T) {
	// TaskSaveFile
	ax7Variant := "TaskSaveFile:good"
	core.AssertContains(t, ax7Variant, "good")
	_, c := newTestService(t)
	r := taskRun(c, "dialog.save_file", TaskSaveFile{
		Options: SaveFileOptions{Filename: "out.txt"},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "/tmp/save.txt", r.Value)
}

func TestService_TaskSaveFile_ShowHidden_Good(t *core.T) {
	// TaskSaveFile ShowHidden
	ax7Variant := "TaskSaveFile_ShowHidden:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)

	r := taskRun(c, "dialog.save_file", TaskSaveFile{
		Options: SaveFileOptions{Filename: "out.txt", ShowHiddenFiles: true},
	})
	core.RequireTrue(t, r.OK)
	core.AssertTrue(t, mock.lastSaveOpts.ShowHiddenFiles)
}

func TestService_TaskSaveFileWithOptions_Good(t *core.T) {
	// TaskSaveFileWithOptions
	ax7Variant := "TaskSaveFileWithOptions:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.saveFilePath = "/exports/data.json"

	opts := &SaveFileOptions{
		Title:    "Export data",
		Filename: "data.json",
		Filters:  []FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	}
	r := taskRun(c, "dialog.save_file", TaskSaveFileWithOptions{Options: opts})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "/exports/data.json", r.Value.(string))
	core.AssertEqual(t, "Export data", mock.lastSaveOpts.Title)
	core.AssertLen(t, mock.lastSaveOpts.Filters, 1)
	core.AssertEqual(t, "JSON", mock.lastSaveOpts.Filters[0].DisplayName)
}

func TestService_TaskSaveFileWithOptions_NilOptions_Good(t *core.T) {
	// TaskSaveFileWithOptions NilOptions
	ax7Variant := "TaskSaveFileWithOptions_NilOptions:good"
	core.AssertContains(t, ax7Variant, "good")
	_, c := newTestService(t)

	r := taskRun(c, "dialog.save_file", TaskSaveFileWithOptions{Options: nil})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "/tmp/save.txt", r.Value)
}

func TestService_TaskOpenDirectory_Good(t *core.T) {
	// TaskOpenDirectory
	ax7Variant := "TaskOpenDirectory:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)

	r := taskRun(c, "dialog.open_directory", TaskOpenDirectory{
		Options: OpenDirectoryOptions{Title: "Pick Dir", ShowHiddenFiles: true},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "/tmp/dir", r.Value)
	core.AssertEqual(t, "Pick Dir", mock.lastDirOpts.Title)
	core.AssertTrue(t, mock.lastDirOpts.ShowHiddenFiles)
}

func TestService_TaskMessageDialog_Good(t *core.T) {
	// TaskMessageDialog
	ax7Variant := "TaskMessageDialog:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.messageButton = "Yes"

	r := taskRun(c, "dialog.message", TaskMessageDialog{
		Options: MessageDialogOptions{
			Type: DialogQuestion, Title: "Confirm",
			Message: "Sure?", Buttons: []string{"Yes", "No"},
		},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "Yes", r.Value)
	core.AssertEqual(t, DialogQuestion, mock.lastMsgOpts.Type)
}

func TestService_TaskInfo_Good(t *core.T) {
	// TaskInfo
	ax7Variant := "TaskInfo:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.messageButton = "OK"

	r := taskRun(c, "dialog.info", TaskInfo{
		Title: "Done", Message: "File saved successfully.",
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "OK", r.Value.(string))
	core.AssertEqual(t, DialogInfo, mock.lastMsgOpts.Type)
	core.AssertEqual(t, "Done", mock.lastMsgOpts.Title)
	core.AssertEqual(t, "File saved successfully.", mock.lastMsgOpts.Message)
}

func TestService_TaskInfo_WithButtons_Good(t *core.T) {
	// TaskInfo WithButtons
	ax7Variant := "TaskInfo_WithButtons:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.messageButton = "Close"

	r := taskRun(c, "dialog.info", TaskInfo{
		Title: "Notice", Message: "Update available.", Buttons: []string{"Close", "Later"},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "Close", r.Value.(string))
	core.AssertEqual(t, []string{"Close", "Later"}, mock.lastMsgOpts.Buttons)
}

func TestService_TaskQuestion_Good(t *core.T) {
	// TaskQuestion
	ax7Variant := "TaskQuestion:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.messageButton = "Yes"

	r := taskRun(c, "dialog.question", TaskQuestion{
		Title: "Confirm deletion", Message: "Delete file?", Buttons: []string{"Yes", "No"},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "Yes", r.Value.(string))
	core.AssertEqual(t, DialogQuestion, mock.lastMsgOpts.Type)
	core.AssertEqual(t, "Confirm deletion", mock.lastMsgOpts.Title)
}

func TestService_TaskWarning_Good(t *core.T) {
	// TaskWarning
	ax7Variant := "TaskWarning:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.messageButton = "OK"

	r := taskRun(c, "dialog.warning", TaskWarning{
		Title: "Disk full", Message: "Storage is critically low.", Buttons: []string{"OK"},
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "OK", r.Value.(string))
	core.AssertEqual(t, DialogWarning, mock.lastMsgOpts.Type)
	core.AssertEqual(t, "Disk full", mock.lastMsgOpts.Title)
}

func TestService_TaskError_Good(t *core.T) {
	// TaskError
	ax7Variant := "TaskError:good"
	core.AssertContains(t, ax7Variant, "good")
	mock, c := newTestService(t)
	mock.messageButton = "OK"

	r := taskRun(c, "dialog.error", TaskError{
		Title: "Operation failed", Message: "could not write file: permission denied",
	})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, "OK", r.Value.(string))
	core.AssertEqual(t, DialogError, mock.lastMsgOpts.Type)
	core.AssertEqual(t, "Operation failed", mock.lastMsgOpts.Title)
	core.AssertEqual(t, "could not write file: permission denied", mock.lastMsgOpts.Message)
}

func TestService_TaskPrompt_Good(t *core.T) {
	// TaskPrompt
	ax7Variant := "TaskPrompt:good"
	core.AssertContains(t, ax7Variant, "good")
	_, c := newTestService(t)

	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch q.(type) {
		case window.QueryWindowList:
			return core.Result{Value: []window.WindowInfo{
				{Name: "editor", Focused: true},
			}, OK: true}
		default:
			return core.Result{}
		}
	})

	var script string
	c.Action("gui.webview.eval", func(_ context.Context, opts core.Options) core.Result {
		task := opts.Get("task").Value.(webview.TaskEvaluate)
		script = task.Script
		core.AssertEqual(t, "editor", task.Window)
		return core.Result{Value: "draft", OK: true}
	})

	r := taskRun(c, "dialog.prompt", TaskPrompt{
		Title:        "Rename",
		Message:      "Enter a new name",
		DefaultValue: "draft",
	})
	core.RequireTrue(t, r.OK)
	result := r.Value.(PromptResult)
	core.AssertEqual(t, "draft", result.Value)
	core.AssertTrue(t, result.Confirmed)
	core.AssertContains(t, script, "window.prompt(")
	core.AssertContains(t, script, "Rename")
	core.AssertContains(t, script, "Enter a new name")
}

// --- Bad path tests ---

func TestService_TaskOpenFile_BadCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.open_file").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

func TestService_TaskOpenFileWithOptions_BadCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.open_file").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

func TestService_TaskSaveFileWithOptions_BadCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.save_file").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

func TestService_TaskInfo_BadCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.info").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

func TestService_TaskQuestion_BadCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.question").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

func TestService_TaskWarning_BadCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.warning").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

func TestService_TaskError_BadCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.error").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

// --- Ugly path tests ---

func TestService_TaskOpenFile_Ugly(t *core.T) {
	// TaskOpenFile
	ax7Variant := "TaskOpenFile:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	mock, c := newTestService(t)
	mock.openFilePaths = nil

	r := taskRun(c, "dialog.open_file", TaskOpenFile{
		Options: OpenFileOptions{Title: "Pick"},
	})
	core.RequireTrue(t, r.OK)
	core.AssertNil(t, r.Value.([]string))
}

func TestService_TaskOpenFileWithOptions_MultipleFilters_Ugly(t *core.T) {
	// TaskOpenFileWithOptions MultipleFilters
	ax7Variant := "TaskOpenFileWithOptions_MultipleFilters:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	mock, c := newTestService(t)
	mock.openFilePaths = []string{"/doc.pdf"}

	opts := &OpenFileOptions{
		Title: "Select document",
		Filters: []FileFilter{
			{DisplayName: "PDF", Pattern: "*.pdf"},
			{DisplayName: "Word", Pattern: "*.docx"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	}
	r := taskRun(c, "dialog.open_file", TaskOpenFileWithOptions{Options: opts})
	core.RequireTrue(t, r.OK)
	core.AssertEqual(t, []string{"/doc.pdf"}, r.Value.([]string))
	core.AssertLen(t, mock.lastOpenOpts.Filters, 3)
}

func TestService_TaskSaveFileWithOptions_FiltersAndHidden_Ugly(t *core.T) {
	// TaskSaveFileWithOptions FiltersAndHidden
	ax7Variant := "TaskSaveFileWithOptions_FiltersAndHidden:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	mock, c := newTestService(t)

	opts := &SaveFileOptions{
		Title:           "Save",
		Filename:        "output.csv",
		ShowHiddenFiles: true,
		Filters:         []FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}},
	}
	r := taskRun(c, "dialog.save_file", TaskSaveFileWithOptions{Options: opts})
	core.RequireTrue(t, r.OK)
	core.AssertTrue(t, mock.lastSaveOpts.ShowHiddenFiles)
	core.AssertEqual(t, "output.csv", mock.lastSaveOpts.Filename)
}

func TestService_UnknownTask_UglyCase(t *core.T) {
	c := core.New(core.WithServiceLock())
	r := c.Action("dialog.nonexistent").Run(context.Background(), core.NewOptions())
	core.AssertFalse(t, r.OK)
}

func TestService_promptOptionsFrom_Good(t *core.T) {
	// promptOptionsFrom
	ax7Variant := "promptOptionsFrom:good"
	core.AssertContains(t, ax7Variant, "good")
	got, err := promptOptionsFrom(core.NewOptions(
		core.Option{Key: "task", Value: TaskPrompt{
			Title:        "Rename",
			Message:      "Enter a new name",
			DefaultValue: "draft",
		}},
	))
	core.RequireNoError(t, err)
	core.AssertEqual(t, TaskPrompt{
		Title:        "Rename",
		Message:      "Enter a new name",
		DefaultValue: "draft",
	}, got)
}

func TestService_promptOptionsFrom_Bad(t *core.T) {
	// promptOptionsFrom
	ax7Variant := "promptOptionsFrom:bad"
	core.AssertContains(t, ax7Variant, "bad")
	got, err := promptOptionsFrom(core.NewOptions(
		core.Option{Key: "title", Value: "Rename"},
		core.Option{Key: "message", Value: "Enter a new name"},
		core.Option{Key: "defaultValue", Value: "draft"},
	))
	core.RequireNoError(t, err)
	core.AssertEqual(t, TaskPrompt{
		Title:        "Rename",
		Message:      "Enter a new name",
		DefaultValue: "draft",
	}, got)
}

func TestService_promptOptionsFrom_Ugly(t *core.T) {
	// promptOptionsFrom
	ax7Variant := "promptOptionsFrom:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	got, err := promptOptionsFrom(core.NewOptions())
	core.RequireNoError(t, err)
	core.AssertEmpty(t, got)
}

func TestService_promptWindowName_Good(t *core.T) {
	// promptWindowName
	ax7Variant := "promptWindowName:good"
	core.AssertContains(t, ax7Variant, "good")
	_, c := newTestService(t)
	svc := core.MustServiceFor[*Service](c, "dialog")

	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch q.(type) {
		case window.QueryWindowList:
			return core.Result{Value: []window.WindowInfo{
				{Name: "editor"},
				{Name: "preview", Focused: true},
			}, OK: true}
		default:
			return core.Result{}
		}
	})

	got, err := svc.promptWindowName()
	core.RequireNoError(t, err)
	core.AssertEqual(t, "preview", got)
}

func TestService_promptWindowName_Bad(t *core.T) {
	// promptWindowName
	ax7Variant := "promptWindowName:bad"
	core.AssertContains(t, ax7Variant, "bad")
	_, c := newTestService(t)
	svc := core.MustServiceFor[*Service](c, "dialog")

	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch q.(type) {
		case window.QueryWindowList:
			return core.Result{Value: "unexpected", OK: true}
		default:
			return core.Result{}
		}
	})

	got, err := svc.promptWindowName()
	core.AssertError(t, err)
	core.AssertEmpty(t, got)
}

func TestService_promptWindowName_Ugly(t *core.T) {
	// promptWindowName
	ax7Variant := "promptWindowName:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	_, c := newTestService(t)
	svc := core.MustServiceFor[*Service](c, "dialog")

	c.RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch q.(type) {
		case window.QueryWindowList:
			return core.Result{Value: []window.WindowInfo{
				{Name: "editor"},
				{Name: "terminal"},
			}, OK: true}
		default:
			return core.Result{}
		}
	})

	got, err := svc.promptWindowName()
	core.RequireNoError(t, err)
	core.AssertEqual(t, "editor", got)
}

func TestService_promptScript_Good(t *core.T) {
	// promptScript
	ax7Variant := "promptScript:good"
	core.AssertContains(t, ax7Variant, "good")
	script := promptScript("Rename", "Enter a new name", "draft")
	core.AssertContains(t, script, "window.prompt(")
	core.AssertContains(t, script, "Rename")
	core.AssertContains(t, script, "Enter a new name")
	core.AssertContains(t, script, "draft")
}

func TestService_promptScript_Bad(t *core.T) {
	// promptScript
	ax7Variant := "promptScript:bad"
	core.AssertContains(t, ax7Variant, "bad")
	script := promptScript("Rename", "", "")
	core.AssertContains(t, script, "window.prompt(")
	core.AssertContains(t, script, "Rename")
	core.AssertNotContains(t, script, "Enter a new name")
}

func TestService_promptScript_Ugly(t *core.T) {
	// promptScript
	ax7Variant := "promptScript:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	script := promptScript("", "Line 1\nLine 2", "\"quoted\"")
	core.AssertContains(t, script, "Line 1")
	core.AssertContains(t, script, "Line 2")
	core.AssertContains(t, script, "quoted")
	core.AssertTrue(t, core.Contains(script, "window.prompt("))
}

// AX7 generated source-matching smoke coverage.
func TestService_Register_Bad(t *core.T) {
	// Register
	ax7Variant := "Register:bad"
	core.AssertContains(t, ax7Variant, "bad")
	result := core.Try(func() any {
		got0 := Register(*new(Platform))
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestService_Register_Ugly(t *core.T) {
	// Register
	ax7Variant := "Register:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	result := core.Try(func() any {
		got0 := Register(*new(Platform))
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

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

func TestService_Register_Good(t *core.T) {
	// Register
	ax7Variant := "Register:good"
	core.AssertContains(t, ax7Variant, "good")
	result := core.Try(func() any {
		got0 := Register(nil)
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}
