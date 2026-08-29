//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

func init() {
	current = &windowsPlatform{}
}

type windowsPlatform struct{}

// --- Registry PATH management ---

const (
	envRegKey       = `Environment`
	hwndBroadcast   = 0xFFFF
	wmSettingChange = 0x001A
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

func (w *windowsPlatform) GetUserPATH() ([]string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envRegKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	val, _, err := k.GetStringValue("Path")
	if err != nil {
		if err == registry.ErrNotExist {
			return []string{}, nil
		}
		return nil, err
	}

	var entries []string
	for _, p := range strings.Split(val, ";") {
		p = strings.TrimSpace(p)
		if p != "" {
			entries = append(entries, p)
		}
	}
	return entries, nil
}

func (w *windowsPlatform) SetUserPATH(entries []string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, envRegKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	val := strings.Join(entries, ";")
	if err := k.SetExpandStringValue("Path", val); err != nil {
		return err
	}

	w.BroadcastPathChange()
	return nil
}

func (w *windowsPlatform) AddToPath(dir string) error {
	entries, err := w.GetUserPATH()
	if err != nil {
		return err
	}

	for _, e := range entries {
		if strings.EqualFold(e, dir) {
			return nil
		}
	}

	entries = append([]string{dir}, entries...)
	return w.SetUserPATH(entries)
}

func (w *windowsPlatform) RemoveFromPath(dir string) error {
	entries, err := w.GetUserPATH()
	if err != nil {
		return err
	}

	var newEntries []string
	for _, e := range entries {
		if !strings.EqualFold(e, dir) {
			newEntries = append(newEntries, e)
		}
	}

	return w.SetUserPATH(newEntries)
}

func (w *windowsPlatform) GetSystemPATH() ([]string, error) {
	out, err := exec.Command("cmd", "/C", "echo", "%PATH%").Output()
	if err != nil {
		return nil, err
	}

	var entries []string
	for _, p := range strings.Split(strings.TrimSpace(string(out)), ";") {
		p = strings.TrimSpace(p)
		if p != "" {
			entries = append(entries, p)
		}
	}
	return entries, nil
}

func (w *windowsPlatform) BroadcastPathChange() {
	envStr, _ := syscall.UTF16PtrFromString("Environment")
	procSendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(envStr)),
		0x0002, // SMTO_ABORTIFHUNG
		5000,   // 5 second timeout
		0,
	)
}

func (w *windowsPlatform) PathContains(dir string) bool {
	entries, err := w.GetUserPATH()
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.EqualFold(e, dir) {
			return true
		}
	}
	return false
}

// --- Process management ---

func (w *windowsPlatform) SetProcessAttrs(cmd *exec.Cmd, createGroup bool, hide bool) {
	attrs := &syscall.SysProcAttr{}
	if createGroup {
		attrs.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP
	}
	if hide {
		attrs.HideWindow = true
	}
	cmd.SysProcAttr = attrs
}

// KillProcessTree uses taskkill /T so children of wrapper processes
// (npm .cmd shims → node, nginx master → workers) die with their parent.
func (w *windowsPlatform) KillProcessTree(pid int) error {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		if !w.IsProcessRunning(pid) {
			return nil // already gone
		}
		return fmt.Errorf("taskkill: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (w *windowsPlatform) IsProcessRunning(pid int) bool {
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

// --- Hosts file management ---

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide               = 0
	swShowNormal         = 1
)

func (w *windowsPlatform) HostsFilePath() string {
	return `C:\Windows\System32\drivers\etc\hosts`
}

func (w *windowsPlatform) ReadHostsFile() ([]byte, error) {
	return os.ReadFile(w.HostsFilePath())
}

func (w *windowsPlatform) WriteHostsFileElevated(content []byte) error {
	tmpDir := filepath.Join(os.TempDir(), "devbox")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("cannot create tmp directory: %w", err)
	}

	tmpFile := filepath.Join(tmpDir, "hosts_new")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		return fmt.Errorf("cannot write temp hosts file: %w", err)
	}
	defer os.Remove(tmpFile)

	psCmd := fmt.Sprintf(
		"Copy-Item -LiteralPath '%s' -Destination '%s' -Force",
		tmpFile, w.HostsFilePath(),
	)

	return w.runElevated(psCmd)
}

// runElevated runs a PowerShell command with UAC elevation using ShellExecuteExW.
func (w *windowsPlatform) runElevated(psCommand string) error {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString("powershell.exe")
	args, _ := syscall.UTF16PtrFromString(`-NoProfile -WindowStyle Hidden -Command "` + psCommand + `"`)

	sei := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: args,
		nShow:        swHide,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))

	ret, _, _ := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if ret == 0 {
		return fmt.Errorf("UAC elevation failed or was cancelled by the user")
	}

	if sei.hProcess == 0 {
		return fmt.Errorf("failed to get elevated process handle")
	}

	handle := syscall.Handle(sei.hProcess)
	defer syscall.CloseHandle(handle)

	// Wait for the elevated process to finish
	syscall.WaitForSingleObject(handle, syscall.INFINITE)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return fmt.Errorf("failed to get exit code: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("elevated command failed with exit code %d", exitCode)
	}

	return nil
}

// LaunchInstaller runs an installer with the "runas" verb: the UAC prompt
// appears and the elevated process continues independently of DevBox (which
// quits right after so the installer can replace its files).
func (w *windowsPlatform) LaunchInstaller(path string) error {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(path)
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(path))
	sei := shellExecuteInfo{
		fMask:       seeMaskNoCloseProcess,
		lpVerb:      verb,
		lpFile:      file,
		lpDirectory: dir,
		nShow:       swShowNormal,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	ret, _, lastErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if ret == 0 {
		return fmt.Errorf("installer was not started (UAC prompt declined or launch failed): %v", lastErr)
	}
	if sei.hProcess != 0 {
		syscall.CloseHandle(syscall.Handle(sei.hProcess))
	}
	return nil
}

// --- Autostart ---

const (
	autoStartRegistryKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartValueName   = "DevBox"
)

func (w *windowsPlatform) SetAutoStart(enabled bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegistryKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	if enabled {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		return key.SetStringValue(autoStartValueName, `"`+exePath+`" `+AutoStartFlag)
	}

	err = key.DeleteValue(autoStartValueName)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("failed to remove auto-start entry: %w", err)
	}
	return nil
}

func (w *windowsPlatform) IsAutoStartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	val, _, err := key.GetStringValue(autoStartValueName)
	return err == nil && val != ""
}

// --- Data location ---

// DefaultDataDir is %SystemDrive%\DevBox (normally C:\DevBox): short, visible and
// easy to browse — the whole point of not hiding everything in a dotfolder.
func (w *windowsPlatform) DefaultDataDir() string {
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	return filepath.Join(drive+string(filepath.Separator), "DevBox")
}

// --- System operations ---

func (w *windowsPlatform) OpenFolder(path string) error {
	cmd := exec.Command("explorer", path)
	return cmd.Start()
}

func (w *windowsPlatform) OpenFile(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	return cmd.Start()
}

// --- Platform naming ---

func (w *windowsPlatform) BinaryName(base string) string { return base + ".exe" }
func (w *windowsPlatform) ScriptName(base string) string { return base + ".cmd" }
func (w *windowsPlatform) LibExt() string                { return ".dll" }
