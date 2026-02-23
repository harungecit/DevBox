package tunnel

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"DevBox/internal/config"
)

var tunnelURLRe = regexp.MustCompile(`(https://[a-zA-Z0-9-]+\.trycloudflare\.com)`)

// tunnelInstance represents a running tunnel for a single project
type tunnelInstance struct {
	process *os.Process
	url     string
}

var (
	tunnels  = make(map[string]*tunnelInstance) // key: project name
	tunnelMu sync.Mutex
)

// cloudflaredPath returns the path to the cloudflared executable
func cloudflaredPath() string {
	return filepath.Join(config.GetDataDir(), "tools", "cloudflared", "cloudflared.exe")
}

// per-project state file helpers
func pidFile(projectName string) string {
	return filepath.Join(config.GetDataDir(), "services", fmt.Sprintf("cloudflared-%s.pid", projectName))
}

func urlFile(projectName string) string {
	return filepath.Join(config.GetDataDir(), "services", fmt.Sprintf("cloudflared-%s.url", projectName))
}

// isProcessAlive checks if a process with the given PID is still running on Windows.
func isProcessAlive(pid int) bool {
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	h, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)
	return true
}

// killStaleProcess kills a cloudflared process left from a previous session for a specific project
func killStaleProcess(projectName string) {
	pf := pidFile(projectName)
	data, err := os.ReadFile(pf)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pf)
		os.Remove(urlFile(projectName))
		return
	}
	if isProcessAlive(pid) {
		proc, err := os.FindProcess(pid)
		if err == nil {
			proc.Kill()
		}
	}
	os.Remove(pf)
	os.Remove(urlFile(projectName))
}

// IsCloudflaredInstalled checks if cloudflared is available
func IsCloudflaredInstalled() bool {
	_, err := os.Stat(cloudflaredPath())
	return err == nil
}

// InstallCloudflared downloads cloudflared binary
func InstallCloudflared() error {
	toolDir := filepath.Dir(cloudflaredPath())
	os.MkdirAll(toolDir, 0755)

	downloadURL := "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe"
	dest := cloudflaredPath()

	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf(`Invoke-WebRequest -Uri "%s" -OutFile "%s" -UseBasicParsing`, downloadURL, dest))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download cloudflared: %w", err)
	}

	return nil
}

// StartTunnel starts a cloudflared quick tunnel for a specific project.
// ssl=true connects via https://127.0.0.1:443 with --no-tls-verify to avoid HTTP→HTTPS redirect.
// Multiple tunnels can run in parallel for different projects.
func StartTunnel(port int, projectName string, domain string, ssl bool) error {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	if _, exists := tunnels[projectName]; exists {
		return fmt.Errorf("tunnel already running for %s", projectName)
	}

	// Kill any stale process for this project from a previous session
	killStaleProcess(projectName)

	if !IsCloudflaredInstalled() {
		if err := InstallCloudflared(); err != nil {
			return err
		}
	}

	exe := cloudflaredPath()

	// For SSL projects, connect directly to HTTPS:443 to avoid the HTTP→HTTPS redirect
	// which would redirect to the .test domain (unreachable externally).
	var originURL string
	if ssl {
		originURL = "https://127.0.0.1:443"
	} else {
		originURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	args := []string{"tunnel", "--url", originURL}

	if ssl {
		args = append(args, "--no-tls-verify")
		// For SSL: do NOT set --http-host-header so the tunnel URL stays as Host.
		// nginx will use the SSL vhost as default for port 443.
		// This prevents Laravel/PHP from redirecting to the .test domain.
	} else if domain != "" {
		// For non-SSL: set Host header so nginx routes to the correct vhost on port 80
		// (port 80 has a default server that would catch unmatched requests).
		args = append(args, "--http-host-header", domain)
	}

	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	inst := &tunnelInstance{
		process: cmd.Process,
	}
	tunnels[projectName] = inst

	// Save PID for state recovery
	pf := pidFile(projectName)
	os.MkdirAll(filepath.Dir(pf), 0755)
	os.WriteFile(pf, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)

	// Scan a reader for the tunnel URL
	scanForURL := func(r interface{ Read([]byte) (int, error) }) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if match := tunnelURLRe.FindString(line); match != "" {
				tunnelMu.Lock()
				if t, ok := tunnels[projectName]; ok {
					t.url = match
				}
				tunnelMu.Unlock()
				// Persist URL for state recovery
				os.WriteFile(urlFile(projectName), []byte(match), 0644)
			}
		}
	}

	go scanForURL(stdout)
	go scanForURL(stderr)

	go func() {
		cmd.Wait()
		tunnelMu.Lock()
		delete(tunnels, projectName)
		tunnelMu.Unlock()
		os.Remove(pidFile(projectName))
		os.Remove(urlFile(projectName))
	}()

	return nil
}

// StopTunnel stops the tunnel for a specific project
func StopTunnel(projectName string) error {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	if inst, ok := tunnels[projectName]; ok {
		inst.process.Kill()
		delete(tunnels, projectName)
	}

	// Also try via PID file (in case process was started in a previous session)
	pf := pidFile(projectName)
	data, err := os.ReadFile(pf)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil {
			proc, err := os.FindProcess(pid)
			if err == nil {
				proc.Kill()
			}
		}
		os.Remove(pf)
	}

	os.Remove(urlFile(projectName))

	return nil
}

// GetTunnelURL returns the public tunnel URL for a specific project
func GetTunnelURL(projectName string) string {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	if inst, ok := tunnels[projectName]; ok && inst.url != "" {
		return inst.url
	}

	// Try to recover from file
	data, err := os.ReadFile(urlFile(projectName))
	if err == nil && len(data) > 0 {
		url := strings.TrimSpace(string(data))
		if inst, ok := tunnels[projectName]; ok {
			inst.url = url
		}
		return url
	}

	return ""
}

// IsTunnelRunning checks if a tunnel is active for a specific project
func IsTunnelRunning(projectName string) bool {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	if _, ok := tunnels[projectName]; ok {
		return true
	}

	// Check PID file for recovery
	pf := pidFile(projectName)
	data, err := os.ReadFile(pf)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}

	if !isProcessAlive(pid) {
		// Dead process, clean up stale files
		os.Remove(pf)
		os.Remove(urlFile(projectName))
		return false
	}

	return true
}

// GetRunningTunnels returns a map of project names to their tunnel URLs
// for all currently running tunnels (both in-memory and recovered from PID files).
func GetRunningTunnels() map[string]string {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	result := make(map[string]string)

	// In-memory tunnels
	for name, inst := range tunnels {
		result[name] = inst.url
	}

	// Scan PID files for tunnels from previous sessions
	servicesDir := filepath.Join(config.GetDataDir(), "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "cloudflared-") || !strings.HasSuffix(name, ".pid") {
			continue
		}
		// Extract project name: cloudflared-{name}.pid
		projName := strings.TrimPrefix(name, "cloudflared-")
		projName = strings.TrimSuffix(projName, ".pid")

		if _, exists := result[projName]; exists {
			continue // already known from in-memory
		}

		data, err := os.ReadFile(filepath.Join(servicesDir, name))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}

		if isProcessAlive(pid) {
			// Recover URL from file
			urlData, err := os.ReadFile(urlFile(projName))
			if err == nil {
				result[projName] = strings.TrimSpace(string(urlData))
			} else {
				result[projName] = ""
			}
		} else {
			// Dead process, clean up
			os.Remove(pidFile(projName))
			os.Remove(urlFile(projName))
		}
	}

	return result
}

// StopAllTunnels stops all running tunnels (called on app shutdown)
func StopAllTunnels() {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	for name, inst := range tunnels {
		inst.process.Kill()
		os.Remove(pidFile(name))
		os.Remove(urlFile(name))
	}
	tunnels = make(map[string]*tunnelInstance)
}
