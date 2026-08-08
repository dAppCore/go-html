// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

// bindingPkg is the package path every FQN in this file is built from.
const bindingPkg = "dappco.re/go/render/display/webkit"

// nameProbeService stands in for a consumer's bound domain service. It
// carries one plain method, one that takes a context (wails allows it),
// an unexported method, and the four lifecycle hooks the runtime
// consumes itself.
type nameProbeService struct{}

func (*nameProbeService) Start(string) error                           { return nil }
func (*nameProbeService) Status(context.Context) (string, error)       { return "", nil }
func (*nameProbeService) internalOnly()                                {}
func (*nameProbeService) ServiceName() string                          { return "probe" }
func (*nameProbeService) ServiceStartup(context.Context, any) error    { return nil }
func (*nameProbeService) ServiceShutdown() error                       { return nil }
func (*nameProbeService) ServeHTTP(http.ResponseWriter, *http.Request) {}

// renamedProbeService is the same surface under the other receiver name
// seen in the wild (`.WailsService.` rather than `.Service.`). It exists
// to pin the fact that the receiver TYPE NAME is part of the wire
// contract — the whole reason the drift gate is needed.
type renamedProbeService struct{}

func (*renamedProbeService) Start(string) error { return nil }

// genericProbeService is a generic type; wails refuses to bind these.
type genericProbeService[T any] struct{ value T }

func (*genericProbeService[T]) Start() error { return nil }

// TestBindingNames_Good asserts the FQN shape wails computes: package
// path, receiver type name, method name — exported methods only, with
// the runtime's own lifecycle hooks excluded, sorted.
func TestBindingNames_Good(t *testing.T) {
	got := BindingNames(&nameProbeService{})
	want := []string{
		bindingPkg + ".nameProbeService.Start",
		bindingPkg + ".nameProbeService.Status",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BindingNames = %v, want %v", got, want)
	}
}

// TestBindingNames_Bad covers every input wails itself rejects at bind
// time. Each must yield no names rather than a partial or panicking
// result, so a caller feeding the wrong thing sees unresolved calls
// instead of a silently permissive gate.
func TestBindingNames_Bad(t *testing.T) {
	cases := []struct {
		name     string
		instance any
	}{
		{"nil", nil},
		{"nil_typed_pointer", (*nameProbeService)(nil)},
		{"value_not_pointer", nameProbeService{}},
		{"pointer_to_unnamed", &struct{ A int }{}},
		{"generic", &genericProbeService[int]{}},
		{"not_a_struct", func() any { n := 3; return &n }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BindingNames(tc.instance); len(got) != 0 {
				t.Fatalf("BindingNames(%s) = %v, want none", tc.name, got)
			}
		})
	}
}

// TestBindingNames_Ugly pins the receiver-rename breakage itself: the
// same method on a renamed struct produces a DIFFERENT call string, and
// wails resolves Call.ByName through an exact-match map. This is the
// silent frontend break the drift gate catches.
func TestBindingNames_Ugly(t *testing.T) {
	original := BindingNames(&nameProbeService{})[0]
	renamed := BindingNames(&renamedProbeService{})[0]

	if original == renamed {
		t.Fatal("receiver rename produced the same FQN; the gate would never fire")
	}
	if want := bindingPkg + ".renamedProbeService.Start"; renamed != want {
		t.Fatalf("renamed FQN = %q, want %q", renamed, want)
	}
	// A frontend pinned to the old string resolves against neither the
	// renamed service nor a service list containing only the new one.
	missing := UnresolvedBindingNames([]string{original}, &renamedProbeService{})
	if len(missing) != 1 || missing[0] != original {
		t.Fatalf("stale call string not reported: %v", missing)
	}
}

// TestBindingName_Good resolves one method to its call string.
func TestBindingName_Good(t *testing.T) {
	got, ok := BindingName(&nameProbeService{}, "Start")
	if !ok {
		t.Fatal("BindingName(Start) not ok")
	}
	if want := bindingPkg + ".nameProbeService.Start"; got != want {
		t.Fatalf("BindingName = %q, want %q", got, want)
	}
}

// TestBindingName_Bad collapses the three "never resolves at runtime"
// cases — absent, unexported, lifecycle hook — onto one false signal.
func TestBindingName_Bad(t *testing.T) {
	cases := []string{"", "Missing", "internalOnly", "ServiceName", "ServiceStartup", "ServiceShutdown", "ServeHTTP"}
	for _, method := range cases {
		t.Run("method_"+method, func(t *testing.T) {
			if name, ok := BindingName(&nameProbeService{}, method); ok {
				t.Fatalf("BindingName(%q) = %q, want not-callable", method, name)
			}
		})
	}
}

// TestUnresolvedBindingNames_Good asserts only the strings no service
// exposes come back, de-duplicated and sorted.
func TestUnresolvedBindingNames_Good(t *testing.T) {
	called := []string{
		bindingPkg + ".nameProbeService.Start",
		bindingPkg + ".nameProbeService.Vanished",
		bindingPkg + ".nameProbeService.Vanished", // duplicate call site
		bindingPkg + ".renamedProbeService.Start",
		"  ", // whitespace-only entries are not call strings
	}
	got := UnresolvedBindingNames(called, &nameProbeService{}, &renamedProbeService{})
	want := []string{bindingPkg + ".nameProbeService.Vanished"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnresolvedBindingNames = %v, want %v", got, want)
	}
}

// TestUnresolvedBindingNames_Bad asserts an empty call list and a
// serviceless call both behave: no calls means nothing to report; no
// services means every call is unresolved rather than vacuously fine.
func TestUnresolvedBindingNames_Bad(t *testing.T) {
	if got := UnresolvedBindingNames(nil, &nameProbeService{}); len(got) != 0 {
		t.Fatalf("no calls should report nothing, got %v", got)
	}
	called := []string{bindingPkg + ".nameProbeService.Start"}
	if got := UnresolvedBindingNames(called); len(got) != 1 {
		t.Fatalf("no services should report every call, got %v", got)
	}
}

// TestInternalBindingMethods_Good pins this package's copy of the wails
// exclusion set against the behaviour it mirrors. If wails adds a hook
// and this map lags, BindingNames advertises a name the runtime rejects.
func TestInternalBindingMethods_Good(t *testing.T) {
	want := []string{"ServeHTTP", "ServiceName", "ServiceShutdown", "ServiceStartup"}
	for _, method := range want {
		if !internalBindingMethods[method] {
			t.Fatalf("%q missing from internalBindingMethods", method)
		}
	}
	if len(internalBindingMethods) != len(want) {
		t.Fatalf("internalBindingMethods has %d entries, want %d — resync with wails pkg/application/bindings.go", len(internalBindingMethods), len(want))
	}
}
