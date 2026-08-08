// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"reflect"
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
