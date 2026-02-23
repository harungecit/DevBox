package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const caddyMaxVersions = 5

var caddyKnownVersions = []AvailableVersion{
	{Version: "2.9.1", Label: "2.9.1 (Latest)", URL: "https://github.com/caddyserver/caddy/releases/download/v2.9.1/caddy_2.9.1_windows_amd64.zip"},
	{Version: "2.8.4", Label: "2.8.4", URL: "https://github.com/caddyserver/caddy/releases/download/v2.8.4/caddy_2.8.4_windows_amd64.zip"},
}

type CaddyManager struct{}

func NewCaddyManager() *CaddyManager { return &CaddyManager{} }

func (c *CaddyManager) Name() string        { return "caddy" }
func (c *CaddyManager) DisplayName() string  { return "Caddy" }
func (c *CaddyManager) DefaultPort() int     { return 8080 }

func (c *CaddyManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(serviceBaseDir("caddy"), "caddy.exe"))
	return err == nil
}

func (c *CaddyManager) ListVersions() ([]AvailableVersion, error) {
	versions, err := c.fetchVersions()
	if err != nil || len(versions) == 0 {
		return caddyKnownVersions, nil
	}
	if len(versions) > caddyMaxVersions {
		versions = versions[:caddyMaxVersions]
	}
	return versions, nil
}

func (c *CaddyManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("caddy")

	if port <= 0 {
		port = c.DefaultPort()
	}

	downloadURL, err := c.findDownloadURL(version)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("caddy_%s_windows_amd64.zip", version)
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

	// Caddy zip contains caddy.exe directly (no subdirectory)
	if _, err := os.Stat(filepath.Join(tmpExtract, "caddy.exe")); os.IsNotExist(err) {
		// Check subdirectories
		entries, _ := os.ReadDir(tmpExtract)
		found := false
		for _, e := range entries {
			if e.IsDir() {
				if _, err := os.Stat(filepath.Join(tmpExtract, e.Name(), "caddy.exe")); err == nil {
					tmpExtract = filepath.Join(tmpExtract, e.Name())
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("caddy.exe not found in extracted files")
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Configuring..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(tmpExtract, base); err != nil {
		return fmt.Errorf("failed to install Caddy: %w", err)
	}

	os.MkdirAll(filepath.Join(base, "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "html"), 0755)

	c.writeConfig(port)
	c.writeDefaultPage()

	SaveServiceConfig("caddy", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Caddy %s installed (port %d)", version, port)}
	}
	return nil
}

func (c *CaddyManager) Uninstall() error {
	if IsRunning("caddy") {
		c.Stop()
	}
	return os.RemoveAll(serviceBaseDir("caddy"))
}

func (c *CaddyManager) Start() error {
	if !c.IsInstalled() {
		return fmt.Errorf("Caddy is not installed")
	}
	if IsRunning("caddy") {
		return nil
	}

	port := c.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	base := serviceBaseDir("caddy")
	exe := filepath.Join(base, "caddy.exe")
	logFile := filepath.Join(base, "logs", "caddy.log")

	_, err := StartProcess("caddy", exe, []string{
		"run",
		"--config", filepath.Join(base, "Caddyfile"),
		"--adapter", "caddyfile",
	}, base, logFile)
	return err
}

func (c *CaddyManager) Stop() error {
	if !IsRunning("caddy") {
		return nil
	}
	return StopProcess("caddy")
}

func (c *CaddyManager) Restart() error {
	port := c.Port()
	c.Stop()
	WaitForPortRelease(port, 5)
	return c.Start()
}

func (c *CaddyManager) Status() ServiceStatus {
	if !c.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("caddy") {
		return StatusRunning
	}
	return StatusStopped
}

func (c *CaddyManager) Port() int {
	cfg := LoadServiceConfig("caddy")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return c.DefaultPort()
}

func (c *CaddyManager) Version() string {
	cfg := LoadServiceConfig("caddy")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (c *CaddyManager) SetPort(port int) error {
	cfg := LoadServiceConfig("caddy")
	cfg.Port = port
	if err := SaveServiceConfig("caddy", cfg); err != nil {
		return err
	}
	c.writeConfig(port)
	return nil
}

func (c *CaddyManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("caddy"), "logs", "caddy.log")
	return readLastLines(logFile, lines)
}

func (c *CaddyManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        c.Name(),
		DisplayName: c.DisplayName(),
		Status:      c.Status(),
		Port:        c.Port(),
		Version:     c.Version(),
		Installed:   c.IsInstalled(),
	}
}

func (c *CaddyManager) writeConfig(port int) {
	base := serviceBaseDir("caddy")
	confPath := filepath.Join(base, "Caddyfile")

	conf := fmt.Sprintf(`# DevBox Caddy Configuration
:%d {
    root * html
    file_server
    encode gzip
}
`, port)

	os.WriteFile(confPath, []byte(conf), 0644)
}

func (c *CaddyManager) writeDefaultPage() {
	indexPath := filepath.Join(serviceBaseDir("caddy"), "html", "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return // Don't overwrite existing
	}
	html := `<!DOCTYPE html>
<html><head><title>Caddy - DevBox</title></head>
<body><h1>Caddy is running!</h1><p>Managed by DevBox.</p></body>
</html>`
	os.WriteFile(indexPath, []byte(html), 0644)
}

func (c *CaddyManager) findDownloadURL(version string) (string, error) {
	for _, v := range caddyKnownVersions {
		if v.Version == version {
			return v.URL, nil
		}
	}
	versions, err := c.fetchVersions()
	if err == nil {
		for _, v := range versions {
			if v.Version == version {
				return v.URL, nil
			}
		}
	}
	// Construct URL as fallback
	return fmt.Sprintf("https://github.com/caddyserver/caddy/releases/download/v%s/caddy_%s_windows_amd64.zip", version, version), nil
}

func (c *CaddyManager) fetchVersions() ([]AvailableVersion, error) {
	releases, err := fetchGitHubReleases("caddyserver", "caddy")
	if err != nil {
		return nil, err
	}

	var versions []AvailableVersion
	for _, rel := range releases {
		tag := strings.TrimPrefix(rel.TagName, "v")
		for _, asset := range rel.Assets {
			if asset.Name == fmt.Sprintf("caddy_%s_windows_amd64.zip", tag) {
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
		if len(versions) >= caddyMaxVersions {
			break
		}
	}
	return versions, nil
}
