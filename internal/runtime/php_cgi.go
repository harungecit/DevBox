package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"DevBox/internal/config"
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

	pharPath := filepath.Join(phpDir, "composer.phar")
	batPath := filepath.Join(phpDir, "composer.bat")

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

	// Create composer.bat wrapper
	bat := fmt.Sprintf("@echo off\r\nphp \"%%~dp0composer.phar\" %%*\r\n")
	if err := os.WriteFile(batPath, []byte(bat), 0644); err != nil {
		return err
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

	phpExe := filepath.Join(phpDir, "php.exe")
	pharPath := filepath.Join(phpDir, "composer.phar")

	if _, err := os.Stat(pharPath); os.IsNotExist(err) {
		return ""
	}

	cmd := exec.Command(phpExe, pharPath, "--version", "--no-ansi")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
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

// --- PHP-CGI ---

var phpCgiPidFile = ""

// StartPHPCGI starts php-cgi.exe in FastCGI mode
func StartPHPCGI(version string, port int) error {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	cgiExe := filepath.Join(phpDir, "php-cgi.exe")

	if _, err := os.Stat(cgiExe); os.IsNotExist(err) {
		return fmt.Errorf("php-cgi.exe not found for PHP %s", version)
	}

	if IsPHPCGIRunning() {
		return nil
	}

	// Ensure php.ini exists and common extensions are enabled
	GetPHPExtensions(version) // creates php.ini if needed + sets extension_dir
	EnableCommonExtensions(version)

	logDir := filepath.Join(config.GetDataDir(), "logs")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, "php-cgi.log")

	cmd := exec.Command(cgiExe, "-b", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Dir = phpDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if err := cmd.Start(); err != nil {
		if f != nil {
			f.Close()
		}
		return fmt.Errorf("failed to start php-cgi: %w", err)
	}

	pid := cmd.Process.Pid
	phpCgiPidFile = filepath.Join(config.GetDataDir(), "services", "php-cgi.pid")
	os.MkdirAll(filepath.Dir(phpCgiPidFile), 0755)
	os.WriteFile(phpCgiPidFile, []byte(strconv.Itoa(pid)), 0644)

	go func() {
		cmd.Wait()
		if f != nil {
			f.Close()
		}
	}()

	// Brief check to confirm it started
	time.Sleep(500 * time.Millisecond)
	if !isProcessAlive(pid) {
		os.Remove(phpCgiPidFile)
		return fmt.Errorf("php-cgi exited immediately")
	}

	return nil
}

// StopPHPCGI stops the running php-cgi process
func StopPHPCGI() error {
	if phpCgiPidFile == "" {
		phpCgiPidFile = filepath.Join(config.GetDataDir(), "services", "php-cgi.pid")
	}

	data, err := os.ReadFile(phpCgiPidFile)
	if err != nil {
		return nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(phpCgiPidFile)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err == nil {
		proc.Kill()
	}

	os.Remove(phpCgiPidFile)
	return nil
}

// IsPHPCGIRunning checks if php-cgi process is alive
func IsPHPCGIRunning() bool {
	pidFile := filepath.Join(config.GetDataDir(), "services", "php-cgi.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}

	return isProcessAlive(pid)
}

// --- helpers ---

func activePHPDir() string {
	global, _ := getGlobalVersion("php")
	if global == "" {
		return ""
	}
	return filepath.Join(runtimeBaseDir("php"), global)
}

func isProcessAlive(pid int) bool {
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	const STILL_ACTIVE = 259

	handle, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	err = syscall.GetExitCodeProcess(handle, &exitCode)
	if err != nil {
		return false
	}
	return exitCode == STILL_ACTIVE
}
