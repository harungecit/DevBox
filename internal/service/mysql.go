package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"

	"DevBox/internal/platform"
)

// mysqlKnownVersionsList returns platform-appropriate MySQL download URLs
func mysqlKnownVersionsList() []AvailableVersion {
	if goruntime.GOOS == "darwin" {
		arch := "arm64"
		if goruntime.GOARCH == "amd64" {
			arch = "x86_64"
		}
		return []AvailableVersion{
			{Version: "9.2.0", Label: "9.2.0 (Innovation)", URL: fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-9.2/mysql-9.2.0-macos14-%s.tar.gz", arch)},
			{Version: "9.1.0", Label: "9.1.0 (Innovation)", URL: fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-9.1/mysql-9.1.0-macos14-%s.tar.gz", arch)},
			{Version: "9.0.1", Label: "9.0.1 (Innovation)", URL: fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-9.0/mysql-9.0.1-macos14-%s.tar.gz", arch)},
			{Version: "8.4.4", Label: "8.4.4 (LTS)", URL: fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-8.4/mysql-8.4.4-macos14-%s.tar.gz", arch)},
			{Version: "8.0.41", Label: "8.0.41", URL: fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-8.0/mysql-8.0.41-macos14-%s.tar.gz", arch)},
		}
	}
	return []AvailableVersion{
		{Version: "9.2.0", Label: "9.2.0 (Innovation)", URL: "https://cdn.mysql.com/Downloads/MySQL-9.2/mysql-9.2.0-winx64.zip"},
		{Version: "9.1.0", Label: "9.1.0 (Innovation)", URL: "https://cdn.mysql.com/Downloads/MySQL-9.1/mysql-9.1.0-winx64.zip"},
		{Version: "9.0.1", Label: "9.0.1 (Innovation)", URL: "https://cdn.mysql.com/Downloads/MySQL-9.0/mysql-9.0.1-winx64.zip"},
		{Version: "8.4.4", Label: "8.4.4 (LTS)", URL: "https://cdn.mysql.com/Downloads/MySQL-8.4/mysql-8.4.4-winx64.zip"},
		{Version: "8.0.41", Label: "8.0.41", URL: "https://cdn.mysql.com/Downloads/MySQL-8.0/mysql-8.0.41-winx64.zip"},
	}
}

const maxMySQLVersions = 5

type MySQLManager struct{}

func NewMySQLManager() *MySQLManager { return &MySQLManager{} }

func (m *MySQLManager) Name() string       { return "mysql" }
func (m *MySQLManager) DisplayName() string { return "MySQL" }
func (m *MySQLManager) DefaultPort() int    { return 3306 }

func (m *MySQLManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(m.binDir(), platform.BinaryName("mysqld")))
	return err == nil
}

func (m *MySQLManager) ListVersions() ([]AvailableVersion, error) {
	versions, err := m.scrapeVersions()
	if err != nil || len(versions) == 0 {
		return mysqlKnownVersionsList(), nil
	}
	// Limit to last 5 versions
	if len(versions) > maxMySQLVersions {
		versions = versions[:maxMySQLVersions]
	}
	return versions, nil
}

func (m *MySQLManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("mysql")

	if port <= 0 {
		port = m.DefaultPort()
	}

	downloadURL, err := m.findDownloadURL(version)
	if err != nil {
		return err
	}

	var filename string
	if goruntime.GOOS == "darwin" {
		arch := "arm64"
		if goruntime.GOARCH == "amd64" {
			arch = "x86_64"
		}
		filename = fmt.Sprintf("mysql-%s-macos14-%s.tar.gz", version, arch)
	} else {
		filename = fmt.Sprintf("mysql-%s-winx64.zip", version)
	}
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

	if err := extractArchive(tmpFile, tmpExtract); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// MySQL archive has "mysql-VERSION-*/" top-level directory
	entries, _ := os.ReadDir(tmpExtract)
	extractedDir := ""
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "mysql") {
			extractedDir = filepath.Join(tmpExtract, e.Name())
			break
		}
	}
	if extractedDir == "" {
		return fmt.Errorf("MySQL directory not found after extraction")
	}

	// Verify critical binary exists
	if _, err := os.Stat(filepath.Join(extractedDir, "bin", platform.BinaryName("mysqld"))); os.IsNotExist(err) {
		return fmt.Errorf("mysqld binary not found in extracted files - download may be corrupt")
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install MySQL: %w", err)
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Initializing MySQL..."}
	}

	os.MkdirAll(filepath.Join(base, "data"), 0755)
	os.MkdirAll(filepath.Join(base, "logs"), 0755)

	m.writeConfig(port)

	// Save config early so port/version are remembered even if init fails
	SaveServiceConfig("mysql", ServiceConfig{Port: port, Version: version})

	if err := m.initialize(); err != nil {
		fmt.Printf("MySQL init note: %v\n", err)
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("MySQL %s installed (port %d)", version, port)}
	}
	return nil
}

func (m *MySQLManager) Uninstall() error {
	if IsRunning("mysql") {
		m.Stop()
	}
	return os.RemoveAll(serviceBaseDir("mysql"))
}

func (m *MySQLManager) Start() error {
	if !m.IsInstalled() {
		return fmt.Errorf("MySQL is not installed")
	}
	if IsRunning("mysql") {
		return nil
	}

	port := m.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use - change the port in service settings", port)
	}

	base := serviceBaseDir("mysql")
	mysqld := filepath.Join(m.binDir(), platform.BinaryName("mysqld"))
	logFile := filepath.Join(base, "logs", "mysql-start.log")

	_, err := StartProcess("mysql", mysqld, []string{
		fmt.Sprintf("--defaults-file=%s", filepath.Join(base, MysqlConfigName())),
	}, base, logFile)
	return err
}

func (m *MySQLManager) Stop() error {
	if !IsRunning("mysql") {
		return nil
	}

	mysqladmin := filepath.Join(m.binDir(), platform.BinaryName("mysqladmin"))
	if _, err := os.Stat(mysqladmin); err == nil {
		args, env := mysqlAdminEnv("mysql")
		runWithEnv(mysqladmin, append(args, "shutdown"), serviceBaseDir("mysql"), env...)
	}

	return StopProcess("mysql")
}

func (m *MySQLManager) Restart() error {
	port := m.Port()
	m.Stop()
	WaitForPortRelease(port, 5)
	return m.Start()
}

func (m *MySQLManager) Status() ServiceStatus {
	if !m.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("mysql") {
		return StatusRunning
	}
	return StatusStopped
}

func (m *MySQLManager) Port() int {
	cfg := LoadServiceConfig("mysql")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return m.DefaultPort()
}

func (m *MySQLManager) Version() string {
	cfg := LoadServiceConfig("mysql")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (m *MySQLManager) SetPort(port int) error {
	cfg := LoadServiceConfig("mysql")
	cfg.Port = port
	if err := SaveServiceConfig("mysql", cfg); err != nil {
		return err
	}
	m.writeConfig(port)
	return nil
}

func (m *MySQLManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("mysql"), "logs", "mysql-error.log")
	return readLastLines(logFile, lines)
}

func (m *MySQLManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        m.Name(),
		DisplayName: m.DisplayName(),
		Status:      m.Status(),
		Port:        m.Port(),
		Version:     m.Version(),
		Installed:   m.IsInstalled(),
	}
}

func (m *MySQLManager) binDir() string {
	return filepath.Join(serviceBaseDir("mysql"), "bin")
}

func (m *MySQLManager) writeConfig(port int) {
	base := serviceBaseDir("mysql")
	confPath := filepath.Join(base, MysqlConfigName())
	basePath := strings.ReplaceAll(base, "\\", "/")

	conf := fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s/data
port=%d
max_connections=100
character-set-server=utf8mb4
default-storage-engine=INNODB
log-error=%s/logs/mysql-error.log

[client]
port=%d
default-character-set=utf8mb4
`, basePath, basePath, port, basePath, port)

	os.WriteFile(confPath, []byte(conf), 0644)
}

func (m *MySQLManager) initialize() error {
	mysqld := filepath.Join(m.binDir(), platform.BinaryName("mysqld"))
	base := serviceBaseDir("mysql")
	iniPath := filepath.Join(base, MysqlConfigName())

	out, err := runCommand(mysqld, []string{
		fmt.Sprintf("--defaults-file=%s", iniPath),
		"--initialize-insecure",
	}, base)

	if err != nil {
		return fmt.Errorf("mysqld --initialize-insecure: %s - %w", out, err)
	}

	return nil
}

func (m *MySQLManager) findDownloadURL(version string) (string, error) {
	for _, v := range mysqlKnownVersionsList() {
		if v.Version == version {
			return v.URL, nil
		}
	}

	// Try constructing URL from version
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		majorMinor := parts[0] + "." + parts[1]
		if goruntime.GOOS == "darwin" {
			arch := "arm64"
			if goruntime.GOARCH == "amd64" {
				arch = "x86_64"
			}
			url := fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-%s/mysql-%s-macos14-%s.tar.gz", majorMinor, version, arch)
			return url, nil
		}
		url := fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-%s/mysql-%s-winx64.zip", majorMinor, version)
		return url, nil
	}

	return "", fmt.Errorf("MySQL %s download URL not found", version)
}

func (m *MySQLManager) scrapeVersions() ([]AvailableVersion, error) {
	// On macOS, skip scraping (the page lists Windows binaries) and use known versions
	if goruntime.GOOS == "darwin" {
		return mysqlKnownVersionsList(), nil
	}

	resp, err := http.Get("https://dev.mysql.com/downloads/mysql/")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`mysql-(\d+\.\d+\.\d+)-winx64\.zip`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	seen := map[string]bool{}
	var versions []AvailableVersion

	for _, match := range matches {
		ver := match[1]
		if seen[ver] {
			continue
		}
		seen[ver] = true

		parts := strings.Split(ver, ".")
		majorMinor := parts[0] + "." + parts[1]
		url := fmt.Sprintf("https://cdn.mysql.com/Downloads/MySQL-%s/mysql-%s-winx64.zip", majorMinor, ver)

		versions = append(versions, AvailableVersion{
			Version: ver,
			Label:   ver,
			URL:     url,
		})

		// Limit to 5 versions
		if len(versions) >= maxMySQLVersions {
			break
		}
	}

	return versions, nil
}
