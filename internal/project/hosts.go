package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const hostsFilePath = `C:\Windows\System32\drivers\etc\hosts`
const devboxMarker = "# DevBox managed"

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteExW  = shell32.NewProc("ShellExecuteExW")
)

// SHELLEXECUTEINFO corresponds to the Windows SHELLEXECUTEINFOW struct.
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
)

// runElevated runs a PowerShell command with UAC elevation using ShellExecuteExW.
// It waits for the elevated process to finish and returns an error if the user
// cancels UAC or the process exits with a non-zero code.
func runElevated(psCommand string) error {
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
	defer windows.CloseHandle(windows.Handle(sei.hProcess))

	// Wait for the elevated process to finish
	event, err := windows.WaitForSingleObject(windows.Handle(sei.hProcess), windows.INFINITE)
	if err != nil {
		return fmt.Errorf("error waiting for elevated process: %w", err)
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected wait result: %d", event)
	}

	var exitCode uint32
	if err := windows.GetExitCodeProcess(windows.Handle(sei.hProcess), &exitCode); err != nil {
		return fmt.Errorf("failed to get exit code: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("elevated command failed with exit code %d", exitCode)
	}

	return nil
}

// AddHostsEntry adds a domain->127.0.0.1 mapping to the hosts file via UAC elevation.
func AddHostsEntry(domain string) error {
	data, err := os.ReadFile(hostsFilePath)
	if err != nil {
		return fmt.Errorf("cannot read hosts file: %w", err)
	}

	// Check if already exists
	if strings.Contains(string(data), domain) {
		return nil
	}

	entry := fmt.Sprintf("127.0.0.1 %s %s", domain, devboxMarker)
	content := strings.TrimRight(string(data), "\r\n") + "\r\n" + entry + "\r\n"

	return writeHostsElevated([]byte(content))
}

// RemoveHostsEntry removes a domain from the hosts file via UAC elevation.
func RemoveHostsEntry(domain string) error {
	data, err := os.ReadFile(hostsFilePath)
	if err != nil {
		return fmt.Errorf("cannot read hosts file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var filtered []string
	found := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, domain) && strings.Contains(trimmed, devboxMarker) {
			found = true
			continue
		}
		filtered = append(filtered, line)
	}

	if !found {
		return nil
	}

	return writeHostsElevated([]byte(strings.Join(filtered, "\n")))
}

// writeHostsElevated writes content to the hosts file via UAC-elevated PowerShell.
// It writes to a temp file first, then uses elevated Copy-Item to overwrite hosts.
func writeHostsElevated(content []byte) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot get home directory: %w", err)
	}

	tmpDir := filepath.Join(homeDir, ".devbox", "tmp")
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
		tmpFile, hostsFilePath,
	)

	return runElevated(psCmd)
}

// ListDevBoxHosts returns all DevBox-managed host entries.
func ListDevBoxHosts() []string {
	data, err := os.ReadFile(hostsFilePath)
	if err != nil {
		return nil
	}

	var hosts []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, devboxMarker) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				hosts = append(hosts, parts[1])
			}
		}
	}
	return hosts
}
