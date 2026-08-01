//go:build !js

// SPDX-Licence-Identifier: EUPL-1.2

package ctmltest

import (
	"sort"
	"strconv"
	"testing"

	core "dappco.re/go"
	coreio "dappco.re/go/io"
	ctml "dappco.re/go/render/engine/ctml"
	html "dappco.re/go/render/engine/html"
)

// Run parses and runs every _test.ctml tape matching glob, each as its own
// subtest named after the tape's filename. glob is resolved like
// filepath.Glob -- relative to the test binary's working directory, which
// under `go test` is the package directory, so `Run(t, "testdata/*_test.ctml")`
// from a package's own _test.go file finds that package's own tapes.
//
// Usage example: func TestCTML(t *testing.T) { ctmltest.Run(t, "testdata/*_test.ctml") }
func Run(t *testing.T, glob string) {
	t.Helper()

	paths, err := matchTapes(glob)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, path := range paths {
		t.Run(core.PathBase(path), func(t *testing.T) {
			RunFile(t, path)
		})
	}
}

// matchTapes resolves glob to a sorted, non-empty list of tape paths, or an
// error naming the glob -- a glob matching nothing is a defect (a typo'd
// pattern, a moved testdata directory), not a silent no-op that would
// otherwise report a suspiciously-green "no tests ran" pass. Sorting makes
// subtest order deterministic regardless of directory enumeration order.
func matchTapes(glob string) ([]string, error) {
	paths := core.PathGlob(glob)
	if len(paths) == 0 {
		return nil, core.E("ctmltest.Run", "no tapes matched "+glob, nil)
	}
	sort.Strings(paths)
	return paths, nil
}

// RunFile parses and runs one _test.ctml tape: prepareRun performs every
// fallible step up to and including the tape's INITIAL render (read the
// tape, parseTape it, buildConfig the Source/Set/Data/Rows commands that
// precede the tape's first render-reading command -- Expect, Golden,
// Click, Snapshot, or Image, see isRenderRead -- into a ctml.Bindings and a
// html.TermOptions, read the Source .ctml, and renderCTML it once); any
// failure there fails the whole tape (t.Fatalf) since there is nothing left
// to assert.
//
// RunFile then walks every remaining command in tape order, threading a
// driveState seeded from that initial render. Expect/Golden/Click/Snapshot/
// Image assert against (or, for Snapshot/Image, capture) the CURRENT frame/
// boxes (t.Errorf on a mismatch, so one failing assertion does not hide the
// next). A Data command reached from this
// point on is a re-drive trigger, not initial config: it merges into the
// running values and re-renders (driveState.redrive), replacing the
// current frame/boxes, so every assertion after it in the tape sees the
// NEW render -- a tape can walk a .ctml through more than one data state
// without a second tape. A re-drive's own render failure fails the whole
// tape (t.Fatalf), matching prepareRun's own render-failure handling,
// though see redrive's doc comment for why that path is effectively
// unreachable from a Data change alone.
//
// Usage example: ctmltest.RunFile(t, "testdata/sample_test.ctml")
func RunFile(t *testing.T, tapePath string) {
	t.Helper()

	result, err := prepareRun(tapePath)
	if err != nil {
		t.Fatalf("%v", err)
	}

	drive := newDriveState(tapePath, result)
	seenAssertion := false
	for _, cmd := range result.cmds {
		switch cmd.Verb {
		case "Data":
			if seenAssertion {
				if err := drive.redrive(cmd); err != nil {
					t.Fatalf("%v", err)
				}
			}
		case "Expect":
			seenAssertion = true
			runExpect(t, tapePath, cmd, drive.frame, drive.boxes, result.fitWidth)
		case "Click":
			seenAssertion = true
			runClick(t, tapePath, cmd, drive.frame, drive.boxes)
		case "Golden":
			seenAssertion = true
			runGolden(t, tapePath, result.tapeDir, cmd, drive.frame)
		case "Snapshot":
			seenAssertion = true
			runSnapshot(t, tapePath, result.tapeDir, cmd, drive.frame, result.fitWidth)
		case "Image":
			seenAssertion = true
			runImage(t, tapePath, result.tapeDir, cmd, drive.frame, result.fitWidth)
		}
	}
}

// renderResult is prepareRun's output: everything RunFile's command loop
// needs to run the rest of the tape, plus tapeDir (Golden's paths, like
// Source's, resolve relative to the tape's own directory) and cmds itself,
// since prepareRun is also where the tape gets parsed. cfg and ctmlSrc
// carry the initial render's resolved inputs forward -- see driveState --
// so a Data re-drive later in the tape can re-Parse without re-reading the
// Source file or re-walking the tape's leading commands.
type renderResult struct {
	cmds     []command
	tapeDir  string
	frame    string
	boxes    html.BoxMap
	fitWidth int

	cfg     tapeConfig
	ctmlSrc []byte
}

// prepareRun performs every fallible step of RunFile up to and including
// the tape's INITIAL render, returning a plain error instead of calling
// t.Fatalf so it is unit-testable without a real *testing.T (see
// runExpect's doc comment for why that matters: a real (sub)test that is
// made to fail always propagates the failure to its ancestors and the
// process exit code). The initial render's config folds only the
// Source/Set/Data/Rows commands BEFORE the tape's first render-reading
// command (cmdsBeforeFirstAssertion) -- a Data line at or after that point
// is not initial config, it is a re-drive trigger RunFile's own walk
// applies later (see driveState.redrive).
func prepareRun(tapePath string) (renderResult, error) {
	raw, err := coreio.Local.Read(tapePath)
	if err != nil {
		return renderResult{}, core.E("ctmltest.RunFile", "reading tape "+tapePath, err)
	}
	cmds, err := parseTape([]byte(raw))
	if err != nil {
		return renderResult{}, err
	}

	tapeDir := core.PathDir(tapePath)
	cfg := buildConfig(tapeDir, cmdsBeforeFirstAssertion(cmds))
	if cfg.sourcePath == "" {
		return renderResult{}, core.E("ctmltest.RunFile", tapePath+": missing Source verb", nil)
	}

	ctmlSrc, err := coreio.Local.Read(cfg.sourcePath)
	if err != nil {
		return renderResult{}, core.E("ctmltest.RunFile", sourceRef(tapePath, cfg)+": reading Source "+cfg.sourcePath, err)
	}

	frame, boxes, err := renderCTML(sourceRef(tapePath, cfg), cfg.sourcePath, []byte(ctmlSrc), cfg.width, cfg.values, cfg.sequences)
	if err != nil {
		return renderResult{}, err
	}

	fitWidth := cfg.width
	if fitWidth <= 0 {
		fitWidth = defaultTermWidth
	}

	return renderResult{
		cmds: cmds, tapeDir: tapeDir, frame: frame, boxes: boxes, fitWidth: fitWidth,
		cfg: cfg, ctmlSrc: []byte(ctmlSrc),
	}, nil
}

// renderCTML parses ctmlSrc against the given Bindings -- resolved at
// PARSE time, not from a live reference read later at render time (see
// dappco.re/go/render/ctml's package doc) -- and RenderTermBoxes the result
// once. Shared by prepareRun's initial render and driveState.redrive's
// mid-tape re-render: the same two steps (parse, render) and the same
// failure shape (naming tapeRef), from two call sites.
func renderCTML(tapeRef, sourcePath string, ctmlSrc []byte, width int, values map[string]any, sequences map[string][]map[string]any) (frame string, boxes html.BoxMap, err error) {
	node, err := ctml.Parse(ctmlSrc, ctml.Bindings{Values: values, Sequences: sequences})
	if err != nil {
		return "", nil, core.E("ctmltest.RunFile", tapeRef+": parsing Source "+sourcePath, err)
	}
	frame, boxes = html.RenderTermBoxes(node, html.NewContext(), html.TermOptions{Width: width})
	return frame, boxes, nil
}

func sourceRef(tapePath string, cfg tapeConfig) string {
	return tapePath + ":" + strconv.Itoa(cfg.sourceLine)
}
