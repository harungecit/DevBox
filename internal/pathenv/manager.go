package pathenv

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	envRegKey  = `Environment`
	hwndBroadcast = 0xFFFF
	wmSettingChange = 0x001A
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

// GetUserPATH reads the user-level PATH from Windows registry
func GetUserPATH() ([]string, error) {
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

// SetUserPATH writes the user-level PATH to Windows registry
func SetUserPATH(entries []string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, envRegKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	// Use REG_EXPAND_SZ to support %VARIABLES%
	val := strings.Join(entries, ";")
	if err := k.SetExpandStringValue("Path", val); err != nil {
		return err
	}

	// Broadcast WM_SETTINGCHANGE so other processes pick up the change
	broadcastSettingChange()

	return nil
}

// AddToPath adds a directory to the user PATH if not already present
func AddToPath(dir string) error {
	entries, err := GetUserPATH()
	if err != nil {
		return err
	}

	// Check if already present (case-insensitive)
	for _, e := range entries {
		if strings.EqualFold(e, dir) {
			return nil // Already in PATH
		}
	}

	// Prepend so DevBox paths take priority
	entries = append([]string{dir}, entries...)
	return SetUserPATH(entries)
}

// RemoveFromPath removes a directory from the user PATH
func RemoveFromPath(dir string) error {
	entries, err := GetUserPATH()
	if err != nil {
		return err
	}

	var newEntries []string
	for _, e := range entries {
		if !strings.EqualFold(e, dir) {
			newEntries = append(newEntries, e)
		}
	}

	return SetUserPATH(newEntries)
}

// Contains checks if a directory is in the user PATH
func Contains(dir string) bool {
	entries, err := GetUserPATH()
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

// SwitchRuntimePath removes old runtime path and adds new one
func SwitchRuntimePath(runtimeName, oldPath, newPath string) error {
	if oldPath != "" {
		if err := RemoveFromPath(oldPath); err != nil {
			return err
		}
	}
	if newPath != "" {
		return AddToPath(newPath)
	}
	return nil
}

// GetSystemPATH reads the system-level PATH (read-only for display)
func GetSystemPATH() ([]string, error) {
	// Use cmd to expand %SystemRoot% etc.
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

// broadcastSettingChange notifies Windows that environment variables changed
func broadcastSettingChange() {
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
