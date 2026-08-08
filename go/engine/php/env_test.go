// SPDX-Licence-Identifier: EUPL-1.2

package php

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAppendEnv_Good asserts a key/value pair is appended in Laravel's
// quoted form, preserving what was already there.
func TestAppendEnv_Good(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("APP_NAME=Lethean\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AppendEnv(root, "DB_DATABASE", "/data/app.sqlite"); err != nil {
		t.Fatalf("AppendEnv: %v", err)
	}

	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "APP_NAME=Lethean\nDB_DATABASE=\"/data/app.sqlite\"\n"
	if string(got) != want {
		t.Fatalf(".env = %q, want %q", got, want)
	}
}

// TestAppendEnv_Bad asserts a missing .env is an error rather than a
// silent no-op. AppendEnv opens with O_APPEND|O_WRONLY and no O_CREATE,
// so the file must already exist — a caller appending before the file is
// written would otherwise lose the value with no signal.
func TestAppendEnv_Bad(t *testing.T) {
	if err := AppendEnv(t.TempDir(), "KEY", "value"); err == nil {
		t.Fatal("AppendEnv = nil error with no .env present")
	}
	if err := AppendEnv(filepath.Join(t.TempDir(), "absent-dir"), "KEY", "value"); err == nil {
		t.Fatal("AppendEnv = nil error for a missing directory")
	}
}

// TestAppendEnv_Ugly asserts repeated appends accumulate rather than
// truncating, and that a value containing quotes or newlines lands
// verbatim — the function does no escaping, which callers must know.
func TestAppendEnv_Ugly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, kv := range [][2]string{{"A", "1"}, {"B", "2"}, {"A", "3"}} {
		if err := AppendEnv(root, kv[0], kv[1]); err != nil {
			t.Fatalf("AppendEnv(%s): %v", kv[0], err)
		}
	}

	got, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	want := "A=\"1\"\nB=\"2\"\nA=\"3\"\n"
	if string(got) != want {
		t.Fatalf(".env = %q, want %q — appends must accumulate, duplicates included", got, want)
	}
}

// TestLoadOrGenerateAppKey_Good asserts a freshly generated key has
// Laravel's expected shape and is persisted for reuse.
func TestLoadOrGenerateAppKey_Good(t *testing.T) {
	dataDir := t.TempDir()

	key, err := loadOrGenerateAppKey(dataDir)
	if err != nil {
		t.Fatalf("loadOrGenerateAppKey: %v", err)
	}
	if !strings.HasPrefix(key, "base64:") {
		t.Fatalf("key = %q, want the base64: prefix Laravel requires", key)
	}
	// 32 random bytes, standard base64 with padding.
	if encoded := strings.TrimPrefix(key, "base64:"); len(encoded) != 44 {
		t.Fatalf("encoded key length = %d, want 44 (32 bytes base64)", len(encoded))
	}

	saved, readErr := os.ReadFile(filepath.Join(dataDir, ".app-key"))
	if readErr != nil {
		t.Fatalf("key not persisted: %v", readErr)
	}
	if string(saved) != key {
		t.Fatalf("persisted key = %q, want %q", saved, key)
	}
}

// TestLoadOrGenerateAppKey_Stable is the property that matters most: the
// key must survive across calls. Regenerating it invalidates every
// existing session and every encrypted column in the database.
func TestLoadOrGenerateAppKey_Stable(t *testing.T) {
	dataDir := t.TempDir()

	first, err := loadOrGenerateAppKey(dataDir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	for i := range 3 {
		again, againErr := loadOrGenerateAppKey(dataDir)
		if againErr != nil {
			t.Fatalf("call %d: %v", i+2, againErr)
		}
		if again != first {
			t.Fatalf("call %d returned a NEW key (%q vs %q); every session and encrypted column would break", i+2, again, first)
		}
	}
}

// TestLoadOrGenerateAppKey_Ugly covers the stored-key edge cases: an
// empty file must be treated as absent and regenerated, while arbitrary
// existing content is returned untouched rather than silently replaced.
func TestLoadOrGenerateAppKey_Ugly(t *testing.T) {
	t.Run("empty_file_regenerates", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, ".app-key"), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		key, err := loadOrGenerateAppKey(dataDir)
		if err != nil {
			t.Fatalf("loadOrGenerateAppKey: %v", err)
		}
		if !strings.HasPrefix(key, "base64:") {
			t.Fatalf("key = %q, want a freshly generated one", key)
		}
	})

	t.Run("existing_content_preserved", func(t *testing.T) {
		dataDir := t.TempDir()
		existing := "base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		if err := os.WriteFile(filepath.Join(dataDir, ".app-key"), []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
		key, err := loadOrGenerateAppKey(dataDir)
		if err != nil {
			t.Fatalf("loadOrGenerateAppKey: %v", err)
		}
		if key != existing {
			t.Fatalf("key = %q, want the stored %q", key, existing)
		}
	})

	t.Run("unwritable_data_dir_errors", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root; permission bits do not deny writes")
		}
		dataDir := t.TempDir()
		if err := os.Chmod(dataDir, 0o500); err != nil {
			t.Skipf("cannot make the directory unwritable: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dataDir, 0o700) })

		if _, err := loadOrGenerateAppKey(dataDir); err == nil {
			t.Fatal("loadOrGenerateAppKey = nil error when the key cannot be saved")
		}
	})
}

// TestWriteEnvFile_Good asserts the generated .env carries the values
// Laravel needs, with the app name and database path substituted in.
func TestWriteEnvFile_Good(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	env := &RuntimeEnvironment{
		DataDir:      dataDir,
		LaravelRoot:  root,
		DatabasePath: filepath.Join(dataDir, "lethean.sqlite"),
	}

	if err := writeEnvFile(root, "Lethean", env); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("no .env written: %v", err)
	}
	content := string(body)

	for _, want := range []string{
		`APP_NAME="Lethean"`,
		"APP_ENV=production",
		"APP_KEY=base64:",
		"APP_DEBUG=false",
		"DB_CONNECTION=sqlite",
		`DB_DATABASE="` + env.DatabasePath + `"`,
		"SESSION_DRIVER=file",
	} {
		if !strings.Contains(content, want) {
			t.Errorf(".env missing %q\n---\n%s", want, content)
		}
	}
}

// TestWriteEnvFile_Ugly asserts rewriting reuses the persisted key. The
// .env is regenerated on every launch, so a key that changed here would
// log every user out on each restart.
func TestWriteEnvFile_Ugly(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	env := &RuntimeEnvironment{DataDir: dataDir, LaravelRoot: root, DatabasePath: filepath.Join(dataDir, "a.sqlite")}

	if err := writeEnvFile(root, "Lethean", env); err != nil {
		t.Fatalf("first writeEnvFile: %v", err)
	}
	first := appKeyFrom(t, filepath.Join(root, ".env"))

	if err := writeEnvFile(root, "Lethean", env); err != nil {
		t.Fatalf("second writeEnvFile: %v", err)
	}
	second := appKeyFrom(t, filepath.Join(root, ".env"))

	if first == "" {
		t.Fatal("no APP_KEY in the generated .env")
	}
	if first != second {
		t.Fatalf("APP_KEY changed across writes (%q → %q); every session would be invalidated on restart", first, second)
	}
}

// TestWriteEnvFile_Bad asserts an unwritable Laravel root surfaces an
// error rather than reporting success with no file.
func TestWriteEnvFile_Bad(t *testing.T) {
	env := &RuntimeEnvironment{DataDir: t.TempDir(), DatabasePath: "/tmp/a.sqlite"}
	missing := filepath.Join(t.TempDir(), "no-such-root")

	if err := writeEnvFile(missing, "Lethean", env); err == nil {
		t.Fatal("writeEnvFile = nil error for a non-existent Laravel root")
	}
}

// TestResolveDataDir_Good asserts the per-platform location, including
// the XDG override that Linux packagers rely on.
func TestResolveDataDir_Good(t *testing.T) {
	dir, err := resolveDataDir("lethean")
	if err != nil {
		t.Fatalf("resolveDataDir: %v", err)
	}
	if dir == "" {
		t.Fatal("resolveDataDir = empty")
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("resolveDataDir = %q, want an absolute path", dir)
	}

	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(dir, filepath.Join("Library", "Application Support", "lethean")) {
			t.Fatalf("darwin data dir = %q, want ~/Library/Application Support/lethean", dir)
		}
	case "linux":
		if !strings.HasSuffix(dir, "lethean") {
			t.Fatalf("linux data dir = %q, want a lethean suffix", dir)
		}
	default:
		if !strings.HasSuffix(dir, ".lethean") {
			t.Fatalf("data dir = %q, want a dot-prefixed home directory", dir)
		}
	}
}

// TestResolveDataDir_XDG pins the XDG_DATA_HOME override on Linux, which
// is how distribution packages relocate application data.
func TestResolveDataDir_XDG(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("XDG_DATA_HOME is only consulted on linux, not %s", runtime.GOOS)
	}
	t.Setenv("XDG_DATA_HOME", "/custom/data")

	dir, err := resolveDataDir("lethean")
	if err != nil {
		t.Fatalf("resolveDataDir: %v", err)
	}
	if want := filepath.Join("/custom/data", "lethean"); dir != want {
		t.Fatalf("resolveDataDir = %q, want %q", dir, want)
	}
}

// TestResolveDataDir_Ugly asserts an empty application name still yields
// a usable absolute path rather than the bare parent directory, which
// would put application data directly in the user's data root.
func TestResolveDataDir_Ugly(t *testing.T) {
	dir, err := resolveDataDir("")
	if err != nil {
		t.Fatalf("resolveDataDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("resolveDataDir(\"\") = %q, want an absolute path", dir)
	}
}

// appKeyFrom extracts the APP_KEY value from a generated .env.
func appKeyFrom(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if after, ok := strings.CutPrefix(line, "APP_KEY="); ok {
			return after
		}
	}
	return ""
}

// TestPrepareRuntimeEnvironment_Good exercises the whole preparation in
// one hermetic run, with HOME (and XDG_DATA_HOME on Linux) redirected at
// a temporary directory so nothing touches the real user profile.
//
// It asserts every persistent directory Laravel writes to exists, the
// database file is created, the extracted storage/ is replaced by a
// symlink to the persistent one, and the .env names the resolved paths.
func TestPrepareRuntimeEnvironment_Good(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	laravelRoot := t.TempDir()
	// The extracted app ships a storage/ tree that must be replaced.
	if err := os.MkdirAll(filepath.Join(laravelRoot, "storage", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(laravelRoot, "storage", "app", "extracted.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := PrepareRuntimeEnvironment(laravelRoot, "lethean")
	if err != nil {
		t.Fatalf("PrepareRuntimeEnvironment: %v", err)
	}

	if env.LaravelRoot != laravelRoot {
		t.Fatalf("LaravelRoot = %q, want %q", env.LaravelRoot, laravelRoot)
	}
	if !strings.HasPrefix(env.DataDir, home) {
		t.Fatalf("DataDir = %q, want it under the redirected home %q", env.DataDir, home)
	}
	if want := filepath.Join(env.DataDir, "lethean.sqlite"); env.DatabasePath != want {
		t.Fatalf("DatabasePath = %q, want %q", env.DatabasePath, want)
	}

	for _, dir := range []string{
		env.DataDir,
		filepath.Join(env.DataDir, "storage", "app"),
		filepath.Join(env.DataDir, "storage", "framework", "cache", "data"),
		filepath.Join(env.DataDir, "storage", "framework", "sessions"),
		filepath.Join(env.DataDir, "storage", "framework", "views"),
		filepath.Join(env.DataDir, "storage", "logs"),
	} {
		info, statErr := os.Stat(dir)
		if statErr != nil {
			t.Errorf("missing persistent directory %s: %v", dir, statErr)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}

	if _, statErr := os.Stat(env.DatabasePath); statErr != nil {
		t.Errorf("database not created: %v", statErr)
	}

	// storage/ in the extracted app must now be a symlink to the
	// persistent tree — that redirection is what makes data survive an
	// application update, since the extracted root is disposable.
	extracted := filepath.Join(laravelRoot, "storage")
	info, err := os.Lstat(extracted)
	if err != nil {
		t.Fatalf("lstat extracted storage: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("extracted storage/ is not a symlink; persistent data would be lost on update")
	}
	target, err := os.Readlink(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(env.DataDir, "storage"); target != want {
		t.Fatalf("storage symlink → %q, want %q", target, want)
	}

	body, err := os.ReadFile(filepath.Join(laravelRoot, ".env"))
	if err != nil {
		t.Fatalf("no .env written: %v", err)
	}
	for _, want := range []string{`APP_NAME="lethean"`, "APP_KEY=base64:", env.DatabasePath} {
		if !strings.Contains(string(body), want) {
			t.Errorf(".env missing %q", want)
		}
	}
}

// TestPrepareRuntimeEnvironment_Idempotent asserts a second launch over
// the same data directory keeps the existing database and app key. This
// is the ordinary case — every restart runs this — and regenerating
// either would lose user data or invalidate every session.
func TestPrepareRuntimeEnvironment_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	first, err := PrepareRuntimeEnvironment(t.TempDir(), "lethean")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Write something the second run must not destroy.
	if err := os.WriteFile(first.DatabasePath, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstKey, err := os.ReadFile(filepath.Join(first.DataDir, ".app-key"))
	if err != nil {
		t.Fatal(err)
	}

	second, err := PrepareRuntimeEnvironment(t.TempDir(), "lethean")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.DataDir != first.DataDir {
		t.Fatalf("DataDir moved between runs: %q → %q", first.DataDir, second.DataDir)
	}

	data, err := os.ReadFile(second.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "user data" {
		t.Fatalf("database = %q, want the existing contents preserved", data)
	}

	secondKey, err := os.ReadFile(filepath.Join(second.DataDir, ".app-key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(secondKey) != string(firstKey) {
		t.Fatal("APP_KEY regenerated on the second run; every session would be invalidated")
	}
}

// TestPrepareRuntimeEnvironment_Bad asserts a Laravel root that cannot be
// written surfaces an error rather than a half-prepared environment.
func TestPrepareRuntimeEnvironment_Bad(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits do not deny writes")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	laravelRoot := t.TempDir()
	if err := os.Chmod(laravelRoot, 0o500); err != nil {
		t.Skipf("cannot make the root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(laravelRoot, 0o700) })

	if _, err := PrepareRuntimeEnvironment(laravelRoot, "lethean"); err == nil {
		t.Fatal("PrepareRuntimeEnvironment = nil error for a read-only Laravel root")
	}
}
