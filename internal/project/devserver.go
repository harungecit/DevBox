package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/service"
)

// devServerInstance holds in-memory process state for a running dev server
type devServerInstance struct {
	process *os.Process
	port    int
}

var (
	devServers  = make(map[string]*devServerInstance)
	devServerMu sync.Mutex
)

// PID file path: ~/.devbox/services/devserver-{projectName}.pid
func devServerPidFile(projectName string) string {
	return filepath.Join(config.GetDataDir(), "services", fmt.Sprintf("devserver-%s.pid", projectName))
}

// Log file path: ~/.devbox/logs/devserver-{projectName}.log
func devServerLogFile(projectName string) string {
	return filepath.Join(config.GetDataDir(), "logs", fmt.Sprintf("devserver-%s.log", projectName))
}

// GetStartCommand returns the command and args to start a dev server for a given framework.
// The port placeholder will be substituted with the actual port.
func GetStartCommand(framework string, port int) (string, []string) {
	portStr := strconv.Itoa(port)

	switch framework {
	case "Next.js":
		return "npx", []string{"next", "dev", "-p", portStr}
	case "Nuxt":
		return "npx", []string{"nuxt", "dev", "--port", portStr}
	case "Vue", "React", "Svelte", "Angular":
		return "npx", []string{"vite", "--port", portStr}
	case "Django":
		return "python", []string{"manage.py", "runserver", fmt.Sprintf("127.0.0.1:%s", portStr)}
	case "Python":
		return "python", []string{"-m", "http.server", portStr}
	case "Go":
		return "go", []string{"run", "."}
	case "Rust":
		return "cargo", []string{"run"}
	}
	return "", nil
}

// parseCustomCommand splits a user-provided start command string into executable and args.
// Supports {port} placeholder replacement.
func parseCustomCommand(cmd string, port int) (string, []string) {
	portStr := strconv.Itoa(port)
	cmd = strings.ReplaceAll(cmd, "{port}", portStr)

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

// StartDevServer starts the dev server for a project.
// Returns the actual port being used.
func StartDevServer(proj Project) (int, error) {
	devServerMu.Lock()
	defer devServerMu.Unlock()

	// Already running in-memory?
	if inst, ok := devServers[proj.Name]; ok {
		if isDevServerProcessAlive(inst.process.Pid) {
			return inst.port, nil
		}
		// Stale entry, clean up
		delete(devServers, proj.Name)
	}

	// Check PID file for a previously-started process
	if isDevServerRunningFromPID(proj.Name) {
		// Read port from existing state
		return 0, fmt.Errorf("dev server for %s is already running", proj.Name)
	}

	// Determine port
	preferredPort := proj.Port
	if preferredPort == 0 {
		preferredPort = DefaultPort(proj.Framework)
	}
	if preferredPort == 0 {
		preferredPort = 3000
	}
	actualPort := service.FindAvailablePort(preferredPort)

	// Build command
	var executable string
	var args []string

	if proj.StartCommand != "" {
		executable, args = parseCustomCommand(proj.StartCommand, actualPort)
	} else {
		executable, args = GetStartCommand(proj.Framework, actualPort)
	}

	if executable == "" {
		return 0, fmt.Errorf("no start command available for framework: %s", proj.Framework)
	}

	cmd := exec.Command(executable, args...)
	cmd.Dir = proj.Path

	// Set environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", actualPort),
		"HOST=127.0.0.1",
	)

	// Windows: create new process group + hide window
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}

	// Redirect output to log file
	logPath := devServerLogFile(proj.Name)
	os.MkdirAll(filepath.Dir(logPath), 0755)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, fmt.Errorf("failed to create log file: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("failed to start dev server for %s: %w", proj.Name, err)
	}

	pid := cmd.Process.Pid

	// Save PID file
	pidPath := devServerPidFile(proj.Name)
	os.MkdirAll(filepath.Dir(pidPath), 0755)
	os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)

	// Store in-memory
	devServers[proj.Name] = &devServerInstance{
		process: cmd.Process,
		port:    actualPort,
	}

	// Let the process run independently
	go func() {
		cmd.Wait()
		logFile.Close()
		devServerMu.Lock()
		delete(devServers, proj.Name)
		devServerMu.Unlock()
		os.Remove(devServerPidFile(proj.Name))
	}()

	// Wait briefly and verify the process is still alive
	time.Sleep(2 * time.Second)

	if !isDevServerProcessAlive(pid) {
		os.Remove(pidPath)
		devServerMu.Lock()
		delete(devServers, proj.Name)
		devServerMu.Unlock()

		// Read error details from log
		errInfo := readDevServerLog(proj.Name)
		if errInfo != "" {
			return 0, fmt.Errorf("dev server for %s exited immediately:\n%s", proj.Name, errInfo)
		}
		return 0, fmt.Errorf("dev server for %s exited immediately after start", proj.Name)
	}

	return actualPort, nil
}

// StopDevServer stops the dev server for a project.
func StopDevServer(projectName string) error {
	devServerMu.Lock()
	defer devServerMu.Unlock()

	// Try in-memory first
	if inst, ok := devServers[projectName]; ok {
		inst.process.Kill()
		delete(devServers, projectName)
	}

	// Also try via PID file
	pidPath := devServerPidFile(projectName)
	data, err := os.ReadFile(pidPath)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil {
			proc, err := os.FindProcess(pid)
			if err == nil {
				proc.Kill()
			}
		}
		os.Remove(pidPath)
	}

	return nil
}

// IsDevServerRunning checks if a dev server is running for a project.
func IsDevServerRunning(projectName string) bool {
	devServerMu.Lock()
	defer devServerMu.Unlock()

	if inst, ok := devServers[projectName]; ok {
		if isDevServerProcessAlive(inst.process.Pid) {
			return true
		}
		delete(devServers, projectName)
	}

	return isDevServerRunningFromPID(projectName)
}

// GetDevServerPort returns the port for a running dev server, or 0 if not running.
func GetDevServerPort(projectName string) int {
	devServerMu.Lock()
	defer devServerMu.Unlock()

	if inst, ok := devServers[projectName]; ok {
		if isDevServerProcessAlive(inst.process.Pid) {
			return inst.port
		}
	}
	return 0
}

// GetRunningDevServers returns all running dev servers as a map of project name to port.
func GetRunningDevServers() map[string]int {
	devServerMu.Lock()
	defer devServerMu.Unlock()

	result := make(map[string]int)

	// In-memory instances
	for name, inst := range devServers {
		if isDevServerProcessAlive(inst.process.Pid) {
			result[name] = inst.port
		}
	}

	// Scan PID files for dev servers from previous sessions
	servicesDir := filepath.Join(config.GetDataDir(), "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		fname := entry.Name()
		if !strings.HasPrefix(fname, "devserver-") || !strings.HasSuffix(fname, ".pid") {
			continue
		}
		projName := strings.TrimPrefix(fname, "devserver-")
		projName = strings.TrimSuffix(projName, ".pid")

		if _, exists := result[projName]; exists {
			continue
		}

		data, err := os.ReadFile(filepath.Join(servicesDir, fname))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}

		if isDevServerProcessAlive(pid) {
			result[projName] = 0 // port unknown for recovered processes
		} else {
			os.Remove(devServerPidFile(projName))
		}
	}

	return result
}

// StopAllDevServers stops all running dev servers (called on app shutdown).
func StopAllDevServers() {
	devServerMu.Lock()
	defer devServerMu.Unlock()

	for name, inst := range devServers {
		inst.process.Kill()
		os.Remove(devServerPidFile(name))
	}
	devServers = make(map[string]*devServerInstance)

	// Also clean up any PID files from previous sessions
	servicesDir := filepath.Join(config.GetDataDir(), "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		fname := entry.Name()
		if !strings.HasPrefix(fname, "devserver-") || !strings.HasSuffix(fname, ".pid") {
			continue
		}
		projName := strings.TrimPrefix(fname, "devserver-")
		projName = strings.TrimSuffix(projName, ".pid")

		data, err := os.ReadFile(filepath.Join(servicesDir, fname))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			os.Remove(filepath.Join(servicesDir, fname))
			continue
		}
		if isDevServerProcessAlive(pid) {
			proc, _ := os.FindProcess(pid)
			if proc != nil {
				proc.Kill()
			}
		}
		os.Remove(filepath.Join(servicesDir, fname))
	}
}

// GetDevServerLogs reads the last N lines of the dev server log file.
func GetDevServerLogs(projectName string, lines int) ([]string, error) {
	logPath := devServerLogFile(projectName)
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	allLines := splitLogLines(string(data))
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}
	return allLines, nil
}

// --- internal helpers ---

func isDevServerProcessAlive(pid int) bool {
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

func isDevServerRunningFromPID(projectName string) bool {
	pidPath := devServerPidFile(projectName)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidPath)
		return false
	}
	if !isDevServerProcessAlive(pid) {
		os.Remove(pidPath)
		return false
	}
	return true
}

func readDevServerLog(projectName string) string {
	logPath := devServerLogFile(projectName)
	data, err := os.ReadFile(logPath)
	if err != nil || len(data) == 0 {
		return ""
	}
	content := strings.TrimSpace(string(data))
	lines := splitLogLines(content)
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return strings.Join(lines, "\n")
}

func splitLogLines(s string) []string {
	var lines []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else if c != '\r' {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
