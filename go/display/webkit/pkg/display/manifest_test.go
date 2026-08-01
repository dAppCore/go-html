package display

import (
	"syscall"

	core "dappco.re/go"
	"sync"
)

func TestInjectAppPreloads_FromManifest(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "preload.js"), "globalThis.__manifestLoaded = true;", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, ".core", "view.yaml"), "preloads:\n  - path: preload.js\n", 0o644))

	svc, err := New()
	core.RequireNoError(t, err)

	script, err := svc.injectAppPreloads(core.PathJoin(root, "index.html"))
	core.RequireNoError(t, err)
	core.RequireTrue(t, core.Contains(script, "__manifestLoaded"))
}

func TestInjectAppPreloads_RejectsTraversal(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "preload.js"), "globalThis.__manifestLoaded = true;", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, ".core", "view.yaml"), "preloads:\n  - path: ../preload.js\n", 0o644))

	svc, err := New()
	core.RequireNoError(t, err)

	_, err = svc.injectAppPreloads(core.PathJoin(root, "index.html"))
	core.AssertError(t, err)
}

func TestManifest_SafeManifestPreloadPath_GoodCase(t *core.T) {
	root := t.TempDir()
	target := core.PathJoin(root, "preload.js")
	core.RequireNoError(t, coreWriteMode(target, "globalThis.ready = true;", 0o644))
	expected, err := pathEvalSymlinks(target)
	core.RequireNoError(t, err)
	got, err := safeManifestPreloadPath(root, "preload.js")

	core.RequireNoError(t, err)
	core.AssertEqual(t, expected, got)
}

func TestManifest_SafeManifestPreloadPath_BadCase(t *core.T) {
	root := t.TempDir()
	_, err := safeManifestPreloadPath(root, "")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "empty")
}

func TestManifest_SafeManifestPreloadPath_UglyCase(t *core.T) {
	root := t.TempDir()
	_, err := safeManifestPreloadPath(root, "../preload.js")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "escapes")
}

func TestManifest_SafeManifestPreloadPath_RejectsSymlinkEscape(t *core.T) {
	root := t.TempDir()
	outside := t.TempDir()
	core.RequireNoError(t, coreWriteMode(core.PathJoin(outside, "preload.js"), "globalThis.__outside = true;", 0o644))
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, "assets")))
	if err := syscall.Symlink(outside, core.PathJoin(root, "assets", "linked")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	_, err := safeManifestPreloadPath(core.PathJoin(root, "assets"), core.PathJoin("linked", "preload.js"))

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "escapes")
}

func TestManifest_DiscoverManifestPath_GoodCase(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	manifestPath := core.PathJoin(root, ".core", "view.yaml")
	core.RequireNoError(t, coreWriteMode(manifestPath, "name: demo\n", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))

	got, err := discoverManifestPath(core.PathJoin(root, "index.html"))

	core.RequireNoError(t, err)
	core.AssertEqual(t, manifestPath, got)
}

func TestManifest_DiscoverManifestPath_BadCase(t *core.T) {
	_, err := discoverManifestPath(core.PathJoin(t.TempDir(), "missing.html"))

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "not found")
}

func TestManifest_DiscoverManifestPath_UglyCase(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	manifestPath := core.PathJoin(root, ".core", "view.yaml")
	core.RequireNoError(t, coreWriteMode(manifestPath, "name: remote\n", 0o644))

	got, err := discoverManifestPath(root)

	core.RequireNoError(t, err)
	core.AssertEqual(t, manifestPath, got)
}

func TestManifest_DiscoverManifestPath_RemoteHost_GoodCase(t *core.T) {
	home := t.TempDir()
	t.Setenv("DIR_HOME", home)
	manifestPath := core.PathJoin(home, ".core", "apps", "example.com", ".core", "view.yaml")
	core.RequireNoError(t, coreEnsureDir(core.PathDir(manifestPath)))
	core.RequireNoError(t, coreWriteMode(manifestPath, "name: remote\n", 0o644))

	got, err := discoverManifestPath("https://example.com/index.html")

	core.RequireNoError(t, err)
	core.AssertEqual(t, manifestPath, got)
}

func TestManifest_DiscoverManifestPath_RemoteHost_StripsPort(t *core.T) {
	home := t.TempDir()
	t.Setenv("DIR_HOME", home)
	manifestPath := core.PathJoin(home, ".core", "apps", "example.com", ".core", "view.yaml")
	core.RequireNoError(t, coreEnsureDir(core.PathDir(manifestPath)))
	core.RequireNoError(t, coreWriteMode(manifestPath, "name: remote\n", 0o644))

	got, err := discoverManifestPath("https://example.com:8080/x")

	core.RequireNoError(t, err)
	core.AssertEqual(t, manifestPath, got)
}

func TestManifest_DiscoverManifestPath_RemoteHost_IPv6Literal(t *core.T) {
	home := t.TempDir()
	t.Setenv("DIR_HOME", home)
	manifestPath := core.PathJoin(home, ".core", "apps", "::1", ".core", "view.yaml")
	core.RequireNoError(t, coreEnsureDir(core.PathDir(manifestPath)))
	core.RequireNoError(t, coreWriteMode(manifestPath, "name: remote\n", 0o644))

	got, err := discoverManifestPath("https://[::1]/x")

	core.RequireNoError(t, err)
	core.AssertEqual(t, manifestPath, got)
}

func TestManifest_DiscoverManifestPath_RemoteHost_RejectsControlCharacter(t *core.T) {
	t.Setenv("DIR_HOME", t.TempDir())

	_, err := discoverManifestPath("https://bad\nhost/x")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "control")
}

func TestManifest_DiscoverManifestPath_RemoteHost_RejectsTraversalHost(t *core.T) {
	t.Setenv("DIR_HOME", t.TempDir())

	_, err := discoverManifestPath("https://../x")

	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "relative path")
}

func TestManifest_ManifestWindowConfig_GoodCase(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, ".core", "view.yaml"), core.Join("\n", []string{
		"windows:",
		"  main:",
		"    title: Core GUI",
		"    width: 1280",
		"    height: 720",
		"    preload: true",
	}...), 0o644))

	svc, err := New()
	core.RequireNoError(t, err)

	got := svc.manifestWindowConfig(core.PathJoin(root, "index.html"))

	core.AssertNotNil(t, got)
	core.AssertContains(t, got, "main")
	core.AssertEqual(t, "Core GUI", got["main"].Title)
	core.AssertEqual(t, 1280, got["main"].Width)
	core.AssertEqual(t, 720, got["main"].Height)
	core.AssertTrue(t, got["main"].Preload)
}

func TestManifest_ManifestWindowConfig_BadCase(t *core.T) {
	svc, err := New()
	core.RequireNoError(t, err)

	got := svc.manifestWindowConfig(core.PathJoin(t.TempDir(), "missing.html"))

	core.AssertNil(t, got)
}

func TestManifest_ManifestWindowConfig_UglyCase(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, ".core", "view.yaml"), "windows: [\n", 0o644))

	svc, err := New()
	core.RequireNoError(t, err)

	got := svc.manifestWindowConfig(core.PathJoin(root, "index.html"))

	core.AssertNil(t, got)
}

func TestManifest_ManifestWindowConfig_ReturnsCopy(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, ".core", "view.yaml"), core.Join("\n", []string{
		"windows:",
		"  main:",
		"    title: Core GUI",
		"    width: 1280",
		"    height: 720",
	}...), 0o644))

	svc, err := New()
	core.RequireNoError(t, err)

	first := svc.manifestWindowConfig(core.PathJoin(root, "index.html"))
	core.AssertNotNil(t, first)
	first["main"] = ManifestWindow{Title: "mutated"}

	second := svc.manifestWindowConfig(core.PathJoin(root, "index.html"))
	core.AssertNotNil(t, second)
	core.AssertEqual(t, "Core GUI", second["main"].Title)
}

func TestManifest_LoadManifestForOrigin_RejectsOversizedFile(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, ".core", "view.yaml"), "name: "+repeatString("a", maxViewManifestBytes), 0o644))

	svc, err := New()
	core.RequireNoError(t, err)

	_, err = svc.loadManifestForOrigin(core.PathJoin(root, "index.html"))
	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "exceeds")
}

func TestManifest_LoadManifestForOrigin_Concurrent(t *core.T) {
	root := t.TempDir()
	core.RequireNoError(t, coreEnsureDir(core.PathJoin(root, ".core")))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, "index.html"), "<html></html>", 0o644))
	core.RequireNoError(t, coreWriteMode(core.PathJoin(root, ".core", "view.yaml"), core.Join("\n", []string{
		"name: demo",
		"windows:",
		"  main:",
		"    title: Core GUI",
	}...), 0o644))

	svc, err := New()
	core.RequireNoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan resultFailure, 16)
	for range 16 {
		wg.Go(func() {
			loaded, loadErr := svc.loadManifestForOrigin(core.PathJoin(root, "index.html"))
			if loadErr != nil {
				errs <- loadErr
				return
			}
			if loaded == nil || loaded.Manifest.Name != "demo" {
				errs <- core.AnError
			}
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		core.RequireNoError(t, err)
	}
}

func TestManifest_ManifestBaseDir_GoodCase(t *core.T) {
	core.AssertEqual(t, "/tmp/app", manifestBaseDir("/tmp/app/.core/view.yaml"))
	core.AssertEqual(t, "/tmp/app/assets", manifestBaseDir("/tmp/app/assets/view.yaml"))
	core.AssertNotEmpty(t, core.Sprintf("%T", manifestBaseDir("/tmp/app/.core/view.yaml")))
}

func TestManifest_ManifestBaseDir_BadCase(t *core.T) {
	core.AssertEqual(t, ".", manifestBaseDir(".core/view.yaml"))
	observedType := core.Sprintf("%T", manifestBaseDir(".core/view.yaml"))
	core.AssertNotEmpty(t, observedType)
}

func TestManifest_ManifestBaseDir_UglyCase(t *core.T) {
	core.AssertEqual(t, "/", manifestBaseDir("/.core/view.yaml"))
	observedType := core.Sprintf("%T", manifestBaseDir("/.core/view.yaml"))
	core.AssertNotEmpty(t, observedType)
}

func TestManifest_SafeManifestRelativePath_GoodCase(t *core.T) {
	root := t.TempDir()
	target := core.PathJoin(root, "preload.js")
	core.RequireNoError(t, coreWriteMode(target, "globalThis.ready = true;", 0o644))
	expected, err := pathEvalSymlinks(target)
	core.RequireNoError(t, err)

	got, err := safeManifestRelativePath(root, "preload.js", "preload path")

	core.RequireNoError(t, err)
	core.AssertEqual(t, expected, got)
}

func TestManifest_SafeManifestRelativePath_BadCase(t *core.T) {
	root := t.TempDir()

	_, err := safeManifestRelativePath(root, "", "preload path")
	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "empty")
}

func TestManifest_SafeManifestRelativePath_Bad_MissingFile(t *core.T) {
	root := t.TempDir()

	_, err := safeManifestRelativePath(root, "missing.js", "preload path")

	core.AssertError(t, err)
}

func TestManifest_SafeManifestRelativePath_UglyCase(t *core.T) {
	root := t.TempDir()

	_, err := safeManifestRelativePath(root, "../escape.js", "preload path")
	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "escapes")
}
