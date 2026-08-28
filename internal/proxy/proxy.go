// Package proxy implements DevBox's front-door reverse proxy: a bundled Caddy
// instance that listens on :80 (and :443 once HTTPS lands) and routes incoming
// requests to per-project backends based on the Host header.
//
// Each registered project's domain (e.g. `myapp.test`) gets a Caddyfile block
// that reverse-proxies to whichever backend the project picked (nginx, caddy,
// apache, frankenphp, or its own dev server). Hosts file maps *.test →
// 127.0.0.1; this proxy splits 127.0.0.1:80 traffic out to the right backend.
//
// The bundled Caddy is independent of any user-installed Caddy service —
// DevBox keeps its own binary under ~/.devbox/proxy/ so the front-door works
// even if the user picks nginx/apache as their primary webserver.
package proxy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
	"DevBox/internal/project"
	"DevBox/internal/service"
)

const (
	// HTTPPort is the loopback port the front-door proxy listens on for HTTP.
	HTTPPort = 80
	// HTTPSPort is reserved for the HTTPS listener wired up in phase 4.
	HTTPSPort = 443
	// AdminAddr is the loopback admin endpoint used for zero-downtime reloads.
	// Not Caddy's default 2019 so a user-installed Caddy service can't collide.
	AdminAddr = "127.0.0.1:20190"
)

func proxyDir() string {
	return filepath.Join(config.GetDataDir(), "proxy")
}

func caddyBinary() string {
	return filepath.Join(proxyDir(), platform.BinaryName("caddy"))
}

func caddyfilePath() string {
	return filepath.Join(proxyDir(), "Caddyfile")
}

func pidFile() string {
	return filepath.Join(proxyDir(), "proxy.pid")
}

func logFile() string {
	return filepath.Join(proxyDir(), "proxy.log")
}

// IsInstalled reports whether the front-door Caddy binary has been downloaded.
func IsInstalled() bool {
	_, err := os.Stat(caddyBinary())
	return err == nil
}

// IsRunning reports whether the proxy process from the PID file is alive.
func IsRunning() bool {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return platform.IsProcessRunning(pid)
}

// Start launches the front-door proxy. Idempotent — returns nil if already running.
// Generates a fresh Caddyfile from the current project list each call.
func Start() error {
	if !IsInstalled() {
		return fmt.Errorf("front-door proxy is not installed — install it from the Dashboard first")
	}
	if IsRunning() {
		return nil
	}

	if err := WriteCaddyfile(); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	logF, err := os.OpenFile(logFile(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("cannot open proxy log: %w", err)
	}

	cmd := exec.Command(caddyBinary(),
		"run",
		"--config", caddyfilePath(),
		"--adapter", "caddyfile",
	)
	cmd.Dir = proxyDir()
	cmd.Stdout = logF
	cmd.Stderr = logF
	platform.SetProcessAttrs(cmd, true, true)

	if err := cmd.Start(); err != nil {
		logF.Close()
		return fmt.Errorf("failed to start proxy: %w", err)
	}

	pid := cmd.Process.Pid
	os.MkdirAll(proxyDir(), 0755)
	if err := os.WriteFile(pidFile(), []byte(strconv.Itoa(pid)), 0644); err != nil {
		cmd.Process.Kill()
		logF.Close()
		return fmt.Errorf("cannot write proxy pid file: %w", err)
	}

	go func() {
		cmd.Wait()
		logF.Close()
	}()

	// Brief health check — Caddy exits immediately if port :80 is taken or
	// the OS denies the bind (no admin on Windows / no setcap on Linux).
	time.Sleep(500 * time.Millisecond)
	if !IsRunning() {
		os.Remove(pidFile())
		return fmt.Errorf("proxy exited immediately — port %d may be in use, or DevBox needs elevated permissions to bind it (check %s)", HTTPPort, logFile())
	}
	return nil
}

// Stop kills the proxy process and removes its PID file. No-op if not running.
func Stop() error {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidFile())
		return nil
	}
	if proc, err := os.FindProcess(pid); err == nil {
		proc.Kill()
	}
	os.Remove(pidFile())
	return nil
}

// Reload regenerates the Caddyfile from current projects and tells the running
// proxy to apply it without restart. If the proxy isn't running it's a no-op
// (the next Start will pick up the new Caddyfile on its own).
func Reload() error {
	if err := WriteCaddyfile(); err != nil {
		return err
	}
	if !IsRunning() {
		return nil
	}
	cmd := exec.Command(caddyBinary(),
		"reload",
		"--config", caddyfilePath(),
		"--adapter", "caddyfile",
		"--address", AdminAddr,
	)
	cmd.Dir = proxyDir()
	platform.SetProcessAttrs(cmd, false, true)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// A proxy started by an older DevBox (admin off) can't be reloaded:
		// restart it so the new routes take effect anyway.
		Stop()
		time.Sleep(300 * time.Millisecond)
		if startErr := Start(); startErr != nil {
			return fmt.Errorf("caddy reload failed (%s) and restart failed: %w", strings.TrimSpace(string(out)), startErr)
		}
	}
	return nil
}

// LogPath exposes the proxy log file path so the UI can show recent errors.
func LogPath() string { return logFile() }

// CaddyfilePathExposed returns the path of the generated Caddyfile (for UI display).
func CaddyfilePathExposed() string { return caddyfilePath() }

// ResolveBackend returns "127.0.0.1:PORT" for a project's chosen webserver, or
// an empty string if no backend can be reached (service not installed, no
// dev server port set, etc.).
func ResolveBackend(p project.Project) string {
	ws := p.Webserver
	if ws == "" || ws == "auto" {
		ws = resolveAutoWebserver(p.Runtime)
	}

	if ws == "devserver" {
		if p.Port > 0 {
			return fmt.Sprintf("127.0.0.1:%d", p.Port)
		}
		return ""
	}

	if mgr, ok := service.Registry[ws]; ok && mgr.IsInstalled() {
		return fmt.Sprintf("127.0.0.1:%d", mgr.Port())
	}
	return ""
}

// resolveAutoWebserver picks a sensible default for projects with Webserver=="auto"
// or empty. App-server runtimes use their own dev server. PHP/static fall back to
// whichever managed webserver is installed first, in preference order.
func resolveAutoWebserver(runtime string) string {
	switch runtime {
	case "node", "go", "python", "rust":
		return "devserver"
	}
	for _, name := range []string{"nginx", "caddy", "apache", "frankenphp"} {
		if mgr, ok := service.Registry[name]; ok && mgr.IsInstalled() {
			return name
		}
	}
	return ""
}
