package display

import (
	"io"
	"maps"
	"net"
	"net/url"

	core "dappco.re/go"
	"gopkg.in/yaml.v3"
)

const maxViewManifestBytes = 1 << 20

type ViewManifest struct {
	AppID       string                    `yaml:"app_id" json:"app_id"`
	Name        string                    `yaml:"name" json:"name"`
	Preloads    []ManifestPreload         `yaml:"preloads" json:"preloads"`
	Windows     map[string]ManifestWindow `yaml:"windows" json:"windows"`
	HLCRF       []HLCRFTemplate           `yaml:"hlcrf" json:"hlcrf"`
	Permissions []string                  `yaml:"permissions" json:"permissions"`
}

type ManifestPreload struct {
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
	Inline  string `yaml:"inline" json:"inline"`
	Enabled *bool  `yaml:"enabled" json:"enabled,omitempty"`
}

type ManifestWindow struct {
	Title   string `yaml:"title" json:"title"`
	Width   int    `yaml:"width" json:"width"`
	Height  int    `yaml:"height" json:"height"`
	Preload bool   `yaml:"preload" json:"preload"`
}

type HLCRFTemplate struct {
	Name     string `yaml:"name" json:"name"`
	Tag      string `yaml:"tag" json:"tag"`
	Template string `yaml:"template" json:"template"`
}

type loadedManifest struct {
	Path     string
	BaseDir  string
	Manifest ViewManifest
}

func (s *Service) loadManifestForOrigin(pageURL string) (*loadedManifest, resultFailure) {
	s.manifestMu.Lock()
	defer s.manifestMu.Unlock()
	if s.manifestCache == nil {
		s.manifestCache = make(map[string]*loadedManifest)
	}
	if cached, ok := s.manifestCache[pageURL]; ok {
		return cached, nil
	}

	path, err := discoverManifestPath(pageURL)
	if err != nil {
		return nil, err
	}
	stream := (&core.Fs{}).NewUnrestricted().Open(path)
	if !stream.OK {
		return nil, coreResultError(stream, "failed to open view manifest")
	}
	reader, ok := stream.Value.(io.Reader)
	if !ok {
		core.CloseStream(stream.Value)
		return nil, core.E("display.loadManifestForOrigin", "view manifest stream is not readable", nil)
	}
	defer core.CloseStream(stream.Value)
	body, err := io.ReadAll(io.LimitReader(reader, maxViewManifestBytes+1))
	if err != nil {
		return nil, core.E("display.loadManifestForOrigin", "failed to read view manifest", err)
	}
	if len(body) > maxViewManifestBytes {
		return nil, core.E("display.loadManifestForOrigin", "view manifest exceeds 1048576 bytes", nil)
	}
	var manifest ViewManifest
	if err := yaml.Unmarshal(body, &manifest); err != nil {
		return nil, core.E("display.loadManifestForOrigin", "failed to parse view manifest", err)
	}
	loaded := &loadedManifest{
		Path:     path,
		BaseDir:  manifestBaseDir(path),
		Manifest: manifest,
	}

	s.manifestCache[pageURL] = loaded
	return loaded, nil
}

func manifestBaseDir(manifestPath string) string {
	baseDir := core.PathDir(manifestPath)
	if core.PathBase(baseDir) == ".core" {
		return core.PathDir(baseDir)
	}
	return baseDir
}

func safeManifestPreloadPath(baseDir, preloadPath string) (string, resultFailure) {
	return safeManifestRelativePath(baseDir, preloadPath, "preload path")
}

func safeManifestRelativePath(baseDir, relativePath, label string) (string, resultFailure) {
	trimmed := core.Trim(relativePath)
	if trimmed == "" {
		return "", core.E("display.safeManifestRelativePath", label+" is empty", nil)
	}
	if core.PathIsAbs(trimmed) {
		return "", core.E("display.safeManifestRelativePath", label+" must be relative", nil)
	}

	baseAbs, err := pathAbs(baseDir)
	if err != nil {
		return "", core.E("display.safeManifestRelativePath", "failed to resolve manifest base directory", err)
	}
	baseResolved, err := pathEvalSymlinks(baseAbs)
	if err != nil {
		return "", core.E("display.safeManifestRelativePath", "failed to resolve manifest base symlinks", err)
	}
	candidateAbs, err := pathAbs(core.PathJoin(baseAbs, trimmed))
	if err != nil {
		return "", core.E("display.safeManifestRelativePath", "failed to resolve manifest relative path", err)
	}
	rel, err := pathRel(baseAbs, candidateAbs)
	if err != nil {
		return "", core.E("display.safeManifestRelativePath", "failed to compare manifest relative path", err)
	}
	if rel == ".." || core.HasPrefix(rel, ".."+string(core.PathSeparator)) {
		return "", core.E("display.safeManifestRelativePath", label+" escapes manifest directory", nil)
	}
	if !(&core.Fs{}).NewUnrestricted().Exists(candidateAbs).OK {
		return "", core.E("display.safeManifestRelativePath", label+" does not exist", coreResultError(core.Stat(candidateAbs), label+" does not exist"))
	}
	candidateResolved, err := pathEvalSymlinks(candidateAbs)
	if err != nil {
		return "", core.E("display.safeManifestRelativePath", "failed to resolve manifest relative path symlinks", err)
	}
	rel, err = pathRel(baseResolved, candidateResolved)
	if err != nil {
		return "", core.E("display.safeManifestRelativePath", "failed to compare resolved manifest relative path", err)
	}
	if rel == ".." || core.HasPrefix(rel, ".."+string(core.PathSeparator)) {
		return "", core.E("display.safeManifestRelativePath", label+" escapes manifest directory", nil)
	}
	return candidateResolved, nil
}

func discoverManifestPath(pageURL string) (string, resultFailure) {
	trimmed := core.Trim(pageURL)
	fsys := (&core.Fs{}).NewUnrestricted()
	candidates := make([]string, 0, 4)
	if pathVolumeName(trimmed) != "" {
		appendLocalManifestCandidates(&candidates, fsys, trimmed)
	} else {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", err
		}
		switch parsed.Scheme {
		case "", "file":
			path := parsed.Path
			if path == "" {
				path = trimmed
			}
			appendLocalManifestCandidates(&candidates, fsys, path)
		default:
			if parsed.Host != "" {
				host, err := manifestHostPathComponent(parsed)
				if err != nil {
					return "", err
				}
				home := core.Trim(core.Getenv("DIR_HOME"))
				if home == "" {
					home = core.Trim(core.Env("DIR_HOME"))
				}
				if home != "" {
					candidates = append(candidates, core.PathJoin(home, ".core", "apps", host, ".core", "view.yaml"))
				}
			}
		}
	}
	for _, candidate := range candidates {
		if fsys.Exists(candidate).OK {
			return candidate, nil
		}
	}
	return "", core.E("display.discoverManifestPath", "view manifest not found", nil)
}

func appendLocalManifestCandidates(candidates *[]string, fsys *core.Fs, path string) {
	if candidates == nil || fsys == nil || !fsys.Exists(path).OK {
		return
	}
	if fsys.IsDir(path).OK {
		*candidates = append(*candidates, core.PathJoin(path, ".core", "view.yaml"))
		return
	}
	dir := core.PathDir(path)
	*candidates = append(*candidates, core.PathJoin(dir, ".core", "view.yaml"))
	*candidates = append(*candidates, core.PathJoin(core.PathDir(dir), ".core", "view.yaml"))
}

func manifestHostPathComponent(parsed *url.URL) (string, resultFailure) {
	host := parsed.Hostname()
	if host == "" {
		return "", core.E("display.manifestHostPathComponent", "manifest host is empty", nil)
	}
	if err := validateManifestHostPathComponent(host); err != nil {
		return "", err
	}
	return host, nil
}

func validateManifestHostPathComponent(host string) resultFailure {
	for i := 0; i < len(host); i++ {
		if host[i] < 0x20 || host[i] == 0x7f {
			return core.E("display.validateManifestHostPathComponent", "manifest host contains control character", nil)
		}
	}
	if containsAny(host, "[]") {
		return core.E("display.validateManifestHostPathComponent", "manifest host contains IPv6 brackets", nil)
	}
	if host == "." || host == ".." || core.HasPrefix(host, "../") || core.Contains(host, "/../") {
		return core.E("display.validateManifestHostPathComponent", "manifest host contains relative path segment", nil)
	}
	if containsAny(host, `/\`) {
		return core.E("display.validateManifestHostPathComponent", "manifest host contains path separator", nil)
	}
	if core.Contains(host, ":") && net.ParseIP(host) == nil {
		return core.E("display.validateManifestHostPathComponent", "manifest host contains invalid colon", nil)
	}
	return nil
}

func containsAny(value, chars string) bool {
	for _, char := range chars {
		if core.Contains(value, string(char)) {
			return true
		}
	}
	return false
}

func (s *Service) manifestWindowConfig(pageURL string) map[string]ManifestWindow {
	loaded, err := s.loadManifestForOrigin(pageURL)
	if err != nil || loaded == nil {
		return nil
	}
	if len(loaded.Manifest.Windows) == 0 {
		return nil
	}
	windows := make(map[string]ManifestWindow, len(loaded.Manifest.Windows))
	maps.Copy(windows, loaded.Manifest.Windows)
	return windows
}

func (s *Service) readManifestPreload(baseDir, preloadPath string) ([]byte, resultFailure) {
	resolvedPath, err := safeManifestPreloadPath(baseDir, preloadPath)
	if err != nil {
		return nil, err
	}
	result := (&core.Fs{}).NewUnrestricted().Read(resolvedPath)
	if !result.OK {
		return nil, coreResultError(result, "failed to read manifest preload")
	}
	content, ok := result.Value.(string)
	if !ok {
		return nil, core.E("display.readManifestPreload", "manifest preload content is not text", nil)
	}
	return []byte(content), nil
}
