// SPDX-Licence-Identifier: EUPL-1.2

package filepicker_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/teatest/v2"

	core "dappco.re/go"
	coreio "dappco.re/go/io"
	tea "dappco.re/go/render/display/tui"
	"dappco.re/go/render/display/tui/filepicker"
	"dappco.re/go/render/display/tui/style"
)

// newFixtureDir populates t.TempDir() with a deterministic tree — one
// subdirectory ("sub") and two files ("alpha.txt", "bravo.txt") — so
// readDir's sort (directories before files, each group alphabetical) always
// yields the same three-entry listing in the same order: sub, alpha.txt,
// bravo.txt.
func newFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if err := coreio.Local.EnsureDir(core.Path(dir, "sub")); err != nil {
		t.Fatalf("EnsureDir(sub): %v", err)
	}
	if err := coreio.Local.Write(core.Path(dir, "alpha.txt"), "alpha"); err != nil {
		t.Fatalf("Write(alpha.txt): %v", err)
	}
	if err := coreio.Local.Write(core.Path(dir, "bravo.txt"), "bravo"); err != nil {
		t.Fatalf("Write(bravo.txt): %v", err)
	}
	return dir
}

// TestModel proves Model is a genuine alias: a composite literal built from
// outside the package round-trips its exported fields unchanged.
func TestModel(t *testing.T) {
	m := filepicker.Model{
		CurrentDirectory: "/tmp",
		Cursor:           ">",
		AllowedTypes:     []string{".go"},
		DirAllowed:       true,
		FileAllowed:      false,
	}

	if m.CurrentDirectory != "/tmp" {
		t.Fatalf("CurrentDirectory = %q, want %q", m.CurrentDirectory, "/tmp")
	}
	if !m.DirAllowed || m.FileAllowed {
		t.Fatalf("DirAllowed=%v FileAllowed=%v, want true/false", m.DirAllowed, m.FileAllowed)
	}
	if got := m.AllowedTypes; len(got) != 1 || got[0] != ".go" {
		t.Fatalf("AllowedTypes = %v, want [.go]", got)
	}
}

// TestNew constructs a bare picker and confirms the documented defaults New
// wires before any field is touched: rooted at ".", files selectable,
// directories not, auto-sizing on, and a live DefaultKeyMap already
// installed.
func TestNew(t *testing.T) {
	m := filepicker.New()

	if m.CurrentDirectory != "." {
		t.Fatalf("CurrentDirectory = %q, want %q", m.CurrentDirectory, ".")
	}
	if m.Cursor != ">" {
		t.Fatalf("Cursor = %q, want %q", m.Cursor, ">")
	}
	if !m.FileAllowed || m.DirAllowed {
		t.Fatalf("FileAllowed=%v DirAllowed=%v, want true/false (files selectable, directories not, by default)", m.FileAllowed, m.DirAllowed)
	}
	if !m.AutoHeight {
		t.Fatal("AutoHeight = false, want true (New's default)")
	}
	if m.AllowedTypes == nil {
		t.Fatal("AllowedTypes = nil, want a non-nil empty slice")
	}
	if !m.KeyMap.Down.Enabled() {
		t.Fatal("KeyMap.Down.Enabled() = false, want true (New wires DefaultKeyMap)")
	}
}

// TestDefaultKeyMap proves the returned KeyMap carries live, enabled
// bindings — Down matches both the down arrow and the vi "j" key, and Select
// matches enter.
func TestDefaultKeyMap(t *testing.T) {
	km := filepicker.DefaultKeyMap()

	if !km.Down.Enabled() {
		t.Fatal("DefaultKeyMap().Down.Enabled() = false, want true")
	}
	if got := km.Down.Keys(); !slices.Contains(got, "down") || !slices.Contains(got, "j") {
		t.Fatalf("DefaultKeyMap().Down.Keys() = %v, want to contain %q and %q", got, "down", "j")
	}
	if got := km.Select.Keys(); !slices.Contains(got, "enter") {
		t.Fatalf("DefaultKeyMap().Select.Keys() = %v, want to contain %q", got, "enter")
	}
}

// TestDefaultStyles proves DefaultStyles is real: FileSize carries the
// package's fixed column width, so rendering through it measures wider than
// the bare text — a zero-value Styles would measure the same as the input.
func TestDefaultStyles(t *testing.T) {
	s := filepicker.DefaultStyles()

	rendered := s.FileSize.Render("1")
	if got, want := style.Measure(rendered), 7; got != want {
		t.Fatalf("DefaultStyles().FileSize.Render(%q) measured %d, want %d (the package's fixed file-size column width)", "1", got, want)
	}
}

// TestIsHidden proves the dotfile-prefix check on the platform running this
// suite (unix — go-html's CI and dev boxes are both unix): a leading dot
// hides an entry, a plain name does not.
func TestIsHidden(t *testing.T) {
	hidden, err := filepicker.IsHidden(".git")
	if err != nil {
		t.Fatalf("IsHidden(%q): unexpected error: %v", ".git", err)
	}
	if !hidden {
		t.Fatalf("IsHidden(%q) = false, want true", ".git")
	}

	visible, err := filepicker.IsHidden("main.go")
	if err != nil {
		t.Fatalf("IsHidden(%q): unexpected error: %v", "main.go", err)
	}
	if visible {
		t.Fatalf("IsHidden(%q) = true, want false", "main.go")
	}
}

// TestModel_Init proves Init returns a genuine read-dir Cmd: running it and
// feeding the result back into Update populates the listing. HighlightedPath
// reads m.files[m.selected] directly, unaffected by the minIdx/maxIdx scroll
// window, so it proves the population happened before any WindowSizeMsg has
// even arrived; a follow-up WindowSizeMsg (what actually grows the scroll
// window — SetHeight alone only ever shrinks it) then proves View renders
// every entry.
func TestModel_Init(t *testing.T) {
	dir := newFixtureDir(t)
	m := filepicker.New()
	m.CurrentDirectory = dir

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil Cmd, want the read-dir command")
	}

	updated, _ := m.Update(cmd())

	if got, want := updated.HighlightedPath(), core.Path(dir, "sub"); got != want {
		t.Fatalf("HighlightedPath() after Init's Cmd = %q, want %q (sub sorts first: directories before files)", got, want)
	}

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	view := updated.View()
	for _, want := range []string{"sub", "alpha.txt", "bravo.txt"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() after a WindowSizeMsg = %q, want it to contain %q", view, want)
		}
	}
}

// TestModel_DidSelectFile drives a real Select keypress through Update
// (which sets Path) and then confirms DidSelectFile reports it — the
// idiomatic order a consuming app's own Update loop follows: update first,
// then ask what happened with the same message.
func TestModel_DidSelectFile(t *testing.T) {
	dir := newFixtureDir(t)
	m := filepicker.New()
	m.CurrentDirectory = dir

	updated, _ := m.Update(m.Init()())
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // sub -> alpha.txt

	selectMsg := tea.KeyPressMsg{Code: tea.KeyEnter}
	updated, _ = updated.Update(selectMsg)

	didSelect, path := updated.DidSelectFile(selectMsg)
	if !didSelect {
		t.Fatal("DidSelectFile() = false, want true after Select on a FileAllowed entry")
	}
	if want := core.Path(dir, "alpha.txt"); path != want {
		t.Fatalf("DidSelectFile() path = %q, want %q", path, want)
	}
}

// TestModel_DidSelectDisabledFile proves the AllowedTypes gate: a file whose
// extension is excluded reports through DidSelectDisabledFile instead of
// DidSelectFile.
func TestModel_DidSelectDisabledFile(t *testing.T) {
	dir := newFixtureDir(t)
	m := filepicker.New()
	m.CurrentDirectory = dir
	m.AllowedTypes = []string{".md"} // alpha.txt does not match

	updated, _ := m.Update(m.Init()())
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // sub -> alpha.txt

	selectMsg := tea.KeyPressMsg{Code: tea.KeyEnter}
	updated, _ = updated.Update(selectMsg)

	disabled, path := updated.DidSelectDisabledFile(selectMsg)
	if !disabled {
		t.Fatal("DidSelectDisabledFile() = false, want true (alpha.txt does not match AllowedTypes)")
	}
	if want := core.Path(dir, "alpha.txt"); path != want {
		t.Fatalf("DidSelectDisabledFile() path = %q, want %q", path, want)
	}

	if didSelect, _ := updated.DidSelectFile(selectMsg); didSelect {
		t.Fatal("DidSelectFile() = true, want false for a disabled (AllowedTypes-excluded) file")
	}
}

// harness wraps Model in the minimal tea.Model a consumer writes to drive
// it: Model.Init/Update return a concrete filepicker.Model, not the tea.Model
// interface, so the surrounding app supplies the outer Init/Update/View that
// satisfies tea.Model. Built entirely on the go-html tui seam (tea,
// filepicker) — no charmbracelet import.
type harness struct {
	filepicker.Model
}

func (h harness) Init() tea.Cmd { return h.Model.Init() }

func (h harness) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	h.Model, cmd = h.Model.Update(msg)
	return h, cmd
}

func (h harness) View() tea.View { return tea.NewView(h.Model.View()) }

// TestModel_DrivenByProgram drives a real bubbletea program via teatest:
// waiting for the initial listing to render proves Init's Cmd is a genuine
// read-dir command running under the actual runtime, and a down-then-enter
// key sequence proves real keyboard navigation moves the cursor and Select
// sets Path on the entry it lands on.
func TestModel_DrivenByProgram(t *testing.T) {
	dir := newFixtureDir(t)
	m := filepicker.New()
	m.CurrentDirectory = dir

	tm := teatest.NewTestModel(t, harness{Model: m})

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("alpha.txt")) &&
			bytes.Contains(bts, []byte("bravo.txt")) &&
			bytes.Contains(bts, []byte("sub"))
	})

	tm.Send(tea.KeyPressMsg{Code: tea.KeyDown})  // sub -> alpha.txt
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter}) // select alpha.txt

	tm.Quit()

	final, ok := tm.FinalModel(t).(harness)
	if !ok {
		t.Fatalf("FinalModel() = %T, want harness", tm.FinalModel(t))
	}
	if got, want := final.HighlightedPath(), core.Path(dir, "alpha.txt"); got != want {
		t.Fatalf("HighlightedPath() after one down press = %q, want %q", got, want)
	}
	if got, want := final.Path, core.Path(dir, "alpha.txt"); got != want {
		t.Fatalf("Path after Enter on alpha.txt = %q, want %q (Select set it)", got, want)
	}
}
