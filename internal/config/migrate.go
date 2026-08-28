package config

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"DevBox/internal/platform"
)

// migrateLegacyDataDir moves the whole ~/.devbox tree to newDir the first time
// DevBox starts after the data-dir change. Returns true when a move happened.
//
// Order matters: every child process DevBox spawned last session (nginx,
// postgres, php-cgi, dev servers, cloudflared, the proxy) still holds files
// open inside the old tree, so they are stopped first; then the tree is
// renamed (instant on the same volume, copied otherwise); then every config
// file that embedded the old absolute path is rewritten, and the user PATH
// entries DevBox manages are re-pointed.
func migrateLegacyDataDir(newDir string) bool {
	legacy := LegacyDataDir()
	if strings.EqualFold(filepath.Clean(legacy), filepath.Clean(newDir)) {
		return false
	}
	if _, err := os.Stat(filepath.Join(legacy, "config.json")); err != nil {
		return false // nothing to migrate
	}
	// A pending marker means a previous attempt died mid-copy (the app was
	// closed while the tree was still being copied): resume instead of skipping.
	pending := filepath.Join(newDir, ".migration-pending")
	_, resume := os.Stat(pending)
	if _, err := os.Stat(filepath.Join(newDir, "config.json")); err == nil && resume != nil {
		return false // new location already initialised — never clobber it
	}

	logf := migrationLogger(newDir)
	if resume == nil {
		logf("resuming interrupted migration %s -> %s", legacy, newDir)
		stopLegacyProcesses(legacy, logf)
		if err := copyTree(legacy, newDir); err != nil {
			logf("resume copy failed: %v", err)
			return false
		}
		rewritten := rewriteEmbeddedPaths(newDir, legacy, newDir)
		logf("rewrote %d config files", rewritten)
		rewriteManagedPATH(legacy, newDir, logf)
		os.Remove(pending)
		os.WriteFile(filepath.Join(legacy, "MOVED-TO.txt"), []byte("DevBox data now lives in "+newDir+"\nThis folder can be deleted.\n"), 0644)
		logf("migration complete (resumed)")
		return true
	}
	logf("migrating %s -> %s", legacy, newDir)

	stopLegacyProcesses(legacy, logf)

	// An empty placeholder at the destination (e.g. created by an earlier
	// failed attempt) would make Rename fail — clear it if it's really empty.
	if entries, err := os.ReadDir(newDir); err == nil && len(entries) == 0 {
		os.Remove(newDir)
	}
	os.MkdirAll(filepath.Dir(newDir), 0755)

	if err := os.Rename(legacy, newDir); err != nil {
		logf("rename failed (%v), falling back to copy", err)
		// Mark the copy as in progress so an interrupted run resumes next launch
		// instead of silently starting with half the data.
		os.MkdirAll(newDir, 0755)
		os.WriteFile(pending, []byte(legacy), 0644)
		if err := copyTree(legacy, newDir); err != nil {
			logf("copy failed: %v — will retry next launch", err)
			return false
		}
		os.Remove(pending)
		// Keep the old tree only as a safety net; mark it so the user knows.
		os.WriteFile(filepath.Join(legacy, "MOVED-TO.txt"), []byte("DevBox data now lives in "+newDir+"\nThis folder can be deleted.\n"), 0644)
	}

	rewritten := rewriteEmbeddedPaths(newDir, legacy, newDir)
	logf("rewrote %d config files", rewritten)

	rewriteManagedPATH(legacy, newDir, logf)
	logf("migration complete")
	return true
}

func migrationLogger(newDir string) func(string, ...interface{}) {
	return func(format string, args ...interface{}) {
		logDir := filepath.Join(newDir, "logs")
		os.MkdirAll(logDir, 0755)
		f, err := os.OpenFile(filepath.Join(logDir, "migration.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	}
}

// stopLegacyProcesses stops everything DevBox may have left running from the
// old tree. Graceful shutdown is attempted for servers that keep state
// (nginx, MySQL/MariaDB); everything else is killed via its PID file.
func stopLegacyProcesses(legacy string, logf func(string, ...interface{})) {
	svcDir := filepath.Join(legacy, "services")

	// nginx: master + workers — only `-s quit` reliably takes the workers down.
	nginxExe := filepath.Join(svcDir, "nginx", platform.BinaryName("nginx"))
	if _, err := os.Stat(nginxExe); err == nil {
		cmd := exec.Command(nginxExe, "-s", "quit")
		cmd.Dir = filepath.Join(svcDir, "nginx")
		platform.SetProcessAttrs(cmd, false, true)
		cmd.Run()
	}

	// MySQL / MariaDB: ask the server to shut down cleanly on its configured port.
	for _, name := range []string{"mysql", "mariadb"} {
		admin := filepath.Join(svcDir, name, "bin", platform.BinaryName("mysqladmin"))
		if _, err := os.Stat(admin); err != nil {
			admin = filepath.Join(svcDir, name, "bin", platform.BinaryName("mariadb-admin"))
		}
		if _, err := os.Stat(admin); err != nil {
			continue
		}
		port := servicePortFromJSON(filepath.Join(svcDir, name, "devbox-service.json"))
		args := []string{"-u", "root", "-h", "127.0.0.1"}
		if port > 0 {
			args = append(args, "-P", strconv.Itoa(port))
		}
		args = append(args, "shutdown")
		cmd := exec.Command(admin, args...)
		platform.SetProcessAttrs(cmd, false, true)
		cmd.Run()
	}

	// Everything with a PID file: services, php-cgi, dev servers, tunnels, proxy.
	var pids []int
	collect := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		first := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
		if pid, err := strconv.Atoi(first); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	filepath.WalkDir(svcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Don't descend into DB data dirs (huge) — postmaster.pid is handled below.
			if d.Name() == "data" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".pid") {
			collect(path)
		}
		return nil
	})
	collect(filepath.Join(svcDir, "postgres", "data", "postmaster.pid"))
	collect(filepath.Join(legacy, "proxy", "proxy.pid"))

	for _, pid := range pids {
		if platform.IsProcessRunning(pid) {
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		}
	}

	// Give the OS a moment to release file handles before the rename.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range pids {
			if platform.IsProcessRunning(pid) {
				alive = true
				break
			}
		}
		if !alive {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	logf("stopped %d legacy processes", len(pids))
}

func servicePortFromJSON(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var c struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(data, &c) != nil {
		return 0
	}
	return c.Port
}

// rewriteEmbeddedPaths replaces every spelling of the old absolute path
// (backslash, forward-slash and JSON-escaped) inside the text config files
// DevBox generates. The walk is deliberately shallow and skips data/log trees.
func rewriteEmbeddedPaths(root, oldDir, newDir string) int {
	type pair struct{ from, to string }
	pairs := []pair{
		{oldDir, newDir},
		{strings.ReplaceAll(oldDir, `\`, `/`), strings.ReplaceAll(newDir, `\`, `/`)},
		{strings.ReplaceAll(oldDir, `\`, `\\`), strings.ReplaceAll(newDir, `\`, `\\`)},
	}
	textExt := map[string]bool{
		".conf": true, ".ini": true, ".cfg": true, ".json": true, ".caddy": true,
		".yml": true, ".yaml": true, ".bat": true, ".cmd": true, ".sh": true, ".txt": true,
	}
	textName := map[string]bool{"Caddyfile": true, "path.sh": true}
	skipDir := map[string]bool{
		"data": true, "logs": true, "node_modules": true, "lib": true, "share": true,
		"include": true, "html": true, "htdocs": true, "ext": true, "tmp": true, "backups": true,
		"cache": true, "ssl": true, "manual": true, "docs": true, "icons": true, "error": true,
	}

	count := 0
	rewriteFile := func(path string) {
		info, err := os.Stat(path)
		if err != nil || info.Size() > 4<<20 {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		s := string(data)
		orig := s
		for _, p := range pairs {
			s = replaceFold(s, p.from, p.to)
		}
		if s != orig {
			if os.WriteFile(path, []byte(s), info.Mode()) == nil {
				count++
			}
		}
	}

	// services/<name>/** (shallow), runtimes/php/<ver>/*.ini|*.bat, proxy/, root files
	roots := []string{filepath.Join(root, "services"), filepath.Join(root, "proxy"), filepath.Join(root, "tools")}
	for _, r := range roots {
		filepath.WalkDir(r, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != r && skipDir[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if textExt[strings.ToLower(filepath.Ext(d.Name()))] || textName[d.Name()] {
				rewriteFile(path)
			}
			return nil
		})
	}
	phpVersions, _ := os.ReadDir(filepath.Join(root, "runtimes", "php"))
	for _, v := range phpVersions {
		if !v.IsDir() {
			continue
		}
		dir := filepath.Join(root, "runtimes", "php", v.Name())
		for _, name := range []string{"php.ini", "composer.bat", "composer"} {
			rewriteFile(filepath.Join(dir, name))
		}
	}
	for _, name := range []string{"config.json", "projects.json", "path.sh"} {
		rewriteFile(filepath.Join(root, name))
	}
	return count
}

// replaceFold replaces old with new case-insensitively (Windows paths).
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	lower := strings.ToLower(s)
	lowerOld := strings.ToLower(old)
	var sb strings.Builder
	i := 0
	for {
		j := strings.Index(lower[i:], lowerOld)
		if j < 0 {
			sb.WriteString(s[i:])
			break
		}
		sb.WriteString(s[i : i+j])
		sb.WriteString(repl)
		i += j + len(old)
	}
	return sb.String()
}

// rewriteManagedPATH re-points user PATH entries that live under the old dir.
func rewriteManagedPATH(oldDir, newDir string, logf func(string, ...interface{})) {
	entries, err := platform.GetUserPATH()
	if err != nil {
		logf("PATH read failed: %v", err)
		return
	}
	changed := false
	for i, e := range entries {
		if strings.HasPrefix(strings.ToLower(filepath.Clean(e)), strings.ToLower(filepath.Clean(oldDir))) {
			entries[i] = newDir + e[len(oldDir):]
			changed = true
		}
	}
	if changed {
		if err := platform.SetUserPATH(entries); err != nil {
			logf("PATH write failed: %v", err)
		} else {
			logf("PATH entries re-pointed")
		}
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// Resume-safe: a file already copied in a previous (interrupted) run is skipped.
		if existing, err := os.Stat(target); err == nil && existing.Size() == info.Size() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
