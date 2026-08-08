// SPDX-Licence-Identifier: EUPL-1.2

package webkit

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	core "dappco.re/go"
)

// callByNamePattern matches a literal first argument to the wails
// runtime's ByName call, in any of the shapes a frontend writes it:
//
//	Call.ByName('pkg.Type.Method')      — the @wailsio/runtime namespace import
//	Calls.ByName("pkg.Type.Method")     — an aliased namespace
//	ByName(`pkg.Type.Method`)           — a named import
//
// Only string LITERALS are matched. A call assembled at runtime
// (ByName(prefix + name)) cannot be checked statically and is reported
// separately by ScanCallByName so it never masquerades as verified.
var callByNamePattern = regexp.MustCompile(`(?:\b[A-Za-z_$][\w$]*\s*\.\s*)?\bByName\s*\(\s*(?:('[^']*')|("[^"]*")|(` + "`[^`]*`" + `))`)

// dynamicByNamePattern matches a ByName call whose first argument is not
// a plain string literal.
var dynamicByNamePattern = regexp.MustCompile(`(?:\b[A-Za-z_$][\w$]*\s*\.\s*)?\bByName\s*\(\s*[^'"` + "`" + `\s)]`)

// scanExtensions are the frontend source suffixes worth reading. Angular
// projects put call strings in .ts; generated bindings and plain scripts
// land in .js/.mjs.
var scanExtensions = map[string]bool{
	".ts":  true,
	".tsx": true,
	".js":  true,
	".mjs": true,
	".mts": true,
}

// scanSkipDirs are directory names never worth walking. node_modules
// carries the runtime's own ByName definition (and megabytes of noise);
// build output duplicates sources that were already scanned.
var scanSkipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".angular":     true,
	".git":         true,
	"coverage":     true,
}

// CallSites is the result of scanning a frontend tree for wails binding
// calls.
type CallSites struct {
	// Names are the distinct fully-qualified binding names the frontend
	// passes to Call.ByName as string literals, sorted. Feed these to
	// UnresolvedBindingNames.
	Names []string

	// Files maps each name to the source files that call it, sorted.
	// Used to point a failing drift gate at the code to fix.
	Files map[string][]string

	// Dynamic lists files containing a ByName call whose argument is
	// not a string literal. Those calls cannot be verified statically —
	// the list exists so a gate can report the blind spot rather than
	// imply full coverage.
	Dynamic []string
}

// ScanCallByName walks a frontend source tree and collects every
// fully-qualified binding name passed literally to the wails runtime's
// Call.ByName.
//
// It is the frontend half of the drift gate: pair it with
// UnresolvedBindingNames and a rename on either side of the seam fails a
// test instead of a user's click.
//
//	sites, err := webkit.ScanCallByName(os.DirFS("frontend/src"))
//	missing := webkit.UnresolvedBindingNames(sites.Names, runnerSvc)
//
// node_modules, dist, .angular, .git and coverage are skipped. Only
// .ts/.tsx/.js/.mjs/.mts files are read. An unreadable individual file
// is a hard error rather than a silent omission — a gate that skips what
// it cannot read reports a false pass.
func ScanCallByName(fsys fs.FS) (CallSites, error) {
	sites := CallSites{Files: make(map[string][]string)}
	if fsys == nil {
		return sites, core.E("webkit.ScanCallByName", "nil filesystem", nil)
	}

	dynamic := make(map[string]bool)
	walkErr := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return core.E("webkit.ScanCallByName", "walk "+name, err)
		}
		if entry.IsDir() {
			if name != "." && scanSkipDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !scanExtensions[strings.ToLower(path.Ext(name))] {
			return nil
		}

		content, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			return core.E("webkit.ScanCallByName", "read "+name, readErr)
		}
		source := string(content)

		for _, match := range callByNamePattern.FindAllStringSubmatch(source, -1) {
			literal, ok := unquoteCallArgument(match)
			if !ok || literal == "" {
				continue
			}
			sites.Files[literal] = appendUnique(sites.Files[literal], name)
		}
		if dynamicByNamePattern.MatchString(source) {
			dynamic[name] = true
		}
		return nil
	})
	if walkErr != nil {
		return CallSites{Files: make(map[string][]string)}, walkErr
	}

	sites.Names = make([]string, 0, len(sites.Files))
	for name := range sites.Files {
		sites.Names = append(sites.Names, name)
		sort.Strings(sites.Files[name])
	}
	sort.Strings(sites.Names)

	sites.Dynamic = make([]string, 0, len(dynamic))
	for name := range dynamic {
		sites.Dynamic = append(sites.Dynamic, name)
	}
	sort.Strings(sites.Dynamic)

	return sites, nil
}

// unquoteCallArgument turns the matched quoted literal into its string
// value. Single- and back-quoted forms are stripped directly; the
// double-quoted form goes through strconv so JS escapes (".") match
// what the runtime actually sends.
func unquoteCallArgument(match []string) (string, bool) {
	switch {
	case match[1] != "":
		return strings.Trim(match[1], "'"), true
	case match[2] != "":
		if unquoted, err := strconv.Unquote(match[2]); err == nil {
			return unquoted, true
		}
		return strings.Trim(match[2], `"`), true
	case match[3] != "":
		return strings.Trim(match[3], "`"), true
	}
	return "", false
}

// appendUnique appends value to list unless already present. The lists
// are single-digit in practice, so the linear scan costs nothing.
func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
