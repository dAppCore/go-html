// SPDX-Licence-Identifier: EUPL-1.2

package php

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadWorkspaceConfig_ThroughSymlink pins the go-io v0.15.2
// regression this code used to sit on.
//
// Since v0.15.2 the coreio local medium routes through os.Root, which by
// design refuses a path that leaves its root — including one that merely
// traverses a symlink. loadWorkspaceConfig probes ancestor directories,
// so no medium root can contain it: reached through a symlinked project
// directory (an ordinary macOS layout, and what any worktree or
// /tmp-based test produces), Local.Read failed "path escapes from
// parent" AND Local.IsFile returned false, so the walk continued past a
// config that exists and reported the workspace as unconfigured.
//
// The config must be found through the symlink exactly as through the
// real path.
func TestLoadWorkspaceConfig_ThroughSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(real, ".core"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: 1\nactive: web\npackages_dir: ./pkgs\n"
	if err := os.WriteFile(filepath.Join(real, ".core", "workspace.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(base, "linked-project")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, tc := range []struct{ name, dir string }{
		{"direct", real},
		{"through_symlink", link},
		{"child_through_symlink", filepath.Join(link, "nested")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "child_through_symlink" {
				if err := os.MkdirAll(tc.dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			config, err := loadWorkspaceConfig(tc.dir)
			if err != nil {
				t.Fatalf("loadWorkspaceConfig: %v", err)
			}
			if config == nil {
				t.Fatal("config not found; a symlinked path must resolve like the real one")
			}
			if config.Active != "web" || config.PackagesDir != "./pkgs" {
				t.Fatalf("config = %+v, want active=web packages_dir=./pkgs", config)
			}
		})
	}
}

// TestLoadWorkspaceConfig_Absent asserts a genuinely missing config
// still walks to the root and reports nothing, rather than erroring.
func TestLoadWorkspaceConfig_Absent(t *testing.T) {
	dir := t.TempDir()
	config, err := loadWorkspaceConfig(dir)
	if err != nil {
		t.Fatalf("loadWorkspaceConfig: %v", err)
	}
	if config != nil {
		t.Fatalf("config = %+v, want nil for a directory with no workspace", config)
	}
}

// TestIsRegularFile covers the probe's three answers: a file, a
// directory, and an absent path — including each reached via a symlink,
// which is the case the coreio medium refuses.
func TestIsRegularFile(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "workspace.yaml")
	if err := os.WriteFile(file, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "subdir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"file", file, true},
		{"directory", dir, false},
		{"absent", filepath.Join(base, "nope.yaml"), false},
		{"file_via_symlink", filepath.Join(link, "workspace.yaml"), true},
		{"directory_via_symlink", filepath.Join(link, "subdir"), false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRegularFile(tc.path); got != tc.want {
				t.Fatalf("isRegularFile(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestFindWorkspaceRoot_Good asserts the upward search finds the nearest
// ancestor carrying .core/workspace.yaml, from the root itself and from
// a nested subdirectory.
func TestFindWorkspaceRoot_Good(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	nested := filepath.Join(root, "packages", "web", "src")
	if err := os.MkdirAll(filepath.Join(root, ".core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".core", "workspace.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, start := range []string{root, nested} {
		t.Run(start, func(t *testing.T) {
			t.Chdir(start)
			got, err := findWorkspaceRoot()
			if err != nil {
				t.Fatalf("findWorkspaceRoot: %v", err)
			}
			// The temp path may itself be reached through a symlink, so
			// compare resolved forms.
			gotResolved, _ := filepath.EvalSymlinks(got)
			wantResolved, _ := filepath.EvalSymlinks(root)
			if gotResolved != wantResolved {
				t.Fatalf("findWorkspaceRoot = %q, want %q", gotResolved, wantResolved)
			}
		})
	}
}

// TestFindWorkspaceRoot_Bad asserts a directory outside any workspace
// reports an error rather than walking to / and returning it.
func TestFindWorkspaceRoot_Bad(t *testing.T) {
	t.Chdir(t.TempDir())

	got, err := findWorkspaceRoot()
	if err == nil {
		t.Fatalf("findWorkspaceRoot = %q, want an error outside any workspace", got)
	}
	if got != "" {
		t.Fatalf("findWorkspaceRoot = %q alongside an error, want empty", got)
	}
}

// TestLoadWorkspaceConfig_Ugly covers the malformed-config faults: YAML
// that will not parse, and a version the loader does not support. Both
// must error rather than silently falling back to defaults, which would
// point the workspace at the wrong packages directory.
func TestLoadWorkspaceConfig_Ugly(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not_yaml", "\tthis: [is: not: valid: yaml\n"},
		{"unsupported_version", "version: 99\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ".core"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".core", "workspace.yaml"), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadWorkspaceConfig(dir); err == nil {
				t.Fatalf("loadWorkspaceConfig = nil error for %s", tc.name)
			}
		})
	}
}

// TestLoadWorkspaceConfig_Defaults asserts fields the file omits keep
// their defaults rather than arriving empty.
func TestLoadWorkspaceConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".core", "workspace.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadWorkspaceConfig(dir)
	if err != nil {
		t.Fatalf("loadWorkspaceConfig: %v", err)
	}
	if config == nil {
		t.Fatal("config = nil")
	}
	if config.PackagesDir != "./packages" {
		t.Fatalf("PackagesDir = %q, want the ./packages default", config.PackagesDir)
	}

	// A file that omits `version:` entirely inherits the default of 1
	// rather than failing the version check on a zero value.
	omitted := t.TempDir()
	if err := os.MkdirAll(filepath.Join(omitted, ".core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(omitted, ".core", "workspace.yaml"), []byte("active: web\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err = loadWorkspaceConfig(omitted)
	if err != nil {
		t.Fatalf("loadWorkspaceConfig with no version key: %v", err)
	}
	if config == nil || config.Version != 1 || config.Active != "web" {
		t.Fatalf("config = %+v, want version 1 and active=web", config)
	}
}
