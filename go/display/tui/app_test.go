// SPDX-Licence-Identifier: EUPL-1.2

package tui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	teatest "github.com/charmbracelet/x/exp/teatest/v2"

	tui "dappco.re/go/render/display/tui"
	ctml "dappco.re/go/render/engine/ctml"
	html "dappco.re/go/render/engine/html"
)

// parse compiles a .ctml source to a node, failing the test on a parse error.
func parse(t *testing.T, src string) html.Node {
	t.Helper()
	n, err := ctml.Parse([]byte(src))
	if err != nil {
		t.Fatalf("ctml.Parse(%q): %v", src, err)
	}
	return n
}

// TestNewApp proves NewApp constructs a live model whose Init requests the
// initial window size (so the first frame renders at the real width).
func TestNewApp(t *testing.T) {
	app := tui.NewApp(parse(t, `<p>hi</p>`))
	if app == nil {
		t.Fatal("NewApp returned nil")
	}
	if app.Init() == nil {
		t.Fatal("Init() returned nil, want the RequestWindowSize Cmd")
	}
}

// TestApp_Update_Resize proves a WindowSizeMsg is absorbed and the resulting
// View turns on the alternate screen + mouse cell-motion (the v2 View-level
// runtime shell the manager owns).
func TestApp_Update_Resize(t *testing.T) {
	app := tui.NewApp(parse(t, `<p>hello world</p>`))
	app.Update(tui.WindowSizeMsg{Width: 60, Height: 20})

	v := app.View()
	if !v.AltScreen {
		t.Error("View.AltScreen = false, want true (manager runs in the alt screen)")
	}
	if v.MouseMode != tui.MouseModeCellMotion {
		t.Errorf("View.MouseMode = %v, want MouseModeCellMotion", v.MouseMode)
	}
}

// TestApp_Update_QuitCtrlC proves ctrl+c returns the Quit Cmd.
func TestApp_Update_QuitCtrlC(t *testing.T) {
	app := tui.NewApp(parse(t, `<p>x</p>`))
	_, cmd := app.Update(tui.KeyPressMsg{Code: 'c', Mod: tui.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned a nil Cmd, want Quit")
	}
	if _, ok := cmd().(tui.QuitMsg); !ok {
		t.Fatalf("ctrl+c Cmd yielded %T, want QuitMsg", cmd())
	}
}

// TestApp_Update_QuitQ proves a bare q also quits (the second default idiom).
func TestApp_Update_QuitQ(t *testing.T) {
	app := tui.NewApp(parse(t, `<p>x</p>`))
	_, cmd := app.Update(tui.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil || func() bool { _, ok := cmd().(tui.QuitMsg); return !ok }() {
		t.Fatal("q should return the Quit Cmd")
	}
}

// TestWithQuitKeys proves the quit predicate is replaceable -- here ctrl+c only,
// so a bare q no longer quits.
func TestWithQuitKeys(t *testing.T) {
	ctrlCOnly := func(m tui.KeyPressMsg) bool { return m.Code == 'c' && m.Mod.Contains(tui.ModCtrl) }
	app := tui.NewApp(parse(t, `<p>x</p>`), tui.WithQuitKeys(ctrlCOnly))
	if _, cmd := app.Update(tui.KeyPressMsg{Code: 'q', Text: "q"}); cmd != nil {
		t.Error("q quit despite WithQuitKeys(ctrl+c only)")
	}
	if _, cmd := app.Update(tui.KeyPressMsg{Code: 'c', Mod: tui.ModCtrl}); cmd == nil {
		t.Error("ctrl+c did not quit under WithQuitKeys(ctrl+c only)")
	}
}

// TestApp_DrivenByProgram runs the manager under a real Bubble Tea program via
// teatest: the .ctml renders (the marker text reaches the screen) and a q
// keypress quits the program cleanly.
func TestApp_DrivenByProgram(t *testing.T) {
	app := tui.NewApp(parse(t, `<h1>Marker</h1>`))
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(40, 12))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(ansi.Strip(string(b)), "Marker")
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tui.KeyPressMsg{Code: 'q', Text: "q"})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}
