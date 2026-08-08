// SPDX-Licence-Identifier: EUPL-1.2

package php

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// embeddedApp mimics the shape of an embedded Laravel tree: nested
// directories, a file at the root of the prefix, and a deeply nested one.
func embeddedApp() fstest.MapFS {
	return fstest.MapFS{
		"laravel/artisan":                 &fstest.MapFile{Data: []byte("#!/usr/bin/env php\n")},
		"laravel/composer.json":           &fstest.MapFile{Data: []byte(`{"require":{}}`)},
		"laravel/app/Models/User.php":     &fstest.MapFile{Data: []byte("<?php class User {}")},
		"laravel/storage/logs/.gitignore": &fstest.MapFile{Data: []byte("*\n")},
		"laravel/public/index.php":        &fstest.MapFile{Data: []byte("<?php require '../vendor/autoload.php';")},
		"other/not-part-of-the-app.txt":   &fstest.MapFile{Data: []byte("ignore me")},
	}
}

// TestExtract_Good asserts the whole prefixed subtree lands on disk with
// its structure intact, contents preserved, and nothing from outside the
// prefix copied across.
func TestExtract_Good(t *testing.T) {
	dir, err := Extract(embeddedApp(), "laravel")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	want := map[string]string{
		"artisan":                 "#!/usr/bin/env php\n",
		"composer.json":           `{"require":{}}`,
		"app/Models/User.php":     "<?php class User {}",
		"storage/logs/.gitignore": "*\n",
		"public/index.php":        "<?php require '../vendor/autoload.php';",
	}
	for name, body := range want {
		got, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if string(got) != body {
			t.Fatalf("%s = %q, want %q", name, got, body)
		}
	}

	// The prefix itself must be stripped, not recreated inside the target.
	if _, statErr := os.Stat(filepath.Join(dir, "laravel")); statErr == nil {
		t.Fatal("the prefix directory was recreated inside the target")
	}
	// Content outside the prefix must not be copied.
	if _, statErr := os.Stat(filepath.Join(dir, "not-part-of-the-app.txt")); statErr == nil {
		t.Fatal("a file from outside the prefix was extracted")
	}
}

// TestExtract_Bad asserts a prefix that names nothing fails, and leaves
// no temporary directory behind — a caller that got an error will not
// call RemoveAll, so a leak here is permanent for the process's lifetime.
func TestExtract_Bad(t *testing.T) {
	before := tempDirEntries(t)

	dir, err := Extract(embeddedApp(), "no-such-prefix")
	if err == nil {
		t.Fatalf("Extract = %q, want an error for a missing prefix", dir)
	}
	if dir != "" {
		t.Fatalf("Extract returned %q alongside an error; want an empty path", dir)
	}

	after := tempDirEntries(t)
	for name := range after {
		if !before[name] {
			t.Errorf("Extract leaked a temporary directory on the error path: %s", name)
		}
	}
}

// TestExtract_Ugly injects a read fault mid-walk: a filesystem whose
// entries list fine but whose contents fail to read. Extract must
// surface the error rather than producing a half-populated tree a caller
// would then serve.
func TestExtract_Ugly(t *testing.T) {
	before := tempDirEntries(t)

	dir, err := Extract(&faultyFS{MapFS: embeddedApp(), failOn: "laravel/public/index.php"}, "laravel")
	if err == nil {
		t.Fatalf("Extract = %q, want an error when a file cannot be read", dir)
	}
	if dir != "" {
		t.Fatalf("Extract returned %q alongside an error", dir)
	}

	after := tempDirEntries(t)
	for name := range after {
		if !before[name] {
			t.Errorf("Extract leaked a partially-populated directory: %s", name)
		}
	}
}

// TestExtract_EmptyPrefixSubtree asserts a prefix naming a directory with
// no files still succeeds, yielding an empty extraction rather than an
// error — an app with no assets is unusual, not invalid.
func TestExtract_EmptyPrefixSubtree(t *testing.T) {
	fsys := fstest.MapFS{
		"laravel/placeholder/.keep": &fstest.MapFile{Data: []byte{}},
	}
	dir, err := Extract(fsys, "laravel")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	if _, statErr := os.Stat(filepath.Join(dir, "placeholder", ".keep")); statErr != nil {
		t.Fatalf("placeholder not extracted: %v", statErr)
	}
}

// faultyFS wraps a MapFS and fails the read of one named path, so a
// mid-walk I/O fault can be injected deterministically.
//
// ReadFile must be overridden as well as Open: fstest.MapFS implements
// fs.ReadFileFS, so fs.ReadFile dispatches to the embedded method and an
// Open-only override is silently bypassed.
type faultyFS struct {
	fstest.MapFS
	failOn string
}

func (f *faultyFS) Open(name string) (fs.File, error) {
	if name == f.failOn {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Open(name)
}

func (f *faultyFS) ReadFile(name string) ([]byte, error) {
	if name == f.failOn {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.ReadFile(name)
}

// tempDirEntries snapshots the names in the system temp directory that
// Extract would create into, so a leak can be detected by difference.
func tempDirEntries(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Skipf("cannot list the temp directory: %v", err)
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	return names
}
