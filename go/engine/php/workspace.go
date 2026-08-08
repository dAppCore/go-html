package php

import (
	core "dappco.re/go"
	"gopkg.in/yaml.v3"
)

// workspaceConfig holds workspace-level configuration from .core/workspace.yaml.
type workspaceConfig struct {
	Version     int      `yaml:"version"`
	Active      string   `yaml:"active"`       // Active package name
	DefaultOnly []string `yaml:"default_only"` // Default types for setup
	PackagesDir string   `yaml:"packages_dir"` // Where packages are cloned
}

// defaultWorkspaceConfig returns a config with default values.
func defaultWorkspaceConfig() *workspaceConfig {
	return &workspaceConfig{
		Version:     1,
		PackagesDir: "./packages",
	}
}

// loadWorkspaceConfig tries to load workspace.yaml from the given directory's .core subfolder.
// Returns nil if no config file exists.
//
// The probe walks UP an arbitrary ancestor chain, so it is deliberately
// not coreio.Local: since go-io v0.15.2 the local medium routes through
// os.Root, which by design refuses any path leaving its root — including
// one that merely traverses a symlink. Correct for sandboxed media,
// wrong for a search that is unbounded by construction. Reached through
// a symlinked project directory (an ordinary macOS layout), Local.Read
// fails "path escapes from parent" AND Local.IsFile returns false, so
// this walked past a config that exists and reported the workspace as
// unconfigured. The os-level core.Stat / core.ReadFile are the right
// tool for probing; coreio media stay correct for sandboxed I/O.
func loadWorkspaceConfig(dir string) (*workspaceConfig, error) { // Result boundary
	path := core.PathJoin(dir, ".core", "workspace.yaml")
	readResult := core.ReadFile(path)
	if !readResult.OK {
		if stat := core.Stat(path); !stat.OK {
			parent := core.PathDir(dir)
			if parent != dir {
				return loadWorkspaceConfig(parent)
			}
			return nil, nil
		}
		return nil, core.Errorf("failed to read workspace config: %w", readResult.Err())
	}
	data := string(readResult.Value.([]byte))

	config := defaultWorkspaceConfig()
	if err := yaml.Unmarshal([]byte(data), config); err != nil {
		return nil, core.Errorf("failed to parse workspace config: %w", err)
	}

	if config.Version != 1 {
		return nil, core.Errorf("unsupported workspace config version: %d", config.Version)
	}

	return config, nil
}

// findWorkspaceRoot searches for the root directory containing .core/workspace.yaml.
func findWorkspaceRoot() (string, error) { // Result boundary
	cwdResult := core.Getwd()
	if !cwdResult.OK {
		err, _ := cwdResult.Value.(error)
		return "", err
	}
	dir, _ := cwdResult.Value.(string)

	for {
		// os-level stat, for the same reason as loadWorkspaceConfig: this
		// walks up to the filesystem root, so no medium root can contain it.
		if isRegularFile(core.PathJoin(dir, ".core", "workspace.yaml")) {
			return dir, nil
		}

		parent := core.PathDir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", core.Errorf("not in a workspace")
}

// isRegularFile reports whether path names an existing non-directory,
// following symlinks. Uses core.Stat (os.Stat) rather than a coreio
// medium because the callers probe ancestor directories that no medium
// root contains — see loadWorkspaceConfig.
func isRegularFile(path string) bool {
	result := core.Stat(path)
	if !result.OK {
		return false
	}
	info, ok := result.Value.(core.FsFileInfo)
	return ok && !info.IsDir()
}
