package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"DevBox/internal/platform"
)

// CloudflareConfig holds the optional Cloudflare account link used for named
// tunnels on the user's own domain (as opposed to random *.trycloudflare.com
// quick tunnels, which need no account at all).
type CloudflareConfig struct {
	APIToken    string `json:"apiToken"`
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	ZoneID      string `json:"zoneId"`
	ZoneName    string `json:"zoneName"` // e.g. "example.com"
	TunnelID    string `json:"tunnelId"`
	TunnelName  string `json:"tunnelName"`
	TunnelToken string `json:"tunnelToken"`
}

type Config struct {
	Language       string            `json:"language"`       // "en" or "tr"
	Theme          string            `json:"theme"`          // "dark", "light" or "system"
	AutoStart      bool              `json:"autoStart"`      // Launch DevBox at OS login
	StartMinimized bool              `json:"startMinimized"` // Launch hidden in the tray (login launches always do)
	CloseToTray    bool              `json:"closeToTray"`    // Window close hides to tray instead of quitting
	DataDir        string            `json:"dataDir"`        // platform.DefaultDataDir() by default
	ActiveRuntimes map[string]string `json:"activeRuntimes"` // {"go": "1.26.0", "node": "24.13.1"}
	AutoStartSvcs  []string          `json:"autoStartSvcs"`  // ["nginx", "postgres"]
	// ProxyEnabled controls whether the bundled front-door proxy (port 80/443
	// reverse proxy that routes *.test domains to per-project backends) starts
	// with DevBox. Off by default — first run needs an explicit Enable click.
	ProxyEnabled bool `json:"proxyEnabled"`
	// VersionCacheHours is how long fetched remote version lists (runtimes and
	// services) are considered fresh before DevBox re-fetches them in the background.
	VersionCacheHours int `json:"versionCacheHours"`
	// PhpCgiPorts maps a PHP version to the FastCGI port DevBox assigned to it.
	// The globally active version always gets 9000; pinned versions get 9001+.
	PhpCgiPorts map[string]int   `json:"phpCgiPorts"`
	Cloudflare  CloudflareConfig `json:"cloudflare"`
}

var (
	instance *Config
	once     sync.Once
	mu       sync.RWMutex
	cfgPath  string
	// Migrated is set when this launch moved data from the legacy ~/.devbox
	// location; the UI can show a one-time notice.
	Migrated     bool
	MigratedFrom string
)

func DefaultConfig() *Config {
	return &Config{
		Language:          "en",
		Theme:             "system",
		AutoStart:         false,
		StartMinimized:    false,
		CloseToTray:       true,
		DataDir:           defaultDataDir(),
		ActiveRuntimes:    make(map[string]string),
		AutoStartSvcs:     []string{},
		VersionCacheHours: 48,
		PhpCgiPorts:       map[string]int{},
	}
}

func defaultDataDir() string {
	return platform.DefaultDataDir()
}

// LegacyDataDir is where DevBox < 0.2 kept everything (~/.devbox).
func LegacyDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".devbox")
}

func configFilePath() string {
	return filepath.Join(defaultDataDir(), "config.json")
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// Load reads config from disk or creates default. On first launch after the
// data-dir move it transparently migrates ~/.devbox → the new default location.
func Load() (*Config, error) {
	var loadErr error
	once.Do(func() {
		cfgPath = configFilePath()

		if migrateLegacyDataDir(defaultDataDir()) {
			Migrated = true
			MigratedFrom = LegacyDataDir()
		}

		if err := ensureDir(filepath.Dir(cfgPath)); err != nil {
			loadErr = err
			return
		}

		data, err := os.ReadFile(cfgPath)
		if err != nil {
			if os.IsNotExist(err) {
				instance = DefaultConfig()
				loadErr = Save()
				return
			}
			loadErr = err
			return
		}

		instance = DefaultConfig()
		if err := json.Unmarshal(data, instance); err != nil {
			loadErr = err
			return
		}
		if instance.ActiveRuntimes == nil {
			instance.ActiveRuntimes = map[string]string{}
		}
		if instance.PhpCgiPorts == nil {
			instance.PhpCgiPorts = map[string]int{}
		}
		if instance.VersionCacheHours <= 0 {
			instance.VersionCacheHours = 48
		}

		// A config that still points at the legacy dir (or at a dir that no longer
		// exists) is re-homed next to the config file itself.
		if instance.DataDir == "" ||
			strings.EqualFold(filepath.Clean(instance.DataDir), filepath.Clean(LegacyDataDir())) {
			instance.DataDir = defaultDataDir()
			loadErr = Save()
			return
		}
		if _, err := os.Stat(instance.DataDir); err != nil {
			instance.DataDir = defaultDataDir()
			loadErr = Save()
		}
	})
	return instance, loadErr
}

// Save writes current config to disk
func Save() error {
	mu.Lock()
	defer mu.Unlock()

	if instance == nil {
		instance = DefaultConfig()
	}
	if cfgPath == "" {
		cfgPath = configFilePath()
	}

	if err := ensureDir(filepath.Dir(cfgPath)); err != nil {
		return err
	}

	data, err := json.MarshalIndent(instance, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}

// Get returns the current config (read-only access)
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	if instance == nil {
		instance = DefaultConfig()
	}
	return instance
}

// SetLanguage updates the language setting
func SetLanguage(lang string) error {
	mu.Lock()
	instance.Language = lang
	mu.Unlock()
	return Save()
}

// SetTheme updates the theme setting
func SetTheme(theme string) error {
	mu.Lock()
	instance.Theme = theme
	mu.Unlock()
	return Save()
}

// GetDataDir returns the data directory, ensuring it exists
func GetDataDir() string {
	cfg := Get()
	ensureDir(cfg.DataDir)
	return cfg.DataDir
}

// ConfigPath returns the on-disk location of config.json (for the Settings UI).
func ConfigPath() string {
	if cfgPath == "" {
		return configFilePath()
	}
	return cfgPath
}

// EnsureDirectories creates all required subdirectories.
// Note: service-specific dirs (nginx/, postgres/, mysql/) are NOT created here;
// they are created only during install to avoid conflicts with os.RemoveAll on Windows.
func EnsureDirectories() error {
	base := GetDataDir()
	dirs := []string{
		filepath.Join(base, "runtimes", "go"),
		filepath.Join(base, "runtimes", "node"),
		filepath.Join(base, "runtimes", "php"),
		filepath.Join(base, "runtimes", "python"),
		filepath.Join(base, "runtimes", "rust"),
		filepath.Join(base, "services"),
		filepath.Join(base, "projects"),
		filepath.Join(base, "ssl", "certs"),
		filepath.Join(base, "logs"),
		filepath.Join(base, "tmp"),
		filepath.Join(base, "cache"),
		filepath.Join(base, "backups"),
	}
	for _, d := range dirs {
		if err := ensureDir(d); err != nil {
			return err
		}
	}
	return nil
}
