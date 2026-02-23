package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"DevBox/internal/config"
)

// Version represents a runtime version
type Version struct {
	Number  string `json:"number"`
	Stable  bool   `json:"stable"`
	Current bool   `json:"current"`
}

// Progress represents download/install progress
type Progress struct {
	Percent int    `json:"percent"`
	Message string `json:"message"`
}

// RuntimeManager defines the interface for managing runtime versions
type RuntimeManager interface {
	Name() string
	ListRemote() ([]Version, error)
	ListInstalled() ([]Version, error)
	Install(version string, progress chan<- Progress) error
	Uninstall(version string) error
	SetGlobal(version string) error
	GetGlobal() (string, error)
	BinaryPath(version string) string
}

// Registry holds all runtime managers
var Registry = map[string]RuntimeManager{}

// Register adds a runtime manager to the registry
func Register(rm RuntimeManager) {
	Registry[rm.Name()] = rm
}

// InitAll initializes all runtime managers
func InitAll() {
	Register(NewGoManager())
	Register(NewNodeManager())
	Register(NewPHPManager())
	Register(NewPythonManager())
	Register(NewRustManager())
}

// runtimeBaseDir returns the base directory for a given runtime
func runtimeBaseDir(name string) string {
	return filepath.Join(config.GetDataDir(), "runtimes", name)
}

// listInstalledVersions scans the runtime directory for installed versions
func listInstalledVersions(name string) ([]string, error) {
	baseDir := runtimeBaseDir(name)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var versions []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			versions = append(versions, e.Name())
		}
	}
	return versions, nil
}

// uninstallVersion removes an installed version directory
func uninstallVersion(name, version string) error {
	dir := filepath.Join(runtimeBaseDir(name), version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("version %s is not installed", version)
	}
	return os.RemoveAll(dir)
}

// getGlobalVersion reads the active version from config
func getGlobalVersion(name string) (string, error) {
	cfg := config.Get()
	v, ok := cfg.ActiveRuntimes[name]
	if !ok {
		return "", nil
	}
	return v, nil
}

// setGlobalVersion writes the active version to config
func setGlobalVersion(name, version string) error {
	cfg := config.Get()
	if cfg.ActiveRuntimes == nil {
		cfg.ActiveRuntimes = make(map[string]string)
	}
	cfg.ActiveRuntimes[name] = version
	return config.Save()
}

// tmpDir returns a temp directory for downloads
func tmpDir() string {
	return filepath.Join(config.GetDataDir(), "tmp")
}

// compareVersions compares two dotted version strings numerically.
// Returns positive if a > b, negative if a < b, zero if equal.
func compareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			numB, _ = strconv.Atoi(partsB[i])
		}
		if numA != numB {
			return numA - numB
		}
	}
	return 0
}

// execCommand creates an exec.Cmd with hidden window on Windows
func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
