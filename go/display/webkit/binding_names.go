// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"reflect"
	"sort"
	"strings"
)

// internalBindingMethods mirrors the exclusion set wails applies when it
// walks a bound service. These are lifecycle hooks the runtime consumes
// itself, so they never appear as callable bindings and must not appear
// in the names this package reports either.
//
// Kept in sync with application.internalServiceMethods (wails v3
// pkg/application/bindings.go). A drift here shows up as a name this
// package claims is callable but the runtime rejects — which is exactly
// the failure UnresolvedBindingNames exists to catch, so the test suite
// pins the set explicitly.
var internalBindingMethods = map[string]bool{
	"ServiceName":     true,
	"ServiceStartup":  true,
	"ServiceShutdown": true,
	"ServeHTTP":       true,
}

// BindingNames returns every fully-qualified name the renderer may pass
// to @wailsio/runtime's `Call.ByName` for a bound service, sorted.
//
// The name shape is the one wails computes by reflection:
//
//	<package path>.<receiver type name>.<method name>
//
// e.g. "dappco.re/lthn/desktop/pkg/runner.Service.Start".
//
// The receiver TYPE NAME is part of the wire contract. Renaming the Go
// struct — the `.Service` → `.WailsService` rename that already happened
// in the wild — silently invalidates every hardcoded call string in the
// frontend, because wails resolves Call.ByName through an exact-match
// map (application.Bindings.Get) with no aliasing on the name path.
// Aliases exist only for the numeric ID path (Call.ByID), so a renamed
// receiver cannot be papered over at runtime.
//
// Pass the same pointer given to Bind:
//
//	names := webkit.BindingNames(runnerSvc)
//
// Returns nil when instance is not a pointer to a named, non-generic
// struct — the same inputs wails itself rejects at bind time.
func BindingNames(instance any) []string {
	pkgPath, typeName, ptrType, ok := bindingReceiver(instance)
	if !ok {
		return nil
	}

	prefix := pkgPath + "." + typeName + "."
	names := make([]string, 0, ptrType.NumMethod())
	for i := range ptrType.NumMethod() {
		method := ptrType.Method(i)
		if internalBindingMethods[method.Name] {
			continue
		}
		names = append(names, prefix+method.Name)
	}
	sort.Strings(names)
	return names
}

// BindingName returns the fully-qualified Call.ByName string for one
// method on a bound service, and whether that method is actually
// callable from the renderer.
//
// The bool is false for a method that does not exist, is unexported, or
// is one of the wails lifecycle hooks (ServiceName / ServiceStartup /
// ServiceShutdown / ServeHTTP) — all three cases produce the same
// renderer-side symptom, a call that never resolves, so they collapse
// into one signal.
//
//	name, ok := webkit.BindingName(runnerSvc, "Start")
//	// name == "dappco.re/lthn/desktop/pkg/runner.Service.Start", ok == true
func BindingName(instance any, method string) (string, bool) {
	pkgPath, typeName, ptrType, ok := bindingReceiver(instance)
	if !ok || method == "" || internalBindingMethods[method] {
		return "", false
	}
	if _, found := ptrType.MethodByName(method); !found {
		return "", false
	}
	return pkgPath + "." + typeName + "." + method, true
}

// UnresolvedBindingNames returns the subset of called that no service in
// services exposes — the frontend's dead call strings, sorted and
// de-duplicated.
//
// This is the drift gate. A renderer holds Call.ByName literals; the Go
// side holds the receivers those literals name. Nothing in the build
// couples them, so a receiver rename or a deleted method only surfaces
// when a user clicks the thing. Feed the frontend's literals (see
// ScanCallByName) and the bound services into this and the breakage
// becomes a failing test instead:
//
//	missing := webkit.UnresolvedBindingNames(called, runnerSvc, serverSvc)
//	if len(missing) > 0 {
//	    t.Fatalf("frontend calls bindings Go does not expose: %v", missing)
//	}
//
// An empty result means every call string resolves. Entries in services
// that are not bindable pointers contribute no names, so passing one by
// mistake shows up as calls that fail to resolve rather than as a
// silently permissive pass.
func UnresolvedBindingNames(called []string, services ...any) []string {
	exposed := make(map[string]bool)
	for _, svc := range services {
		for _, name := range BindingNames(svc) {
			exposed[name] = true
		}
	}

	seen := make(map[string]bool, len(called))
	missing := make([]string, 0)
	for _, name := range called {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || exposed[name] {
			continue
		}
		seen[name] = true
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return missing
}

// bindingReceiver extracts the package path, type name and pointer type
// from a bindable service value, applying the same admissibility rules
// wails does: the value must be a non-nil pointer to a named struct, and
// generic instantiations are rejected (wails cannot bind them).
func bindingReceiver(instance any) (pkgPath string, typeName string, ptrType reflect.Type, ok bool) {
	if instance == nil {
		return "", "", nil, false
	}
	value := reflect.ValueOf(instance)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return "", "", nil, false
	}

	ptrType = value.Type()
	namedType := ptrType.Elem()
	if namedType.Name() == "" {
		return "", "", nil, false
	}
	// Generic instantiations carry their type arguments in String() —
	// "pkg.Box[int]" — and wails refuses to bind them.
	if strings.Contains(namedType.String(), "[") {
		return "", "", nil, false
	}
	return namedType.PkgPath(), namedType.Name(), ptrType, true
}
