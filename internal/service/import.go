package service

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

// ImportExternal brings an existing external service installation (system
// installer, XAMPP/Laragon, scoop, Homebrew, manual unzip, ...) under DevBox
// management WITHOUT moving or copying its program files: the binary
// directories are linked (NTFS junction on Windows, symlink on macOS) into
// services/{name}/, while configuration, logs and a fresh data directory
// (initialized for the databases) are DevBox's own. The external
// installation — its binaries, service registration and data — is left
// untouched; removing the imported service only removes the links.
//
// Existing databases stay with the external installation; DevBox runs the
// linked binaries with its own clean data directory on the DevBox default
// port. Single-file services (Caddy, Mailpit) are hardlinked; tiny text
// assets a server needs next to DevBox-owned config (nginx mime.types,
// Apache conf) are copied.
func ImportExternal(name, srcRoot, version string, progress chan<- Progress) error {
	report := func(pct int, msg string) {
		if progress != nil {
			progress <- Progress{Percent: pct, Message: msg}
		}
	}

	mgr, ok := Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	if mgr.IsInstalled() {
		return fmt.Errorf("%s is already installed in DevBox", mgr.DisplayName())
	}
	if name == "apache" && goruntime.GOOS != "windows" {
		return fmt.Errorf("Apache import is currently supported on Windows only")
	}

	srcRoot = filepath.Clean(srcRoot)
	fi, err := os.Stat(srcRoot)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("source directory not found: %s", srcRoot)
	}
	if isImportSubPath(config.GetDataDir(), srcRoot) {
		return fmt.Errorf("%s is already managed by DevBox", srcRoot)
	}

	// Prefer the default port, but step past ports that are already claimed —
	// by another DevBox service (installed, maybe stopped) or by anything
	// currently listening (typically the still-running external instance).
	// MySQL next to MariaDB then lands on 3307 instead of colliding on 3306.
	port := pickFreePort(name, mgr.DefaultPort())
	base := serviceBaseDir(name)

	report(5, fmt.Sprintf("Linking %s %s (files stay at %s)...", mgr.DisplayName(), version, srcRoot))

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean service directory: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := importLink(name, srcRoot, base); err != nil {
		os.RemoveAll(base)
		return fmt.Errorf("import failed: %w", err)
	}

	if !mgr.IsInstalled() {
		os.RemoveAll(base)
		return fmt.Errorf("import verification failed: %s binary missing after copy", mgr.DisplayName())
	}

	report(80, "Configuring...")

	os.MkdirAll(filepath.Join(base, "data"), 0755)
	os.MkdirAll(filepath.Join(base, "logs"), 0755)

	if err := importProvision(name, srcRoot, version, port, report); err != nil {
		return err
	}

	SaveServiceConfig(name, ServiceConfig{Port: port, Version: version})

	if port != mgr.DefaultPort() {
		report(99, fmt.Sprintf("Port %d was taken — %s will use port %d (changeable in service settings).", mgr.DefaultPort(), mgr.DisplayName(), port))
	}

	report(100, fmt.Sprintf("%s %s imported (port %d)", mgr.DisplayName(), version, port))
	return nil
}

// pickFreePort returns the first port at or after `start` that is neither
// configured for another installed DevBox service nor currently in use on
// the machine.
func pickFreePort(name string, start int) int {
	claimed := map[int]bool{}
	registryMu.RLock()
	for svcName, mgr := range Registry {
		if svcName != name && mgr.IsInstalled() {
			claimed[mgr.Port()] = true
		}
	}
	registryMu.RUnlock()

	port := start
	for i := 0; i < 100; i++ {
		if !claimed[port] && !IsPortInUse(port) {
			return port
		}
		port++
	}
	return start
}

// importLink wires the parts of an external installation that DevBox's
// layout needs into services/{name}/ — by linking, never by moving. Data
// directories, logs and other machine state stay external and untouched.
func importLink(name, srcRoot, base string) error {
	switch name {
	case "postgres":
		// EDB installers keep the live data dir (ACL-protected) inside the
		// install root; only the program directories are linked.
		return linkSubdirs(srcRoot, base, []string{"bin"}, []string{"lib", "share"})
	case "mysql", "mariadb":
		return linkSubdirs(srcRoot, base, []string{"bin"}, []string{"share", "lib"})
	case "mongodb":
		return linkSubdirs(srcRoot, base, []string{"bin"}, nil)
	case "caddy":
		os.MkdirAll(base, 0755)
		exe := platform.BinaryName("caddy")
		return linkOrCopyFile(filepath.Join(srcRoot, exe), filepath.Join(base, exe))
	case "mailpit":
		os.MkdirAll(base, 0755)
		exe := platform.BinaryName("mailpit")
		return linkOrCopyFile(filepath.Join(srcRoot, exe), filepath.Join(base, exe))
	case "nginx":
		return linkNginx(srcRoot, base)
	case "redis", "valkey":
		return linkRedisLike(name, srcRoot, base)
	case "apache":
		return linkApache(srcRoot, base)
	}
	return fmt.Errorf("import is not supported for %s", name)
}

// linkNginx links/hardlinks the nginx binary into the DevBox service dir.
// nginx resolves conf/, logs/ and temp dirs relative to its working
// directory (DevBox starts it with cwd = base), so DevBox owns the config
// while the program files stay external. mime.types and the default html
// page are tiny text assets the DevBox-owned config references — copied.
func linkNginx(srcRoot, base string) error {
	if _, err := os.Stat(filepath.Join(srcRoot, "conf", "nginx.conf")); err != nil {
		return fmt.Errorf("%s does not look like an nginx installation (conf/nginx.conf missing)", srcRoot)
	}
	if goruntime.GOOS != "windows" {
		// macOS layout keeps the binary under sbin/.
		if err := linkSubdirs(srcRoot, base, []string{"sbin"}, nil); err != nil {
			return err
		}
	} else {
		os.MkdirAll(base, 0755)
		exe := platform.BinaryName("nginx")
		if err := linkOrCopyFile(filepath.Join(srcRoot, exe), filepath.Join(base, exe)); err != nil {
			return err
		}
	}
	os.MkdirAll(filepath.Join(base, "conf"), 0755)
	copyImportFile(filepath.Join(srcRoot, "conf", "mime.types"), filepath.Join(base, "conf", "mime.types"))
	htmlDir := filepath.Join(base, "html")
	os.MkdirAll(htmlDir, 0755)
	if _, err := os.Stat(filepath.Join(srcRoot, "html", "index.html")); err == nil {
		copyImportFile(filepath.Join(srcRoot, "html", "index.html"), filepath.Join(htmlDir, "index.html"))
	} else {
		os.WriteFile(filepath.Join(htmlDir, "index.html"), []byte("<!doctype html><title>DevBox nginx</title><h1>DevBox nginx is running</h1>"), 0644)
	}
	return nil
}

// linkRedisLike hardlinks the server binaries (and, on Windows, the MSYS
// runtime DLLs next to them) into the DevBox service dir; the DevBox-owned
// redis.conf/valkey.conf is written by the provisioning step.
func linkRedisLike(name, srcRoot, base string) error {
	lowerBase := strings.ToLower(filepath.Base(srcRoot))
	_, confErr := os.Stat(filepath.Join(srcRoot, name+".conf"))
	_, winConfErr := os.Stat(filepath.Join(srcRoot, "redis.windows.conf"))
	if !strings.Contains(lowerBase, name) && confErr != nil && winConfErr != nil {
		return fmt.Errorf("%s does not look like a %s installation", srcRoot, name)
	}
	os.MkdirAll(base, 0755)
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return err
	}
	linked := 0
	prefix := name + "-" // redis- / valkey-
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		if strings.HasPrefix(n, prefix) || strings.HasSuffix(n, ".dll") {
			if err := linkOrCopyFile(filepath.Join(srcRoot, e.Name()), filepath.Join(base, e.Name())); err != nil {
				return err
			}
			linked++
		}
	}
	if linked == 0 {
		return fmt.Errorf("no %s binaries found in %s", name, srcRoot)
	}
	return nil
}

// linkApache links the program directories and copies the (small, text-only)
// conf/ tree so DevBox can patch ServerRoot/Listen/modules without touching
// the external installation. DocumentRoot gets a fresh htdocs.
func linkApache(srcRoot, base string) error {
	if err := linkSubdirs(srcRoot, base, []string{"bin"}, []string{"modules", "icons", "error", "cgi-bin", "lib"}); err != nil {
		return err
	}
	if err := copyTreeExcluding(filepath.Join(srcRoot, "conf"), filepath.Join(base, "conf"), nil); err != nil {
		return fmt.Errorf("could not copy Apache conf: %w", err)
	}
	htdocs := filepath.Join(base, "htdocs")
	os.MkdirAll(htdocs, 0755)
	if _, err := os.Stat(filepath.Join(htdocs, "index.html")); err != nil {
		os.WriteFile(filepath.Join(htdocs, "index.html"), []byte("<!doctype html><title>DevBox Apache</title><h1>DevBox Apache is running</h1>"), 0644)
	}
	return nil
}

// importProvision runs the same post-extract initialization the normal
// Install flow performs for each service.
func importProvision(name, srcRoot, version string, port int, report func(int, string)) error {
	base := serviceBaseDir(name)
	switch name {
	case "nginx":
		os.MkdirAll(filepath.Join(base, "conf", "vhosts"), 0755)
		NewNginxManager().writeConfig(port)
	case "apache":
		// Embedded absolute paths still point at the external root; rewrite
		// them to the DevBox copy before the standard patch pass.
		rewriteApachePaths(base, srcRoot)
		NewApacheManager().patchConfig(port)
	case "caddy":
		os.MkdirAll(filepath.Join(base, "html"), 0755)
		c := NewCaddyManager()
		c.writeConfig(port)
		c.writeDefaultPage()
	case "redis":
		NewRedisManager().writeConfig(port)
	case "valkey":
		NewValkeyManager().writeConfig(port)
	case "mongodb":
		NewMongoDBManager().writeConfig(port)
	case "mailpit":
		// nothing to write — flags carry the configuration
	case "postgres":
		report(85, "Initializing database cluster...")
		p := NewPostgresManager()
		if err := p.initDB(filepath.Join(base, "data"), port); err != nil {
			return fmt.Errorf("initdb failed: %w", err)
		}
	case "mysql":
		report(85, "Initializing MySQL...")
		m := NewMySQLManager()
		m.writeConfig(port)
		if err := m.initialize(); err != nil {
			report(95, "MySQL init note: "+err.Error())
		}
	case "mariadb":
		report(85, "Initializing MariaDB...")
		m := NewMariaDBManager()
		m.writeConfig(port)
		if err := m.initialize(); err != nil {
			report(95, "MariaDB init note: "+err.Error())
		}
	}
	return nil
}

// rewriteApachePaths replaces the external install root inside httpd.conf with
// the DevBox service dir so ServerRoot/DocumentRoot/module paths resolve.
func rewriteApachePaths(base, srcRoot string) {
	confPath := filepath.Join(base, "conf", "httpd.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		return
	}
	content := string(data)
	baseFwd := strings.ReplaceAll(base, "\\", "/")
	srcFwd := strings.ReplaceAll(srcRoot, "\\", "/")
	for _, variant := range []string{srcFwd, strings.ToLower(srcFwd), srcRoot, strings.ReplaceAll(srcRoot, "\\", "\\\\")} {
		content = strings.ReplaceAll(content, variant, baseFwd)
	}
	os.WriteFile(confPath, []byte(content), 0644)
}

// linkSubdirs links the listed subdirectories of srcRoot into base (junction
// on Windows, symlink on macOS). Directories in `required` must exist;
// `optional` ones are linked when present. On filesystems where linking is
// impossible the subdirectory is copied instead — the external installation
// is never modified either way.
func linkSubdirs(srcRoot, base string, required, optional []string) error {
	os.MkdirAll(base, 0755)
	link := func(sub string, mustExist bool) error {
		src := filepath.Join(srcRoot, sub)
		if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
			if mustExist {
				return fmt.Errorf("%s not found in %s", sub, srcRoot)
			}
			return nil
		}
		dst := filepath.Join(base, sub)
		if err := platform.LinkDir(dst, src); err != nil {
			os.RemoveAll(dst)
			return copyTreeExcluding(src, dst, nil)
		}
		return nil
	}
	for _, sub := range required {
		if err := link(sub, true); err != nil {
			return err
		}
	}
	for _, sub := range optional {
		if err := link(sub, false); err != nil {
			return err
		}
	}
	return nil
}

// linkOrCopyFile hardlinks a single file (no privilege needed, no data
// duplicated); if hardlinking fails (different volume, unsupported FS) the
// file is copied. The source file is never modified.
func linkOrCopyFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("not found: %s", src)
	}
	os.MkdirAll(filepath.Dir(dst), 0755)
	os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyImportFile(src, dst)
}

// copySubdirs copies the listed subdirectories of srcRoot into base.
// Directories in `required` must exist; `optional` ones are copied when present.
func copySubdirs(srcRoot, base string, required, optional []string) error {
	for _, sub := range required {
		src := filepath.Join(srcRoot, sub)
		if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
			return fmt.Errorf("%s not found in %s", sub, srcRoot)
		}
		if err := copyTreeExcluding(src, filepath.Join(base, sub), nil); err != nil {
			return err
		}
	}
	for _, sub := range optional {
		src := filepath.Join(srcRoot, sub)
		if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
			continue
		}
		if err := copyTreeExcluding(src, filepath.Join(base, sub), nil); err != nil {
			return err
		}
	}
	return nil
}

// copyTreeExcluding recursively copies a directory tree preserving file modes,
// skipping top-level entries whose lowercased name is in exclude.
func copyTreeExcluding(src, dst string, exclude map[string]bool) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel != "." && exclude != nil {
			top := strings.ToLower(strings.Split(filepath.ToSlash(rel), "/")[0])
			if exclude[top] {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		target := filepath.Join(dst, rel)

		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			os.MkdirAll(filepath.Dir(target), 0755)
			if os.Symlink(link, target) == nil {
				return nil
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil // dangling link — skip
			}
			if fi, err := os.Stat(resolved); err == nil && fi.IsDir() {
				return copyTreeExcluding(resolved, target, nil)
			}
			return copyImportFile(resolved, target)
		}

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyImportFile(path, target)
	})
}

func copyImportFile(src, dst string) error {
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

func isImportSubPath(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}
