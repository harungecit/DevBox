package runtime

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// ImportExternal brings an existing external installation (system installer,
// nvm, scoop, Homebrew, manual unzip, ...) under DevBox management WITHOUT
// moving or copying it: runtimes/{name}/{version}/ becomes a directory link
// (NTFS junction on Windows, symlink on macOS) pointing at the external
// install. From then on the version behaves exactly like one DevBox
// downloaded itself — it can be activated, pinned to projects, updated in
// place and removed (removal only unlinks; the external files stay).
//
// Only when linking is impossible on the filesystem does the import fall
// back to copying the installation.
func ImportExternal(name, srcRoot, version string, progress chan<- Progress) error {
	report := func(pct int, msg string) {
		if progress != nil {
			progress <- Progress{Percent: pct, Message: msg}
		}
	}

	if _, ok := Registry[name]; !ok {
		return fmt.Errorf("unknown runtime: %s", name)
	}
	if version == "" {
		return fmt.Errorf("version could not be detected for %s at %s", name, srcRoot)
	}

	srcRoot = filepath.Clean(srcRoot)
	fi, err := os.Stat(srcRoot)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("source directory not found: %s", srcRoot)
	}
	// Never import from inside the DevBox data dir — those are our own installs.
	if isSubPath(config.GetDataDir(), srcRoot) {
		return fmt.Errorf("%s is already managed by DevBox", srcRoot)
	}
	if exe := importedRuntimeBinary(name, srcRoot); exe == "" {
		return fmt.Errorf("%s does not look like a %s installation (binary not found)", srcRoot, name)
	}

	destDir := filepath.Join(runtimeBaseDir(name), version)
	if _, err := os.Lstat(destDir); err == nil {
		return fmt.Errorf("%s %s is already installed in DevBox", name, version)
	}

	report(10, fmt.Sprintf("Linking %s %s (files stay at %s)...", name, version, srcRoot))
	os.MkdirAll(filepath.Dir(destDir), 0755)

	if err := platform.LinkDir(destDir, srcRoot); err == nil {
		// Verify the link resolves to the expected binary.
		if exe := importedRuntimeBinary(name, destDir); exe == "" {
			os.RemoveAll(destDir)
			return fmt.Errorf("import verification failed: binary not reachable through the link")
		}
		report(100, fmt.Sprintf("%s %s is now managed by DevBox (in place — nothing was moved)", name, version))
		return nil
	} else {
		report(15, "Linking not possible on this filesystem — copying instead: "+err.Error())
	}

	// Fallback: copy the installation (e.g. filesystems without junction
	// support). The external install is still left untouched.
	if err := copyTree(srcRoot, destDir, report); err != nil {
		os.RemoveAll(destDir)
		return fmt.Errorf("copy failed: %w", err)
	}
	if exe := importedRuntimeBinary(name, destDir); exe == "" {
		os.RemoveAll(destDir)
		return fmt.Errorf("import verification failed: binary missing after copy")
	}

	// Copies moved the files, so embedded absolute paths need repair.
	report(90, "Finalizing import...")
	switch name {
	case "php":
		finalizeImportedPHP(destDir, version, report)
	case "python":
		finalizeImportedPython(destDir, report)
	}

	report(100, fmt.Sprintf("%s %s imported", name, version))
	return nil
}

// importedRuntimeBinary returns the runtime's main binary inside a root using
// the same layout the managers expect (see BinaryPath implementations), or ""
// when it is missing.
func importedRuntimeBinary(name, root string) string {
	exe := map[string]string{
		"node": "node", "go": "go", "php": "php", "python": "python", "rust": "rustc",
	}[name]
	if exe == "" {
		return ""
	}
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
		if name == "python" {
			// Homebrew/system Pythons often ship only "python3"
			if _, err := os.Stat(cand); err != nil {
				cand = filepath.Join(root, "bin", "python3")
			}
		}
	}
	if _, err := os.Stat(cand); err != nil {
		return ""
	}
	return cand
}

// finalizeImportedPHP makes the copied PHP self-contained: php.ini must point
// at the copy's own ext/ directory, and installs without any php.ini get the
// DevBox dev preset so extensions work out of the box.
func finalizeImportedPHP(destDir, version string, report func(int, string)) {
	iniPath := filepath.Join(destDir, "php.ini")
	if _, err := os.Stat(iniPath); os.IsNotExist(err) {
		report(92, "No php.ini found — applying DevBox dev preset...")
		if err := ApplyDevPreset(version); err != nil {
			report(95, "Dev preset warning: "+err.Error())
		}
		return
	}
	// The imported php.ini still points at the old install's extension dir.
	if err := ensureExtensionDir(destDir); err != nil {
		report(95, "php.ini warning: "+err.Error())
	}
}

// finalizeImportedPython repairs pip's console-script launchers, which embed
// the absolute path of the interpreter they were installed with and would
// otherwise keep pointing at the old location. Best-effort.
func finalizeImportedPython(destDir string, report func(int, string)) {
	py := filepath.Join(destDir, platform.BinaryName("python"))
	if goruntime.GOOS != "windows" {
		py = filepath.Join(destDir, "bin", "python")
		if _, err := os.Stat(py); err != nil {
			py = filepath.Join(destDir, "bin", "python3")
		}
	}
	if _, err := os.Stat(py); err != nil {
		return
	}
	// Only when pip is present at all.
	check := execCommand(py, "-m", "pip", "--version")
	platform.SetProcessAttrs(check, false, true)
	if check.Run() != nil {
		return
	}
	report(93, "Repairing pip launchers for the new location...")
	fix := execCommand(py, "-m", "pip", "install", "--force-reinstall", "--no-warn-script-location", "pip")
	platform.SetProcessAttrs(fix, false, true)
	if out, err := fix.CombinedOutput(); err != nil {
		report(95, "pip repair warning: "+lastLine(string(out)))
	}
}

// copyTree recursively copies a directory preserving file modes and symlinks.
// Unreadable optional content (docs, caches) fails the copy — imports must be
// complete or not happen at all.
func copyTree(src, dst string, report func(int, string)) error {
	fileCount := 0
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			os.MkdirAll(filepath.Dir(target), 0755)
			// Best-effort: symlink creation can fail on Windows without
			// privileges; fall back to copying the resolved file.
			if os.Symlink(link, target) == nil {
				return nil
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil // dangling link — skip
			}
			if fi, err := os.Stat(resolved); err == nil && fi.IsDir() {
				return copyTree(resolved, target, nil)
			}
			return copyFileMode(resolved, target)
		}

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		fileCount++
		if report != nil && fileCount%500 == 0 {
			report(min(85, 10+fileCount/200), "Copying files...")
		}
		return copyFileMode(path, target)
	})
}

func copyFileMode(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	fi, err := in.Stat()
	if err != nil {
		return err
	}

	os.MkdirAll(filepath.Dir(dst), 0755)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func isSubPath(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}
