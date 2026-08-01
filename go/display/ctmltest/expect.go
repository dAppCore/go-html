//go:build !js

// SPDX-Licence-Identifier: EUPL-1.2

package ctmltest

import (
	"flag"
	"slices"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	core "dappco.re/go"
	coreio "dappco.re/go/io"
	html "dappco.re/go/render/engine/html"
	"github.com/charmbracelet/x/ansi"
)

// update, when set (`go test ./... -update`), makes every Golden command in
// the run regenerate its file instead of comparing against it -- the
// standard Go golden-file convention. It is a package-level var (not
// confined to a _test.go file) because runGolden is reached from RunFile,
// an ordinary exported function any consuming package's own tests call.
var update = flag.Bool("update", false, "update ctmltest Golden files instead of comparing against them")

// defaultTermWidth mirrors html's own unexported terminal-render default
// (100 columns, see html.RenderTerm) so Expect Fits checks against the
// same effective width the renderer just used even when a tape never sets
// one with `Set Width`.
const defaultTermWidth = 100

// runExpect dispatches one Expect command and reports a failure through t.
// The decision (and the message) lives in evalExpect, kept deliberately
// free of *testing.T: a *testing.T that is made to fail always propagates
// that failure to every ancestor test and the process exit code (there is
// no way to observe a deliberate failure via a real (sub)test without
// poisoning the run), so the matching/formatting logic is tested directly
// via evalExpect instead -- this wrapper is thin enough to not need its
// own test.
func runExpect(t *testing.T, tapePath string, cmd command, frame string, boxes html.BoxMap, fitWidth int) {
	t.Helper()
	if ok, msg := evalExpect(tapePath, cmd, frame, boxes, fitWidth); !ok {
		t.Error(msg)
	}
}

// evalExpect evaluates one Expect command against a render, returning
// (true, "") on a match or (false, message) on a mismatch -- message is
// exactly what runExpect reports, naming the tape "file:line" and showing
// the offending frame.
func evalExpect(tapePath string, cmd command, frame string, boxes html.BoxMap, fitWidth int) (ok bool, msg string) {
	var detail string
	switch cmd.Args[0] {
	case "Text":
		ok, detail = matchText(frame, cmd.Args[1])
	case "NotText":
		ok, detail = matchNotText(frame, cmd.Args[1])
	case "Line":
		n, _ := strconv.Atoi(cmd.Args[1]) // parseTape's validateExpectLine guarantees this parses
		ok, detail = matchLine(frame, n, cmd.Args[2])
	case "Width":
		n, _ := strconv.Atoi(cmd.Args[1]) // parseTape's validateExpectWidth guarantees this parses
		ok, detail = matchWidth(frame, n)
	case "Box":
		ok, detail = matchBox(boxes, cmd.Args[1])
	case "Fits":
		ok, detail = matchFits(frame, fitWidth)
	default:
		// Unreachable: parseTape's validateExpect already rejects any other
		// kind before RunFile ever sees this command.
		detail = "internal: unrecognised Expect kind " + strconv.Quote(cmd.Args[0])
	}
	if ok {
		return true, ""
	}
	return false, tapePath + ":" + strconv.Itoa(cmd.Line) + ": " + detail + "\nframe:\n" + frame
}

// matchText reports whether frame contains substr -- Expect Text.
func matchText(frame, substr string) (ok bool, detail string) {
	if strings.Contains(ansi.Strip(frame), substr) {
		return true, ""
	}
	return false, "Expect Text " + strconv.Quote(substr) + ": not found in the rendered frame"
}

// matchNotText reports whether frame does NOT contain substr -- Expect
// NotText, the negation of Expect Text (see matchText). It proves an
// element is genuinely absent from the render (a conditional branch that
// should not have fired, text a Data re-drive should have replaced), which
// asserting on what IS present cannot: a frame can contain plenty of other
// text while still containing the one substring it must not.
func matchNotText(frame, substr string) (ok bool, detail string) {
	if !strings.Contains(ansi.Strip(frame), substr) {
		return true, ""
	}
	return false, "Expect NotText " + strconv.Quote(substr) + ": found in the rendered frame"
}

// matchLine reports whether frame's line n (0-indexed), trimmed of
// trailing space, equals want exactly -- Expect Line. Trailing space is
// trimmed because the terminal renderer right-pads block content to its
// box width (see term.go); a line number at or past the end of the frame
// fails and says how many lines the frame actually has, rather than
// panicking or silently comparing against "".
func matchLine(frame string, n int, want string) (ok bool, detail string) {
	lines := strings.Split(ansi.Strip(frame), "\n")
	if n < 0 || n >= len(lines) {
		return false, "Expect Line " + strconv.Itoa(n) + ": frame has only " + strconv.Itoa(len(lines)) + " line(s)"
	}
	got := strings.TrimRight(lines[n], " ")
	if got == want {
		return true, ""
	}
	return false, "Expect Line " + strconv.Itoa(n) + ": got: " + strconv.Quote(got) + ", want: " + strconv.Quote(want)
}

// matchWidth reports whether frame's display width -- its widest line,
// measured the same ANSI- and wide-rune-aware way Expect Fits measures
// (lipgloss.Width, see frameWidth) -- equals want exactly. Where Expect
// Fits is a ceiling (every line AT OR UNDER a budget), Expect Width pins
// the exact figure, e.g. proving a Set Width did what it said.
func matchWidth(frame string, want int) (ok bool, detail string) {
	got := frameWidth(frame)
	if got == want {
		return true, ""
	}
	return false, "Expect Width " + strconv.Itoa(want) + ": frame is " + strconv.Itoa(got) + " cell(s) wide"
}

// frameWidth returns the widest line in frame, in display cells
// (charm.land/lipgloss/v2.Width) -- the same per-line measure
// matchFits checks against a budget and matchWidth checks against an exact
// figure.
func frameWidth(frame string) int {
	width := 0
	for line := range strings.SplitSeq(frame, "\n") {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width
}

// hitBox reports whether id names a recorded, non-empty rectangle in boxes
// -- present, with positive width and height. The shared hit-test Expect
// Box (matchBox) and Click (evalClick, click.go) both make: a box that is
// merely present but zero-area (recorded, but nothing there to see or
// click) fails exactly like an absent one. availableIDs lists every id
// actually recorded, sorted, so a caller's failure message can name what
// WAS there without re-running with -v.
func hitBox(boxes html.BoxMap, id string) (ok bool, availableIDs string) {
	box, present := boxes[id]
	if present && box.Width > 0 && box.Height > 0 {
		return true, ""
	}
	ids := make([]string, 0, len(boxes))
	for k := range boxes {
		ids = append(ids, k)
	}
	slices.Sort(ids)
	return false, strings.Join(ids, ", ")
}

// matchBox reports whether id names a recorded, non-empty rectangle in
// boxes -- Expect Box. The failure detail lists every id actually
// recorded, sorted, so a typo'd id is obvious without re-running with -v.
func matchBox(boxes html.BoxMap, id string) (ok bool, detail string) {
	hit, have := hitBox(boxes, id)
	if hit {
		return true, ""
	}
	return false, "Expect Box " + strconv.Quote(id) + ": not recorded (have: " + have + ")"
}

// matchFits reports whether every line of frame fits within width display
// cells (charm.land/lipgloss/v2.Width -- ANSI- and wide-rune-
// aware, the same measure the renderer itself wraps against) -- Expect
// Fits. The failure detail names the first offending line.
func matchFits(frame string, width int) (ok bool, detail string) {
	for i, line := range strings.Split(frame, "\n") {
		if w := lipgloss.Width(line); w > width {
			return false, "Expect Fits: line " + strconv.Itoa(i+1) + " is " + strconv.Itoa(w) +
				" cells wide, exceeds Width " + strconv.Itoa(width)
		}
	}
	return true, ""
}

// runGolden implements the Golden command: under -update, write frame to
// the golden file (writeGolden, real I/O -- a failure here is t.Fatalf,
// there being no sensible "continue" from a golden write that failed);
// otherwise compare against it (evalGolden) and t.Error on a mismatch or a
// missing file. See runExpect's doc comment for why the decision logic
// (evalGolden) is kept free of *testing.T while this wrapper is not
// separately tested.
func runGolden(t *testing.T, tapePath, tapeDir string, cmd command, frame string) {
	t.Helper()
	if *update {
		if err := writeGolden(tapeDir, cmd, frame); err != nil {
			t.Fatalf("%s:%d: %v", tapePath, cmd.Line, err)
		}
		return
	}
	if ok, msg := evalGolden(tapePath, tapeDir, cmd, frame); !ok {
		t.Error(msg)
	}
}

// writeGolden creates cmd's golden file's directory if needed and writes
// frame to it with a trailing newline (evalGolden trims exactly one
// trailing newline back off on read, so the round trip is exact).
func writeGolden(tapeDir string, cmd command, frame string) error {
	goldenPath := core.PathJoin(tapeDir, cmd.Args[0])
	if dir := core.PathDir(goldenPath); dir != "." {
		if err := coreio.Local.EnsureDir(dir); err != nil {
			return core.E("ctmltest.writeGolden", "creating golden directory "+dir, err)
		}
	}
	if err := coreio.Local.Write(goldenPath, frame+"\n"); err != nil {
		return core.E("ctmltest.writeGolden", "writing golden "+goldenPath, err)
	}
	return nil
}

// evalGolden compares frame to cmd's golden file's content (trailing
// newline ignored), returning (true, "") on a match or (false, message) on
// a mismatch or an unreadable golden file -- message is exactly what
// runGolden reports.
func evalGolden(tapePath, tapeDir string, cmd command, frame string) (ok bool, msg string) {
	goldenPath := core.PathJoin(tapeDir, cmd.Args[0])
	line := strconv.Itoa(cmd.Line)

	raw, err := coreio.Local.Read(goldenPath)
	if err != nil {
		return false, tapePath + ":" + line + ": reading golden " + goldenPath + ": " + err.Error() +
			" (run `go test -update` to create it)"
	}
	want := strings.TrimSuffix(raw, "\n")
	if want == frame {
		return true, ""
	}
	return false, tapePath + ":" + line + ": golden mismatch against " + goldenPath + "\n" + diffLines(want, frame)
}

// diffLines renders a minimal line-by-line diff of want vs got for a
// Golden mismatch: every differing line index, with both sides quoted so
// leading/trailing whitespace differences are visible.
func diffLines(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	n := max(len(wantLines), len(gotLines))

	var b strings.Builder
	for i := range n {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w == g {
			continue
		}
		b.WriteString("  line " + strconv.Itoa(i+1) + ":\n    want: " + strconv.Quote(w) + "\n    got:  " + strconv.Quote(g) + "\n")
	}
	return b.String()
}
