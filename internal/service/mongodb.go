package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var mongoKnownVersions = []AvailableVersion{
	{Version: "8.0.4", Label: "8.0.4 (Latest)", URL: "https://fastdl.mongodb.org/windows/mongodb-windows-x86_64-8.0.4.zip"},
	{Version: "7.0.16", Label: "7.0.16 (Previous)", URL: "https://fastdl.mongodb.org/windows/mongodb-windows-x86_64-7.0.16.zip"},
}

type MongoDBManager struct{}

func NewMongoDBManager() *MongoDBManager { return &MongoDBManager{} }

func (m *MongoDBManager) Name() string        { return "mongodb" }
func (m *MongoDBManager) DisplayName() string  { return "MongoDB" }
func (m *MongoDBManager) DefaultPort() int     { return 27017 }

func (m *MongoDBManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(m.binDir(), "mongod.exe"))
	return err == nil
}

func (m *MongoDBManager) ListVersions() ([]AvailableVersion, error) {
	return mongoKnownVersions, nil
}

func (m *MongoDBManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("mongodb")

	if port <= 0 {
		port = m.DefaultPort()
	}

	downloadURL, err := m.findDownloadURL(version)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("mongodb-windows-x86_64-%s.zip", version)
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

	// Find extracted directory (e.g., mongodb-win32-x86_64-windows-8.0.4/)
	entries, _ := os.ReadDir(tmpExtract)
	extractedDir := ""
	for _, e := range entries {
		if e.IsDir() && strings.Contains(strings.ToLower(e.Name()), "mongo") {
			extractedDir = filepath.Join(tmpExtract, e.Name())
			break
		}
	}
	if extractedDir == "" {
		return fmt.Errorf("MongoDB directory not found after extraction")
	}

	if _, err := os.Stat(filepath.Join(extractedDir, "bin", "mongod.exe")); os.IsNotExist(err) {
		return fmt.Errorf("mongod.exe not found in extracted files - download may be corrupt")
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Configuring..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install MongoDB: %w", err)
	}

	os.MkdirAll(filepath.Join(base, "data"), 0755)
	os.MkdirAll(filepath.Join(base, "logs"), 0755)

	m.writeConfig(port)
	SaveServiceConfig("mongodb", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("MongoDB %s installed (port %d)", version, port)}
	}
	return nil
}

func (m *MongoDBManager) Uninstall() error {
	if IsRunning("mongodb") {
		m.Stop()
	}
	return os.RemoveAll(serviceBaseDir("mongodb"))
}

func (m *MongoDBManager) Start() error {
	if !m.IsInstalled() {
		return fmt.Errorf("MongoDB is not installed")
	}
	if IsRunning("mongodb") {
		return nil
	}

	port := m.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	base := serviceBaseDir("mongodb")
	mongod := filepath.Join(m.binDir(), "mongod.exe")
	configFile := filepath.Join(base, "mongod.cfg")
	logFile := filepath.Join(base, "logs", "mongod.log")

	_, err := StartProcess("mongodb", mongod, []string{
		"--config", configFile,
	}, base, logFile)
	return err
}

func (m *MongoDBManager) Stop() error {
	if !IsRunning("mongodb") {
		return nil
	}
	return StopProcess("mongodb")
}

func (m *MongoDBManager) Restart() error {
	port := m.Port()
	m.Stop()
	WaitForPortRelease(port, 5)
	return m.Start()
}

func (m *MongoDBManager) Status() ServiceStatus {
	if !m.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("mongodb") {
		return StatusRunning
	}
	return StatusStopped
}

func (m *MongoDBManager) Port() int {
	cfg := LoadServiceConfig("mongodb")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return m.DefaultPort()
}

func (m *MongoDBManager) Version() string {
	cfg := LoadServiceConfig("mongodb")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (m *MongoDBManager) SetPort(port int) error {
	cfg := LoadServiceConfig("mongodb")
	cfg.Port = port
	if err := SaveServiceConfig("mongodb", cfg); err != nil {
		return err
	}
	m.writeConfig(port)
	return nil
}

func (m *MongoDBManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("mongodb"), "logs", "mongod.log")
	return readLastLines(logFile, lines)
}

func (m *MongoDBManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        m.Name(),
		DisplayName: m.DisplayName(),
		Status:      m.Status(),
		Port:        m.Port(),
		Version:     m.Version(),
		Installed:   m.IsInstalled(),
	}
}

func (m *MongoDBManager) binDir() string {
	return filepath.Join(serviceBaseDir("mongodb"), "bin")
}

func (m *MongoDBManager) writeConfig(port int) {
	base := serviceBaseDir("mongodb")
	confPath := filepath.Join(base, "mongod.cfg")
	dataDir := strings.ReplaceAll(filepath.Join(base, "data"), "\\", "/")
	logPath := strings.ReplaceAll(filepath.Join(base, "logs", "mongod-server.log"), "\\", "/")

	conf := fmt.Sprintf(`# DevBox MongoDB Configuration
storage:
  dbPath: %s

net:
  port: %d
  bindIp: 127.0.0.1

systemLog:
  destination: file
  path: %s
  logAppend: true
`, dataDir, port, logPath)

	os.WriteFile(confPath, []byte(conf), 0644)
}

func (m *MongoDBManager) findDownloadURL(version string) (string, error) {
	for _, v := range mongoKnownVersions {
		if v.Version == version {
			return v.URL, nil
		}
	}
	// Construct URL from version
	return fmt.Sprintf("https://fastdl.mongodb.org/windows/mongodb-windows-x86_64-%s.zip", version), nil
}
