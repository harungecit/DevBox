package platform

import "os/exec"

// Platform abstracts OS-level operations for cross-platform support.
type Platform interface {
	// Process management
	SetProcessAttrs(cmd *exec.Cmd, createGroup bool, hide bool)
	IsProcessRunning(pid int) bool
	// KillProcessTree ends a process and every descendant it spawned (cmd/npx
	// shims, nginx/apache workers) so a Stop never leaves an orphan behind.
	KillProcessTree(pid int) error
	// LaunchInstaller starts an installer package the way the shell would
	// (Windows: ShellExecuteEx with UAC elevation, so an admin installer is
	// not rejected with ERROR_ELEVATION_REQUIRED; macOS: open).
	LaunchInstaller(path string, args ...string) error
	// LaunchInstallerWait starts an installer like LaunchInstaller and waits
	// for it to finish, returning its exit code. During a successful silent
	// in-app update the installer kills this process mid-wait — so a return
	// means the installer finished while we are still alive: exit code 0 =
	// done (caller should quit), anything else = a failure to surface.
	LaunchInstallerWait(path string, args ...string) (uint32, error)

	// PATH management
	GetUserPATH() ([]string, error)
	SetUserPATH(entries []string) error
	AddToPath(dir string) error
	RemoveFromPath(dir string) error
	GetSystemPATH() ([]string, error)
	BroadcastPathChange()
	PathContains(dir string) bool

	// Hosts file management
	HostsFilePath() string
	ReadHostsFile() ([]byte, error)
	WriteHostsFileElevated(content []byte) error

	// Autostart
	SetAutoStart(enabled bool) error
	IsAutoStartEnabled() bool

	// System operations
	OpenFolder(path string) error
	OpenFile(path string) error

	// LinkDir makes `link` point at the existing directory `target` without
	// copying anything: an NTFS junction on Windows (no elevation needed),
	// a symlink on macOS. Removing the link never touches the target.
	LinkDir(link, target string) error

	// Data location
	DefaultDataDir() string // Windows: %SystemDrive%\DevBox — macOS: ~/DevBox

	// Platform-specific naming
	BinaryName(base string) string // "go" → "go.exe" | "go"
	ScriptName(base string) string // "npx" → "npx.cmd" | "npx"
	LibExt() string                // ".dll" | ".so" | ".dylib"
}

var current Platform

// Current returns the active platform implementation.
func Current() Platform { return current }

// Convenience functions for short calls.

func BinaryName(b string) string                      { return current.BinaryName(b) }
func ScriptName(b string) string                      { return current.ScriptName(b) }
func LibExt() string                                  { return current.LibExt() }
func SetProcessAttrs(cmd *exec.Cmd, group, hide bool) { current.SetProcessAttrs(cmd, group, hide) }
func IsProcessRunning(pid int) bool                   { return current.IsProcessRunning(pid) }
func KillProcessTree(pid int) error                   { return current.KillProcessTree(pid) }
func LaunchInstaller(path string, args ...string) error {
	return current.LaunchInstaller(path, args...)
}
func LaunchInstallerWait(path string, args ...string) (uint32, error) {
	return current.LaunchInstallerWait(path, args...)
}
func OpenFolder(p string) error                       { return current.OpenFolder(p) }
func OpenFile(p string) error                         { return current.OpenFile(p) }
func LinkDir(link, target string) error               { return current.LinkDir(link, target) }
func WriteHostsFileElevated(content []byte) error     { return current.WriteHostsFileElevated(content) }
func HostsFilePath() string                           { return current.HostsFilePath() }
func ReadHostsFile() ([]byte, error)                  { return current.ReadHostsFile() }
func SetAutoStart(enabled bool) error                 { return current.SetAutoStart(enabled) }
func IsAutoStartEnabled() bool                        { return current.IsAutoStartEnabled() }
func GetUserPATH() ([]string, error)                  { return current.GetUserPATH() }
func SetUserPATH(entries []string) error              { return current.SetUserPATH(entries) }
func AddToPath(dir string) error                      { return current.AddToPath(dir) }
func RemoveFromPath(dir string) error                 { return current.RemoveFromPath(dir) }
func GetSystemPATH() ([]string, error)                { return current.GetSystemPATH() }
func BroadcastPathChange()                            { current.BroadcastPathChange() }
func PathContains(dir string) bool                    { return current.PathContains(dir) }
func DefaultDataDir() string                          { return current.DefaultDataDir() }

// AutoStartFlag is the CLI flag appended to the login-launch command so DevBox
// knows it was started by the OS (not the user) and should open hidden in the tray.
const AutoStartFlag = "--minimized"
