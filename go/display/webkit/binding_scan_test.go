// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// TestScanCallByName_Good covers the three literal quoting styles, the
// namespace-import and named-import call shapes, per-name file
// attribution, and de-duplication across call sites.
func TestScanCallByName_Good(t *testing.T) {
	fsys := fstest.MapFS{
		"app/runner.service.ts": &fstest.MapFile{Data: []byte(`
			import { Call } from '@wailsio/runtime';
			export const start = () => Call.ByName('pkg/runner.Service.Start');
			export const stop  = () => Call.ByName("pkg/runner.Service.Stop");
		`)},
		"app/status.component.ts": &fstest.MapFile{Data: []byte(
			"import { ByName } from '@wailsio/runtime';\n" +
				"const poll = () => ByName(`pkg/runner.Service.Status`);\n" +
				"const again = () => ByName('pkg/runner.Service.Start');\n")},
	}

	sites, err := ScanCallByName(fsys)
	if err != nil {
		t.Fatalf("ScanCallByName: %v", err)
	}

	wantNames := []string{
		"pkg/runner.Service.Start",
		"pkg/runner.Service.Status",
		"pkg/runner.Service.Stop",
	}
	if !reflect.DeepEqual(sites.Names, wantNames) {
		t.Fatalf("Names = %v, want %v", sites.Names, wantNames)
	}

	wantFiles := []string{"app/runner.service.ts", "app/status.component.ts"}
	if got := sites.Files["pkg/runner.Service.Start"]; !reflect.DeepEqual(got, wantFiles) {
		t.Fatalf("Files[Start] = %v, want %v", got, wantFiles)
	}
	if len(sites.Dynamic) != 0 {
		t.Fatalf("Dynamic = %v, want none", sites.Dynamic)
	}
}

// TestScanCallByName_Bad asserts the scanner does not invent names from
// files it has no business reading — vendored runtime code, build
// output, and non-source extensions — and rejects a nil filesystem
// rather than reporting a clean scan of nothing.
func TestScanCallByName_Bad(t *testing.T) {
	if _, err := ScanCallByName(nil); err == nil {
		t.Fatal("nil filesystem should error, not report a clean scan")
	}

	fsys := fstest.MapFS{
		"node_modules/@wailsio/runtime/calls.js": &fstest.MapFile{
			Data: []byte(`export function ByName(methodName) { return Call({ methodName }); }`)},
		"dist/main.js":  &fstest.MapFile{Data: []byte(`Call.ByName('pkg.Built.Bundled')`)},
		"README.md":     &fstest.MapFile{Data: []byte(`Call.ByName('pkg.Doc.Example')`)},
		"src/notes.txt": &fstest.MapFile{Data: []byte(`Call.ByName('pkg.Note.Ignored')`)},
	}
	sites, err := ScanCallByName(fsys)
	if err != nil {
		t.Fatalf("ScanCallByName: %v", err)
	}
	if len(sites.Names) != 0 {
		t.Fatalf("Names = %v, want none — skipped trees and non-source files must not contribute", sites.Names)
	}
}

// TestScanCallByName_Ugly pins the blind spot honestly: a call assembled
// at runtime cannot be verified statically, so it must be reported as
// Dynamic rather than dropped (which would let the gate claim full
// coverage it does not have).
func TestScanCallByName_Ugly(t *testing.T) {
	fsys := fstest.MapFS{
		"app/dynamic.service.ts": &fstest.MapFile{Data: []byte(`
			const PREFIX = 'pkg/runner.Service.';
			export const invoke = (m: string) => Call.ByName(PREFIX + m);
			export const known  = () => Call.ByName('pkg/runner.Service.Start');
		`)},
	}

	sites, err := ScanCallByName(fsys)
	if err != nil {
		t.Fatalf("ScanCallByName: %v", err)
	}
	if want := []string{"pkg/runner.Service.Start"}; !reflect.DeepEqual(sites.Names, want) {
		t.Fatalf("Names = %v, want %v", sites.Names, want)
	}
	if want := []string{"app/dynamic.service.ts"}; !reflect.DeepEqual(sites.Dynamic, want) {
		t.Fatalf("Dynamic = %v, want %v — an unverifiable call must be reported", sites.Dynamic, want)
	}
}

// TestScanCallByName_Drift is the gate itself, end to end: frontend
// literals scanned out of source, resolved against the Go services that
// back them, with the stale one named.
func TestScanCallByName_Drift(t *testing.T) {
	fsys := fstest.MapFS{
		"app/probe.service.ts": &fstest.MapFile{Data: []byte(
			`Call.ByName('` + bindingPkg + `.nameProbeService.Start');` +
				`Call.ByName('` + bindingPkg + `.nameProbeService.Renamed');`)},
	}

	sites, err := ScanCallByName(fsys)
	if err != nil {
		t.Fatalf("ScanCallByName: %v", err)
	}
	missing := UnresolvedBindingNames(sites.Names, &nameProbeService{})
	want := []string{bindingPkg + ".nameProbeService.Renamed"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("UnresolvedBindingNames = %v, want %v", missing, want)
	}
	if got := sites.Files[want[0]]; len(got) != 1 || got[0] != "app/probe.service.ts" {
		t.Fatalf("Files for the stale call = %v, want [app/probe.service.ts]", got)
	}
}

// TestScanCallByName_InterpolatedTemplate pins the trap the in-tree
// Angular example walked straight into: factoring the package prefix
// into a constant and calling Call.ByName(`${PKG}.Type.Method`) is the
// natural way to write these, and the source text is NOT the binding
// name. Emitting it would invent a name no service exposes and fail the
// gate on a perfectly correct call, so it must be reported as
// unverifiable instead.
func TestScanCallByName_InterpolatedTemplate(t *testing.T) {
	fsys := fstest.MapFS{
		"app/wails.service.ts": &fstest.MapFile{Data: []byte(
			"const PKG = 'pkg/runner';\n" +
				"const echo = () => Call.ByName(`${PKG}.Service.Echo`);\n")},
	}

	sites, err := ScanCallByName(fsys)
	if err != nil {
		t.Fatalf("ScanCallByName: %v", err)
	}
	for _, name := range sites.Names {
		if strings.Contains(name, "${") {
			t.Fatalf("Names contains raw source text %q — that is not a binding name", name)
		}
	}
	if len(sites.Names) != 0 {
		t.Fatalf("Names = %v, want none — an interpolated call cannot be verified", sites.Names)
	}
	if want := []string{"app/wails.service.ts"}; !reflect.DeepEqual(sites.Dynamic, want) {
		t.Fatalf("Dynamic = %v, want %v", sites.Dynamic, want)
	}
}

// TestScanCallByName_CommentsAreNotCalls pins the trap the in-tree
// example hit second: documenting a binding must not register a call to
// it. Before comment stripping, the doc comment on the example's own
// wails.service.ts made the drift gate fail on prose.
func TestScanCallByName_CommentsAreNotCalls(t *testing.T) {
	fsys := fstest.MapFS{
		"app/documented.service.ts": &fstest.MapFile{Data: []byte(
			"/**\n" +
				" * Prefer Call.ByName('pkg.Ghost.FromBlockComment') over the old API.\n" +
				" * The interpolated form Call.ByName(`${PKG}.T.M`) cannot be checked.\n" +
				" */\n" +
				"// Call.ByName('pkg.Ghost.FromLineComment') was removed in v2.\n" +
				"export const real = () => Call.ByName('pkg.Runner.Start');\n")},
	}

	sites, err := ScanCallByName(fsys)
	if err != nil {
		t.Fatalf("ScanCallByName: %v", err)
	}
	if want := []string{"pkg.Runner.Start"}; !reflect.DeepEqual(sites.Names, want) {
		t.Fatalf("Names = %v, want %v — only the real call counts", sites.Names, want)
	}
	if len(sites.Dynamic) != 0 {
		t.Fatalf("Dynamic = %v, want none — an interpolated call in a COMMENT is not a blind spot", sites.Dynamic)
	}
}

// TestStripComments_Good asserts a // sequence inside a string literal
// survives, so a URL in a call argument is not mistaken for a comment
// and silently truncated.
func TestStripComments_Good(t *testing.T) {
	source := `const url = "https://example.test/a"; // trailing note
const tick = 'a//b';
/* block */ const kept = 1;`

	stripped := stripComments(source)

	for _, keep := range []string{`"https://example.test/a"`, `'a//b'`, "const kept = 1;"} {
		if !strings.Contains(stripped, keep) {
			t.Errorf("stripped source lost %q:\n%s", keep, stripped)
		}
	}
	for _, gone := range []string{"trailing note", "block"} {
		if strings.Contains(stripped, gone) {
			t.Errorf("stripped source still carries comment text %q:\n%s", gone, stripped)
		}
	}
	if got, want := strings.Count(stripped, "\n"), strings.Count(source, "\n"); got != want {
		t.Fatalf("newline count = %d, want %d — offsets must be preserved", got, want)
	}
	if len(stripped) != len(source) {
		t.Fatalf("length = %d, want %d — stripping must preserve byte offsets", len(stripped), len(source))
	}
}
