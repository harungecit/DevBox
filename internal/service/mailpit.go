package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const mailpitMaxVersions = 5

var mailpitKnownVersions = []AvailableVersion{
	{Version: "1.22.3", Label: "1.22.3 (Latest)", URL: "https://github.com/axllent/mailpit/releases/download/v1.22.3/mailpit-windows-amd64.zip"},
	{Version: "1.21.7", Label: "1.21.7", URL: "https://github.com/axllent/mailpit/releases/download/v1.21.7/mailpit-windows-amd64.zip"},
}

type MailpitManager struct{}

func NewMailpitManager() *MailpitManager { return &MailpitManager{} }

func (mp *MailpitManager) Name() string        { return "mailpit" }
func (mp *MailpitManager) DisplayName() string  { return "Mailpit" }
func (mp *MailpitManager) DefaultPort() int     { return 1025 }

func (mp *MailpitManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(serviceBaseDir("mailpit"), "mailpit.exe"))
	return err == nil
}

func (mp *MailpitManager) ListVersions() ([]AvailableVersion, error) {
	versions, err := mp.fetchVersions()
	if err != nil || len(versions) == 0 {
		return mailpitKnownVersions, nil
	}
	if len(versions) > mailpitMaxVersions {
		versions = versions[:mailpitMaxVersions]
	}
	return versions, nil
}

func (mp *MailpitManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("mailpit")

	if port <= 0 {
		port = mp.DefaultPort()
	}

	downloadURL, err := mp.findDownloadURL(version)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("mailpit-%s-windows-amd64.zip", version)
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

	// Mailpit zip contains just mailpit.exe (no subdirectory)
	// Check both root and any subdirectory
	exePath := filepath.Join(tmpExtract, "mailpit.exe")
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		// Look in subdirectories
		entries, _ := os.ReadDir(tmpExtract)
		found := false
		for _, e := range entries {
			if e.IsDir() {
				candidate := filepath.Join(tmpExtract, e.Name(), "mailpit.exe")
				if _, err := os.Stat(candidate); err == nil {
					tmpExtract = filepath.Join(tmpExtract, e.Name())
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("mailpit.exe not found in extracted files")
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Installing..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(base, 0755)
	os.MkdirAll(filepath.Join(base, "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "data"), 0755)

	// Move extracted files to base
	if err := moveDir(tmpExtract, base); err != nil {
		// If moveDir fails (e.g., base already has dirs we created), copy just the exe
		entries, _ := os.ReadDir(tmpExtract)
		for _, e := range entries {
			src := filepath.Join(tmpExtract, e.Name())
			dst := filepath.Join(base, e.Name())
			os.Rename(src, dst)
		}
	}

	SaveServiceConfig("mailpit", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Mailpit %s installed (SMTP port %d, UI port %d)", version, port, mp.uiPort(port))}
	}
	return nil
}

func (mp *MailpitManager) Uninstall() error {
	if IsRunning("mailpit") {
		mp.Stop()
	}
	return os.RemoveAll(serviceBaseDir("mailpit"))
}

func (mp *MailpitManager) Start() error {
	if !mp.IsInstalled() {
		return fmt.Errorf("Mailpit is not installed")
	}
	if IsRunning("mailpit") {
		return nil
	}

	port := mp.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("SMTP port %d is already in use", port)
	}

	base := serviceBaseDir("mailpit")
	exe := filepath.Join(base, "mailpit.exe")
	logFile := filepath.Join(base, "logs", "mailpit.log")
	uiPort := mp.uiPort(port)

	_, err := StartProcess("mailpit", exe, []string{
		"--smtp", fmt.Sprintf("127.0.0.1:%d", port),
		"--listen", fmt.Sprintf("127.0.0.1:%d", uiPort),
		"--db-file", filepath.Join(base, "data", "mailpit.db"),
	}, base, logFile)
	return err
}

func (mp *MailpitManager) Stop() error {
	if !IsRunning("mailpit") {
		return nil
	}
	return StopProcess("mailpit")
}

func (mp *MailpitManager) Restart() error {
	port := mp.Port()
	mp.Stop()
	WaitForPortRelease(port, 5)
	return mp.Start()
}

func (mp *MailpitManager) Status() ServiceStatus {
	if !mp.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("mailpit") {
		return StatusRunning
	}
	return StatusStopped
}

func (mp *MailpitManager) Port() int {
	cfg := LoadServiceConfig("mailpit")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return mp.DefaultPort()
}

func (mp *MailpitManager) Version() string {
	cfg := LoadServiceConfig("mailpit")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (mp *MailpitManager) SetPort(port int) error {
	cfg := LoadServiceConfig("mailpit")
	cfg.Port = port
	return SaveServiceConfig("mailpit", cfg)
}

func (mp *MailpitManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("mailpit"), "logs", "mailpit.log")
	return readLastLines(logFile, lines)
}

func (mp *MailpitManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        mp.Name(),
		DisplayName: mp.DisplayName(),
		Status:      mp.Status(),
		Port:        mp.Port(),
		Version:     mp.Version(),
		Installed:   mp.IsInstalled(),
	}
}

// uiPort returns the web UI port derived from the SMTP port (SMTP + 7000)
func (mp *MailpitManager) uiPort(smtpPort int) int {
	return smtpPort + 7000
}

func (mp *MailpitManager) findDownloadURL(version string) (string, error) {
	for _, v := range mailpitKnownVersions {
		if v.Version == version {
			return v.URL, nil
		}
	}
	// Try GitHub API for dynamic versions
	versions, err := mp.fetchVersions()
	if err == nil {
		for _, v := range versions {
			if v.Version == version {
				return v.URL, nil
			}
		}
	}
	// Construct URL as fallback
	return fmt.Sprintf("https://github.com/axllent/mailpit/releases/download/v%s/mailpit-windows-amd64.zip", version), nil
}

func (mp *MailpitManager) fetchVersions() ([]AvailableVersion, error) {
	releases, err := fetchGitHubReleases("axllent", "mailpit")
	if err != nil {
		return nil, err
	}

	var versions []AvailableVersion
	for _, rel := range releases {
		tag := strings.TrimPrefix(rel.TagName, "v")
		for _, asset := range rel.Assets {
			if asset.Name == "mailpit-windows-amd64.zip" {
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
		if len(versions) >= mailpitMaxVersions {
			break
		}
	}
	return versions, nil
}
