package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const valkeyMaxVersions = 5

var valkeyKnownVersions = []AvailableVersion{
	{Version: "8.1.1", Label: "8.1.1 (Latest)", URL: "https://github.com/nicenemo/valkey-windows/releases/download/v8.1.1/valkey-8.1.1-windows-x86_64.zip"},
	{Version: "8.0.2", Label: "8.0.2", URL: "https://github.com/nicenemo/valkey-windows/releases/download/v8.0.2/valkey-8.0.2-windows-x86_64.zip"},
}

type ValkeyManager struct{}

func NewValkeyManager() *ValkeyManager { return &ValkeyManager{} }

func (v *ValkeyManager) Name() string        { return "valkey" }
func (v *ValkeyManager) DisplayName() string  { return "Valkey" }
func (v *ValkeyManager) DefaultPort() int     { return 6379 }

func (v *ValkeyManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(serviceBaseDir("valkey"), "valkey-server.exe"))
	return err == nil
}

func (v *ValkeyManager) ListVersions() ([]AvailableVersion, error) {
	versions, err := v.fetchVersions()
	if err != nil || len(versions) == 0 {
		return valkeyKnownVersions, nil
	}
	if len(versions) > valkeyMaxVersions {
		versions = versions[:valkeyMaxVersions]
	}
	return versions, nil
}

func (v *ValkeyManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("valkey")

	if port <= 0 {
		port = v.DefaultPort()
	}

	downloadURL, err := v.findDownloadURL(version)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("valkey-%s-windows-x86_64.zip", version)
	tmpFile, err := downloadToTmp(downloadURL, filename, progress)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpFile)

	if progress != nil {
		progress <- Progress{Percent: 75, Message: "Extracting..."}
	}

	tmpExtract := tmpFile + "-extract"
	os.RemoveAll(tmpExtract)
	defer os.RemoveAll(tmpExtract)

	if err := extractZip(tmpFile, tmpExtract); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Find extracted directory
	entries, _ := os.ReadDir(tmpExtract)
	extractedDir := ""
	for _, e := range entries {
		if e.IsDir() && strings.Contains(strings.ToLower(e.Name()), "valkey") {
			extractedDir = filepath.Join(tmpExtract, e.Name())
			break
		}
	}
	if extractedDir == "" {
		// Files might be directly in tmpExtract
		if _, err := os.Stat(filepath.Join(tmpExtract, "valkey-server.exe")); err == nil {
			extractedDir = tmpExtract
		} else {
			return fmt.Errorf("Valkey directory not found after extraction")
		}
	}

	if _, err := os.Stat(filepath.Join(extractedDir, "valkey-server.exe")); os.IsNotExist(err) {
		return fmt.Errorf("valkey-server.exe not found in extracted files")
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Configuring..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install Valkey: %w", err)
	}

	os.MkdirAll(filepath.Join(base, "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "data"), 0755)

	v.writeConfig(port)
	SaveServiceConfig("valkey", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Valkey %s installed (port %d)", version, port)}
	}
	return nil
}

func (v *ValkeyManager) Uninstall() error {
	if IsRunning("valkey") {
		v.Stop()
	}
	return os.RemoveAll(serviceBaseDir("valkey"))
}

func (v *ValkeyManager) Start() error {
	if !v.IsInstalled() {
		return fmt.Errorf("Valkey is not installed")
	}
	if IsRunning("valkey") {
		return nil
	}

	port := v.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	base := serviceBaseDir("valkey")
	exe := filepath.Join(base, "valkey-server.exe")
	confFile := filepath.Join(base, "valkey.conf")
	logFile := filepath.Join(base, "logs", "valkey.log")

	_, err := StartProcess("valkey", exe, []string{confFile}, base, logFile)
	return err
}

func (v *ValkeyManager) Stop() error {
	if !IsRunning("valkey") {
		return nil
	}
	return StopProcess("valkey")
}

func (v *ValkeyManager) Restart() error {
	port := v.Port()
	v.Stop()
	WaitForPortRelease(port, 5)
	return v.Start()
}

func (v *ValkeyManager) Status() ServiceStatus {
	if !v.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("valkey") {
		return StatusRunning
	}
	return StatusStopped
}

func (v *ValkeyManager) Port() int {
	cfg := LoadServiceConfig("valkey")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return v.DefaultPort()
}

func (v *ValkeyManager) Version() string {
	cfg := LoadServiceConfig("valkey")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (v *ValkeyManager) SetPort(port int) error {
	cfg := LoadServiceConfig("valkey")
	cfg.Port = port
	if err := SaveServiceConfig("valkey", cfg); err != nil {
		return err
	}
	v.writeConfig(port)
	return nil
}

func (v *ValkeyManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("valkey"), "logs", "valkey.log")
	return readLastLines(logFile, lines)
}

func (v *ValkeyManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        v.Name(),
		DisplayName: v.DisplayName(),
		Status:      v.Status(),
		Port:        v.Port(),
		Version:     v.Version(),
		Installed:   v.IsInstalled(),
	}
}

func (v *ValkeyManager) writeConfig(port int) {
	base := serviceBaseDir("valkey")
	confPath := filepath.Join(base, "valkey.conf")
	dataDir := strings.ReplaceAll(filepath.Join(base, "data"), "\\", "/")

	conf := fmt.Sprintf(`# DevBox Valkey Configuration
bind 127.0.0.1
port %d
dir %s
dbfilename dump.rdb
appendonly yes
appendfilename "appendonly.aof"
maxmemory 256mb
maxmemory-policy allkeys-lru
`, port, dataDir)

	os.WriteFile(confPath, []byte(conf), 0644)
}

func (v *ValkeyManager) findDownloadURL(version string) (string, error) {
	for _, kv := range valkeyKnownVersions {
		if kv.Version == version {
			return kv.URL, nil
		}
	}
	versions, err := v.fetchVersions()
	if err == nil {
		for _, kv := range versions {
			if kv.Version == version {
				return kv.URL, nil
			}
		}
	}
	return fmt.Sprintf("https://github.com/nicenemo/valkey-windows/releases/download/v%s/valkey-%s-windows-x86_64.zip", version, version), nil
}

func (v *ValkeyManager) fetchVersions() ([]AvailableVersion, error) {
	releases, err := fetchGitHubReleases("nicenemo", "valkey-windows")
	if err != nil {
		return nil, err
	}

	var versions []AvailableVersion
	for _, rel := range releases {
		tag := strings.TrimPrefix(rel.TagName, "v")
		for _, asset := range rel.Assets {
			if strings.Contains(asset.Name, "windows") && strings.HasSuffix(asset.Name, ".zip") {
				label := tag
				if len(versions) == 0 {
					label = tag + " (Latest)"
				}
				versions = append(versions, AvailableVersion{
					Version: tag,
					Label:   label,
					URL:     asset.BrowserDownloadURL,
				})
				break
			}
		}
		if len(versions) >= valkeyMaxVersions {
			break
		}
	}
	return versions, nil
}
