package service

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/platform"
)

func mariadbKnownVersionsList() []AvailableVersion {
	if goruntime.GOOS == "darwin" {
		arch := darwinArch()
		return []AvailableVersion{
			{Version: "11.6.2", Label: "11.6.2 (Latest)", URL: fmt.Sprintf("https://archive.mariadb.org/mariadb-11.6.2/bintar-darwin-%s/mariadb-11.6.2-darwin-%s.tar.gz", arch, arch)},
			{Version: "11.4.4", Label: "11.4.4 (LTS)", URL: fmt.Sprintf("https://archive.mariadb.org/mariadb-11.4.4/bintar-darwin-%s/mariadb-11.4.4-darwin-%s.tar.gz", arch, arch)},
			{Version: "10.11.10", Label: "10.11.10 (LTS)", URL: fmt.Sprintf("https://archive.mariadb.org/mariadb-10.11.10/bintar-darwin-%s/mariadb-10.11.10-darwin-%s.tar.gz", arch, arch)},
		}
	}
	return []AvailableVersion{
		{Version: "11.6.2", Label: "11.6.2 (Latest)", URL: "https://archive.mariadb.org/mariadb-11.6.2/winx64-packages/mariadb-11.6.2-winx64.zip"},
		{Version: "11.4.4", Label: "11.4.4 (LTS)", URL: "https://archive.mariadb.org/mariadb-11.4.4/winx64-packages/mariadb-11.4.4-winx64.zip"},
		{Version: "10.11.10", Label: "10.11.10 (LTS)", URL: "https://archive.mariadb.org/mariadb-10.11.10/winx64-packages/mariadb-10.11.10-winx64.zip"},
	}
}

type MariaDBManager struct{}

func NewMariaDBManager() *MariaDBManager { return &MariaDBManager{} }

func (m *MariaDBManager) Name() string        { return "mariadb" }
func (m *MariaDBManager) DisplayName() string  { return "MariaDB" }
func (m *MariaDBManager) DefaultPort() int     { return 3306 }

func (m *MariaDBManager) IsInstalled() bool {
	// MariaDB 11+ uses mariadbd.exe, older uses mysqld.exe
	if _, err := os.Stat(filepath.Join(m.binDir(), platform.BinaryName("mariadbd"))); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(m.binDir(), platform.BinaryName("mysqld"))); err == nil {
		return true
	}
	return false
}

func (m *MariaDBManager) ListVersions() ([]AvailableVersion, error) {
	return mariadbKnownVersionsList(), nil
}

func (m *MariaDBManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("mariadb")

	if port <= 0 {
		port = m.DefaultPort()
	}

	downloadURL, err := m.findDownloadURL(version)
	if err != nil {
		return err
	}

	var filename string
	if goruntime.GOOS == "darwin" {
		arch := darwinArch()
		filename = fmt.Sprintf("mariadb-%s-darwin-%s.tar.gz", version, arch)
	} else {
		filename = fmt.Sprintf("mariadb-%s-winx64.zip", version)
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

	// MariaDB archive has "mariadb-VERSION-*/" top-level directory
	entries, _ := os.ReadDir(tmpExtract)
	extractedDir := ""
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "mariadb") {
			extractedDir = filepath.Join(tmpExtract, e.Name())
			break
		}
	}
	if extractedDir == "" {
		return fmt.Errorf("MariaDB directory not found after extraction")
	}

	// Verify critical binary exists (mariadbd.exe or mysqld.exe)
	hasServer := false
	if _, err := os.Stat(filepath.Join(extractedDir, "bin", platform.BinaryName("mariadbd"))); err == nil {
		hasServer = true
	}
	if _, err := os.Stat(filepath.Join(extractedDir, "bin", platform.BinaryName("mysqld"))); err == nil {
		hasServer = true
	}
	if !hasServer {
		return fmt.Errorf("MariaDB server binary not found in extracted files")
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install MariaDB: %w", err)
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Initializing MariaDB..."}
	}

	os.MkdirAll(filepath.Join(base, "data"), 0755)
	os.MkdirAll(filepath.Join(base, "logs"), 0755)

	m.writeConfig(port)
	SaveServiceConfig("mariadb", ServiceConfig{Port: port, Version: version})

	if err := m.initialize(); err != nil {
		fmt.Printf("MariaDB init note: %v\n", err)
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("MariaDB %s installed (port %d)", version, port)}
	}
	return nil
}

func (m *MariaDBManager) Uninstall() error {
	if IsRunning("mariadb") {
		m.Stop()
	}
	return os.RemoveAll(serviceBaseDir("mariadb"))
}

func (m *MariaDBManager) Start() error {
	if !m.IsInstalled() {
		return fmt.Errorf("MariaDB is not installed")
	}
	if IsRunning("mariadb") {
		return nil
	}

	port := m.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	base := serviceBaseDir("mariadb")
	serverExe := m.serverExe()
	logFile := filepath.Join(base, "logs", "mariadb-start.log")

	_, err := StartProcess("mariadb", serverExe, []string{
		fmt.Sprintf("--defaults-file=%s", filepath.Join(base, MysqlConfigName())),
	}, base, logFile)
	return err
}

func (m *MariaDBManager) Stop() error {
	if !IsRunning("mariadb") {
		return nil
	}

	// Try graceful shutdown
	admin := filepath.Join(m.binDir(), platform.BinaryName("mariadb-admin"))
	if _, err := os.Stat(admin); os.IsNotExist(err) {
		admin = filepath.Join(m.binDir(), platform.BinaryName("mysqladmin"))
	}
	if _, err := os.Stat(admin); err == nil {
		args, env := mysqlAdminEnv("mariadb")
		runWithEnv(admin, append(args, "shutdown"), serviceBaseDir("mariadb"), env...)
	}

	return StopProcess("mariadb")
}

func (m *MariaDBManager) Restart() error {
	port := m.Port()
	m.Stop()
	WaitForPortRelease(port, 5)
	return m.Start()
}

func (m *MariaDBManager) Status() ServiceStatus {
	if !m.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("mariadb") {
		return StatusRunning
	}
	return StatusStopped
}

func (m *MariaDBManager) Port() int {
	cfg := LoadServiceConfig("mariadb")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return m.DefaultPort()
}

func (m *MariaDBManager) Version() string {
	cfg := LoadServiceConfig("mariadb")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (m *MariaDBManager) SetPort(port int) error {
	cfg := LoadServiceConfig("mariadb")
	cfg.Port = port
	if err := SaveServiceConfig("mariadb", cfg); err != nil {
		return err
	}
	m.writeConfig(port)
	return nil
}

func (m *MariaDBManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("mariadb"), "logs", "mariadb-error.log")
	return readLastLines(logFile, lines)
}

func (m *MariaDBManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        m.Name(),
		DisplayName: m.DisplayName(),
		Status:      m.Status(),
		Port:        m.Port(),
		Version:     m.Version(),
		Installed:   m.IsInstalled(),
	}
}

func (m *MariaDBManager) binDir() string {
	return filepath.Join(serviceBaseDir("mariadb"), "bin")
}

// serverExe returns the path to the MariaDB server binary
func (m *MariaDBManager) serverExe() string {
	mariadbd := filepath.Join(m.binDir(), platform.BinaryName("mariadbd"))
	if _, err := os.Stat(mariadbd); err == nil {
		return mariadbd
	}
	return filepath.Join(m.binDir(), platform.BinaryName("mysqld"))
}

func (m *MariaDBManager) writeConfig(port int) {
	base := serviceBaseDir("mariadb")
	confPath := filepath.Join(base, MysqlConfigName())
	basePath := strings.ReplaceAll(base, "\\", "/")

	conf := fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s/data
port=%d
max_connections=100
character-set-server=utf8mb4
default-storage-engine=InnoDB
log-error=%s/logs/mariadb-error.log

[client]
port=%d
default-character-set=utf8mb4
`, basePath, basePath, port, basePath, port)

	os.WriteFile(confPath, []byte(conf), 0644)
}

func (m *MariaDBManager) initialize() error {
	base := serviceBaseDir("mariadb")

	// Try mariadb-install-db first (newer MariaDB), then mysql_install_db
	installDB := filepath.Join(m.binDir(), platform.BinaryName("mariadb-install-db"))
	if _, err := os.Stat(installDB); os.IsNotExist(err) {
		installDB = filepath.Join(m.binDir(), platform.BinaryName("mysql_install_db"))
	}

	if _, err := os.Stat(installDB); err == nil {
		out, err := runCommand(installDB, []string{
			fmt.Sprintf("--datadir=%s", filepath.Join(base, "data")),
			"--password=",
		}, base)
		if err != nil {
			return fmt.Errorf("install-db: %s - %w", out, err)
		}
		return nil
	}

	// Fallback: use mysqld --initialize-insecure (works for older MariaDB versions that ship mysqld)
	serverExe := m.serverExe()
	iniPath := filepath.Join(base, MysqlConfigName())
	out, err := runCommand(serverExe, []string{
		fmt.Sprintf("--defaults-file=%s", iniPath),
		"--initialize-insecure",
	}, base)
	if err != nil {
		return fmt.Errorf("initialize: %s - %w", out, err)
	}

	return nil
}

func (m *MariaDBManager) findDownloadURL(version string) (string, error) {
	for _, v := range mariadbKnownVersionsList() {
		if v.Version == version {
			return v.URL, nil
		}
	}
	// Construct URL from version
	if goruntime.GOOS == "darwin" {
		arch := darwinArch()
		return fmt.Sprintf("https://archive.mariadb.org/mariadb-%s/bintar-darwin-%s/mariadb-%s-darwin-%s.tar.gz", version, arch, version, arch), nil
	}
	return fmt.Sprintf("https://archive.mariadb.org/mariadb-%s/winx64-packages/mariadb-%s-winx64.zip", version, version), nil
}
