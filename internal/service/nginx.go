package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"

	"DevBox/internal/platform"
)

const (
	nginxDownloadPage = "https://nginx.org/en/download.html"
	nginxDownloadBase = "https://nginx.org/download/"
	nginxMinVersion   = "1.20.2"
)

// nginxFallbackVersions is used when scraping nginx.org returns nothing
// (network issue, page restructured, etc.) so the UI never shows an empty list.
// Keep recent + a couple of older stable lines; update opportunistically.
var nginxFallbackVersions = []string{"1.27.4", "1.26.3", "1.24.0", "1.22.1"}

type NginxManager struct{}

func NewNginxManager() *NginxManager { return &NginxManager{} }

func (n *NginxManager) Name() string       { return "nginx" }
func (n *NginxManager) DisplayName() string { return "Nginx" }
func (n *NginxManager) DefaultPort() int    { return 8503 }

func (n *NginxManager) IsInstalled() bool {
	if goruntime.GOOS == "darwin" {
		_, err := os.Stat(filepath.Join(serviceBaseDir("nginx"), "sbin", platform.BinaryName("nginx")))
		return err == nil
	}
	_, err := os.Stat(filepath.Join(serviceBaseDir("nginx"), platform.BinaryName("nginx")))
	return err == nil
}

func (n *NginxManager) ListVersions() ([]AvailableVersion, error) {
	versions := n.scrapeVersions()
	if len(versions) == 0 {
		// Scraping failed or page restructured — fall back so the UI is never empty.
		versions = n.fallbackVersions()
	}
	return versions, nil
}

// scrapeVersions parses nginx.org/en/download.html for available versions.
// Returns an empty slice on network error or no matches; caller handles fallback.
func (n *NginxManager) scrapeVersions() []AvailableVersion {
	resp, err := http.Get(nginxDownloadPage)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var re *regexp.Regexp
	if goruntime.GOOS == "windows" {
		re = regexp.MustCompile(`/download/nginx-(\d+\.\d+\.\d+)\.zip`)
	} else {
		re = regexp.MustCompile(`/download/nginx-(\d+\.\d+\.\d+)\.tar\.gz`)
	}
	matches := re.FindAllStringSubmatch(string(body), -1)

	seen := map[string]bool{}
	var versions []AvailableVersion

	for _, m := range matches {
		ver := m[1]
		if seen[ver] || !versionAtLeast(ver, nginxMinVersion) {
			continue
		}
		seen[ver] = true
		versions = append(versions, n.buildVersionEntry(ver))
	}

	return versions
}

func (n *NginxManager) fallbackVersions() []AvailableVersion {
	var versions []AvailableVersion
	for _, ver := range nginxFallbackVersions {
		versions = append(versions, n.buildVersionEntry(ver))
	}
	return versions
}

// buildVersionEntry builds the version entry with the correct mainline/stable label
// (odd minor = mainline, even minor = stable per nginx's release scheme) and download URL.
func (n *NginxManager) buildVersionEntry(ver string) AvailableVersion {
	label := ver
	if track := nginxReleaseTrack(ver); track != "" {
		label = ver + " (" + track + ")"
	}

	var url string
	if goruntime.GOOS == "windows" {
		url = nginxDownloadBase + fmt.Sprintf("nginx-%s.zip", ver)
	} else {
		url = nginxDownloadBase + fmt.Sprintf("nginx-%s.tar.gz", ver)
	}

	return AvailableVersion{Version: ver, Label: label, URL: url}
}

func nginxReleaseTrack(ver string) string {
	parts := strings.Split(ver, ".")
	if len(parts) < 2 {
		return ""
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return ""
	}
	if minor%2 == 1 {
		return "Mainline"
	}
	return "Stable"
}

func (n *NginxManager) Install(version string, port int, progress chan<- Progress) error {
	if goruntime.GOOS == "darwin" {
		return n.installDarwin(version, port, progress)
	}
	return n.installWindows(version, port, progress)
}

func (n *NginxManager) installWindows(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("nginx")

	if port <= 0 {
		port = n.DefaultPort()
	}

	filename := fmt.Sprintf("nginx-%s.zip", version)
	downloadURL := nginxDownloadBase + filename

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

	// Find the extracted nginx directory
	extractedDir := filepath.Join(tmpExtract, fmt.Sprintf("nginx-%s", version))
	if _, err := os.Stat(extractedDir); os.IsNotExist(err) {
		// Try finding any nginx directory
		entries, _ := os.ReadDir(tmpExtract)
		extractedDir = "" // reset
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "nginx") {
				extractedDir = filepath.Join(tmpExtract, e.Name())
				break
			}
		}
		if extractedDir == "" {
			return fmt.Errorf("nginx directory not found after extraction")
		}
	}

	// Verify nginx.exe exists in extracted dir
	if _, err := os.Stat(filepath.Join(extractedDir, platform.BinaryName("nginx"))); os.IsNotExist(err) {
		return fmt.Errorf("nginx.exe not found in extracted files - download may be corrupt")
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Configuring..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	// Use moveDir for reliable cross-directory moves on Windows
	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install nginx: %w", err)
	}

	os.MkdirAll(filepath.Join(base, "conf", "vhosts"), 0755)
	os.MkdirAll(filepath.Join(base, "logs"), 0755)

	// Write config with selected port
	n.writeConfig(port)

	// Save service config
	SaveServiceConfig("nginx", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Nginx %s installed (port %d)", version, port)}
	}
	return nil
}

func (n *NginxManager) installDarwin(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("nginx")

	if port <= 0 {
		port = n.DefaultPort()
	}

	filename := fmt.Sprintf("nginx-%s.tar.gz", version)
	downloadURL := nginxDownloadBase + filename

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

	srcDir := filepath.Join(tmpExtract, fmt.Sprintf("nginx-%s", version))
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		entries, _ := os.ReadDir(tmpExtract)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "nginx") {
				srcDir = filepath.Join(tmpExtract, e.Name())
				break
			}
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 60, Message: "Compiling nginx (this may take a minute)..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(base, 0755)

	// Configure
	configureCmd := exec.Command("./configure", fmt.Sprintf("--prefix=%s", base))
	configureCmd.Dir = srcDir
	if out, err := configureCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("configure failed: %s - %w", string(out), err)
	}

	// Make
	makeCmd := exec.Command("make")
	makeCmd.Dir = srcDir
	if out, err := makeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("make failed: %s - %w", string(out), err)
	}

	// Make install
	installCmd := exec.Command("make", "install")
	installCmd.Dir = srcDir
	if out, err := installCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("make install failed: %s - %w", string(out), err)
	}

	os.MkdirAll(filepath.Join(base, "conf", "vhosts"), 0755)
	os.MkdirAll(filepath.Join(base, "logs"), 0755)

	n.writeConfig(port)
	SaveServiceConfig("nginx", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Nginx %s installed (port %d)", version, port)}
	}
	return nil
}

func (n *NginxManager) Uninstall() error {
	if IsRunning("nginx") {
		n.Stop()
	}
	return os.RemoveAll(serviceBaseDir("nginx"))
}

func (n *NginxManager) Start() error {
	if !n.IsInstalled() {
		return fmt.Errorf("nginx is not installed")
	}
	if IsRunning("nginx") {
		return nil
	}

	port := n.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use - change the port in service settings or stop the conflicting service", port)
	}

	base := serviceBaseDir("nginx")
	var exe string
	if goruntime.GOOS == "darwin" {
		exe = filepath.Join(base, "sbin", platform.BinaryName("nginx"))
	} else {
		exe = filepath.Join(base, platform.BinaryName("nginx"))
	}

	// Test config before starting
	testOut, testErr := runCommand(exe, []string{"-t"}, base)
	if testErr != nil {
		return fmt.Errorf("nginx config test failed:\n%s", testOut)
	}

	// Clear old error log before start
	errorLog := filepath.Join(base, "logs", "error.log")
	os.Truncate(errorLog, 0)

	// Start nginx
	logFile := filepath.Join(base, "logs", "devbox-nginx.log")
	_, err := StartProcess("nginx", exe, []string{}, base, logFile)
	if err != nil {
		// Also check nginx's own error log for more details
		nginxErrors := readRecentLog(errorLog)
		if nginxErrors != "" {
			return fmt.Errorf("%w\n\nnginx error.log:\n%s", err, nginxErrors)
		}
		return err
	}
	return nil
}

func (n *NginxManager) Stop() error {
	if !IsRunning("nginx") {
		return nil
	}

	base := serviceBaseDir("nginx")
	var exe string
	if goruntime.GOOS == "darwin" {
		exe = filepath.Join(base, "sbin", platform.BinaryName("nginx"))
	} else {
		exe = filepath.Join(base, platform.BinaryName("nginx"))
	}

	runCommandSilent(exe, []string{"-s", "stop"}, base)
	return StopProcess("nginx")
}

func (n *NginxManager) Restart() error {
	port := n.Port()
	n.Stop()
	WaitForPortRelease(port, 5)
	return n.Start()
}

func (n *NginxManager) Status() ServiceStatus {
	if !n.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("nginx") {
		return StatusRunning
	}
	return StatusStopped
}

func (n *NginxManager) Port() int {
	cfg := LoadServiceConfig("nginx")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return n.DefaultPort()
}

func (n *NginxManager) Version() string {
	cfg := LoadServiceConfig("nginx")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (n *NginxManager) SetPort(port int) error {
	cfg := LoadServiceConfig("nginx")
	cfg.Port = port
	if err := SaveServiceConfig("nginx", cfg); err != nil {
		return err
	}
	n.writeConfig(port)
	return nil
}

func (n *NginxManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("nginx"), "logs", "error.log")
	return readLastLines(logFile, lines)
}

func (n *NginxManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        n.Name(),
		DisplayName: n.DisplayName(),
		Status:      n.Status(),
		Port:        n.Port(),
		Version:     n.Version(),
		Installed:   n.IsInstalled(),
	}
}

func (n *NginxManager) writeConfig(port int) {
	confPath := filepath.Join(serviceBaseDir("nginx"), "conf", "nginx.conf")

	conf := fmt.Sprintf(`worker_processes  1;

events {
    worker_connections  1024;
}

http {
    include       mime.types;
    default_type  application/octet-stream;
    sendfile      on;
    keepalive_timeout  65;

    server {
        listen       %d;
        server_name  localhost;

        location / {
            root   html;
            index  index.html index.htm;
        }
    }

    # DevBox virtual hosts
    include vhosts/*.conf;
}
`, port)
	os.WriteFile(confPath, []byte(conf), 0644)
}
