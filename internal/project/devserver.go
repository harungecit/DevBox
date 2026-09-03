package project

import (
	"fmt"
	"regexp"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
	"DevBox/internal/runtime"
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
	// stopping marks projects whose dev server DevBox is killing on purpose,
	// so the exit hook can tell a crash from a Stop click.
	stopping = make(map[string]bool)

	// OnDevServerExit, when set, is called (on its own goroutine) every time a
	// dev server process started by DevBox ends. crashed is false when the exit
	// was requested through StopDevServer / StopAllDevServers.
	OnDevServerExit func(name string, crashed bool)
)

// PID file path: ~/.devbox/services/devserver-{projectName}.pid
func devServerPidFile(projectName string) string {
	return filepath.Join(config.GetDataDir(), "services", fmt.Sprintf("devserver-%s.pid", projectName))
}

// Log file path: ~/.devbox/logs/devserver-{projectName}.log
func devServerLogFile(projectName string) string {
	return filepath.Join(config.GetDataDir(), "logs", fmt.Sprintf("devserver-%s.log", projectName))
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
		executable, args = GetStartCommand(proj.Framework, proj.Path, actualPort)
	}

	if executable == "" {
		return 0, fmt.Errorf("no start command available for framework: %s", proj.Framework)
	}

	// Resolve the runtime the project should run under: its pinned version, or
	// the globally active one. The bin dir goes first on PATH for the child
	// (so `npx`, `python`, `go`, `cargo` resolve to that version) and the
	// executable itself is resolved there too — exec.Command's own lookup uses
	// DevBox's PATH, which may predate the runtime install.
	binDir := ResolveRuntimeBinDir(proj)
	if binDir != "" {
		for _, candidate := range []string{
			filepath.Join(binDir, platform.ScriptName(executable)),
			filepath.Join(binDir, platform.BinaryName(executable)),
		} {
			if _, err := os.Stat(candidate); err == nil {
				executable = candidate
				break
			}
		}
	}

	cmd := exec.Command(executable, args...)
	cmd.Dir = proj.Path

	// Set environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", actualPort),
		"HOST=127.0.0.1",
	)
	if binDir != "" {
		// Plugin runtimes may need several dirs and variables (JAVA_HOME...).
		pathDirs := []string{binDir}
		if mgr, ok := runtime.Registry[proj.Runtime]; ok {
			if ver := ResolveRuntimeVersion(proj); ver != "" {
				pathDirs = runtime.ActivationPaths(mgr, ver)
				for k, v := range runtime.ActivationVars(mgr, ver) {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
			}
		}
		cmd.Env = append(cmd.Env, "PATH="+strings.Join(pathDirs, string(os.PathListSeparator))+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	// Create new process group + hide window (platform-aware)
	platform.SetProcessAttrs(cmd, true, true)

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
	started := time.Now()
	go func() {
		cmd.Wait()
		logFile.Close()
		devServerMu.Lock()
		// Only clear the entry if it is still ours (a restart may have replaced it).
		if inst, ok := devServers[proj.Name]; ok && inst.process.Pid == pid {
			delete(devServers, proj.Name)
			os.Remove(devServerPidFile(proj.Name))
		}
		crashed := !stopping[proj.Name]
		delete(stopping, proj.Name)
		devServerMu.Unlock()
		// Immediate exits are reported synchronously by StartDevServer below.
		if hook := OnDevServerExit; hook != nil && time.Since(started) > 2*time.Second {
			go hook(proj.Name, crashed)
		}
	}()

	// Wait briefly and verify the process is still alive
	time.Sleep(2 * time.Second)

	if !isDevServerProcessAlive(pid) {
		os.Remove(pidPath)
		devServerMu.Lock()
		delete(devServers, proj.Name)
		delete(stopping, proj.Name)
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
		stopping[projectName] = true
		platform.KillProcessTree(inst.process.Pid)
		delete(devServers, projectName)
	}

	// Also try via PID file
	pidPath := devServerPidFile(projectName)
	data, err := os.ReadFile(pidPath)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil {
			platform.KillProcessTree(pid)
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
		stopping[name] = true
		platform.KillProcessTree(inst.process.Pid)
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
	for i, l := range allLines {
		allLines[i] = StripANSI(l)
	}
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}
	return allLines, nil
}

// ansiRe matches terminal escape sequences (colours, cursor control such as
// "ESC[?25h") that dev servers write to their log.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[()][A-Z0-9]|\x1b[=>]`)

// StripANSI removes terminal escape sequences and control characters so log
// lines can be shown in the UI.
func StripANSI(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// LastMeaningfulLogLine returns the last log line that looks like a message
// (non-empty, not a lone bracket/brace from pretty-printed JSON) for a crash
// notice; empty when there is none.
func LastMeaningfulLogLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if len(l) < 4 || strings.Trim(l, "{}[](),;|-=_ ") == "" {
			continue
		}
		return l
	}
	return ""
}

// ResolveRuntimeVersion returns the runtime version a project should use: its
// pinned version when that version is installed, otherwise the global one.
// Returns "" when the runtime has no usable version at all.
func ResolveRuntimeVersion(proj Project) string {
	if proj.Runtime == "" || proj.Runtime == "static" {
		return ""
	}
	mgr, ok := runtime.Registry[proj.Runtime]
	if !ok {
		return ""
	}
	if proj.RuntimeVersion != "" {
		if _, err := os.Stat(mgr.BinaryPath(proj.RuntimeVersion)); err == nil {
			return proj.RuntimeVersion
		}
	}
	global, _ := mgr.GetGlobal()
	return global
}

// ResolveRuntimeBinDir returns the bin directory of the resolved runtime version, or "".
func ResolveRuntimeBinDir(proj Project) string {
	ver := ResolveRuntimeVersion(proj)
	if ver == "" {
		return ""
	}
	mgr, ok := runtime.Registry[proj.Runtime]
	if !ok {
		return ""
	}
	dir := mgr.BinaryPath(ver)
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

// --- internal helpers ---

func isDevServerProcessAlive(pid int) bool {
	return platform.IsProcessRunning(pid)
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
