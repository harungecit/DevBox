//go:build darwin

package platform

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func init() {
	current = &darwinPlatform{}
}

type darwinPlatform struct{}

// --- PATH management ---

// pathShFile returns the DevBox-managed PATH file, kept inside the data dir.
func pathShFile() string {
	return filepath.Join((&darwinPlatform{}).DefaultDataDir(), "path.sh")
}

func (d *darwinPlatform) GetUserPATH() ([]string, error) {
	data, err := os.ReadFile(pathShFile())
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var entries []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Lines like: export PATH="/some/dir:$PATH"
		if strings.HasPrefix(line, "export PATH=\"") && strings.HasSuffix(line, ":$PATH\"") {
			dir := strings.TrimPrefix(line, "export PATH=\"")
			dir = strings.TrimSuffix(dir, ":$PATH\"")
			if dir != "" {
				entries = append(entries, dir)
			}
		}
	}
	return entries, nil
}

func (d *darwinPlatform) SetUserPATH(entries []string) error {
	var lines []string
	lines = append(lines, "# DevBox managed PATH entries — do not edit manually")
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("export PATH=\"%s:$PATH\"", e))
	}

	shFile := pathShFile()
	os.MkdirAll(filepath.Dir(shFile), 0755)
	if err := os.WriteFile(shFile, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return err
	}

	return d.ensureShellSource()
}

func (d *darwinPlatform) AddToPath(dir string) error {
	entries, err := d.GetUserPATH()
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e == dir {
			return nil
		}
	}

	entries = append([]string{dir}, entries...)
	return d.SetUserPATH(entries)
}

func (d *darwinPlatform) RemoveFromPath(dir string) error {
	entries, err := d.GetUserPATH()
	if err != nil {
		return err
	}

	var newEntries []string
	for _, e := range entries {
		if e != dir {
			newEntries = append(newEntries, e)
		}
	}

	return d.SetUserPATH(newEntries)
}

func (d *darwinPlatform) GetSystemPATH() ([]string, error) {
	out, err := exec.Command("bash", "-l", "-c", "echo $PATH").Output()
	if err != nil {
		return nil, err
	}

	var entries []string
	for _, p := range strings.Split(strings.TrimSpace(string(out)), ":") {
		p = strings.TrimSpace(p)
		if p != "" {
			entries = append(entries, p)
		}
	}
	return entries, nil
}

func (d *darwinPlatform) BroadcastPathChange() {
	// No-op on macOS — new terminals pick up changes automatically.
}

func (d *darwinPlatform) PathContains(dir string) bool {
	entries, err := d.GetUserPATH()
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e == dir {
			return true
		}
	}
	return false
}

// ensureShellSource adds a source line to ~/.zshrc if not already present.
func (d *darwinPlatform) ensureShellSource() error {
	home, _ := os.UserHomeDir()
	zshrc := filepath.Join(home, ".zshrc")
	sourceLine := fmt.Sprintf(`[ -f "%s" ] && source "%s"`, pathShFile(), pathShFile())

	data, err := os.ReadFile(zshrc)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if strings.Contains(string(data), sourceLine) {
		return nil
	}

	f, err := os.OpenFile(zshrc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n# DevBox PATH\n%s\n", sourceLine)
	return err
}

// --- Process management ---

func (d *darwinPlatform) SetProcessAttrs(cmd *exec.Cmd, createGroup bool, hide bool) {
	if createGroup {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	// hide is a no-op on macOS (no console windows)
}

// KillProcessTree kills the process group DevBox children are started in
// (SetProcessAttrs uses Setpgid), falling back to the single PID.
func (d *darwinPlatform) KillProcessTree(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
		return nil
	}
	if p, err := os.FindProcess(pid); err == nil {
		return p.Kill()
	}
	return nil
}

func (d *darwinPlatform) LaunchInstaller(path string, args ...string) error {
	return exec.Command("open", path).Start()
}

func (d *darwinPlatform) IsProcessRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// --- Hosts file management ---

func (d *darwinPlatform) HostsFilePath() string {
	return "/etc/hosts"
}

func (d *darwinPlatform) ReadHostsFile() ([]byte, error) {
	return os.ReadFile(d.HostsFilePath())
}

func (d *darwinPlatform) WriteHostsFileElevated(content []byte) error {
	tmpDir := filepath.Join(os.TempDir(), "devbox")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("cannot create tmp directory: %w", err)
	}

	tmpFile := filepath.Join(tmpDir, "hosts_new")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		return fmt.Errorf("cannot write temp hosts file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Use osascript to elevate with admin privileges
	script := fmt.Sprintf(`do shell script "cp '%s' '/etc/hosts'" with administrator privileges`, tmpFile)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("elevated write failed: %s - %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// --- Autostart ---

const launchAgentLabel = "com.devbox.app"

func (d *darwinPlatform) SetAutoStart(enabled bool) error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")

	if !enabled {
		os.Remove(plistPath)
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, launchAgentLabel, exePath, AutoStartFlag)

	os.MkdirAll(filepath.Dir(plistPath), 0755)
	return os.WriteFile(plistPath, []byte(plist), 0644)
}

func (d *darwinPlatform) IsAutoStartEnabled() bool {
	home, _ := os.UserHomeDir()
	_, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"))
	return err == nil
}

// --- Data location ---

// DefaultDataDir is ~/DevBox — visible in Finder, unlike the old ~/.devbox dotfolder.
func (d *darwinPlatform) DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "DevBox")
}

// --- System operations ---

func (d *darwinPlatform) OpenFolder(path string) error {
	return exec.Command("open", path).Start()
}

func (d *darwinPlatform) OpenFile(path string) error {
	return exec.Command("open", path).Start()
}

// --- Platform naming ---

func (d *darwinPlatform) BinaryName(base string) string { return base }
func (d *darwinPlatform) ScriptName(base string) string { return base }
func (d *darwinPlatform) LibExt() string                { return ".so" }
