// SPDX-Licence-Identifier: EUPL-1.2

package php

import (
	"os"
	"path/filepath"
	"testing"
)

// laravelFixture writes a minimal but realistic Laravel project into a
// fresh temporary directory and returns its path. Options are applied as
// extra files, so each test states exactly the shape it depends on.
func laravelFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// laravelBase is the file set that makes IsLaravelProject true.
func laravelBase() map[string]string {
	return map[string]string{
		"artisan":       "#!/usr/bin/env php\n",
		"composer.json": `{"require":{"laravel/framework":"^11.0"}}`,
	}
}

// TestIsLaravelProject_Good accepts a project declaring laravel/framework
// in either require block, which is what the function documents.
func TestIsLaravelProject_Good(t *testing.T) {
	cases := []struct {
		name     string
		composer string
	}{
		{"require", `{"require":{"laravel/framework":"^11.0","php":"^8.3"}}`},
		{"require_dev", `{"require-dev":{"laravel/framework":"^10.0"}}`},
		{"both_blocks", `{"require":{"laravel/framework":"^11.0"},"require-dev":{"phpunit/phpunit":"^11.0"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{
				"artisan":       "#!/usr/bin/env php\n",
				"composer.json": tc.composer,
			})
			if !IsLaravelProject(dir) {
				t.Fatal("IsLaravelProject = false, want true")
			}
		})
	}
}

// TestIsLaravelProject_Bad covers every way a directory legitimately
// fails the check. Each must be false, not a panic and not a true.
func TestIsLaravelProject_Bad(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"empty_directory", map[string]string{}},
		{"artisan_without_composer", map[string]string{"artisan": "#!/usr/bin/env php\n"}},
		{"composer_without_artisan", map[string]string{"composer.json": `{"require":{"laravel/framework":"^11.0"}}`}},
		{"composer_without_laravel", map[string]string{
			"artisan":       "#!/usr/bin/env php\n",
			"composer.json": `{"require":{"symfony/console":"^7.0"}}`,
		}},
		{"empty_require", map[string]string{
			"artisan":       "#!/usr/bin/env php\n",
			"composer.json": `{"require":{}}`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsLaravelProject(laravelFixture(t, tc.files)) {
				t.Fatal("IsLaravelProject = true, want false")
			}
		})
	}

	if IsLaravelProject(filepath.Join(t.TempDir(), "does-not-exist")) {
		t.Fatal("IsLaravelProject = true for a missing directory")
	}
}

// TestIsLaravelProject_Ugly is the fault injection: a composer.json that
// exists but cannot be parsed. A truncated or hand-edited manifest is
// ordinary in the wild, and the function must answer false rather than
// panic on the JSON decode.
func TestIsLaravelProject_Ugly(t *testing.T) {
	cases := []struct {
		name     string
		composer string
	}{
		{"truncated", `{"require":{"laravel/framework":`},
		{"not_json", "this is not json at all"},
		{"empty_file", ""},
		{"json_array", `["laravel/framework"]`},
		{"require_is_a_string", `{"require":"laravel/framework"}`},
		{"null_require", `{"require":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{
				"artisan":       "#!/usr/bin/env php\n",
				"composer.json": tc.composer,
			})
			if IsLaravelProject(dir) {
				t.Fatalf("IsLaravelProject = true for a malformed composer.json (%s)", tc.name)
			}
		})
	}
}

// TestIsLaravelProject_ThroughSymlink pins the go-io os.Root regression
// this file's probing used to sit on — the same class as the one fixed
// in workspace.go.
//
// A project reached through a symlinked directory (~/Sites/app pointing
// at a volume, a git worktree, a bind-mounted container path) had every
// file reported absent by the confined medium, so a Laravel project
// answered false. Silently: no error, just a wrong answer.
func TestIsLaravelProject_ThroughSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "app")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range laravelBase() {
		if err := os.WriteFile(filepath.Join(real, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "app-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if !IsLaravelProject(real) {
		t.Fatal("IsLaravelProject(real) = false; the fixture is wrong")
	}
	if !IsLaravelProject(link) {
		t.Fatal("IsLaravelProject(symlink) = false; a symlinked project path must resolve like the real one")
	}
}

// TestIsFrankenPHPProject_Good covers both accepting paths: octane with
// an explicit frankenphp config, and octane with no config at all (the
// documented "assume frankenphp" default).
func TestIsFrankenPHPProject_Good(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"octane_with_frankenphp_config", map[string]string{
			"composer.json":     `{"require":{"laravel/octane":"^2.0"}}`,
			"config/octane.php": `<?php return ['server' => 'frankenphp'];`,
		}},
		{"octane_without_config", map[string]string{
			"composer.json": `{"require":{"laravel/octane":"^2.0"}}`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !IsFrankenPHPProject(laravelFixture(t, tc.files)) {
				t.Fatal("IsFrankenPHPProject = false, want true")
			}
		})
	}
}

// TestIsFrankenPHPProject_Bad covers the rejecting paths.
func TestIsFrankenPHPProject_Bad(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"no_composer", map[string]string{}},
		{"no_octane", map[string]string{"composer.json": `{"require":{"laravel/framework":"^11.0"}}`}},
		{"octane_with_swoole_config", map[string]string{
			"composer.json":     `{"require":{"laravel/octane":"^2.0"}}`,
			"config/octane.php": `<?php return ['server' => 'swoole'];`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsFrankenPHPProject(laravelFixture(t, tc.files)) {
				t.Fatal("IsFrankenPHPProject = true, want false")
			}
		})
	}
}

// TestIsFrankenPHPProject_Ugly asserts a malformed composer.json is
// false, while an UNREADABLE octane config keeps the documented
// assume-frankenphp fallback rather than flipping the answer.
func TestIsFrankenPHPProject_Ugly(t *testing.T) {
	if IsFrankenPHPProject(laravelFixture(t, map[string]string{
		"composer.json": `{"require":{"laravel/octane":`,
	})) {
		t.Fatal("IsFrankenPHPProject = true for a malformed composer.json")
	}

	// An octane config that exists but cannot be read: the function
	// documents "assume frankenphp if we can't read config".
	dir := laravelFixture(t, map[string]string{
		"composer.json":     `{"require":{"laravel/octane":"^2.0"}}`,
		"config/octane.php": `<?php return ['server' => 'swoole'];`,
	})
	config := filepath.Join(dir, "config", "octane.php")
	if err := os.Chmod(config, 0o000); err != nil {
		t.Skipf("cannot make the config unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(config, 0o600) })
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits do not deny reads")
	}

	if !IsFrankenPHPProject(dir) {
		t.Fatal("IsFrankenPHPProject = false for an unreadable octane config, want the assume-frankenphp fallback")
	}
}

// TestDetectServices_Good asserts the composition and, critically, the
// ORDER — callers start services in the returned sequence, so FrankenPHP
// must lead.
func TestDetectServices_Good(t *testing.T) {
	dir := laravelFixture(t, map[string]string{
		"artisan":            "#!/usr/bin/env php\n",
		"composer.json":      `{"require":{"laravel/framework":"^11.0"}}`,
		"vite.config.ts":     `export default {};`,
		"config/horizon.php": `<?php return [];`,
		"config/reverb.php":  `<?php return [];`,
		".env":               "REDIS_HOST=127.0.0.1\n",
	})

	got := DetectServices(dir)
	want := []DetectedService{ServiceFrankenPHP, ServiceVite, ServiceHorizon, ServiceReverb, ServiceRedis}

	if len(got) != len(want) {
		t.Fatalf("DetectServices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DetectServices[%d] = %q, want %q (order is load-bearing)", i, got[i], want[i])
		}
	}
}

// TestDetectServices_Bad asserts a directory with nothing in it yields an
// empty, non-nil slice — callers range over it without a nil check.
func TestDetectServices_Bad(t *testing.T) {
	got := DetectServices(t.TempDir())
	if got == nil {
		t.Fatal("DetectServices = nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("DetectServices = %v, want empty", got)
	}
}

// TestDetectServices_Ugly asserts each service is detected exactly once
// even when several triggers are present, and that a Laravel project with
// no extras still gets FrankenPHP.
func TestDetectServices_Ugly(t *testing.T) {
	// All four vite config names at once must not yield four Vite entries.
	dir := laravelFixture(t, map[string]string{
		"artisan":         "#!/usr/bin/env php\n",
		"composer.json":   `{"require":{"laravel/framework":"^11.0","laravel/octane":"^2.0"}}`,
		"vite.config.js":  `export default {};`,
		"vite.config.ts":  `export default {};`,
		"vite.config.mjs": `export default {};`,
		"vite.config.mts": `export default {};`,
	})

	got := DetectServices(dir)
	counts := map[DetectedService]int{}
	for _, service := range got {
		counts[service]++
	}
	for service, n := range counts {
		if n != 1 {
			t.Fatalf("%q appears %d times in %v, want once", service, n, got)
		}
	}
	// Octane AND Laravel both true must still yield one FrankenPHP entry.
	if counts[ServiceFrankenPHP] != 1 {
		t.Fatalf("FrankenPHP count = %d, want 1", counts[ServiceFrankenPHP])
	}
	if counts[ServiceVite] != 1 {
		t.Fatalf("Vite count = %d, want 1", counts[ServiceVite])
	}
}

// TestDetectPackageManager_Good asserts the documented preference order.
func TestDetectPackageManager_Good(t *testing.T) {
	cases := []struct {
		lockFile string
		want     string
	}{
		{"bun.lockb", "bun"},
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{tc.lockFile: ""})
			if got := DetectPackageManager(dir); got != tc.want {
				t.Fatalf("DetectPackageManager = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDetectPackageManager_Bad asserts the npm default when no lock file
// exists, including for a directory that is not there at all.
func TestDetectPackageManager_Bad(t *testing.T) {
	if got := DetectPackageManager(t.TempDir()); got != "npm" {
		t.Fatalf("DetectPackageManager = %q, want the npm default", got)
	}
	if got := DetectPackageManager(filepath.Join(t.TempDir(), "absent")); got != "npm" {
		t.Fatalf("DetectPackageManager(absent) = %q, want the npm default", got)
	}
}

// TestDetectPackageManager_Ugly pins the precedence when a repository
// carries several lock files — a real state after switching managers, and
// the answer must be deterministic rather than filesystem-order dependent.
func TestDetectPackageManager_Ugly(t *testing.T) {
	dir := laravelFixture(t, map[string]string{
		"bun.lockb":         "",
		"pnpm-lock.yaml":    "",
		"yarn.lock":         "",
		"package-lock.json": "",
	})
	for i := range 5 {
		if got := DetectPackageManager(dir); got != "bun" {
			t.Fatalf("call %d: DetectPackageManager = %q, want the highest-precedence %q", i, got, "bun")
		}
	}
}

// TestGetLaravelAppName_Good covers the quoting forms an .env carries.
func TestGetLaravelAppName_Good(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"bare", "APP_NAME=Lethean\n", "Lethean"},
		{"double_quoted", `APP_NAME="Lethean Desktop"` + "\n", "Lethean Desktop"},
		{"single_quoted", "APP_NAME='Lethean Desktop'\n", "Lethean Desktop"},
		{"leading_whitespace", "   APP_NAME=Lethean\n", "Lethean"},
		{"after_other_keys", "APP_ENV=local\nAPP_DEBUG=true\nAPP_NAME=Lethean\n", "Lethean"},
		{"crlf_line_endings", "APP_ENV=local\r\nAPP_NAME=Lethean\r\n", "Lethean"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{".env": tc.env})
			if got := GetLaravelAppName(dir); got != tc.want {
				t.Fatalf("GetLaravelAppName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGetLaravelAppName_Bad asserts the empty answer for every absent
// case, rather than a partial or a panic.
func TestGetLaravelAppName_Bad(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"no_env_file", map[string]string{}},
		{"env_without_app_name", map[string]string{".env": "APP_ENV=local\nAPP_DEBUG=true\n"}},
		{"empty_env", map[string]string{".env": ""}},
		{"app_name_empty", map[string]string{".env": "APP_NAME=\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GetLaravelAppName(laravelFixture(t, tc.files)); got != "" {
				t.Fatalf("GetLaravelAppName = %q, want empty", got)
			}
		})
	}
}

// TestGetLaravelAppURL_Good covers the same quoting forms for APP_URL.
func TestGetLaravelAppURL_Good(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"bare", "APP_URL=https://lethean.test\n", "https://lethean.test"},
		{"quoted", `APP_URL="http://localhost:8000"` + "\n", "http://localhost:8000"},
		{"after_app_name", "APP_NAME=Lethean\nAPP_URL=https://lethean.test\n", "https://lethean.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{".env": tc.env})
			if got := GetLaravelAppURL(dir); got != tc.want {
				t.Fatalf("GetLaravelAppURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGetLaravelAppURL_Bad asserts the empty answer when absent.
func TestGetLaravelAppURL_Bad(t *testing.T) {
	if got := GetLaravelAppURL(t.TempDir()); got != "" {
		t.Fatalf("GetLaravelAppURL = %q, want empty with no .env", got)
	}
	dir := laravelFixture(t, map[string]string{".env": "APP_NAME=Lethean\n"})
	if got := GetLaravelAppURL(dir); got != "" {
		t.Fatalf("GetLaravelAppURL = %q, want empty with no APP_URL", got)
	}
}

// TestNeedsRedis_Good covers each indicator the function recognises.
func TestNeedsRedis_Good(t *testing.T) {
	cases := []struct {
		name string
		env  string
	}{
		{"redis_host_localhost", "REDIS_HOST=localhost\n"},
		{"redis_host_loopback", "REDIS_HOST=127.0.0.1\n"},
		{"cache_driver", "CACHE_DRIVER=redis\n"},
		{"queue_connection", "QUEUE_CONNECTION=redis\n"},
		{"session_driver", "SESSION_DRIVER=redis\n"},
		{"broadcast_driver", "BROADCAST_DRIVER=redis\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{".env": tc.env})
			if !needsRedis(dir) {
				t.Fatal("needsRedis = false, want true")
			}
		})
	}
}

// TestNeedsRedis_Bad asserts the false cases, including the documented
// carve-out: a REDIS_HOST pointing at a REMOTE server needs no local
// Redis service started.
func TestNeedsRedis_Bad(t *testing.T) {
	cases := []struct {
		name string
		env  string
	}{
		{"no_redis_at_all", "CACHE_DRIVER=file\nQUEUE_CONNECTION=sync\n"},
		{"remote_redis_host", "REDIS_HOST=redis.example.com\n"},
		{"empty_env", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{".env": tc.env})
			if needsRedis(dir) {
				t.Fatal("needsRedis = true, want false")
			}
		})
	}

	if needsRedis(t.TempDir()) {
		t.Fatal("needsRedis = true with no .env file")
	}
}

// TestNeedsRedis_Ugly asserts commented-out configuration is ignored —
// a commented REDIS_HOST is the single most common thing in a stock
// Laravel .env, and treating it as live would start Redis for every
// project that never uses it.
func TestNeedsRedis_Ugly(t *testing.T) {
	dir := laravelFixture(t, map[string]string{
		".env": "# REDIS_HOST=127.0.0.1\n# CACHE_DRIVER=redis\nCACHE_DRIVER=file\n",
	})
	if needsRedis(dir) {
		t.Fatal("needsRedis = true for commented-out Redis configuration")
	}

	// A commented line followed by a live one must still be detected.
	live := laravelFixture(t, map[string]string{
		".env": "# REDIS_HOST=disabled\nQUEUE_CONNECTION=redis\n",
	})
	if !needsRedis(live) {
		t.Fatal("needsRedis = false; a live indicator after a comment must count")
	}
}

// TestHasVite_Good covers each recognised config filename.
func TestHasVite_Good(t *testing.T) {
	for _, name := range []string{"vite.config.js", "vite.config.ts", "vite.config.mjs", "vite.config.mts"} {
		t.Run(name, func(t *testing.T) {
			if !hasVite(laravelFixture(t, map[string]string{name: "export default {};"})) {
				t.Fatalf("hasVite = false for %s", name)
			}
		})
	}
}

// TestHasVite_Bad asserts unrecognised names do not count.
func TestHasVite_Bad(t *testing.T) {
	cases := map[string]string{
		"webpack.config.js": "module.exports = {};",
		"vite.config.json":  "{}",
		"vite.config":       "",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if hasVite(laravelFixture(t, map[string]string{name: body})) {
				t.Fatalf("hasVite = true for %s", name)
			}
		})
	}
	if hasVite(t.TempDir()) {
		t.Fatal("hasVite = true for an empty directory")
	}
}

// TestHasHorizon covers both answers for the Horizon config probe.
func TestHasHorizon(t *testing.T) {
	if !hasHorizon(laravelFixture(t, map[string]string{"config/horizon.php": "<?php return [];"})) {
		t.Fatal("hasHorizon = false with config/horizon.php present")
	}
	if hasHorizon(t.TempDir()) {
		t.Fatal("hasHorizon = true for an empty directory")
	}
	// A DIRECTORY at the config path is not a config file, but the probe
	// is existence-based; pin whichever answer ships so a change is visible.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config", "horizon.php"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasHorizon(dir) {
		t.Fatal("hasHorizon = false for a directory at the config path; the probe is existence-based")
	}
}

// TestHasReverb covers both answers for the Reverb config probe.
func TestHasReverb(t *testing.T) {
	if !hasReverb(laravelFixture(t, map[string]string{"config/reverb.php": "<?php return [];"})) {
		t.Fatal("hasReverb = false with config/reverb.php present")
	}
	if hasReverb(t.TempDir()) {
		t.Fatal("hasReverb = true for an empty directory")
	}
}

// TestExtractDomainFromURL_Good covers the stripping the function does.
func TestExtractDomainFromURL_Good(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://lethean.test", "lethean.test"},
		{"http://lethean.test", "lethean.test"},
		{"https://lethean.test/path/to/thing", "lethean.test"},
		{"http://localhost:8000", "localhost"},
		{"https://lethean.test:8443/admin", "lethean.test"},
		{"lethean.test", "lethean.test"},
		{"lethean.test/path", "lethean.test"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := ExtractDomainFromURL(tc.in); got != tc.want {
				t.Fatalf("ExtractDomainFromURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractDomainFromURL_Bad asserts degenerate input does not panic
// and yields something a caller can test for emptiness.
func TestExtractDomainFromURL_Bad(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"https://", ""},
		{"http://", ""},
		{"/just/a/path", ""},
		{":8000", ""},
	}
	for _, tc := range cases {
		t.Run("input_"+tc.in, func(t *testing.T) {
			if got := ExtractDomainFromURL(tc.in); got != tc.want {
				t.Fatalf("ExtractDomainFromURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTrimQuotes covers the helper's three shapes plus the mismatched
// case, which must not strip.
func TestTrimQuotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"quoted"`, "quoted"},
		{`'quoted'`, "quoted"},
		{"bare", "bare"},
		{`"`, ""},
		{``, ""},
		{`"unbalanced`, "unbalanced"},
		{`unbalanced"`, "unbalanced"},
		{`""double""`, "double"},
	}
	for _, tc := range cases {
		t.Run("input_"+tc.in, func(t *testing.T) {
			if got := trimQuotes(tc.in); got != tc.want {
				t.Fatalf("trimQuotes(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestProbeExists covers the os-level probe that replaced the confined
// medium: present, absent, a directory, and each through a symlink.
func TestProbeExists(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "artisan")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
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
		{"directory", base, true},
		{"absent", filepath.Join(base, "nope"), false},
		{"empty_path", "", false},
		{"file_via_symlink", filepath.Join(link, "artisan"), true},
		{"absent_via_symlink", filepath.Join(link, "nope"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := probeExists(tc.path); got != tc.want {
				t.Fatalf("probeExists(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestProbeRead covers the read probe, including through a symlink,
// which is the case the confined medium refuses.
func TestProbeRead(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "composer.json")
	body := `{"require":{}}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if got, ok := probeRead(file); !ok || got != body {
		t.Fatalf("probeRead(direct) = %q, %v; want %q, true", got, ok, body)
	}
	if got, ok := probeRead(filepath.Join(link, "composer.json")); !ok || got != body {
		t.Fatalf("probeRead(via symlink) = %q, %v; want %q, true", got, ok, body)
	}
	if _, ok := probeRead(filepath.Join(base, "absent")); ok {
		t.Fatal("probeRead(absent) reported success")
	}
	if _, ok := probeRead(base); ok {
		t.Fatal("probeRead(directory) reported success")
	}
}

// TestGetLaravelAppName_Ugly pins the parsing edge cases a real .env
// produces: the first assignment wins, a commented assignment is ignored,
// and a longer key that merely starts with the same letters must not
// match.
func TestGetLaravelAppName_Ugly(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"duplicate_first_wins", "APP_NAME=First\nAPP_NAME=Second\n", "First"},
		{"commented_out_ignored", "# APP_NAME=Ghost\nAPP_NAME=Real\n", "Real"},
		{"only_commented", "# APP_NAME=Ghost\n", ""},
		{"prefix_collision", "APP_NAME_SUFFIX=nope\nAPP_NAME=Real\n", "Real"},
		{"value_contains_equals", "APP_NAME=a=b\n", "a=b"},
		{"value_with_inner_quotes", `APP_NAME="say ""hi"""` + "\n", `say ""hi`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{".env": tc.env})
			if got := GetLaravelAppName(dir); got != tc.want {
				t.Fatalf("GetLaravelAppName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGetLaravelAppURL_Ugly mirrors the name cases for APP_URL.
func TestGetLaravelAppURL_Ugly(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"duplicate_first_wins", "APP_URL=https://first.test\nAPP_URL=https://second.test\n", "https://first.test"},
		{"commented_out_ignored", "# APP_URL=https://ghost.test\nAPP_URL=https://real.test\n", "https://real.test"},
		{"prefix_collision", "APP_URL_ALT=https://nope.test\nAPP_URL=https://real.test\n", "https://real.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laravelFixture(t, map[string]string{".env": tc.env})
			if got := GetLaravelAppURL(dir); got != tc.want {
				t.Fatalf("GetLaravelAppURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractDomainFromURL_Ugly pins the order the strips happen in.
// Port-before-path matters: a URL carrying both must lose both, and a
// path segment containing a colon must not be mistaken for a port.
func TestExtractDomainFromURL_Ugly(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"port_and_path", "https://lethean.test:8443/admin/users", "lethean.test"},
		{"path_before_port_like_segment", "https://lethean.test/a:b/c", "lethean.test"},
		{"trailing_slash", "https://lethean.test/", "lethean.test"},
		{"query_string", "https://lethean.test/?x=1", "lethean.test"},
		// Both prefixes are stripped in turn, so a doubled scheme still
		// yields the host rather than the inner scheme.
		{"double_scheme", "https://http://lethean.test", "lethean.test"},
		{"uppercase_scheme_not_stripped", "HTTPS://lethean.test", "HTTPS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractDomainFromURL(tc.in); got != tc.want {
				t.Fatalf("ExtractDomainFromURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
