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

const (
	apacheDownloadPage = "https://www.apachelounge.com/download/"
	apacheMaxVersions  = 3
)

var apacheKnownVersions = []AvailableVersion{
	{Version: "2.4.62", Label: "2.4.62 (Latest)", URL: "https://www.apachelounge.com/download/VS17/binaries/httpd-2.4.62-240904-win64-VS17.zip"},
}

type ApacheManager struct{}

func NewApacheManager() *ApacheManager { return &ApacheManager{} }

func (a *ApacheManager) Name() string        { return "apache" }
func (a *ApacheManager) DisplayName() string  { return "Apache" }
func (a *ApacheManager) DefaultPort() int     { return 8504 }

func (a *ApacheManager) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(serviceBaseDir("apache"), "bin", platform.BinaryName("httpd")))
	return err == nil
}

func (a *ApacheManager) ListVersions() ([]AvailableVersion, error) {
	if goruntime.GOOS != "windows" {
		return nil, fmt.Errorf("Apache is currently available on Windows only")
	}
	versions, err := a.scrapeVersions()
	if err != nil || len(versions) == 0 {
		return apacheKnownVersions, nil
	}
	if len(versions) > apacheMaxVersions {
		versions = versions[:apacheMaxVersions]
	}
	return versions, nil
}

func (a *ApacheManager) Install(version string, port int, progress chan<- Progress) error {
	if goruntime.GOOS != "windows" {
		return fmt.Errorf("Apache installation is currently supported on Windows only. On macOS, consider using Nginx or Caddy instead")
	}

	base := serviceBaseDir("apache")

	if port <= 0 {
		port = a.DefaultPort()
	}

	downloadURL, err := a.findDownloadURL(version)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("httpd-%s-win64.zip", version)
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

	// Apache Lounge zip extracts to "Apache24/" directory
	extractedDir := filepath.Join(tmpExtract, "Apache24")
	if _, err := os.Stat(extractedDir); os.IsNotExist(err) {
		entries, _ := os.ReadDir(tmpExtract)
		extractedDir = ""
		for _, e := range entries {
			if e.IsDir() && (strings.Contains(strings.ToLower(e.Name()), "apache") || strings.Contains(strings.ToLower(e.Name()), "httpd")) {
				extractedDir = filepath.Join(tmpExtract, e.Name())
				break
			}
		}
		if extractedDir == "" {
			return fmt.Errorf("Apache directory not found after extraction")
		}
	}

	if _, err := os.Stat(filepath.Join(extractedDir, "bin", platform.BinaryName("httpd"))); os.IsNotExist(err) {
		return fmt.Errorf("httpd.exe not found in extracted files")
	}

	if progress != nil {
		progress <- Progress{Percent: 85, Message: "Configuring..."}
	}

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(filepath.Dir(base), 0755)

	if err := moveDir(extractedDir, base); err != nil {
		return fmt.Errorf("failed to install Apache: %w", err)
	}

	os.MkdirAll(filepath.Join(base, "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "htdocs"), 0755)

	// Patch default config with correct ServerRoot and port
	a.patchConfig(port)

	SaveServiceConfig("apache", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("Apache %s installed (port %d)", version, port)}
	}
	return nil
}

func (a *ApacheManager) Uninstall() error {
	if IsRunning("apache") {
		a.Stop()
	}
	return os.RemoveAll(serviceBaseDir("apache"))
}

func (a *ApacheManager) Start() error {
	if !a.IsInstalled() {
		return fmt.Errorf("Apache is not installed")
	}
	if IsRunning("apache") {
		return nil
	}

	port := a.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	base := serviceBaseDir("apache")
	exe := filepath.Join(base, "bin", platform.BinaryName("httpd"))
	logFile := filepath.Join(base, "logs", "devbox-apache.log")

	_, err := StartProcess("apache", exe, []string{}, base, logFile)
	if err != nil {
		errorLog := readRecentLog(filepath.Join(base, "logs", "error.log"))
		if errorLog != "" {
			return fmt.Errorf("%w\n\nApache error.log:\n%s", err, errorLog)
		}
		return err
	}
	return nil
}

func (a *ApacheManager) Stop() error {
	if !IsRunning("apache") {
		return nil
	}
	return StopProcess("apache")
}

func (a *ApacheManager) Restart() error {
	port := a.Port()
	a.Stop()
	WaitForPortRelease(port, 5)
	return a.Start()
}

func (a *ApacheManager) Status() ServiceStatus {
	if !a.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("apache") {
		return StatusRunning
	}
	return StatusStopped
}

func (a *ApacheManager) Port() int {
	cfg := LoadServiceConfig("apache")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return a.DefaultPort()
}

func (a *ApacheManager) Version() string {
	cfg := LoadServiceConfig("apache")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (a *ApacheManager) SetPort(port int) error {
	cfg := LoadServiceConfig("apache")
	cfg.Port = port
	if err := SaveServiceConfig("apache", cfg); err != nil {
		return err
	}
	a.patchConfig(port)
	return nil
}

func (a *ApacheManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("apache"), "logs", "error.log")
	return readLastLines(logFile, lines)
}

func (a *ApacheManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        a.Name(),
		DisplayName: a.DisplayName(),
		Status:      a.Status(),
		Port:        a.Port(),
		Version:     a.Version(),
		Installed:   a.IsInstalled(),
	}
}

// patchConfig modifies the default httpd.conf to use the correct ServerRoot and port
func (a *ApacheManager) patchConfig(port int) {
	base := serviceBaseDir("apache")
	confPath := filepath.Join(base, "conf", "httpd.conf")
	baseFwd := strings.ReplaceAll(base, "\\", "/")

	data, err := os.ReadFile(confPath)
	if err != nil {
		// Write minimal config if default doesn't exist
		a.writeMinimalConfig(port)
		return
	}

	content := string(data)

	// Replace ServerRoot - Apache Lounge default is "c:/Apache24"
	content = regexp.MustCompile(`(?i)ServerRoot\s+"[^"]*"`).ReplaceAllString(content, fmt.Sprintf(`ServerRoot "%s"`, baseFwd))

	// Replace all occurrences of the default path
	content = strings.ReplaceAll(content, "c:/Apache24", baseFwd)
	content = strings.ReplaceAll(content, "C:/Apache24", baseFwd)

	// Replace Listen directive
	content = regexp.MustCompile(`(?m)^Listen\s+\d+`).ReplaceAllString(content, fmt.Sprintf("Listen %d", port))

	// Replace ServerName if present
	content = regexp.MustCompile(`(?m)^#?ServerName\s+.*`).ReplaceAllString(content, fmt.Sprintf("ServerName localhost:%d", port))

	// Modules DevBox vhosts rely on: reverse proxy for app servers, FastCGI proxy
	// for PHP (php-cgi), rewrite for framework front controllers (.htaccess).
	for _, mod := range []string{"proxy_module", "proxy_http_module", "proxy_fcgi_module", "rewrite_module", "headers_module"} {
		re := regexp.MustCompile(`(?m)^#\s*(LoadModule\s+` + mod + `\s+.*)$`)
		content = re.ReplaceAllString(content, "$1")
	}

	// Per-project vhosts written by project/vhost.go live in conf/extra/vhost-*.conf.
	if !strings.Contains(content, "conf/extra/vhost-*.conf") {
		content = strings.TrimRight(content, "\r\n") + "\n\n# DevBox project vhosts\nIncludeOptional conf/extra/vhost-*.conf\n"
	}

	os.WriteFile(confPath, []byte(content), 0644)
}

func (a *ApacheManager) writeMinimalConfig(port int) {
	base := serviceBaseDir("apache")
	confPath := filepath.Join(base, "conf", "httpd.conf")
	baseFwd := strings.ReplaceAll(base, "\\", "/")

	conf := fmt.Sprintf(`ServerRoot "%s"
Listen %d
ServerName localhost:%d

LoadModule authz_core_module modules/mod_authz_core.so
LoadModule authz_host_module modules/mod_authz_host.so
LoadModule dir_module modules/mod_dir.so
LoadModule mime_module modules/mod_mime.so
LoadModule log_config_module modules/mod_log_config.so
LoadModule proxy_module modules/mod_proxy.so
LoadModule proxy_http_module modules/mod_proxy_http.so
LoadModule proxy_fcgi_module modules/mod_proxy_fcgi.so
LoadModule rewrite_module modules/mod_rewrite.so
LoadModule headers_module modules/mod_headers.so

ErrorLog "logs/error.log"
LogLevel warn

DocumentRoot "%s/htdocs"
<Directory "%s/htdocs">
    Options Indexes FollowSymLinks
    AllowOverride All
    Require all granted
</Directory>

DirectoryIndex index.html
TypesConfig conf/mime.types

# DevBox project vhosts
IncludeOptional conf/extra/vhost-*.conf
`, baseFwd, port, port, baseFwd, baseFwd)

	os.MkdirAll(filepath.Dir(confPath), 0755)
	os.WriteFile(confPath, []byte(conf), 0644)
}

func (a *ApacheManager) findDownloadURL(version string) (string, error) {
	for _, v := range apacheKnownVersions {
		if v.Version == version {
			return v.URL, nil
		}
	}
	// Try scraping for the version
	versions, err := a.scrapeVersions()
	if err == nil {
		for _, v := range versions {
			if v.Version == version {
				return v.URL, nil
			}
		}
	}
	return "", fmt.Errorf("Apache %s download URL not found", version)
}

func (a *ApacheManager) scrapeVersions() ([]AvailableVersion, error) {
	resp, err := http.Get(apacheDownloadPage)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Find all VS17 win64 zip links
	re := regexp.MustCompile(`VS17/binaries/(httpd-(\d+\.\d+\.\d+)-[^"]+win64-VS17\.zip)`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	seen := map[string]bool{}
	var versions []AvailableVersion

	for _, m := range matches {
		filename := m[1]
		ver := m[2]
		if seen[ver] {
			continue
		}
		seen[ver] = true

		label := ver
		if len(versions) == 0 {
			label = ver + " (Latest)"
		}

		versions = append(versions, AvailableVersion{
			Version: ver,
			Label:   label,
			URL:     "https://www.apachelounge.com/download/VS17/binaries/" + filename,
		})
	}

	return versions, nil
}
