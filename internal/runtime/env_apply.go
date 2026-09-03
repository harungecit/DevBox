package runtime

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// EnvProvider is implemented by managers whose versions contribute more than
// one bin directory or environment variables (plugin runtimes: JAVA_HOME,
// GOROOT…). Built-in managers only expose BinaryPath.
type EnvProvider interface {
	// EnvPaths lists every directory the version puts on PATH, main bin first.
	EnvPaths(version string) []string
	// EnvVars lists the variables the version needs (key → value).
	EnvVars(version string) map[string]string
}

// ActivationPaths returns every PATH directory a version contributes.
func ActivationPaths(mgr RuntimeManager, version string) []string {
	if version == "" {
		return nil
	}
	if ep, ok := mgr.(EnvProvider); ok {
		if paths := ep.EnvPaths(version); len(paths) > 0 {
			return paths
		}
	}
	if bp := mgr.BinaryPath(version); bp != "" {
		return []string{bp}
	}
	return nil
}

// ActivationVars returns the environment variables a version needs (nil for built-ins).
func ActivationVars(mgr RuntimeManager, version string) map[string]string {
	if version == "" {
		return nil
	}
	if ep, ok := mgr.(EnvProvider); ok {
		return ep.EnvVars(version)
	}
	return nil
}

// ApplyGlobalEnv puts a version's directories on the user PATH and writes its
// variables to the user environment, recording them in config.ManagedEnv so
// the Path page can show them and removal stays possible after the plugin is gone.
func ApplyGlobalEnv(mgr RuntimeManager, version string) error {
	paths := ActivationPaths(mgr, version)
	// AddToPath prepends, so add in reverse to keep the plugin's order.
	for i := len(paths) - 1; i >= 0; i-- {
		if err := platform.AddToPath(paths[i]); err != nil {
			return err
		}
	}
	vars := ActivationVars(mgr, version)
	if len(vars) == 0 {
		return nil
	}
	cfg := config.Get()
	if cfg.ManagedEnv == nil {
		cfg.ManagedEnv = map[string]config.ManagedEnvEntry{}
	}
	var firstErr error
	for k, v := range vars {
		if strings.EqualFold(k, "PATH") {
			continue
		}
		if err := platform.SetUserEnv(k, v); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		cfg.ManagedEnv[k] = config.ManagedEnvEntry{Value: v, Runtime: mgr.Name(), Version: version}
	}
	if err := config.Save(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// ClearGlobalEnv undoes ApplyGlobalEnv for a version. Variables are only
// removed when DevBox recorded them for this runtime (never a user's own).
func ClearGlobalEnv(mgr RuntimeManager, version string) error {
	var firstErr error
	for _, p := range ActivationPaths(mgr, version) {
		if err := platform.RemoveFromPath(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := ClearManagedVars(mgr.Name(), version); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// ClearManagedVars removes every variable DevBox recorded for a runtime
// (all versions when version is ""). Used when a plugin is removed.
func ClearManagedVars(runtimeName, version string) error {
	cfg := config.Get()
	if len(cfg.ManagedEnv) == 0 {
		return nil
	}
	var firstErr error
	changed := false
	for k, e := range cfg.ManagedEnv {
		if e.Runtime != runtimeName || (version != "" && e.Version != version) {
			continue
		}
		if err := platform.UnsetUserEnv(k); err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		delete(cfg.ManagedEnv, k)
		changed = true
	}
	if changed {
		if err := config.Save(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ManagedPathDirs lists the bin directories of every runtime's global
// version (built-ins first, then plugins) — what a DevBox terminal prepends.
func ManagedPathDirs() []string {
	var dirs []string
	for _, name := range Names() {
		mgr := Registry[name]
		v, _ := mgr.GetGlobal()
		if v == "" {
			continue
		}
		for _, d := range ActivationPaths(mgr, v) {
			if _, err := os.Stat(d); err == nil {
				dirs = append(dirs, d)
			}
		}
		// Python console scripts live next to the interpreter on Windows.
		if name == "python" && goruntime.GOOS == "windows" {
			if s := filepath.Join(mgr.BinaryPath(v), "Scripts"); dirExists(s) {
				dirs = append(dirs, s)
			}
		}
	}
	return dirs
}

// ManagedEnvVars merges the variables of every plugin runtime's global version.
func ManagedEnvVars() map[string]string {
	out := map[string]string{}
	for _, name := range Names() {
		mgr := Registry[name]
		v, _ := mgr.GetGlobal()
		for k, val := range ActivationVars(mgr, v) {
			out[k] = val
		}
	}
	return out
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
