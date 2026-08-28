package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/platform"
)

// MigrateVersionData carries per-version state from an old install to the
// freshly installed replacement so an in-place update feels like an update
// rather than a fresh install:
//   - PHP: php.ini (extension toggles + ini edits) and Composer
//   - Node: corepack-enabled yarn/pnpm shims and globally installed packages
//   - Python: pip packages (freeze → install)
//
// Everything is best-effort: failures are reported through progress messages
// and never fail the update.
func MigrateVersionData(name, from, to string, progress chan<- Progress) {
	report := func(msg string) {
		if progress != nil {
			progress <- Progress{Percent: 99, Message: msg}
		}
	}
	oldDir := filepath.Join(runtimeBaseDir(name), from)
	newDir := filepath.Join(runtimeBaseDir(name), to)

	switch name {
	case "php":
		migratePHP(oldDir, newDir, to, report)
	case "node":
		migrateNode(name, from, to, report)
	case "python":
		migratePython(name, from, to, report)
	}
}

func migratePHP(oldDir, newDir, to string, report func(string)) {
	if data, err := os.ReadFile(filepath.Join(oldDir, "php.ini")); err == nil {
		if err := os.WriteFile(filepath.Join(newDir, "php.ini"), data, 0644); err == nil {
			report("Carried over php.ini settings")
			// extension_dir inside the copied ini still points at the old version.
			ensureExtensionDir(newDir)
			// Extensions that the new build ships are (re-)enabled; entries for
			// DLLs that no longer exist are dropped so PHP doesn't warn on start.
			pruneMissingExtensions(newDir)
			EnableCommonExtensions(to)
		}
	}
	for _, f := range []string{"composer.phar", "composer.bat", "composer"} {
		src := filepath.Join(oldDir, f)
		if data, err := os.ReadFile(src); err == nil {
			mode := os.FileMode(0644)
			if f == "composer" {
				mode = 0755
			}
			os.WriteFile(filepath.Join(newDir, f), data, mode)
		}
	}
	if _, err := os.Stat(filepath.Join(newDir, "composer.phar")); err == nil {
		report("Carried over Composer")
	}
}

// pruneMissingExtensions comments out `extension=x` lines whose file is absent
// in this build's ext/ directory.
func pruneMissingExtensions(phpDir string) {
	iniPath := filepath.Join(phpDir, "php.ini")
	data, err := os.ReadFile(iniPath)
	if err != nil {
		return
	}
	extDir := filepath.Join(phpDir, "ext")
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "extension=") {
			continue
		}
		ext := strings.TrimSpace(strings.TrimPrefix(trimmed, "extension="))
		ext = strings.TrimSuffix(strings.TrimSuffix(ext, ".dll"), ".so")
		if strings.ContainsAny(ext, `/\`) {
			continue // absolute path — leave alone
		}
		if _, err := os.Stat(filepath.Join(extDir, extFileName(ext))); os.IsNotExist(err) {
			lines[i] = ";" + line
			changed = true
		}
	}
	if changed {
		os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")), 0644)
	}
}

func migrateNode(name, from, to string, report func(string)) {
	nm := NewNodeManager()
	oldBin := nm.BinaryPath(from)
	newBin := nm.BinaryPath(to)

	for _, pm := range []string{"yarn", "pnpm"} {
		if IsPkgMgrEnabledIn(oldBin, pm) {
			if err := EnablePkgMgrIn(newBin, pm); err != nil {
				report("Could not re-enable " + pm + ": " + err.Error())
			} else {
				report("Re-enabled " + pm)
			}
		}
	}

	pkgs := globalNodePackages(from)
	if len(pkgs) == 0 {
		return
	}
	npm := filepath.Join(newBin, platform.ScriptName("npm"))
	for _, pkg := range pkgs {
		report("Reinstalling global package " + pkg + "...")
		cmd := exec.Command(npm, "install", "-g", pkg)
		platform.SetProcessAttrs(cmd, false, true)
		if out, err := cmd.CombinedOutput(); err != nil {
			report("Could not reinstall " + pkg + ": " + strings.TrimSpace(string(out)))
		}
	}
}

// globalNodePackages lists `name@version` of packages installed with
// `npm install -g` into a Node version dir (npm and corepack excluded).
func globalNodePackages(version string) []string {
	nm := NewNodeManager()
	root := filepath.Join(nm.BinaryPath(version), "node_modules")
	if goruntime.GOOS != "windows" {
		root = filepath.Join(runtimeBaseDir("node"), version, "lib", "node_modules")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	addPkg := func(dir string) {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			return
		}
		var meta struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &meta) != nil || meta.Name == "" {
			return
		}
		if meta.Name == "npm" || meta.Name == "corepack" || meta.Name == "yarn" || meta.Name == "pnpm" {
			return
		}
		if meta.Version != "" {
			out = append(out, meta.Name+"@"+meta.Version)
		} else {
			out = append(out, meta.Name)
		}
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if strings.HasPrefix(e.Name(), "@") {
			scoped, _ := os.ReadDir(filepath.Join(root, e.Name()))
			for _, s := range scoped {
				if s.IsDir() {
					addPkg(filepath.Join(root, e.Name(), s.Name()))
				}
			}
			continue
		}
		addPkg(filepath.Join(root, e.Name()))
	}
	return out
}

func migratePython(name, from, to string, report func(string)) {
	pm := NewPythonManager()
	oldPy := filepath.Join(pm.BinaryPath(from), platform.BinaryName("python"))
	newPy := filepath.Join(pm.BinaryPath(to), platform.BinaryName("python"))
	if goruntime.GOOS != "windows" {
		oldPy = filepath.Join(pm.BinaryPath(from), "python3")
		newPy = filepath.Join(pm.BinaryPath(to), "python3")
	}
	freeze := exec.Command(oldPy, "-m", "pip", "freeze", "--exclude", "pip", "--exclude", "setuptools", "--exclude", "wheel")
	platform.SetProcessAttrs(freeze, false, true)
	out, err := freeze.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return
	}
	reqFile := filepath.Join(tmpDir(), fmt.Sprintf("python-%s-requirements.txt", from))
	if os.WriteFile(reqFile, out, 0644) != nil {
		return
	}
	defer os.Remove(reqFile)
	report("Reinstalling pip packages...")
	install := exec.Command(newPy, "-m", "pip", "install", "-r", reqFile)
	platform.SetProcessAttrs(install, false, true)
	if o, err := install.CombinedOutput(); err != nil {
		report("pip reinstall had errors: " + lastLine(string(o)))
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
