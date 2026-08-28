package tools

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
)

// AdminerServerPort is the loopback port DevBox uses for the Adminer dev server.
// We deliberately stay in the 8500+ band (away from the very common 8080/9999)
// to minimize collision with random local services. Fixed so the URL stays
// predictable; collisions surface as a start error from the user.
const AdminerServerPort = 8505

func adminerPidPath() string {
	return filepath.Join(config.GetDataDir(), "tools", "adminer.pid")
}

func adminerLogPath() string {
	return filepath.Join(config.GetDataDir(), "tools", "adminer.log")
}

// StartAdminerServer launches PHP's built-in web server to serve adminer.php.
// phpBinDir must be the directory containing the active PHP binary. Idempotent —
// returns nil if already running.
func StartAdminerServer(phpBinDir string) error {
	if IsAdminerServerRunning() {
		return nil
	}
	if !IsAdminerInstalled() {
		return fmt.Errorf("Adminer is not installed")
	}

	php := filepath.Join(phpBinDir, platform.BinaryName("php"))
	if _, err := os.Stat(php); os.IsNotExist(err) {
		return fmt.Errorf("php binary not found at %s — install and activate a PHP version first", php)
	}

	adminerDir := filepath.Join(config.GetDataDir(), "tools", "adminer")

	logFile, err := os.OpenFile(adminerLogPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("cannot open adminer log: %w", err)
	}

	// Adminer is third-party PHP code. The DevBox dev preset turns on display_errors
	// + E_ALL which surfaces every deprecation notice from upstream Adminer (e.g.
	// PHP 8.4 nullable-parameter warnings in AdminerEvo 4.8.4). Override those for
	// the Adminer server only — errors still go to the log file. The user's own
	// projects keep their own php.ini settings.
	cmd := exec.Command(php,
		"-d", "display_errors=Off",
		"-d", "log_errors=On",
		"-d", "error_reporting=E_ALL & ~E_DEPRECATED & ~E_STRICT",
		"-S", fmt.Sprintf("127.0.0.1:%d", AdminerServerPort),
		"-t", adminerDir,
	)
	cmd.Dir = adminerDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	platform.SetProcessAttrs(cmd, true, true)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start Adminer server: %w", err)
	}

	pid := cmd.Process.Pid
	os.MkdirAll(filepath.Dir(adminerPidPath()), 0755)
	if err := os.WriteFile(adminerPidPath(), []byte(strconv.Itoa(pid)), 0644); err != nil {
		// Best-effort cleanup if we can't track it.
		cmd.Process.Kill()
		logFile.Close()
		return fmt.Errorf("cannot write adminer pid file: %w", err)
	}

	// Detach so the process keeps running after Start returns.
	go func() {
		cmd.Wait()
		logFile.Close()
	}()

	// Brief health check — `php -S` exits immediately if the port is in use.
	time.Sleep(400 * time.Millisecond)
	if !IsAdminerServerRunning() {
		os.Remove(adminerPidPath())
		return fmt.Errorf("Adminer server exited immediately — port %d may be in use", AdminerServerPort)
	}
	return nil
}

// StopAdminerServer kills the dev server process and removes its PID file.
// No-op if nothing is running.
func StopAdminerServer() error {
	data, err := os.ReadFile(adminerPidPath())
	if err != nil {
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(adminerPidPath())
		return nil
	}
	if proc, err := os.FindProcess(pid); err == nil {
		proc.Kill()
	}
	os.Remove(adminerPidPath())
	return nil
}

// IsAdminerServerRunning reports whether the dev server PID is alive.
func IsAdminerServerRunning() bool {
	data, err := os.ReadFile(adminerPidPath())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return platform.IsProcessRunning(pid)
}

// GetAdminerServerURL returns the loopback URL for the running dev server.
func GetAdminerServerURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/adminer.php", AdminerServerPort)
}

// UninstallAdminer stops the dev server (if running) and removes Adminer files.
func UninstallAdminer() error {
	StopAdminerServer()
	toolDir := filepath.Join(config.GetDataDir(), "tools", "adminer")
	return os.RemoveAll(toolDir)
}
