package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/platform"
)

const valkeyMaxVersions = 5

// errValkeyNoWindowsSupport is returned when listing/installing Valkey on Windows.
// Valkey upstream does not publish Windows binaries, and the only community port
// (nicenemo/valkey-windows) was removed in 2025. Redis is API-compatible and works
// as a drop-in replacement on Windows via DevBox's Redis service.
var errValkeyNoWindowsSupport = fmt.Errorf("Valkey has no official or maintained community Windows build. Use Redis instead — it is API-compatible with Valkey on Windows")

type ValkeyManager struct{}

func NewValkeyManager() *ValkeyManager { return &ValkeyManager{} }

func (v *ValkeyManager) Name() string        { return "valkey" }
func (v *ValkeyManager) DisplayName() string  { return "Valkey" }
func (v *ValkeyManager) DefaultPort() int     { return 6379 }

func (v *ValkeyManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(serviceBaseDir("valkey"), platform.BinaryName("valkey-server")))
	return err == nil
}

func (v *ValkeyManager) ListVersions() ([]AvailableVersion, error) {
	if goruntime.GOOS == "darwin" {
		return v.listVersionsDarwin()
	}
	return nil, errValkeyNoWindowsSupport
}

func (v *ValkeyManager) listVersionsDarwin() ([]AvailableVersion, error) {
	return []AvailableVersion{
		{Version: "8.1.1", Label: "8.1.1 (Latest)", URL: "https://github.com/valkey-io/valkey/archive/refs/tags/8.1.1.tar.gz"},
		{Version: "8.0.2", Label: "8.0.2", URL: "https://github.com/valkey-io/valkey/archive/refs/tags/8.0.2.tar.gz"},
	}, nil
}

func (v *ValkeyManager) Install(version string, port int, progress chan<- Progress) error {
	if goruntime.GOOS == "darwin" {
		return v.installDarwin(version, port, progress)
	}
	return v.installWindows(version, port, progress)
}

func (v *ValkeyManager) installWindows(version string, port int, progress chan<- Progress) error {
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
		if _, err := os.Stat(filepath.Join(tmpExtract, platform.BinaryName("valkey-server"))); err == nil {
			extractedDir = tmpExtract
		} else {
			return fmt.Errorf("Valkey directory not found after extraction")
		}
	}

	if _, err := os.Stat(filepath.Join(extractedDir, platform.BinaryName("valkey-server"))); os.IsNotExist(err) {
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

func (v *ValkeyManager) installDarwin(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("valkey")
	if port <= 0 {
		port = v.DefaultPort()
	}

	filename := fmt.Sprintf("valkey-%s.tar.gz", version)
	downloadURL := fmt.Sprintf("https://github.com/valkey-io/valkey/archive/refs/tags/%s.tar.gz", version)

	tmpFile, err := downloadToTmp(downloadURL, filename, progress)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpFile)

	if progress != nil {
		progress <- Progress{Percent: 50, Message: "Extracting source..."}
	}

	tmpExtract := tmpFile + "-extract"
	os.RemoveAll(tmpExtract)
	defer os.RemoveAll(tmpExtract)

	if err := extractTarGz(tmpFile, tmpExtract); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	srcDir := filepath.Join(tmpExtract, fmt.Sprintf("valkey-%s", version))
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		entries, _ := os.ReadDir(tmpExtract)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "valkey") {
				srcDir = filepath.Join(tmpExtract, e.Name())
				break
			}
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 60, Message: "Compiling Valkey (this may take a minute)..."}
	}

	makeCmd := exec.Command("make")
	makeCmd.Dir = srcDir
	if out, err := makeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("make failed: %s - %w", string(out), err)
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(base, 0755)

	// Copy built binaries from src/ to base dir
	srcBin := filepath.Join(srcDir, "src")
	for _, bin := range []string{"valkey-server", "valkey-cli", "valkey-benchmark"} {
		src := filepath.Join(srcBin, bin)
		dst := filepath.Join(base, bin)
		if data, err := os.ReadFile(src); err == nil {
			os.WriteFile(dst, data, 0755)
		}
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
	exe := filepath.Join(base, platform.BinaryName("valkey-server"))
	logFile := filepath.Join(base, "logs", "valkey.log")

		// The Windows builds are MSYS-based and take the service dir as their
	// filesystem root, so absolute Windows paths ("C:\...") are unusable —
	// the config is passed relative to the working directory (base).
	_, err := StartProcess("valkey", exe, []string{"valkey.conf"}, base, logFile)
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
	// Relative to the service dir (see Start: MSYS builds can't use C:\ paths).
	dataDir := "data"

	conf := fmt.Sprintf(`# DevBox Valkey Configuration
bind 127.0.0.1
port %d
dir %s
dbfilename dump.rdb
appendonly yes
appendfilename "appendonly.aof"
maxmemory 256mb
maxmemory-policy allkeys-lru
%s`, port, dataDir, redisExtraConfig("valkey"))

	os.WriteFile(confPath, []byte(conf), 0644)
}

func (v *ValkeyManager) findDownloadURL(version string) (string, error) {
	// Windows path is unreachable now (ListVersions errors out), but kept defensive
	// in case Install is called with a stale version string.
	return "", errValkeyNoWindowsSupport
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
