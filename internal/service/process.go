package service

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StartProcess starts a service process, verifies it stays alive, and saves its PID.
// Returns an error if the process crashes immediately after starting.
func StartProcess(name string, executable string, args []string, workDir string, logFile string) (*os.Process, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = workDir

	// Use CREATE_NEW_PROCESS_GROUP + HideWindow (NOT CREATE_NO_WINDOW!)
	// CREATE_NO_WINDOW removes the console entirely, which breaks services
	// like nginx that use Windows console events for signal handling.
	// HideWindow creates a hidden console - the process works but no visible window.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}

	// Redirect output to log file
	var logFileHandle *os.File
	if logFile != "" {
		os.MkdirAll(dirOf(logFile), 0755)
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
			logFileHandle = f
		}
	}

	if err := cmd.Start(); err != nil {
		if logFileHandle != nil {
			logFileHandle.Close()
		}
		return nil, fmt.Errorf("failed to start %s: %w", name, err)
	}

	pid := cmd.Process.Pid
	pidFile := pidFilePath(name)
	os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644)

	// Let the process run independently
	go func() {
		cmd.Wait()
		if logFileHandle != nil {
			logFileHandle.Close()
		}
	}()

	// Wait briefly and verify the process is still alive
	// Services like nginx crash immediately if port is in use or config is invalid
	time.Sleep(1500 * time.Millisecond)

	if !isProcessRunning(pid) {
		os.Remove(pidFile)

		// Read error details from log file
		errInfo := readRecentLog(logFile)

		if errInfo != "" {
			return nil, fmt.Errorf("%s exited immediately after start:\n%s", name, errInfo)
		}
		return nil, fmt.Errorf("%s exited immediately after start - check service logs for details", name)
	}

	return cmd.Process, nil
}

// StopProcess stops a running process by reading its PID file
func StopProcess(name string) error {
	pidFile := pidFilePath(name)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("%s is not running (no PID file)", name)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("invalid PID file for %s", name)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidFile)
		return nil
	}

	err = proc.Kill()
	os.Remove(pidFile)

	if err != nil {
		if !isProcessRunning(pid) {
			return nil
		}
		return fmt.Errorf("failed to stop %s (PID %d): %w", name, pid, err)
	}

	return nil
}

// IsRunning checks if a service process is currently running
func IsRunning(name string) bool {
	pidFile := pidFilePath(name)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidFile)
		return false
	}

	if !isProcessRunning(pid) {
		os.Remove(pidFile)
		return false
	}

	return true
}

// isProcessRunning checks if a process with given PID exists on Windows
func isProcessRunning(pid int) bool {
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

// GetPID reads the PID from file
func GetPID(name string) int {
	data, err := os.ReadFile(pidFilePath(name))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// readRecentLog reads the last few lines of a log file for error reporting
func readRecentLog(logFile string) string {
	if logFile == "" {
		return ""
	}
	data, err := os.ReadFile(logFile)
	if err != nil || len(data) == 0 {
		return ""
	}
	content := strings.TrimSpace(string(data))
	lines := strings.Split(content, "\n")
	// Keep last 10 lines
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return strings.Join(lines, "\n")
}

// dirOf returns the directory part of a file path
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
