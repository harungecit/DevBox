// Package discovery finds runtimes and services that are installed on the
// machine outside of DevBox (system installers, package managers, manual
// unzips) so they can be imported into DevBox's managed layout instead of
// being downloaded again.
//
// Detection is two-pronged: every directory on PATH is checked for the
// defining binary, and a per-OS list of well-known install locations
// (Program Files, nvm, scoop, Homebrew, ...) is globbed. Anything already
// living under the DevBox data directory is ignored — those are DevBox's own
// installs. Versions are probed by running the binary with its version flag
// (hidden window, bounded by a timeout).
package discovery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
	"DevBox/internal/runtime"
	"DevBox/internal/service"
)

// Found describes one external installation that DevBox can import.
type Found struct {
	Kind        string `json:"kind"` // "runtime" or "service"
	Name        string `json:"name"` // registry key: "node", "php", "mysql", ...
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	Path        string `json:"path"` // root directory to import from
	// Conflict is the display name of an installed DevBox service in the same
	// conflict group (e.g. MariaDB when a MySQL install is found). Import is
	// blocked until the conflicting service is uninstalled.
	Conflict string `json:"conflict"`
}

const probeTimeout = 8 * time.Second

// --- runtime specs ---

type runtimeSpec struct {
	name    string
	display string
	exe     string // base binary name without extension
	verArgs []string
	verRe   *regexp.Regexp
}

var runtimeSpecs = []runtimeSpec{
	{"node", "Node.js", "node", []string{"--version"}, regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)},
	{"go", "Go", "go", []string{"version"}, regexp.MustCompile(`go(\d+\.\d+(?:\.\d+)?)`)},
	{"php", "PHP", "php", []string{"-n", "-v"}, regexp.MustCompile(`PHP (\d+\.\d+\.\d+)`)},
	{"python", "Python", "python", []string{"--version"}, regexp.MustCompile(`Python (\d+\.\d+\.\d+)`)},
	{"rust", "Rust", "rustc", []string{"--version"}, regexp.MustCompile(`rustc (\d+\.\d+\.\d+)`)},
}

// --- service specs ---

type serviceSpec struct {
	name    string
	display string
	// exeRel lists candidate defining binaries relative to the install root,
	// without OS extension ("bin/mysqld", "nginx"). The first match wins.
	exeRel  []string
	verArgs []string
	verRe   *regexp.Regexp
	// reject: version output containing this substring disqualifies the hit
	// (MySQL scan must not swallow a MariaDB build shipping "mysqld").
	reject string
	// require: version output must contain this substring (MariaDB via "mysqld").
	require string
}

var serviceSpecs = []serviceSpec{
	{name: "nginx", display: "Nginx", exeRel: []string{"nginx", "sbin/nginx"}, verArgs: []string{"-v"}, verRe: regexp.MustCompile(`nginx/(\d+\.\d+\.\d+)`)},
	{name: "apache", display: "Apache", exeRel: []string{"bin/httpd"}, verArgs: []string{"-v"}, verRe: regexp.MustCompile(`Apache/(\d+\.\d+\.\d+)`)},
	{name: "caddy", display: "Caddy", exeRel: []string{"caddy"}, verArgs: []string{"version"}, verRe: regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)},
	{name: "postgres", display: "PostgreSQL", exeRel: []string{"bin/pg_ctl"}, verArgs: []string{"--version"}, verRe: regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)},
	{name: "mysql", display: "MySQL", exeRel: []string{"bin/mysqld"}, verArgs: []string{"--version"}, verRe: regexp.MustCompile(`Ver (\d+\.\d+\.\d+)`), reject: "MariaDB"},
	{name: "mariadb", display: "MariaDB", exeRel: []string{"bin/mariadbd", "bin/mysqld"}, verArgs: []string{"--version"}, verRe: regexp.MustCompile(`Ver (\d+\.\d+\.\d+)`), require: "MariaDB"},
	{name: "mongodb", display: "MongoDB", exeRel: []string{"bin/mongod"}, verArgs: []string{"--version"}, verRe: regexp.MustCompile(`db version v(\d+\.\d+\.\d+)`)},
	{name: "redis", display: "Redis", exeRel: []string{"redis-server", "bin/redis-server"}, verArgs: []string{"--version"}, verRe: regexp.MustCompile(`v=(\d+\.\d+\.\d+)`)},
	{name: "valkey", display: "Valkey", exeRel: []string{"valkey-server", "bin/valkey-server"}, verArgs: []string{"--version"}, verRe: regexp.MustCompile(`v=(\d+\.\d+\.\d+)`)},
	{name: "mailpit", display: "Mailpit", exeRel: []string{"mailpit"}, verArgs: []string{"version"}, verRe: regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)},
}

// ScanAll scans the machine for external runtime and service installations.
// Results exclude anything DevBox already manages: a runtime version that is
// already installed in DevBox, or a service that DevBox has installed at all.
func ScanAll() []Found {
	var out []Found
	out = append(out, ScanRuntimes()...)
	out = append(out, ScanServices()...)
	return out
}

// ScanRuntimes finds external runtime installations.
func ScanRuntimes() []Found {
	var out []Found
	for _, spec := range runtimeSpecs {
		roots := map[string]bool{}

		// PATH hits
		for _, exe := range findInPATH(platform.BinaryName(spec.exe)) {
			if root := runtimeRootFromExe(exe); root != "" {
				roots[normPath(root)] = true
			}
		}
		// Well-known locations
		for _, root := range expandGlobs(runtimeCandidateRoots(spec.name)) {
			roots[normPath(root)] = true
		}
		// rustup: resolve the real toolchain root through the shim
		if spec.name == "rust" {
			for root := range roots {
				if sysroot := rustSysroot(root); sysroot != "" {
					delete(roots, root)
					roots[normPath(sysroot)] = true
				}
			}
		}

		for _, root := range sortedSlice(roots) {
			if f, ok := probeRuntime(spec, root); ok {
				out = append(out, f)
			}
		}
	}
	return dedupeByVersion(out)
}

// ScanServices finds external service installations.
func ScanServices() []Found {
	var out []Found
	for _, spec := range serviceSpecs {
		// Skip services that are not registered on this OS (e.g. Valkey on Windows).
		mgr, registered := service.Registry[spec.name]
		if !registered {
			continue
		}
		// DevBox already manages this service — nothing to import.
		if mgr.IsInstalled() {
			continue
		}

		roots := map[string]bool{}
		for _, rel := range spec.exeRel {
			base := platform.BinaryName(filepath.Base(rel))
			relDir := filepath.Dir(rel)
			for _, exe := range findInPATH(base) {
				root := filepath.Dir(exe)
				if relDir != "." {
					// binary lives in a subdir (bin/) — root is one level up
					if strings.EqualFold(filepath.Base(root), relDir) {
						root = filepath.Dir(root)
					} else {
						continue
					}
				}
				roots[normPath(root)] = true
			}
		}
		for _, root := range expandGlobs(serviceCandidateRoots(spec.name)) {
			roots[normPath(root)] = true
		}

		conflict := service.GetConflictingService(spec.name)
		for _, root := range sortedSlice(roots) {
			if f, ok := probeService(spec, root); ok {
				f.Conflict = conflict
				out = append(out, f)
				break // one install per service is enough for the import UI
			}
		}
	}
	return out
}

// --- probing ---

func probeRuntime(spec runtimeSpec, root string) (Found, bool) {
	if root == "" || underDataDir(root) || isExcludedRoot(root) {
		return Found{}, false
	}
	exe := runtimeExeInRoot(spec.name, root, spec.exe)
	if exe == "" {
		return Found{}, false
	}
	// A Python virtualenv is a project artifact, not an installation.
	if spec.name == "python" {
		if _, err := os.Stat(filepath.Join(root, "pyvenv.cfg")); err == nil {
			return Found{}, false
		}
	}
	if !plausibleRuntimeRoot(spec.name, root) {
		return Found{}, false
	}
	outStr := versionOutput(exe, spec.verArgs...)
	ver := firstMatch(spec.verRe, outStr)
	if ver == "" {
		return Found{}, false
	}
	if runtimeVersionInstalled(spec.name, ver) {
		return Found{}, false
	}
	return Found{
		Kind:        "runtime",
		Name:        spec.name,
		DisplayName: spec.display,
		Version:     ver,
		Path:        root,
	}, true
}

func probeService(spec serviceSpec, root string) (Found, bool) {
	if root == "" || underDataDir(root) || isExcludedRoot(root) {
		return Found{}, false
	}
	exe := ""
	for _, rel := range spec.exeRel {
		cand := filepath.Join(root, filepath.Dir(rel), platform.BinaryName(filepath.Base(rel)))
		cand = filepath.Clean(cand)
		if _, err := os.Stat(cand); err == nil {
			exe = cand
			break
		}
	}
	if exe == "" {
		return Found{}, false
	}
	if !plausibleServiceRoot(spec.name, root) {
		return Found{}, false
	}
	outStr := versionOutput(exe, spec.verArgs...)
	if spec.reject != "" && strings.Contains(outStr, spec.reject) {
		return Found{}, false
	}
	if spec.require != "" && !strings.Contains(outStr, spec.require) {
		return Found{}, false
	}
	ver := firstMatch(spec.verRe, outStr)
	if ver == "" {
		return Found{}, false
	}
	return Found{
		Kind:        "service",
		Name:        spec.name,
		DisplayName: spec.display,
		Version:     ver,
		Path:        root,
	}, true
}

// runtimeExeInRoot returns the runtime's binary path inside an install root,
// or "" when the root doesn't look like an installation. Layouts follow what
// the DevBox managers expect: on Windows node/php/python keep the binary at
// the root, go/rust under bin/; on macOS everything is under bin/.
func runtimeExeInRoot(name, root, exe string) string {
	var cand string
	if goruntime.GOOS == "windows" {
		switch name {
		case "go", "rust":
			cand = filepath.Join(root, "bin", platform.BinaryName(exe))
		default:
			cand = filepath.Join(root, platform.BinaryName(exe))
		}
	} else {
		cand = filepath.Join(root, "bin", exe)
	}
	if _, err := os.Stat(cand); err != nil {
		return ""
	}
	return cand
}

// runtimeRootFromExe derives the install root from a binary found on PATH.
func runtimeRootFromExe(exe string) string {
	dir := filepath.Dir(exe)
	if strings.EqualFold(filepath.Base(dir), "bin") {
		return filepath.Dir(dir)
	}
	return dir
}

// rustSysroot resolves the real toolchain directory behind a rustup shim
// (~/.cargo/bin/rustc is a proxy; the toolchain lives under ~/.rustup).
func rustSysroot(root string) string {
	exe := runtimeExeInRoot("rust", root, "rustc")
	if exe == "" {
		// PATH-derived roots for rust may point at ~/.cargo (bin stripped)
		cand := filepath.Join(root, "bin", platform.BinaryName("rustc"))
		if _, err := os.Stat(cand); err != nil {
			return ""
		}
		exe = cand
	}
	out := strings.TrimSpace(versionOutput(exe, "--print", "sysroot"))
	if out == "" {
		return ""
	}
	// Last line guards against warnings printed before the path.
	lines := strings.Split(out, "\n")
	sysroot := strings.TrimSpace(lines[len(lines)-1])
	if fi, err := os.Stat(sysroot); err == nil && fi.IsDir() {
		return sysroot
	}
	return ""
}

// --- helpers ---

func versionOutput(exe string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	platform.SetProcessAttrs(cmd, false, true)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// findInPATH returns every occurrence of an executable across PATH,
// excluding DevBox-managed directories and Windows Store alias stubs.
func findInPATH(exeName string) []string {
	var out []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, exeName)
		fi, err := os.Stat(cand)
		if err != nil || fi.IsDir() {
			continue
		}
		if underDataDir(cand) || isExcludedRoot(dir) {
			continue
		}
		out = append(out, cand)
	}
	return out
}

func expandGlobs(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if p == "" {
			continue
		}
		matches, err := filepath.Glob(p)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.IsDir() {
				out = append(out, m)
			}
		}
	}
	return out
}

func underDataDir(p string) bool {
	data := filepath.Clean(config.GetDataDir())
	rel, err := filepath.Rel(data, filepath.Clean(p))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// isExcludedRoot filters directories that must never be treated as an
// importable installation (Store alias stubs, package-manager shim dirs).
// Shim dirs hold tiny proxy executables — the version probe would succeed but
// the directory is not the installation.
func isExcludedRoot(p string) bool {
	// Normalize both separator styles explicitly — filepath.ToSlash only
	// converts the current OS's separator, which leaves Windows-style paths
	// untouched when this code runs (or is tested) on Linux.
	lower := strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	return strings.Contains(lower, "/windowsapps") ||
		strings.HasSuffix(lower, "/.cargo/bin") ||
		strings.Contains(lower, "/chocolatey/bin") ||
		strings.Contains(lower, "/scoop/shims") ||
		strings.Contains(lower, "/node_modules/")
}

// plausibleRuntimeRoot rejects roots that merely contain a matching binary
// without the rest of the installation (shared bin directories, shims).
func plausibleRuntimeRoot(name, root string) bool {
	anyDir := func(names ...string) bool {
		for _, n := range names {
			if fi, err := os.Stat(filepath.Join(root, n)); err == nil && fi.IsDir() {
				return true
			}
		}
		return false
	}
	if goruntime.GOOS != "windows" {
		// bin/-anchored layouts (Homebrew opt dirs, nvm versions) are specific
		// enough; require at least a lib/ or libexec/ sibling.
		return anyDir("lib", "libexec", "share", "include")
	}
	switch name {
	case "node":
		return anyDir("node_modules") || fileExists(filepath.Join(root, "npm.cmd"))
	case "go":
		return anyDir("pkg", "src", "api")
	case "php":
		return anyDir("ext") || fileExists(filepath.Join(root, "php.ini")) ||
			fileExists(filepath.Join(root, "php.ini-development"))
	case "python":
		return anyDir("Lib", "lib", "DLLs") || len(globMatches(filepath.Join(root, "python3*._pth"))) > 0
	case "rust":
		return anyDir("lib")
	}
	return true
}

// plausibleServiceRoot rejects service roots whose whole tree would be copied
// but that are really shared binary directories.
func plausibleServiceRoot(name, root string) bool {
	switch name {
	case "nginx":
		return fileExists(filepath.Join(root, "conf", "nginx.conf"))
	case "redis", "valkey":
		base := strings.ToLower(filepath.Base(root))
		return strings.Contains(base, name) ||
			fileExists(filepath.Join(root, name+".conf")) ||
			fileExists(filepath.Join(root, "redis.windows.conf"))
	}
	return true
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func globMatches(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

func runtimeVersionInstalled(name, version string) bool {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return false
	}
	installed, err := mgr.ListInstalled()
	if err != nil {
		return false
	}
	for _, v := range installed {
		if v.Number == version {
			return true
		}
	}
	return false
}

func normPath(p string) string {
	return filepath.Clean(p)
}

func sortedSlice(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// dedupeByVersion drops duplicate (name, version) runtime hits — the same
// installation frequently shows up both on PATH and in a well-known location.
func dedupeByVersion(items []Found) []Found {
	seen := map[string]bool{}
	var out []Found
	for _, f := range items {
		key := f.Kind + "|" + f.Name + "|" + f.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}
