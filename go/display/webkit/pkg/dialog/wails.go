// pkg/dialog/wails.go
package dialog

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// WailsPlatform implements Platform via Wails v3's app.Dialog manager.
//
// File / directory dialogs return synchronously through the
// PromptForSingleSelection / PromptForMultipleSelection calls on the
// builders Wails exposes. Message dialogs are inherently async on Wails
// (Show returns immediately, the user click fires a Button.OnClick
// callback on the main thread later); we adapt that to the synchronous
// Platform contract by blocking on a buffered channel that the
// per-button callback writes into.
//
//	wp := dialog.NewWailsPlatform(app)
//	core.WithService(dialog.Register(wp))
type WailsPlatform struct {
	app *application.App
}

func NewWailsPlatform(app *application.App) *WailsPlatform {
	return &WailsPlatform{app: app}
}

// OpenFile shows the native file picker. AllowMultiple controls which of
// the two PromptFor* methods we call — the single-selection variant is
// the common path and matches the macOS / Windows / Linux native UX.
func (wp *WailsPlatform) OpenFile(opts OpenFileOptions) ([]string, resultFailure) {
	if wp == nil || wp.app == nil {
		return nil, nil
	}
	d := wp.app.Dialog.OpenFile()
	applyOpenFileOptions(d, opts)
	if opts.AllowMultiple {
		paths, err := d.PromptForMultipleSelection()
		if err != nil {
			return nil, err
		}
		return paths, nil
	}
	path, err := d.PromptForSingleSelection()
	if err != nil {
		return nil, err
	}
	if path == "" {
		// User cancelled — return empty slice rather than [""], so the
		// caller's len()==0 check is the canonical "cancelled" signal.
		return nil, nil
	}
	return []string{path}, nil
}

// SaveFile shows the native save dialog and returns the chosen path,
// or an empty string if the user cancelled.
func (wp *WailsPlatform) SaveFile(opts SaveFileOptions) (string, resultFailure) {
	if wp == nil || wp.app == nil {
		return "", nil
	}
	d := wp.app.Dialog.SaveFile()
	applySaveFileOptions(d, opts)
	path, err := d.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}

// OpenDirectory shows the native folder picker. Wails routes folder
// selection through the same OpenFile builder with CanChooseFiles=false
// + CanChooseDirectories=true, which matches macOS NSOpenPanel semantics.
func (wp *WailsPlatform) OpenDirectory(opts OpenDirectoryOptions) (string, resultFailure) {
	if wp == nil || wp.app == nil {
		return "", nil
	}
	d := wp.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		ShowHiddenFiles(opts.ShowHiddenFiles)
	if opts.Title != "" {
		d.SetTitle(opts.Title)
	}
	if opts.Directory != "" {
		d.SetDirectory(opts.Directory)
	}
	path, err := d.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}

// MessageDialog shows an info / warning / error / question dialog with
// the supplied buttons and blocks until the user clicks one. Returns
// the label of the clicked button, or empty string if the dialog was
// dismissed without a click (e.g. ESC, window close).
//
// Wails MessageDialog.Show is non-blocking — Button.OnClick callbacks
// fire on the main thread when the user picks. We adapt by giving every
// button a callback that writes its label into a buffered channel, then
// block on the channel after Show. The channel is buffered so the
// callback never deadlocks if we time out first.
func (wp *WailsPlatform) MessageDialog(opts MessageDialogOptions) (string, resultFailure) {
	if wp == nil || wp.app == nil {
		return "", nil
	}
	dlg := dialogForType(wp.app, opts.Type)
	if opts.Title != "" {
		dlg.SetTitle(opts.Title)
	}
	if opts.Message != "" {
		dlg.SetMessage(opts.Message)
	}

	clicked := make(chan string, len(opts.Buttons)+1)
	if len(opts.Buttons) == 0 {
		// No buttons — Wails will render a single OK; no point waiting
		// for a click that has no label to report. Show + return empty.
		dlg.Show()
		return "", nil
	}
	for _, label := range opts.Buttons {
		b := dlg.AddButton(label)
		b.OnClick(func() { clicked <- label })
	}
	dlg.Show()
	return <-clicked, nil
}

// applyOpenFileOptions walks our cross-platform OpenFileOptions and
// pushes each set field onto the Wails fluent builder. Any zero-value
// field is left untouched so Wails can use its platform default.
func applyOpenFileOptions(d *application.OpenFileDialogStruct, opts OpenFileOptions) {
	if opts.Title != "" {
		d.SetTitle(opts.Title)
	}
	if opts.Directory != "" {
		d.SetDirectory(opts.Directory)
	}
	for _, f := range opts.Filters {
		d.AddFilter(f.DisplayName, f.Pattern)
	}
	d.CanChooseDirectories(opts.CanChooseDirectories).
		CanChooseFiles(orDefault(opts.CanChooseFiles, !opts.CanChooseDirectories)).
		ShowHiddenFiles(opts.ShowHiddenFiles)
}

func applySaveFileOptions(d *application.SaveFileDialogStruct, opts SaveFileOptions) {
	if opts.Title != "" {
		d.SetMessage(opts.Title)
	}
	if opts.Directory != "" {
		d.SetDirectory(opts.Directory)
	}
	for _, f := range opts.Filters {
		d.AddFilter(f.DisplayName, f.Pattern)
	}
	d.ShowHiddenFiles(opts.ShowHiddenFiles)
}

// orDefault returns explicit when the consumer set CanChooseFiles
// explicitly true, otherwise falls back to whatever default makes sense
// — when the dialog is for a directory picker, we want CanChooseFiles=false;
// when it's a file picker (the common case), CanChooseFiles=true.
func orDefault(explicit, fallback bool) bool {
	if explicit {
		return true
	}
	return fallback
}

// dialogForType picks the correct DialogManager builder for the gui's
// DialogType enum. Wails distinguishes between Info/Question/Warning/Error
// at the constructor level rather than via an option, so the mapping is
// 1:1.
func dialogForType(app *application.App, t DialogType) *application.MessageDialog {
	switch t {
	case DialogWarning:
		return app.Dialog.Warning()
	case DialogError:
		return app.Dialog.Error()
	case DialogQuestion:
		return app.Dialog.Question()
	}
	return app.Dialog.Info()
}
