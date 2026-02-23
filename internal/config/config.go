package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Language       string            `json:"language"`        // "en" or "tr"
	Theme          string            `json:"theme"`           // "dark", "light" or "system"
	AutoStart      bool              `json:"autoStart"`       // Start on Windows login
	DataDir        string            `json:"dataDir"`         // ~/.devbox by default
	ActiveRuntimes map[string]string `json:"activeRuntimes"`  // {"go": "1.26.0", "node": "24.13.1"}
	AutoStartSvcs  []string          `json:"autoStartSvcs"`   // ["nginx", "postgres"]
}

var (
	instance *Config
	once     sync.Once
	mu       sync.RWMutex
	cfgPath  string
)

func DefaultConfig() *Config {
	return &Config{
		Language:       "en",
		Theme:          "system",
		AutoStart:      false,
		DataDir:        defaultDataDir(),
		ActiveRuntimes: make(map[string]string),
		AutoStartSvcs:  []string{},
	}
}

func defaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".devbox")
}

func configFilePath() string {
	return filepath.Join(defaultDataDir(), "config.json")
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// Load reads config from disk or creates default
func Load() (*Config, error) {
	var loadErr error
	once.Do(func() {
		cfgPath = configFilePath()
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
		filepath.Join(base, "backups"),
	}
	for _, d := range dirs {
		if err := ensureDir(d); err != nil {
			return err
		}
	}
	return nil
}
