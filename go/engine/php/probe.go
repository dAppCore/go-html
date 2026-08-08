// SPDX-Licence-Identifier: EUPL-1.2

package php

import core "dappco.re/go"

// Project probing deliberately bypasses the coreio medium.
//
// Since go-io v0.15.2 the local medium routes through os.Root, which by
// design refuses any path leaving its root — including one that merely
// traverses a symlink. That is correct for sandboxed media and wrong
// here: these functions probe an arbitrary user-supplied project
// directory, which is unbounded by construction. Handed a symlinked
// project path — ~/Sites/app pointing at a volume, a git worktree, a
// bind-mounted container path — the medium reported every file absent,
// so IsLaravelProject returned false for a Laravel project and
// GetLaravelAppName returned "". Silently: no error, just a wrong answer.
//
// Same shape and same fix as engine/php/workspace.go. go-io is correct
// and untouched; coreio media stay in place for sandboxed I/O.

// probeExists reports whether path exists, following symlinks.
func probeExists(path string) bool {
	return core.Stat(path).OK
}

// probeRead reads path in full, following symlinks. The bool is false
// when the file cannot be read, mirroring the medium's (content, error)
// shape at the call sites.
func probeRead(path string) (string, bool) {
	result := core.ReadFile(path)
	if !result.OK {
		return "", false
	}
	data, ok := result.Value.([]byte)
	if !ok {
		return "", false
	}
	return string(data), true
}
