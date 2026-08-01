//go:build !js

// SPDX-Licence-Identifier: EUPL-1.2

package ctmltest

import (
	"strconv"
	"strings"
	"testing"

	coreio "dappco.re/go/io"
	html "dappco.re/go/render/engine/html"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchText(t *testing.T) {
	t.Run("good: the substring is present", func(t *testing.T) {
		ok, detail := matchText("hello world", "world")
		assert.True(t, ok)
		assert.Empty(t, detail)
	})

	t.Run("bad: the substring is absent", func(t *testing.T) {
		ok, detail := matchText("hello world", "goodbye")
		assert.False(t, ok)
		assert.Contains(t, detail, `"goodbye"`)
	})

	t.Run("ugly: an empty substring always matches", func(t *testing.T) {
		ok, _ := matchText("anything", "")
		assert.True(t, ok)
	})
}

func TestMatchNotText(t *testing.T) {
	t.Run("good: the substring is absent", func(t *testing.T) {
		ok, detail := matchNotText("hello world", "goodbye")
		assert.True(t, ok)
		assert.Empty(t, detail)
	})

	t.Run("bad: the substring is present", func(t *testing.T) {
		ok, detail := matchNotText("hello world", "world")
		assert.False(t, ok)
		assert.Contains(t, detail, `"world"`)
	})

	t.Run("ugly: an empty substring is always \"present\", so NotText \"\" always fails", func(t *testing.T) {
		ok, _ := matchNotText("anything", "")
		assert.False(t, ok)
	})
}

func TestMatchLine(t *testing.T) {
	frame := "Welcome   \nBuild 1.0\nlast"

	t.Run("good: line 0 matches after trailing space is trimmed", func(t *testing.T) {
		ok, detail := matchLine(frame, 0, "Welcome")
		assert.True(t, ok)
		assert.Empty(t, detail)
	})

	t.Run("good: a middle line matches exactly", func(t *testing.T) {
		ok, _ := matchLine(frame, 1, "Build 1.0")
		assert.True(t, ok)
	})

	t.Run("bad: a mismatched line reports got and want", func(t *testing.T) {
		ok, detail := matchLine(frame, 1, "Build 2.0")
		assert.False(t, ok)
		assert.Contains(t, detail, `got: "Build 1.0"`)
		assert.Contains(t, detail, `want: "Build 2.0"`)
	})

	t.Run("bad: a line number past the end of the frame fails and says how many lines there are", func(t *testing.T) {
		ok, detail := matchLine(frame, 9, "anything")
		assert.False(t, ok)
		assert.Contains(t, detail, "only 3 line(s)")
	})

	t.Run("ugly: a negative line number fails the same way as one past the end", func(t *testing.T) {
		ok, detail := matchLine(frame, -1, "anything")
		assert.False(t, ok)
		assert.Contains(t, detail, "Expect Line -1")
	})

	t.Run("ugly: only trailing space is trimmed, not leading space", func(t *testing.T) {
		ok, _ := matchLine("  indented   ", 0, "  indented")
		assert.True(t, ok)
	})
}

func TestMatchWidth(t *testing.T) {
	t.Run("good: the widest line matches want exactly", func(t *testing.T) {
		ok, detail := matchWidth("short\nlonger line", 11)
		assert.True(t, ok)
		assert.Empty(t, detail)
	})

	t.Run("bad: a mismatch reports the actual width", func(t *testing.T) {
		ok, detail := matchWidth("1234567890", 5)
		assert.False(t, ok)
		assert.Contains(t, detail, "Expect Width 5")
		assert.Contains(t, detail, "is 10 cell(s) wide")
	})

	t.Run("ugly: a frame narrower than want fails too -- Width pins exactly, Fits only ceils", func(t *testing.T) {
		ok, _ := matchWidth("12345", 10)
		assert.False(t, ok)
	})
}

func TestFrameWidth(t *testing.T) {
	t.Run("good: returns the widest line's display width", func(t *testing.T) {
		assert.Equal(t, 11, frameWidth("short\nlonger line\nmid"))
	})

	t.Run("ugly: an empty frame has zero width", func(t *testing.T) {
		assert.Equal(t, 0, frameWidth(""))
	})
}

func TestHitBox(t *testing.T) {
	boxes := html.BoxMap{"banner": {Row: 0, Col: 0, Width: 10, Height: 1}}

	t.Run("good: a recorded, non-empty box hits", func(t *testing.T) {
		hit, have := hitBox(boxes, "banner")
		assert.True(t, hit)
		assert.Empty(t, have)
	})

	t.Run("bad: an id absent from the box map misses and lists what was recorded", func(t *testing.T) {
		hit, have := hitBox(boxes, "missing")
		assert.False(t, hit)
		assert.Contains(t, have, "banner")
	})

	t.Run("ugly: a recorded id with a zero-area rectangle misses like an absent one", func(t *testing.T) {
		zero := html.BoxMap{"empty": {Row: 0, Col: 0, Width: 0, Height: 0}}
		hit, _ := hitBox(zero, "empty")
		assert.False(t, hit)
	})
}

func TestMatchBox(t *testing.T) {
	boxes := html.BoxMap{"banner": {Row: 0, Col: 0, Width: 10, Height: 1}}

	t.Run("good: a recorded, non-empty box matches", func(t *testing.T) {
		ok, detail := matchBox(boxes, "banner")
		assert.True(t, ok)
		assert.Empty(t, detail)
	})

	t.Run("bad: an id absent from the box map fails and lists what was recorded", func(t *testing.T) {
		ok, detail := matchBox(boxes, "missing")
		assert.False(t, ok)
		assert.Contains(t, detail, `"missing"`)
		assert.Contains(t, detail, "banner")
	})

	t.Run("ugly: a recorded id with a zero-area rectangle fails like an absent one", func(t *testing.T) {
		zero := html.BoxMap{"empty": {Row: 0, Col: 0, Width: 0, Height: 0}}
		ok, _ := matchBox(zero, "empty")
		assert.False(t, ok)
	})
}

func TestMatchFits(t *testing.T) {
	t.Run("good: every line at or under width fits", func(t *testing.T) {
		ok, detail := matchFits("short\nlines", 10)
		assert.True(t, ok)
		assert.Empty(t, detail)
	})

	t.Run("bad: a line wider than width fails and names the line number", func(t *testing.T) {
		ok, detail := matchFits("ok\nthis line is far too wide for ten cells", 10)
		assert.False(t, ok)
		assert.Contains(t, detail, "line 2")
	})

	t.Run("ugly: a line exactly at width fits (boundary is inclusive)", func(t *testing.T) {
		ok, _ := matchFits("1234567890", 10)
		assert.True(t, ok)
	})
}

func TestDiffLines(t *testing.T) {
	t.Run("good: identical text produces an empty diff", func(t *testing.T) {
		assert.Empty(t, diffLines("a\nb", "a\nb"))
	})

	t.Run("bad: a differing line is reported with both sides", func(t *testing.T) {
		out := diffLines("a\nb\nc", "a\nX\nc")
		assert.Contains(t, out, "line 2")
		assert.Contains(t, out, `"b"`)
		assert.Contains(t, out, `"X"`)
		assert.NotContains(t, out, "line 1")
		assert.NotContains(t, out, "line 3")
	})

	t.Run("ugly: a length mismatch reports the missing side as empty", func(t *testing.T) {
		out := diffLines("a", "a\nb")
		assert.Contains(t, out, "line 2")
		assert.Contains(t, out, `want: ""`)
		assert.Contains(t, out, `got:  "b"`)
	})
}

// evalExpect/evalGolden are the pure decision layer runExpect/runGolden
// wrap in a t.Error call -- see runExpect's doc comment in expect.go for
// why they are tested here directly rather than by making a real
// *testing.T deliberately fail.

func TestEvalExpect(t *testing.T) {
	frame := "line one\nline two"
	boxes := html.BoxMap{"banner": {Row: 0, Col: 0, Width: 5, Height: 1}}

	t.Run("good: a matching kind returns ok with no message", func(t *testing.T) {
		ok, msg := evalExpect("tape.ctml", command{Args: []string{"Text", "line one"}, Line: 3}, frame, boxes, 80)
		assert.True(t, ok)
		assert.Empty(t, msg)
	})

	t.Run("bad: a failing Expect Text names the tape file:line and shows the frame", func(t *testing.T) {
		ok, msg := evalExpect("tape.ctml", command{Args: []string{"Text", "nope"}, Line: 3}, frame, boxes, 80)
		assert.False(t, ok)
		assert.Contains(t, msg, "tape.ctml:3:")
		assert.Contains(t, msg, "frame:")
		assert.Contains(t, msg, frame)
	})

	t.Run("bad: Expect Box dispatches to matchBox", func(t *testing.T) {
		ok, msg := evalExpect("tape.ctml", command{Args: []string{"Box", "nope"}, Line: 1}, frame, boxes, 80)
		assert.False(t, ok)
		assert.Contains(t, msg, "banner")
	})

	t.Run("good: Expect Fits dispatches to matchFits using fitWidth", func(t *testing.T) {
		ok, _ := evalExpect("tape.ctml", command{Args: []string{"Fits"}, Line: 1}, frame, boxes, 80)
		assert.True(t, ok)
	})

	t.Run("good: Expect NotText dispatches to matchNotText", func(t *testing.T) {
		ok, _ := evalExpect("tape.ctml", command{Args: []string{"NotText", "nope"}, Line: 1}, frame, boxes, 80)
		assert.True(t, ok)
	})

	t.Run("bad: Expect Line dispatches to matchLine using the parsed line number", func(t *testing.T) {
		ok, msg := evalExpect("tape.ctml", command{Args: []string{"Line", "1", "nope"}, Line: 4}, frame, boxes, 80)
		assert.False(t, ok)
		assert.Contains(t, msg, "tape.ctml:4:")
		assert.Contains(t, msg, `want: "nope"`)
	})

	t.Run("good: Expect Width dispatches to matchWidth using the parsed width", func(t *testing.T) {
		ok, _ := evalExpect("tape.ctml", command{Args: []string{"Width", strconv.Itoa(frameWidth(frame))}, Line: 1}, frame, boxes, 80)
		assert.True(t, ok)
	})
}

func TestEvalGolden(t *testing.T) {
	t.Run("bad: a missing golden file names the tape file:line and hints -update", func(t *testing.T) {
		cmd := command{Args: []string{"does-not-exist.golden"}, Line: 9}
		ok, msg := evalGolden("tape.ctml", "testdata/edge", cmd, "anything")
		assert.False(t, ok)
		assert.Contains(t, msg, "tape.ctml:9:")
		assert.Contains(t, msg, "-update")
	})

	t.Run("bad: a mismatch against the on-disk golden reports a diff", func(t *testing.T) {
		cmd := command{Args: []string{"golden_mismatch.golden"}, Line: 9}
		ok, msg := evalGolden("tape.ctml", "testdata/edge", cmd, "actual rendered frame")
		assert.False(t, ok)
		assert.Contains(t, msg, "golden mismatch")
		assert.Contains(t, msg, "DELIBERATELY WRONG")
	})

	t.Run("good: content matching the on-disk golden passes", func(t *testing.T) {
		cmd := command{Args: []string{"golden_mismatch.golden"}, Line: 9}
		raw, err := coreio.Local.Read("testdata/edge/golden_mismatch.golden")
		require.NoError(t, err)
		ok, msg := evalGolden("tape.ctml", "testdata/edge", cmd, strings.TrimSuffix(raw, "\n"))
		assert.True(t, ok)
		assert.Empty(t, msg)
	})
}

func TestWriteGolden(t *testing.T) {
	t.Run("good: writes the frame with a trailing newline, round-tripping through evalGolden", func(t *testing.T) {
		dir := t.TempDir()
		cmd := command{Args: []string{"out.golden"}, Line: 1}
		require.NoError(t, writeGolden(dir, cmd, "hello"))

		ok, _ := evalGolden("tape.ctml", dir, cmd, "hello")
		assert.True(t, ok)
	})

	t.Run("good: creates a missing golden directory", func(t *testing.T) {
		dir := t.TempDir() + "/nested"
		cmd := command{Args: []string{"out.golden"}, Line: 1}
		require.NoError(t, writeGolden(dir, cmd, "hello"))

		ok, _ := evalGolden("tape.ctml", dir, cmd, "hello")
		assert.True(t, ok)
	})
}
