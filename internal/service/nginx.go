package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	nginxDownloadPage = "https://nginx.org/en/download.html"
	nginxDownloadBase = "https://nginx.org/download/"
	nginxMinVersion   = "1.20.2"
)

type NginxManager struct{}

func NewNginxManager() *NginxManager { return &NginxManager{} }

func (n *NginxManager) Name() string       { return "nginx" }
func (n *NginxManager) DisplayName() string { return "Nginx" }
func (n *NginxManager) DefaultPort() int    { return 8080 }

func (n *NginxManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(serviceBaseDir("nginx"), "nginx.exe"))
	return err == nil
}

func (n *NginxManager) ListVersions() ([]AvailableVersion, error) {
	resp, err := http.Get(nginxDownloadPage)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch nginx versions: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	pageStr := string(body)

	// Find all zip download links
	re := regexp.MustCompile(`/download/nginx-(\d+\.\d+\.\d+)\.zip`)
	matches := re.FindAllStringSubmatch(pageStr, -1)

	seen := map[string]bool{}
	var versions []AvailableVersion

	for _, m := range matches {
		ver := m[1]
		if seen[ver] {
			continue
		}

		// Filter: minimum nginx 1.20.2
		if !versionAtLeast(ver, nginxMinVersion) {
			continue
		}

		seen[ver] = true

		label := ver
		if len(versions) == 0 {
			label = ver + " (Mainline)"
		} else if len(versions) == 1 {
			label = ver + " (Stable)"
		}

		versions = append(versions, AvailableVersion{
			Version: ver,
			Label:   label,
			URL:     nginxDownloadBase + fmt.Sprintf("nginx-%s.zip", ver),
		})
	}

	return versions, nil
}

func (n *NginxManager) Install(version string, port int, progress chan<- Progress) error {
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
	if _, err := os.Stat(filepath.Join(extractedDir, "nginx.exe")); os.IsNotExist(err) {
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
	exe := filepath.Join(base, "nginx.exe")

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
	exe := filepath.Join(base, "nginx.exe")

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
