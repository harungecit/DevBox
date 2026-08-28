package runtime

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

const composerDownloadURL = "https://getcomposer.org/download/latest-stable/composer.phar"

// --- Composer ---

// IsComposerInstalled checks if composer.phar exists in the active PHP directory
func IsComposerInstalled() bool {
	phpDir := activePHPDir()
	if phpDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(phpDir, "composer.phar"))
	return err == nil
}

// InstallComposer downloads composer.phar and creates a bat wrapper
func InstallComposer(progress chan<- Progress) error {
	phpDir := activePHPDir()
	if phpDir == "" {
		return fmt.Errorf("no active PHP version set")
	}
	return InstallComposerInto(phpDir, progress)
}

// InstallComposerInto installs Composer into a specific PHP version directory.
func InstallComposerInto(phpDir string, progress chan<- Progress) error {
	pharPath := filepath.Join(phpDir, "composer.phar")

	if progress != nil {
		progress <- Progress{Percent: 10, Message: "Downloading composer.phar..."}
	}

	resp, err := http.Get(composerDownloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(pharPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(pharPath)
		return err
	}

	if progress != nil {
		progress <- Progress{Percent: 80, Message: "Creating wrapper..."}
	}

	// Create platform-appropriate wrapper script
	if goruntime.GOOS == "windows" {
		batPath := filepath.Join(phpDir, "composer.bat")
		bat := "@echo off\r\nphp \"%~dp0composer.phar\" %*\r\n"
		if err := os.WriteFile(batPath, []byte(bat), 0644); err != nil {
			return err
		}
	} else {
		shPath := filepath.Join(phpDir, "composer")
		sh := "#!/bin/sh\nexec php \"$(dirname \"$0\")/composer.phar\" \"$@\"\n"
		if err := os.WriteFile(shPath, []byte(sh), 0755); err != nil {
			return err
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Composer installed"}
	}
	return nil
}

// GetComposerVersion returns the installed Composer version string
func GetComposerVersion() string {
	phpDir := activePHPDir()
	if phpDir == "" {
		return ""
	}

	phpExe := filepath.Join(phpDir, platform.BinaryName("php"))
	pharPath := filepath.Join(phpDir, "composer.phar")

	if _, err := os.Stat(pharPath); os.IsNotExist(err) {
		return ""
	}

	cmd := exec.Command(phpExe, pharPath, "--version", "--no-ansi")
	platform.SetProcessAttrs(cmd, false, true)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Output: "Composer version 2.8.x ..."
	parts := strings.Fields(strings.TrimSpace(string(out)))
	for i, p := range parts {
		if p == "version" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return strings.TrimSpace(string(out))
}

// --- PHP-CGI (FastCGI) — one instance per PHP version in use ---
//
// The globally active PHP version always listens on 9000. Projects pinned to a
// different version get their own php-cgi instance on a port allocated from
// config.PhpCgiPorts (9001+), so nginx/caddy/apache vhosts can hand each
// project to the exact PHP version it asked for. Instance state lives in
// services/php-cgi-<version>.pid ("pid\nport").

// PHPCGIBasePort is the FastCGI port of the globally active PHP version.
const PHPCGIBasePort = 9000

// PHPCGIInstance describes a running php-cgi process.
type PHPCGIInstance struct {
	Version string `json:"version"`
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
}

func phpCgiStateFile(version string) string {
	return filepath.Join(config.GetDataDir(), "services", "php-cgi-"+version+".pid")
}

func legacyPhpCgiPidFile() string {
	return filepath.Join(config.GetDataDir(), "services", "php-cgi.pid")
}

// PHPCGIPortFor returns (allocating if needed) the FastCGI port for a PHP version.
func PHPCGIPortFor(version string) int {
	global, _ := getGlobalVersion("php")
	if version == global || version == "" {
		return PHPCGIBasePort
	}
	cfg := config.Get()
	if cfg.PhpCgiPorts == nil {
		cfg.PhpCgiPorts = map[string]int{}
	}
	if p, ok := cfg.PhpCgiPorts[version]; ok && p != PHPCGIBasePort {
		return p
	}
	used := map[int]bool{PHPCGIBasePort: true}
	for _, p := range cfg.PhpCgiPorts {
		used[p] = true
	}
	for p := PHPCGIBasePort + 1; p < PHPCGIBasePort+100; p++ {
		if used[p] {
			continue
		}
		cfg.PhpCgiPorts[version] = p
		config.Save()
		return p
	}
	return PHPCGIBasePort
}

// RunningPHPCGIInstances lists live php-cgi processes started by DevBox.
// Stale state files are cleaned up on the way; a php-cgi.pid left by an older
// DevBox (single-instance era) is killed because its version is unknown.
func RunningPHPCGIInstances() []PHPCGIInstance {
	svcDir := filepath.Join(config.GetDataDir(), "services")

	if data, err := os.ReadFile(legacyPhpCgiPidFile()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && isProcessAlive(pid) {
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		}
		os.Remove(legacyPhpCgiPidFile())
	}

	entries, err := os.ReadDir(svcDir)
	if err != nil {
		return nil
	}
	var out []PHPCGIInstance
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "php-cgi-") || !strings.HasSuffix(name, ".pid") {
			continue
		}
		version := strings.TrimSuffix(strings.TrimPrefix(name, "php-cgi-"), ".pid")
		path := filepath.Join(svcDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
		port := PHPCGIBasePort
		if len(lines) > 1 {
			port, _ = strconv.Atoi(strings.TrimSpace(lines[1]))
		}
		if pid <= 0 || !isProcessAlive(pid) {
			os.Remove(path)
			continue
		}
		out = append(out, PHPCGIInstance{Version: version, Port: port, PID: pid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

func findPHPCGIInstance(version string) *PHPCGIInstance {
	for _, inst := range RunningPHPCGIInstances() {
		if inst.Version == version {
			i := inst
			return &i
		}
	}
	return nil
}

// StartPHPCGIVersion starts php-cgi for a PHP version on its assigned port.
// Returns the port. Idempotent when the instance is already up on that port.
func StartPHPCGIVersion(version string) (int, error) {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	cgiExe := filepath.Join(phpDir, platform.BinaryName("php-cgi"))
	if _, err := os.Stat(cgiExe); os.IsNotExist(err) {
		return 0, fmt.Errorf("php-cgi not found for PHP %s", version)
	}

	port := PHPCGIPortFor(version)
	if inst := findPHPCGIInstance(version); inst != nil {
		if inst.Port == port {
			return port, nil
		}
		stopInstance(*inst)
	}
	if portInUse(port) {
		// Whatever holds the port is not one of ours (we just checked) — most
		// likely a php-cgi from a previous session whose state file was lost.
		return 0, fmt.Errorf("FastCGI port %d is already in use", port)
	}

	// Ensure php.ini exists and common extensions are enabled
	GetPHPExtensions(version) // creates php.ini if needed + sets extension_dir
	EnableCommonExtensions(version)

	logDir := filepath.Join(config.GetDataDir(), "logs")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, "php-cgi-"+version+".log")

	cmd := exec.Command(cgiExe, "-b", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Dir = phpDir
	// PHP_FCGI_MAX_REQUESTS=0 keeps php-cgi alive instead of exiting after 500
	// requests; PHP_FCGI_CHILDREN lets it serve a few requests concurrently.
	cmd.Env = append(os.Environ(), "PHP_FCGI_MAX_REQUESTS=0", "PHP_FCGI_CHILDREN=4")
	platform.SetProcessAttrs(cmd, true, true)

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if err := cmd.Start(); err != nil {
		if f != nil {
			f.Close()
		}
		return 0, fmt.Errorf("failed to start php-cgi: %w", err)
	}

	pid := cmd.Process.Pid
	state := phpCgiStateFile(version)
	os.MkdirAll(filepath.Dir(state), 0755)
	os.WriteFile(state, []byte(fmt.Sprintf("%d\n%d", pid, port)), 0644)

	go func() {
		cmd.Wait()
		if f != nil {
			f.Close()
		}
	}()

	time.Sleep(500 * time.Millisecond)
	if !isProcessAlive(pid) {
		os.Remove(state)
		return 0, fmt.Errorf("php-cgi %s exited immediately (see logs/php-cgi-%s.log)", version, version)
	}
	return port, nil
}

func stopInstance(inst PHPCGIInstance) {
	if p, err := os.FindProcess(inst.PID); err == nil {
		p.Kill()
	}
	os.Remove(phpCgiStateFile(inst.Version))
}

// StopPHPCGIVersion stops the php-cgi instance serving a PHP version.
func StopPHPCGIVersion(version string) {
	if inst := findPHPCGIInstance(version); inst != nil {
		stopInstance(*inst)
	}
}

// EnsurePHPCGI reconciles running instances with the set of versions that
// projects need: extra instances are stopped, missing ones started, and any
// instance whose assigned port changed (e.g. the global version switched, so
// 9000 now belongs to another version) is restarted. Returns version→port for
// every version that ended up running; failures are reported per version.
func EnsurePHPCGI(versions []string) (map[string]int, map[string]error) {
	want := map[string]bool{}
	for _, v := range versions {
		if v != "" {
			want[v] = true
		}
	}
	for _, inst := range RunningPHPCGIInstances() {
		if !want[inst.Version] || inst.Port != PHPCGIPortFor(inst.Version) {
			stopInstance(inst)
		}
	}
	ports := map[string]int{}
	errs := map[string]error{}
	for v := range want {
		port, err := StartPHPCGIVersion(v)
		if err != nil {
			errs[v] = err
			continue
		}
		ports[v] = port
	}
	return ports, errs
}

// StartPHPCGI starts the FastCGI instance for a version. The port argument is
// kept for API compatibility; ports are managed by PHPCGIPortFor.
func StartPHPCGI(version string, _ int) error {
	_, err := StartPHPCGIVersion(version)
	return err
}

// StopPHPCGI stops every php-cgi instance.
func StopPHPCGI() error {
	for _, inst := range RunningPHPCGIInstances() {
		stopInstance(inst)
	}
	return nil
}

// IsPHPCGIRunning reports whether any php-cgi instance is alive.
func IsPHPCGIRunning() bool {
	return len(RunningPHPCGIInstances()) > 0
}

func portInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// --- helpers ---

func activePHPDir() string {
	global, _ := getGlobalVersion("php")
	if global == "" {
		return ""
	}
	return filepath.Join(runtimeBaseDir("php"), global)
}

// PHPVersionInstalled reports whether a PHP version directory exists.
func PHPVersionInstalled(version string) bool {
	if version == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(runtimeBaseDir("php"), version, platform.BinaryName("php")))
	return err == nil
}

func isProcessAlive(pid int) bool {
	return platform.IsProcessRunning(pid)
}
