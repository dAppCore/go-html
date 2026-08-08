// SPDX-Licence-Identifier: EUPL-1.2

// Package angular is the in-tree worked example of hosting an Angular
// application on display/webkit.
//
// It exists to reproduce, in a repository that does not depend on any
// consumer, every part of the seam a real hosted Angular app leans on:
//
//   - Call.ByName round-trips against BOTH receiver shapes seen in the
//     wild (`.Service.` and `.WailsService.`)
//   - events emitted from Go and subscribed in the renderer
//   - asset serving with hash routing, deep links and reload
//   - a CSP that permits exactly what the transport needs
//   - window state / layout persistence
//   - the dev-mode (vite) vs built-assets split
//
// The parts that can be gated headless ARE gated — see seam_test.go,
// which resolves every Call.ByName literal in ui/src against the Go
// services below without building the frontend or opening a WebView.
// What genuinely needs a real WebView is listed in README.md.
package angular

import (
	"sort"
	"strconv"
	"sync"
	"time"

	core "dappco.re/go"
)

// RunnerService is the conventional receiver shape: a domain service
// named Service, bound with webkit.Bind, called from the renderer as
//
//	Call.ByName('dappco.re/go/render/display/webkit/example/angular.RunnerService.Start', name)
//
// Every exported method here is reachable from the renderer, which is
// exactly why the drift gate matters: renaming this struct rewrites all
// of those call strings.
type RunnerService struct {
	mu      sync.Mutex
	running map[string]time.Time
	clock   func() time.Time
}

// NewRunnerService builds a RunnerService with the real clock.
func NewRunnerService() *RunnerService {
	return &RunnerService{running: make(map[string]time.Time), clock: time.Now}
}

// Start marks a named job as running and returns its start time in
// RFC3339. Demonstrates the plain value-in / value-out round-trip.
func (s *RunnerService) Start(name string) (string, error) {
	if name == "" {
		return "", core.E("angular.RunnerService.Start", "job name is required", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.running[name]; exists {
		return "", core.E("angular.RunnerService.Start", "job already running: "+name, nil)
	}
	started := s.clock()
	s.running[name] = started
	return started.UTC().Format(time.RFC3339), nil
}

// Stop clears a running job. Returns an error for an unknown name so
// the renderer has a rejected-promise path to exercise.
func (s *RunnerService) Stop(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.running[name]; !exists {
		return core.E("angular.RunnerService.Stop", "no such job: "+name, nil)
	}
	delete(s.running, name)
	return nil
}

// Running lists the running job names, sorted. Demonstrates a slice
// return, which the generated TS types as string[].
func (s *RunnerService) Running() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.running))
	for name := range s.running {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Echo returns its argument. The renderer uses it as a liveness probe:
// a successful Echo proves bindings resolved, which distinguishes "the
// binding name is wrong" from "the transport is down" — two failures
// that otherwise look identical in the console.
func (s *RunnerService) Echo(message string) string {
	return message
}

// StatsWailsService is the OTHER receiver shape found in the wild. It is
// deliberately named WailsService rather than Service, because that
// difference lands in the call string the renderer must use:
//
//	…/example/angular.StatsWailsService.Snapshot
//
// Nothing about wails makes one name more correct than the other. The
// example carries both so the gate proves it catches either, and so the
// asymmetry is visible rather than folklore.
type StatsWailsService struct {
	runner *RunnerService
}

// NewStatsWailsService builds a StatsWailsService over a runner.
func NewStatsWailsService(runner *RunnerService) *StatsWailsService {
	return &StatsWailsService{runner: runner}
}

// Snapshot reports the current job count as a struct, exercising the
// struct-return path the binding generator turns into a TS interface.
func (s *StatsWailsService) Snapshot() Stats {
	if s.runner == nil {
		return Stats{}
	}
	names := s.runner.Running()
	return Stats{Running: len(names), Names: names}
}

// Describe renders the snapshot as text, so the renderer has something
// to display without depending on generated types.
func (s *StatsWailsService) Describe() string {
	snapshot := s.Snapshot()
	return strconv.Itoa(snapshot.Running) + " running"
}

// Stats is the struct returned across the binding boundary. Field tags
// matter: the generated TS uses the JSON names, so an untagged field
// arrives in the renderer capitalised and every consumer silently reads
// undefined.
type Stats struct {
	Running int      `json:"running"`
	Names   []string `json:"names"`
}
