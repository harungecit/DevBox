package service

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"

	"DevBox/internal/platform"
)

// pgKnownVersionsList returns platform-appropriate PostgreSQL download URLs
func pgKnownVersionsList() []AvailableVersion {
	if goruntime.GOOS == "darwin" {
		return []AvailableVersion{
			{Version: "17.8", Label: "PostgreSQL 17 (17.8)", URL: "https://sbp.enterprisedb.com/getfile.jsp?fileid=1260008"},
			{Version: "16.12", Label: "PostgreSQL 16 (16.12)", URL: "https://sbp.enterprisedb.com/getfile.jsp?fileid=1260007"},
		}
	}
	return []AvailableVersion{
		{Version: "18.2", Label: "PostgreSQL 18 (18.2)", URL: "https://sbp.enterprisedb.com/getfile.jsp?fileid=1260010"},
		{Version: "17.8", Label: "PostgreSQL 17 (17.8)", URL: "https://sbp.enterprisedb.com/getfile.jsp?fileid=1260006"},
		{Version: "16.12", Label: "PostgreSQL 16 (16.12)", URL: "https://sbp.enterprisedb.com/getfile.jsp?fileid=1260005"},
		{Version: "15.16", Label: "PostgreSQL 15 (15.16)", URL: "https://sbp.enterprisedb.com/getfile.jsp?fileid=1260002"},
	}
}

type PostgresManager struct{}

func NewPostgresManager() *PostgresManager { return &PostgresManager{} }

func (p *PostgresManager) Name() string       { return "postgres" }
func (p *PostgresManager) DisplayName() string { return "PostgreSQL" }
func (p *PostgresManager) DefaultPort() int    { return 5432 }

func (p *PostgresManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(p.binDir(), platform.BinaryName("pg_ctl")))
	return err == nil
}

func (p *PostgresManager) ListVersions() ([]AvailableVersion, error) {
	// Use known versions only - EDB page scraper is unreliable
	return pgKnownVersionsList(), nil
}

func (p *PostgresManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("postgres")

	if port <= 0 {
		port = p.DefaultPort()
	}

	downloadURL, err := p.findDownloadURL(version)
	if err != nil {
		return err
	}

	var filename string
	if goruntime.GOOS == "darwin" {
		filename = fmt.Sprintf("postgresql-%s-osx-binaries.tar.gz", version)
	} else {
		filename = fmt.Sprintf("postgresql-%s-windows-x64-binaries.zip", version)
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

	// EDB archive has "pgsql/" top-level directory
	extractedDir := filepath.Join(tmpExtract, "pgsql")
	if _, err := os.Stat(extractedDir); os.IsNotExist(err) {
		entries, _ := os.ReadDir(tmpExtract)
		extractedDir = ""
		for _, e := range entries {
			if e.IsDir() {
				extractedDir = filepath.Join(tmpExtract, e.Name())
				break
			}
		}
		if extractedDir == "" {
			return fmt.Errorf("PostgreSQL directory not found after extraction")
		}
	}

	// Verify critical binary exists
	if _, err := os.Stat(filepath.Join(extractedDir, "bin", platform.BinaryName("pg_ctl"))); os.IsNotExist(err) {
		return fmt.Errorf("pg_ctl binary not found in extracted files - download may be corrupt")
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install PostgreSQL: %w", err)
	}

	os.MkdirAll(filepath.Join(base, "logs"), 0755)

	// Save config early so port/version are remembered even if init fails
	SaveServiceConfig("postgres", ServiceConfig{Port: port, Version: version})

	// Initialize database
	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Initializing database cluster..."}
	}

	dataDir := filepath.Join(base, "data")
	if err := p.initDB(dataDir, port); err != nil {
		return fmt.Errorf("initdb failed: %w", err)
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("PostgreSQL %s installed (port %d)", version, port)}
	}
	return nil
}

func (p *PostgresManager) Uninstall() error {
	if IsRunning("postgres") {
		p.Stop()
	}
	return os.RemoveAll(serviceBaseDir("postgres"))
}

func (p *PostgresManager) Start() error {
	if !p.IsInstalled() {
		return fmt.Errorf("PostgreSQL is not installed")
	}
	if IsRunning("postgres") {
		return nil
	}

	// Check if postgres is still running from a previous DevBox session
	// (e.g., after app restart / hot-reload) by reading its own postmaster.pid
	if p.adoptExistingProcess() {
		return nil
	}

	port := p.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use (maybe WSL PostgreSQL?) - change the port in service settings", port)
	}

	base := serviceBaseDir("postgres")
	postgresExe := filepath.Join(p.binDir(), platform.BinaryName("postgres"))
	dataDir := filepath.Join(base, "data")
	logFile := filepath.Join(base, "logs", "postgresql.log")

	// Start postgres.exe directly (pg_ctl hangs when run from Go due to console piping)
	_, err := StartProcess("postgres", postgresExe, []string{
		"-D", dataDir,
		"-p", fmt.Sprintf("%d", port),
	}, base, logFile)

	if err != nil {
		logContent := readRecentLog(logFile)
		if logContent != "" {
			return fmt.Errorf("PostgreSQL failed to start (port %d):\n%w\n\nLog:\n%s", port, err, logContent)
		}
		return err
	}

	return nil
}

func (p *PostgresManager) Stop() error {
	if !IsRunning("postgres") {
		return nil
	}
	return StopProcess("postgres")
}

func (p *PostgresManager) Restart() error {
	port := p.Port()
	p.Stop()
	WaitForPortRelease(port, 5)
	return p.Start()
}

func (p *PostgresManager) Status() ServiceStatus {
	if !p.IsInstalled() {
		return StatusNotInstalled
	}
	// Use PID-based check only (pg_ctl status hangs from Go)
	if IsRunning("postgres") {
		return StatusRunning
	}
	// Check if postgres survived a DevBox restart (postmaster.pid still valid)
	if p.adoptExistingProcess() {
		return StatusRunning
	}
	return StatusStopped
}

func (p *PostgresManager) Port() int {
	cfg := LoadServiceConfig("postgres")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return p.DefaultPort()
}

func (p *PostgresManager) Version() string {
	cfg := LoadServiceConfig("postgres")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (p *PostgresManager) SetPort(port int) error {
	cfg := LoadServiceConfig("postgres")
	cfg.Port = port
	if err := SaveServiceConfig("postgres", cfg); err != nil {
		return err
	}

	// Update postgresql.conf
	dataDir := filepath.Join(serviceBaseDir("postgres"), "data")
	confFile := filepath.Join(dataDir, "postgresql.conf")
	data, err := os.ReadFile(confFile)
	if err != nil {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "port") {
			lines[i] = fmt.Sprintf("port = %d", port)
			found = true
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("port = %d", port))
	}

	return os.WriteFile(confFile, []byte(strings.Join(lines, "\n")), 0644)
}

func (p *PostgresManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("postgres"), "logs", "postgresql.log")
	return readLastLines(logFile, lines)
}

func (p *PostgresManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        p.Name(),
		DisplayName: p.DisplayName(),
		Status:      p.Status(),
		Port:        p.Port(),
		Version:     p.Version(),
		Installed:   p.IsInstalled(),
	}
}

func (p *PostgresManager) binDir() string {
	return filepath.Join(serviceBaseDir("postgres"), "bin")
}

func (p *PostgresManager) initDB(dataDir string, port int) error {
	initdb := filepath.Join(p.binDir(), platform.BinaryName("initdb"))
	if _, err := os.Stat(initdb); os.IsNotExist(err) {
		return fmt.Errorf("initdb not found at %s", initdb)
	}

	out, err := runCommand(initdb, []string{
		"-D", dataDir,
		"-U", "postgres",
		"-E", "UTF8",
		"--no-locale",
	}, serviceBaseDir("postgres"))

	if err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}

	// Configure port in postgresql.conf
	confFile := filepath.Join(dataDir, "postgresql.conf")
	f, err := os.OpenFile(confFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		fmt.Fprintf(f, "\n# DevBox configuration\nport = %d\nlisten_addresses = 'localhost'\n", port)
		f.Close()
	}

	return nil
}

// adoptExistingProcess checks if a postgres process from a previous DevBox session
// is still running by reading PostgreSQL's own postmaster.pid file.
// If found alive, it saves the PID so IsRunning() works correctly.
func (p *PostgresManager) adoptExistingProcess() bool {
	base := serviceBaseDir("postgres")
	postmasterPid := filepath.Join(base, "data", "postmaster.pid")
	data, err := os.ReadFile(postmasterPid)
	if err != nil {
		return false
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return false
	}

	if !platform.IsProcessRunning(pid) {
		// Stale postmaster.pid — postgres crashed or was killed externally
		os.Remove(postmasterPid)
		return false
	}

	// Postgres is alive — save its PID so DevBox can track/stop it
	pidFile := pidFilePath("postgres")
	os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644)
	return true
}

func (p *PostgresManager) findDownloadURL(version string) (string, error) {
	for _, v := range pgKnownVersionsList() {
		if v.Version == version {
			return v.URL, nil
		}
	}
	return "", fmt.Errorf("PostgreSQL %s download URL not found", version)
}


