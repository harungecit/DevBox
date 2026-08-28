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

const redisMaxVersions = 5

// redis-windows ships zips as Redis-X.Y.Z-Windows-x64-msys2.zip (preferred) or
// Redis-X.Y.Z-Windows-x64-cygwin.zip. We standardize on the msys2 variant —
// lighter and what every recent release publishes. The plain "Windows-x64.zip"
// suffix used by older releases is no longer produced and 404s.
var redisKnownVersions = []AvailableVersion{
	{Version: "8.6.3", Label: "8.6.3 (Latest)", URL: "https://github.com/redis-windows/redis-windows/releases/download/8.6.3/Redis-8.6.3-Windows-x64-msys2.zip"},
	{Version: "8.4.3", Label: "8.4.3", URL: "https://github.com/redis-windows/redis-windows/releases/download/8.4.3/Redis-8.4.3-Windows-x64-msys2.zip"},
	{Version: "8.2.6", Label: "8.2.6", URL: "https://github.com/redis-windows/redis-windows/releases/download/8.2.6/Redis-8.2.6-Windows-x64-msys2.zip"},
	{Version: "7.4.9", Label: "7.4.9 (LTS)", URL: "https://github.com/redis-windows/redis-windows/releases/download/7.4.9/Redis-7.4.9-Windows-x64-msys2.zip"},
	{Version: "7.2.14", Label: "7.2.14", URL: "https://github.com/redis-windows/redis-windows/releases/download/7.2.14/Redis-7.2.14-Windows-x64-msys2.zip"},
}

type RedisManager struct{}

func NewRedisManager() *RedisManager { return &RedisManager{} }

func (r *RedisManager) Name() string        { return "redis" }
func (r *RedisManager) DisplayName() string  { return "Redis" }
func (r *RedisManager) DefaultPort() int     { return 6379 }

func (r *RedisManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(serviceBaseDir("redis"), platform.BinaryName("redis-server")))
	return err == nil
}

func (r *RedisManager) ListVersions() ([]AvailableVersion, error) {
	if goruntime.GOOS == "darwin" {
		return r.listVersionsDarwin()
	}
	versions, err := r.fetchVersions()
	if err != nil || len(versions) == 0 {
		return redisKnownVersions, nil
	}
	if len(versions) > redisMaxVersions {
		versions = versions[:redisMaxVersions]
	}
	return versions, nil
}

func (r *RedisManager) listVersionsDarwin() ([]AvailableVersion, error) {
	return []AvailableVersion{
		{Version: "7.4.2", Label: "7.4.2 (Latest)", URL: "https://download.redis.io/releases/redis-7.4.2.tar.gz"},
		{Version: "7.2.6", Label: "7.2.6 (Stable)", URL: "https://download.redis.io/releases/redis-7.2.6.tar.gz"},
	}, nil
}

func (r *RedisManager) Install(version string, port int, progress chan<- Progress) error {
	if goruntime.GOOS == "darwin" {
		return r.installDarwin(version, port, progress)
	}
	return r.installWindows(version, port, progress)
}

func (r *RedisManager) installWindows(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("redis")

	if port <= 0 {
		port = r.DefaultPort()
	}

	downloadURL, err := r.findDownloadURL(version)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("Redis-%s-Windows-x64-msys2.zip", version)
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

	// Find extracted directory (e.g., Redis-7.4.2-Windows-x64/)
	entries, _ := os.ReadDir(tmpExtract)
	extractedDir := ""
	for _, e := range entries {
		if e.IsDir() && strings.Contains(strings.ToLower(e.Name()), "redis") {
			extractedDir = filepath.Join(tmpExtract, e.Name())
			break
		}
	}
	if extractedDir == "" {
		// Files might be directly in tmpExtract
		if _, err := os.Stat(filepath.Join(tmpExtract, platform.BinaryName("redis-server"))); err == nil {
			extractedDir = tmpExtract
		} else {
			return fmt.Errorf("Redis directory not found after extraction")
		}
	}

	if _, err := os.Stat(filepath.Join(extractedDir, platform.BinaryName("redis-server"))); os.IsNotExist(err) {
		return fmt.Errorf("redis-server.exe not found in extracted files")
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Configuring..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install Redis: %w", err)
	}

	os.MkdirAll(filepath.Join(base, "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "data"), 0755)

	r.writeConfig(port)
	SaveServiceConfig("redis", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Redis %s installed (port %d)", version, port)}
	}
	return nil
}

func (r *RedisManager) installDarwin(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("redis")
	if port <= 0 {
		port = r.DefaultPort()
	}

	filename := fmt.Sprintf("redis-%s.tar.gz", version)
	downloadURL := fmt.Sprintf("https://download.redis.io/releases/%s", filename)

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

	srcDir := filepath.Join(tmpExtract, fmt.Sprintf("redis-%s", version))
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		entries, _ := os.ReadDir(tmpExtract)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "redis") {
				srcDir = filepath.Join(tmpExtract, e.Name())
				break
			}
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 60, Message: "Compiling Redis (this may take a minute)..."}
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
	for _, bin := range []string{"redis-server", "redis-cli", "redis-benchmark"} {
		src := filepath.Join(srcBin, bin)
		dst := filepath.Join(base, bin)
		if data, err := os.ReadFile(src); err == nil {
			os.WriteFile(dst, data, 0755)
		}
	}

	os.MkdirAll(filepath.Join(base, "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "data"), 0755)

	r.writeConfig(port)
	SaveServiceConfig("redis", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Redis %s installed (port %d)", version, port)}
	}
	return nil
}

func (r *RedisManager) Uninstall() error {
	if IsRunning("redis") {
		r.Stop()
	}
	return os.RemoveAll(serviceBaseDir("redis"))
}

func (r *RedisManager) Start() error {
	if !r.IsInstalled() {
		return fmt.Errorf("Redis is not installed")
	}
	if IsRunning("redis") {
		return nil
	}

	port := r.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	base := serviceBaseDir("redis")
	exe := filepath.Join(base, platform.BinaryName("redis-server"))
	confFile := filepath.Join(base, "redis.conf")
	logFile := filepath.Join(base, "logs", "redis.log")

	_, err := StartProcess("redis", exe, []string{confFile}, base, logFile)
	return err
}

func (r *RedisManager) Stop() error {
	if !IsRunning("redis") {
		return nil
	}
	return StopProcess("redis")
}

func (r *RedisManager) Restart() error {
	port := r.Port()
	r.Stop()
	WaitForPortRelease(port, 5)
	return r.Start()
}

func (r *RedisManager) Status() ServiceStatus {
	if !r.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("redis") {
		return StatusRunning
	}
	return StatusStopped
}

func (r *RedisManager) Port() int {
	cfg := LoadServiceConfig("redis")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return r.DefaultPort()
}

func (r *RedisManager) Version() string {
	cfg := LoadServiceConfig("redis")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (r *RedisManager) SetPort(port int) error {
	cfg := LoadServiceConfig("redis")
	cfg.Port = port
	if err := SaveServiceConfig("redis", cfg); err != nil {
		return err
	}
	r.writeConfig(port)
	return nil
}

func (r *RedisManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("redis"), "logs", "redis.log")
	return readLastLines(logFile, lines)
}

func (r *RedisManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        r.Name(),
		DisplayName: r.DisplayName(),
		Status:      r.Status(),
		Port:        r.Port(),
		Version:     r.Version(),
		Installed:   r.IsInstalled(),
	}
}

func (r *RedisManager) writeConfig(port int) {
	base := serviceBaseDir("redis")
	confPath := filepath.Join(base, "redis.conf")
	dataDir := strings.ReplaceAll(filepath.Join(base, "data"), "\\", "/")

	conf := fmt.Sprintf(`# DevBox Redis Configuration
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

func (r *RedisManager) findDownloadURL(version string) (string, error) {
	for _, v := range redisKnownVersions {
		if v.Version == version {
			return v.URL, nil
		}
	}
	// Try GitHub API for dynamic versions
	versions, err := r.fetchVersions()
	if err == nil {
		for _, v := range versions {
			if v.Version == version {
				return v.URL, nil
			}
		}
	}
	// Construct URL as fallback — same msys2 variant we list everywhere.
	return fmt.Sprintf("https://github.com/redis-windows/redis-windows/releases/download/%s/Redis-%s-Windows-x64-msys2.zip", version, version), nil
}

func (r *RedisManager) fetchVersions() ([]AvailableVersion, error) {
	releases, err := fetchGitHubReleases("redis-windows", "redis-windows")
	if err != nil {
		return nil, err
	}

	var versions []AvailableVersion
	for _, rel := range releases {
		tag := strings.TrimPrefix(rel.TagName, "v")
		// Pick the plain msys2 zip — exclude "-with-Service" packaging variant.
		for _, asset := range rel.Assets {
			if strings.Contains(asset.Name, "Windows-x64") &&
				strings.Contains(asset.Name, "msys2") &&
				!strings.Contains(asset.Name, "Service") &&
				strings.HasSuffix(asset.Name, ".zip") {
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
		if len(versions) >= redisMaxVersions {
			break
		}
	}
	return versions, nil
}
