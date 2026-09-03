package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"DevBox/internal/platform"
)

// pkgMgrShimNames returns the shim filenames corepack creates for a package manager
// in the Node bin directory. Windows has multiple shim wrappers (.cmd / .ps1 / extensionless).
func pkgMgrShimNames(name string) []string {
	if goruntime.GOOS == "windows" {
		return []string{name + ".cmd", name + ".ps1", name}
	}
	return []string{name}
}

// IsPkgMgrEnabled reports whether a Node package manager shim exists in the active Node bin dir.
// Only yarn and pnpm are managed this way; npm is always bundled with Node, and Bun is global.
func IsPkgMgrEnabled(name string) bool {
	return IsPkgMgrEnabledIn(activeNodeBinDir(), name)
}

// IsPkgMgrEnabledIn is IsPkgMgrEnabled for an explicit Node bin directory.
func IsPkgMgrEnabledIn(binDir, name string) bool {
	if binDir == "" {
		return false
	}
	for _, shim := range pkgMgrShimNames(name) {
		if _, err := os.Stat(filepath.Join(binDir, shim)); err == nil {
			return true
		}
	}
	return false
}

// EnablePkgMgr enables a package manager on the active Node version.
func EnablePkgMgr(name string) error {
	binDir := activeNodeBinDir()
	if binDir == "" {
		return fmt.Errorf("no active Node.js version — install Node and set it as global first")
	}
	return EnablePkgMgrIn(binDir, name)
}

// EnablePkgMgrIn enables a package manager inside a specific Node bin dir.
// Prefers corepack (Node 16.10+) which installs the shim and downloads the latest stable
// release on activation. Falls back to `npm install -g` for older Node builds without corepack.
func EnablePkgMgrIn(binDir, name string) error {
	corepack := filepath.Join(binDir, platform.ScriptName("corepack"))
	if _, err := os.Stat(corepack); err == nil {
		// Without --install-directory corepack locates the shim target via
		// `which corepack` on the child's PATH. DevBox's own process env does not
		// contain a Node dir that was installed or made global after launch, so
		// the lookup fails with "not found: corepack". Name the directory
		// explicitly and put it on PATH so the shims and `prepare` resolve node.
		enableCmd := exec.Command(corepack, "enable", "--install-directory", binDir, name)
		enableCmd.Env = nodeEnv(binDir)
		platform.SetProcessAttrs(enableCmd, false, true)
		if out, err := enableCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("corepack enable %s failed: %s - %w", name, strings.TrimSpace(string(out)), err)
		}
		// Activate latest stable so the very first call doesn't prompt for a download.
		// Non-fatal if it fails; the shim is already in place.
		prepCmd := exec.Command(corepack, "prepare", name+"@stable", "--activate")
		prepCmd.Env = nodeEnv(binDir)
		platform.SetProcessAttrs(prepCmd, false, true)
		prepCmd.CombinedOutput()
		return nil
	}

	// Fallback for older Node (<16.10): use npm global install.
	npm := filepath.Join(binDir, platform.ScriptName("npm"))
	if _, err := os.Stat(npm); os.IsNotExist(err) {
		return fmt.Errorf("npm not found in Node install at %s", binDir)
	}
	cmd := exec.Command(npm, "install", "-g", name)
	platform.SetProcessAttrs(cmd, false, true)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm install -g %s failed: %s - %w", name, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// DisablePkgMgr removes the package manager shim from the active Node bin dir.
// Corepack has no `disable` subcommand; the documented way is to remove the shim files.
func DisablePkgMgr(name string) error {
	binDir := activeNodeBinDir()
	if binDir == "" {
		return nil
	}
	for _, shim := range pkgMgrShimNames(name) {
		os.Remove(filepath.Join(binDir, shim))
	}
	return nil
}

// GetPkgMgrVersion runs `<shim> --version`. Returns "" if not enabled or call fails.
func GetPkgMgrVersion(name string) string {
	binDir := activeNodeBinDir()
	if binDir == "" {
		return ""
	}

	var shimPath string
	for _, shim := range pkgMgrShimNames(name) {
		p := filepath.Join(binDir, shim)
		if _, err := os.Stat(p); err == nil {
			shimPath = p
			break
		}
	}
	if shimPath == "" {
		return ""
	}

	cmd := exec.Command(shimPath, "--version")
	platform.SetProcessAttrs(cmd, false, true)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetNpmVersion returns the npm version bundled with the active Node.
// npm is always shipped with Node, so it has no "enable" step.
func GetNpmVersion() string {
	binDir := activeNodeBinDir()
	if binDir == "" {
		return ""
	}
	npm := filepath.Join(binDir, platform.ScriptName("npm"))
	if _, err := os.Stat(npm); os.IsNotExist(err) {
		return ""
	}
	cmd := exec.Command(npm, "--version")
	platform.SetProcessAttrs(cmd, false, true)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetNpmLatestVersion asks the npm registry for the newest npm release.
func GetNpmLatestVersion() string {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://registry.npmjs.org/npm/latest")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	var meta struct {
		Version string `json:"version"`
	}
	if json.NewDecoder(resp.Body).Decode(&meta) != nil {
		return ""
	}
	return meta.Version
}

// UpdateNpm upgrades the npm bundled with the active Node version in place
// (`npm install -g npm@latest`). Each Node version carries its own npm, so
// this affects only the active version.
func UpdateNpm() error {
	binDir := activeNodeBinDir()
	if binDir == "" {
		return fmt.Errorf("no active Node.js version")
	}
	npm := filepath.Join(binDir, platform.ScriptName("npm"))
	if _, err := os.Stat(npm); os.IsNotExist(err) {
		return fmt.Errorf("npm not found in %s", binDir)
	}
	cmd := exec.Command(npm, "install", "-g", "npm@latest")
	cmd.Dir = binDir
	platform.SetProcessAttrs(cmd, false, true)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm self-update failed: %s - %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// nodeEnv returns the current environment with binDir prepended to PATH, so
// child processes (corepack, its shims, npm) resolve the intended node.exe
// even when DevBox itself was launched before that Node version existed.
func nodeEnv(binDir string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		if len(e) > 5 && strings.EqualFold(e[:5], "PATH=") {
			out = append(out, "PATH="+binDir+string(os.PathListSeparator)+e[5:])
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		out = append(out, "PATH="+binDir)
	}
	return out
}

func activeNodeBinDir() string {
	nm := NewNodeManager()
	global, _ := nm.GetGlobal()
	if global == "" {
		return ""
	}
	return nm.BinaryPath(global)
}
