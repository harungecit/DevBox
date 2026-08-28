package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"

	"DevBox/internal/config"
)

// ServiceStatus represents the current state of a service
type ServiceStatus string

const (
	StatusRunning      ServiceStatus = "running"
	StatusStopped      ServiceStatus = "stopped"
	StatusError        ServiceStatus = "error"
	StatusNotInstalled ServiceStatus = "not_installed"
)

// Progress represents download/install progress
type Progress struct {
	Percent int    `json:"percent"`
	Message string `json:"message"`
}

// AvailableVersion represents an installable version
type AvailableVersion struct {
	Version string `json:"version"`
	Label   string `json:"label"` // e.g., "1.28.2 (Stable)", "17.8 (Latest)"
	URL     string `json:"-"`     // Not exposed to frontend
}

// ServiceConfig holds per-service configuration
type ServiceConfig struct {
	Port    int    `json:"port"`
	Version string `json:"version"`
}

// ServiceInfo holds displayable service information
type ServiceInfo struct {
	Name        string        `json:"name"`
	DisplayName string        `json:"displayName"`
	Status      ServiceStatus `json:"status"`
	Port        int           `json:"port"`
	Version     string        `json:"version"`
	Installed   bool          `json:"installed"`
	// UpdateVersion is a newer release installable in place (data kept); "" if none.
	UpdateVersion string `json:"updateVersion"`
	// LatestMajor is a newer release line that needs a fresh install; "" if none.
	LatestMajor string `json:"latestMajor"`
}

// ServiceManager defines the interface for managing services
type ServiceManager interface {
	Name() string
	DisplayName() string
	DefaultPort() int
	IsInstalled() bool
	ListVersions() ([]AvailableVersion, error)
	Install(version string, port int, progress chan<- Progress) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() ServiceStatus
	Port() int
	Version() string
	SetPort(port int) error
	Logs(lines int) ([]string, error)
	Info() ServiceInfo
}

// Registry holds all service managers
var Registry = map[string]ServiceManager{}
var registryMu sync.RWMutex

// Register adds a service manager to the registry
func Register(sm ServiceManager) {
	registryMu.Lock()
	defer registryMu.Unlock()
	Registry[sm.Name()] = sm
}

// ConflictGroups maps service names to their mutual exclusion group.
// Only one service per group can be installed at a time.
// Note: FrankenPHP intentionally is NOT in "webserver" — it bundles its own
// webserver and PHP runtime, and is selected per-project rather than globally,
// so it can coexist with nginx/apache/caddy as long as ports differ.
var ConflictGroups = map[string]string{
	"nginx":   "webserver",
	"apache":  "webserver",
	"caddy":   "webserver",
	"mysql":   "mysql-compat",
	"mariadb": "mysql-compat",
	"redis":   "kv-cache",
	"valkey":  "kv-cache",
}

// GetConflictingService returns the display name of an installed service
// that conflicts with the given service. Returns empty string if no conflict.
func GetConflictingService(name string) string {
	group, ok := ConflictGroups[name]
	if !ok {
		return ""
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	for svcName, mgr := range Registry {
		if svcName != name && ConflictGroups[svcName] == group && mgr.IsInstalled() {
			return mgr.DisplayName()
		}
	}
	return ""
}

// InitAll initializes all service managers
func InitAll() {
	// Web servers
	Register(NewNginxManager())
	Register(NewApacheManager())
	Register(NewCaddyManager())
	Register(NewFrankenPHPManager())
	// Databases
	Register(NewPostgresManager())
	Register(NewMySQLManager())
	Register(NewMariaDBManager())
	Register(NewMongoDBManager())
	// Cache
	Register(NewRedisManager())
	// Valkey has no official or maintained community Windows build — skip on Windows.
	// Redis is API-compatible and registered above as the Windows alternative.
	if goruntime.GOOS != "windows" {
		Register(NewValkeyManager())
	}
	// Mail
	Register(NewMailpitManager())
}

// StopAll stops all running services (called on app shutdown)
func StopAll() {
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, mgr := range Registry {
		if mgr.Status() == StatusRunning {
			mgr.Stop()
		}
	}
}

// GetAll returns info about all services
func GetAll() map[string]ServiceInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()

	result := make(map[string]ServiceInfo)
	for name, mgr := range Registry {
		info := mgr.Info()
		if info.Installed {
			u := CheckUpdate(name)
			info.UpdateVersion = u.Latest
			info.LatestMajor = u.LatestMajor
		}
		result[name] = info
	}
	return result
}

// --- shared helpers ---

func serviceBaseDir(name string) string {
	return filepath.Join(config.GetDataDir(), "services", name)
}

func logsDir(name string) string {
	dir := filepath.Join(serviceBaseDir(name), "logs")
	os.MkdirAll(dir, 0755)
	return dir
}

func pidFilePath(name string) string {
	return filepath.Join(serviceBaseDir(name), fmt.Sprintf("%s.pid", name))
}

// LoadServiceConfig reads the service config from disk
func LoadServiceConfig(name string) ServiceConfig {
	cfgPath := filepath.Join(serviceBaseDir(name), "devbox-service.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return ServiceConfig{}
	}
	var cfg ServiceConfig
	json.Unmarshal(data, &cfg)
	return cfg
}

// SaveServiceConfig writes the service config to disk
func SaveServiceConfig(name string, cfg ServiceConfig) error {
	cfgPath := filepath.Join(serviceBaseDir(name), "devbox-service.json")
	os.MkdirAll(filepath.Dir(cfgPath), 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}

func readLastLines(filePath string, n int) ([]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	lines := splitLines(string(data))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func splitLines(s string) []string {
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
