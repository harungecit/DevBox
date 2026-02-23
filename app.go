package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/i18n"
	"DevBox/internal/pathenv"
	"DevBox/internal/project"
	"DevBox/internal/runtime"
	"DevBox/internal/service"
	"DevBox/internal/tools"
	"DevBox/internal/tunnel"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func debugLog(msg string, args ...interface{}) {
	logDir := filepath.Join(config.GetDataDir(), "logs")
	os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(filepath.Join(logDir, "debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(f, "[%s] %s\n", ts, fmt.Sprintf(msg, args...))
}

// App struct holds the application state
type App struct {
	ctx context.Context
	mu  sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	debugLog("App shutting down, stopping all tunnels, dev servers, and services...")
	tunnel.StopAllTunnels()
	project.StopAllDevServers()
	service.StopAll()
	debugLog("All tunnels, dev servers, and services stopped")
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Load config
	cfg, err := config.Load()
	if err != nil {
		println("Config load error:", err.Error())
	}

	// Ensure all data directories exist
	if err := config.EnsureDirectories(); err != nil {
		println("Directory setup error:", err.Error())
	}

	// Set language from config
	i18n.SetLanguage(cfg.Language)

	// Initialize runtime and service managers
	runtime.InitAll()
	service.InitAll()

	// Regenerate all project vhosts on startup (ensures PHP block etc. are current)
	a.regenerateAllVhosts()

	// Auto-start PHP-CGI if any PHP project has a domain configured
	a.autoStartPHPCGI()

	// Auto-start configured services
	go a.autoStartServices()
}

func (a *App) autoStartPHPCGI() {
	if runtime.IsPHPCGIRunning() {
		return
	}
	projects, err := project.ListProjects()
	if err != nil {
		return
	}
	needsPHP := false
	for _, p := range projects {
		if p.Domain != "" && isPhpFramework(p.Framework) {
			needsPHP = true
			break
		}
	}
	if !needsPHP {
		return
	}
	phpMgr, ok := runtime.Registry["php"]
	if !ok {
		return
	}
	ver, _ := phpMgr.GetGlobal()
	if ver == "" {
		return
	}
	debugLog("Auto-starting PHP-CGI (version %s) for PHP projects", ver)
	if err := runtime.StartPHPCGI(ver, 9000); err != nil {
		debugLog("Auto-start PHP-CGI failed: %v", err)
	}
}

func (a *App) autoStartServices() {
	cfg := config.Get()
	for _, name := range cfg.AutoStartSvcs {
		mgr, ok := service.Registry[name]
		if !ok || !mgr.IsInstalled() {
			continue
		}
		if mgr.Status() == service.StatusRunning {
			continue
		}
		debugLog("Auto-starting service: %s", name)
		if err := mgr.Start(); err != nil {
			debugLog("Auto-start failed for %s: %v", name, err)
		}
	}
}

// --- Config bindings ---

func (a *App) GetConfig() *config.Config {
	return config.Get()
}

func (a *App) SetLanguage(lang string) error {
	i18n.SetLanguage(lang)
	return config.SetLanguage(lang)
}

func (a *App) SetTheme(theme string) error {
	return config.SetTheme(theme)
}

// --- i18n bindings ---

func (a *App) GetLocale(lang string) map[string]string {
	return i18n.GetLocale(lang)
}

func (a *App) GetAvailableLanguages() []string {
	return i18n.AvailableLanguages()
}

// --- Runtime bindings ---

// RuntimeVersionInfo is the struct exposed to frontend
type RuntimeVersionInfo struct {
	Number    string `json:"number"`
	Stable    bool   `json:"stable"`
	Current   bool   `json:"current"`
	Installed bool   `json:"installed"`
}

// GetRemoteVersions fetches available versions for a runtime
func (a *App) GetRemoteVersions(name string) ([]RuntimeVersionInfo, error) {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return nil, nil
	}

	remote, err := mgr.ListRemote()
	if err != nil {
		return nil, err
	}

	// Get installed versions to mark them
	installed, _ := mgr.ListInstalled()
	installedMap := map[string]bool{}
	for _, v := range installed {
		installedMap[v.Number] = true
	}

	var result []RuntimeVersionInfo
	for _, v := range remote {
		result = append(result, RuntimeVersionInfo{
			Number:    v.Number,
			Stable:    v.Stable,
			Current:   v.Current,
			Installed: installedMap[v.Number],
		})
	}

	return result, nil
}

// GetInstalledVersions returns installed versions for a runtime
func (a *App) GetInstalledVersions(name string) ([]RuntimeVersionInfo, error) {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return nil, nil
	}

	versions, err := mgr.ListInstalled()
	if err != nil {
		return nil, err
	}

	var result []RuntimeVersionInfo
	for _, v := range versions {
		result = append(result, RuntimeVersionInfo{
			Number:    v.Number,
			Stable:    v.Stable,
			Current:   v.Current,
			Installed: true,
		})
	}

	return result, nil
}

// InstallRuntime installs a runtime version with progress events.
// Returns immediately - progress/completion/errors via events.
func (a *App) InstallRuntime(name, version string) error {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return fmt.Errorf("unknown runtime: %s", name)
	}

	go func() {
		progress := make(chan runtime.Progress, 10)

		go func() {
			defer close(progress)
			err := mgr.Install(version, progress)
			if err != nil {
				wailsRuntime.EventsEmit(a.ctx, "runtime:error", map[string]interface{}{
					"name":    name,
					"version": version,
					"error":   err.Error(),
				})
				return
			}

			// Auto-set as global if it's the first install
			global, _ := mgr.GetGlobal()
			if global == "" {
				mgr.SetGlobal(version)
				pathenv.AddToPath(mgr.BinaryPath(version))
			}

			wailsRuntime.EventsEmit(a.ctx, "runtime:installed", map[string]interface{}{
				"name":    name,
				"version": version,
			})
		}()

		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "runtime:progress", map[string]interface{}{
				"name":    name,
				"version": version,
				"percent": p.Percent,
				"message": p.Message,
			})
		}
	}()

	return nil
}

// UninstallRuntime removes a runtime version
func (a *App) UninstallRuntime(name, version string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	mgr, ok := runtime.Registry[name]
	if !ok {
		return nil
	}

	// Remove from PATH if this is the global version
	global, _ := mgr.GetGlobal()
	if global == version {
		pathenv.RemoveFromPath(mgr.BinaryPath(version))
	}

	return mgr.Uninstall(version)
}

// SetGlobalRuntime sets a version as the global active version
func (a *App) SetGlobalRuntime(name, version string) error {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return nil
	}

	// Switch PATH: remove old, add new
	oldGlobal, _ := mgr.GetGlobal()
	if oldGlobal != "" {
		pathenv.RemoveFromPath(mgr.BinaryPath(oldGlobal))
	}

	if err := mgr.SetGlobal(version); err != nil {
		return err
	}

	return pathenv.AddToPath(mgr.BinaryPath(version))
}

// GetGlobalRuntime returns the current global version for a runtime
func (a *App) GetGlobalRuntime(name string) string {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return ""
	}
	v, _ := mgr.GetGlobal()
	return v
}

// --- Legacy bindings for Dashboard ---

func (a *App) GetInstalledRuntimes() map[string][]string {
	result := map[string][]string{
		"go":     {},
		"node":   {},
		"php":    {},
		"python": {},
		"rust":   {},
	}
	for name, mgr := range runtime.Registry {
		versions, err := mgr.ListInstalled()
		if err == nil {
			for _, v := range versions {
				result[name] = append(result[name], v.Number)
			}
		}
	}
	return result
}

func (a *App) GetServiceStatus() map[string]string {
	result := make(map[string]string)
	for name, mgr := range service.Registry {
		result[name] = string(mgr.Status())
	}
	// Add placeholders for unimplemented services
	for _, name := range []string{"apache", "mariadb", "redis", "valkey", "mailpit"} {
		if _, ok := result[name]; !ok {
			result[name] = "not_installed"
		}
	}
	return result
}

// --- Service bindings ---

// GetAllServices returns info for all registered services
func (a *App) GetAllServices() map[string]service.ServiceInfo {
	result := service.GetAll()
	for name, info := range result {
		debugLog("GetAllServices: %s installed=%v status=%s port=%d version=%s", name, info.Installed, info.Status, info.Port, info.Version)
	}
	return result
}

// GetServiceVersions returns available versions for a service
func (a *App) GetServiceVersions(name string) ([]service.AvailableVersion, error) {
	mgr, ok := service.Registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown service: %s", name)
	}
	return mgr.ListVersions()
}

// CheckPort checks if a port is available
func (a *App) CheckPort(port int) service.PortStatus {
	return service.CheckPort(port)
}

// SetServicePort changes the port for an installed service
func (a *App) SetServicePort(name string, port int) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	if err := mgr.SetPort(port); err != nil {
		return err
	}

	// If it's a web server, regenerate all project vhosts with the new port
	if name == "nginx" || name == "apache" || name == "caddy" {
		a.regenerateAllVhosts()
	}
	return nil
}

// isPhpFramework returns true if the framework requires PHP-CGI
func isPhpFramework(fw string) bool {
	return !project.IsAppServer(fw) && fw != "Static" && fw != ""
}

// regenerateAllVhosts regenerates vhost configs for all projects using the current web server port
func (a *App) regenerateAllVhosts() {
	projects, err := project.ListProjects()
	if err != nil || len(projects) == 0 {
		return
	}

	// Always include PHP FastCGI block so PHP projects work
	// when PHP-CGI starts (harmless for non-PHP projects).
	phpCgiPort := 9000

	if mgr, ok := service.Registry["nginx"]; ok && mgr.IsInstalled() {
		for _, p := range projects {
			project.GenerateNginxVhost(p, phpCgiPort, mgr.Port())
		}
		if mgr.Status() == service.StatusRunning {
			mgr.Restart()
		}
	} else if mgr, ok := service.Registry["apache"]; ok && mgr.IsInstalled() {
		for _, p := range projects {
			project.GenerateApacheVhost(p, mgr.Port())
		}
		if mgr.Status() == service.StatusRunning {
			mgr.Restart()
		}
	} else if mgr, ok := service.Registry["caddy"]; ok && mgr.IsInstalled() {
		for _, p := range projects {
			project.GenerateCaddyVhost(p, phpCgiPort)
		}
		if mgr.Status() == service.StatusRunning {
			mgr.Restart()
		}
	}
}

// InstallService installs a service with version and port selection
// InstallService starts installation in background and returns immediately.
// Progress, completion and errors are communicated via events:
//   - service:progress  {name, percent, message}
//   - service:installed {name}
//   - service:error     {name, error}
func (a *App) InstallService(name string, version string, port int) error {
	debugLog("InstallService called: name=%s version=%s port=%d", name, version, port)

	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}

	// Check for conflicting services
	if conflict := service.GetConflictingService(name); conflict != "" {
		return fmt.Errorf("%s is already installed. Uninstall it before installing %s", conflict, mgr.DisplayName())
	}

	// Run install entirely in background - return immediately to Wails
	go func() {
		progress := make(chan service.Progress, 10)

		// Inner goroutine does the actual install
		go func() {
			defer close(progress)
			defer func() {
				if r := recover(); r != nil {
					debugLog("InstallService PANIC: %v", r)
				}
			}()
			err := mgr.Install(version, port, progress)
			if err != nil {
				debugLog("Install returned error: %v", err)
				wailsRuntime.EventsEmit(a.ctx, "service:error", map[string]interface{}{
					"name":  name,
					"error": err.Error(),
				})
				return
			}
			debugLog("Install returned OK, installed=%v", mgr.Info().Installed)
			wailsRuntime.EventsEmit(a.ctx, "service:installed", map[string]interface{}{
				"name": name,
			})
		}()

		// Forward progress events to frontend
		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "service:progress", map[string]interface{}{
				"name":    name,
				"percent": p.Percent,
				"message": p.Message,
			})
		}
	}()

	return nil
}

// UninstallService removes a service
func (a *App) UninstallService(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	return mgr.Uninstall()
}

// StartService starts a service
func (a *App) StartService(name string) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	return mgr.Start()
}

// StopService stops a service
func (a *App) StopService(name string) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	return mgr.Stop()
}

// RestartService restarts a service
func (a *App) RestartService(name string) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	return mgr.Restart()
}

// GetServiceLogs returns the last N lines of logs for a service
func (a *App) GetServiceLogs(name string, lines int) ([]string, error) {
	mgr, ok := service.Registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown service: %s", name)
	}
	return mgr.Logs(lines)
}

// --- Auto-start bindings ---

// GetAutoStartServices returns the list of services that auto-start with the app
func (a *App) GetAutoStartServices() []string {
	cfg := config.Get()
	return cfg.AutoStartSvcs
}

// SetServiceAutoStart enables or disables auto-start for a service
func (a *App) SetServiceAutoStart(name string, enabled bool) error {
	cfg := config.Get()

	// Remove existing entry
	var filtered []string
	for _, s := range cfg.AutoStartSvcs {
		if s != name {
			filtered = append(filtered, s)
		}
	}

	if enabled {
		filtered = append(filtered, name)
	}

	cfg.AutoStartSvcs = filtered
	return config.Save()
}

// SetAutoStart enables or disables DevBox launching at Windows login
func (a *App) SetAutoStart(enabled bool) error {
	if err := setWindowsAutoStart(enabled); err != nil {
		return err
	}
	cfg := config.Get()
	cfg.AutoStart = enabled
	return config.Save()
}

// --- PATH bindings ---

// GetPATHEntries returns user PATH entries
func (a *App) GetPATHEntries() ([]string, error) {
	return pathenv.GetUserPATH()
}

// AddToPATH adds a custom entry to PATH
func (a *App) AddToPATH(dir string) error {
	return pathenv.AddToPath(dir)
}

// RemoveFromPATH removes an entry from PATH
func (a *App) RemoveFromPATH(dir string) error {
	return pathenv.RemoveFromPath(dir)
}

// --- Service Detail Info ---

// ConfigFileEntry represents a config file that can be edited
type ConfigFileEntry struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// ConnectionEntry represents a connection detail (key-value)
type ConnectionEntry struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// WebLinkEntry represents a web link to open in browser
type WebLinkEntry struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// ServiceDetailInfo holds detailed information about an installed service
type ServiceDetailInfo struct {
	ConfigFiles    []ConfigFileEntry `json:"configFiles"`
	ConnectionInfo []ConnectionEntry `json:"connectionInfo"`
	WebLinks       []WebLinkEntry    `json:"webLinks"`
}

// GetServiceDetails returns detailed connection info, config files, and web links for a service
func (a *App) GetServiceDetails(name string) ServiceDetailInfo {
	mgr, ok := service.Registry[name]
	if !ok || !mgr.IsInstalled() {
		return ServiceDetailInfo{}
	}

	port := mgr.Port()
	baseDir := filepath.Join(config.GetDataDir(), "services", name)

	switch name {
	case "nginx":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "Document Root", Value: filepath.Join(baseDir, "html")},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "nginx.conf", Path: filepath.Join(baseDir, "conf", "nginx.conf")},
			},
			WebLinks: []WebLinkEntry{
				{Label: "Open in Browser", URL: fmt.Sprintf("http://127.0.0.1:%d", port)},
			},
		}

	case "apache":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "Document Root", Value: filepath.Join(baseDir, "htdocs")},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "httpd.conf", Path: filepath.Join(baseDir, "conf", "httpd.conf")},
			},
			WebLinks: []WebLinkEntry{
				{Label: "Open in Browser", URL: fmt.Sprintf("http://127.0.0.1:%d", port)},
			},
		}

	case "caddy":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "Document Root", Value: filepath.Join(baseDir, "html")},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "Caddyfile", Path: filepath.Join(baseDir, "Caddyfile")},
			},
			WebLinks: []WebLinkEntry{
				{Label: "Open in Browser", URL: fmt.Sprintf("http://127.0.0.1:%d", port)},
			},
		}

	case "postgres":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "User", Value: "postgres"},
				{Label: "Password", Value: "(none)"},
				{Label: "URI", Value: fmt.Sprintf("postgresql://postgres@127.0.0.1:%d/postgres", port)},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "postgresql.conf", Path: filepath.Join(baseDir, "data", "postgresql.conf")},
				{Label: "pg_hba.conf", Path: filepath.Join(baseDir, "data", "pg_hba.conf")},
			},
		}

	case "mysql":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "User", Value: "root"},
				{Label: "Password", Value: "(none)"},
				{Label: "CLI", Value: fmt.Sprintf("mysql -u root -h 127.0.0.1 -P %d", port)},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "my.ini", Path: filepath.Join(baseDir, "my.ini")},
			},
		}

	case "mariadb":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "User", Value: "root"},
				{Label: "Password", Value: "(none)"},
				{Label: "CLI", Value: fmt.Sprintf("mysql -u root -h 127.0.0.1 -P %d", port)},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "my.ini", Path: filepath.Join(baseDir, "my.ini")},
			},
		}

	case "mongodb":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "URI", Value: fmt.Sprintf("mongodb://127.0.0.1:%d", port)},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "mongod.cfg", Path: filepath.Join(baseDir, "mongod.cfg")},
			},
		}

	case "redis":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "CLI", Value: fmt.Sprintf("redis-cli -h 127.0.0.1 -p %d", port)},
				{Label: "URI", Value: fmt.Sprintf("redis://127.0.0.1:%d/0", port)},
				{Label: "Databases", Value: "16 (0-15)"},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "redis.conf", Path: filepath.Join(baseDir, "redis.conf")},
			},
		}

	case "valkey":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port)},
				{Label: "CLI", Value: fmt.Sprintf("valkey-cli -h 127.0.0.1 -p %d", port)},
				{Label: "URI", Value: fmt.Sprintf("redis://127.0.0.1:%d/0", port)},
				{Label: "Databases", Value: "16 (0-15)"},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "valkey.conf", Path: filepath.Join(baseDir, "valkey.conf")},
			},
		}

	case "mailpit":
		smtpPort := port
		uiPort := port + 7000
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "SMTP Host", Value: "127.0.0.1"},
				{Label: "SMTP Port", Value: fmt.Sprintf("%d", smtpPort)},
				{Label: "Web UI Port", Value: fmt.Sprintf("%d", uiPort)},
			},
			WebLinks: []WebLinkEntry{
				{Label: "Mailpit Web UI", URL: fmt.Sprintf("http://127.0.0.1:%d", uiPort)},
			},
		}

	default:
		return ServiceDetailInfo{}
	}
}

// --- Bun Package Manager ---

// IsBunInstalled checks if Bun is installed
func (a *App) IsBunInstalled() bool {
	toolDir := filepath.Join(config.GetDataDir(), "tools", "bun")
	_, err := os.Stat(filepath.Join(toolDir, "bun.exe"))
	return err == nil
}

// InstallBun downloads and installs Bun
func (a *App) InstallBun() error {
	go func() {
		toolDir := filepath.Join(config.GetDataDir(), "tools", "bun")
		os.MkdirAll(toolDir, 0755)

		// Fetch latest release URL from GitHub
		downloadURL := ""
		resp, err := runtime.FetchGitHubReleasesPublic("oven-sh", "bun")
		if err == nil && len(resp) > 0 {
			for _, asset := range resp[0].Assets {
				if asset.Name == "bun-windows-x64.zip" {
					downloadURL = asset.BrowserDownloadURL
					break
				}
			}
		}
		if downloadURL == "" {
			downloadURL = "https://github.com/oven-sh/bun/releases/latest/download/bun-windows-x64.zip"
		}

		tmpFile := filepath.Join(config.GetDataDir(), "tmp", "bun-windows-x64.zip")
		os.MkdirAll(filepath.Dir(tmpFile), 0755)

		if err := runtime.DownloadFile(downloadURL, tmpFile, 0, nil); err != nil {
			wailsRuntime.EventsEmit(a.ctx, "bun:error", map[string]interface{}{"error": err.Error()})
			return
		}
		defer os.Remove(tmpFile)

		// Extract
		tmpExtract := tmpFile + "-extract"
		os.RemoveAll(tmpExtract)
		defer os.RemoveAll(tmpExtract)

		if err := runtime.ExtractZip(tmpFile, tmpExtract, nil); err != nil {
			wailsRuntime.EventsEmit(a.ctx, "bun:error", map[string]interface{}{"error": err.Error()})
			return
		}

		// Find bun.exe (may be in subdirectory bun-windows-x64/)
		bunExe := filepath.Join(tmpExtract, "bun.exe")
		if _, err := os.Stat(bunExe); os.IsNotExist(err) {
			entries, _ := os.ReadDir(tmpExtract)
			for _, e := range entries {
				if e.IsDir() {
					candidate := filepath.Join(tmpExtract, e.Name(), "bun.exe")
					if _, err := os.Stat(candidate); err == nil {
						bunExe = candidate
						break
					}
				}
			}
		}

		if _, err := os.Stat(bunExe); os.IsNotExist(err) {
			wailsRuntime.EventsEmit(a.ctx, "bun:error", map[string]interface{}{"error": "bun.exe not found in archive"})
			return
		}

		// Copy bun.exe to tools dir
		data, err := os.ReadFile(bunExe)
		if err != nil {
			wailsRuntime.EventsEmit(a.ctx, "bun:error", map[string]interface{}{"error": err.Error()})
			return
		}
		if err := os.WriteFile(filepath.Join(toolDir, "bun.exe"), data, 0755); err != nil {
			wailsRuntime.EventsEmit(a.ctx, "bun:error", map[string]interface{}{"error": err.Error()})
			return
		}

		// Add to PATH
		pathenv.AddToPath(toolDir)

		wailsRuntime.EventsEmit(a.ctx, "bun:installed", map[string]interface{}{})
	}()
	return nil
}

// GetBunVersion returns installed Bun version
func (a *App) GetBunVersion() string {
	toolDir := filepath.Join(config.GetDataDir(), "tools", "bun")
	bunExe := filepath.Join(toolDir, "bun.exe")
	if _, err := os.Stat(bunExe); os.IsNotExist(err) {
		return ""
	}

	cmd := exec.Command(bunExe, "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// UninstallBun removes Bun
func (a *App) UninstallBun() error {
	toolDir := filepath.Join(config.GetDataDir(), "tools", "bun")
	pathenv.RemoveFromPath(toolDir)
	return os.RemoveAll(toolDir)
}

// --- PHP Extensions & Composer & PHP-CGI ---

// GetPHPExtensions returns extensions for a PHP version
func (a *App) GetPHPExtensions(version string) ([]runtime.PHPExtension, error) {
	return runtime.GetPHPExtensions(version)
}

// TogglePHPExtension toggles a PHP extension
func (a *App) TogglePHPExtension(version, extName string, enable bool) error {
	return runtime.TogglePHPExtension(version, extName, enable)
}

// IsComposerInstalled checks if Composer is installed
func (a *App) IsComposerInstalled() bool {
	return runtime.IsComposerInstalled()
}

// InstallComposer downloads and installs Composer
func (a *App) InstallComposer() error {
	go func() {
		progress := make(chan runtime.Progress, 10)
		go func() {
			defer close(progress)
			err := runtime.InstallComposer(progress)
			if err != nil {
				wailsRuntime.EventsEmit(a.ctx, "composer:error", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}
			wailsRuntime.EventsEmit(a.ctx, "composer:installed", map[string]interface{}{})
		}()
		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "composer:progress", map[string]interface{}{
				"percent": p.Percent,
				"message": p.Message,
			})
		}
	}()
	return nil
}

// GetComposerVersion returns composer version
func (a *App) GetComposerVersion() string {
	return runtime.GetComposerVersion()
}

// StartPHPCGI starts php-cgi in FastCGI mode
func (a *App) StartPHPCGI(version string, port int) error {
	return runtime.StartPHPCGI(version, port)
}

// StopPHPCGI stops the running php-cgi
func (a *App) StopPHPCGI() error {
	return runtime.StopPHPCGI()
}

// IsPHPCGIRunning checks if php-cgi is running
func (a *App) IsPHPCGIRunning() bool {
	return runtime.IsPHPCGIRunning()
}

// --- Project Management ---

// ListProjects returns all registered projects
func (a *App) ListProjects() ([]project.Project, error) {
	return project.ListProjects()
}

// AddProject adds a project from a selected folder
func (a *App) AddProject(projectPath, domain string) (*project.Project, error) {
	return project.AddProject(projectPath, domain)
}

// RemoveProject removes a project
func (a *App) RemoveProject(name string) error {
	// Remove vhost
	project.RemoveNginxVhost(name)
	// Remove hosts entry
	projects, _ := project.ListProjects()
	for _, p := range projects {
		if p.Name == name {
			project.RemoveHostsEntry(p.Domain)
			break
		}
	}
	return project.RemoveProject(name)
}

// DetectFramework detects framework for a path
func (a *App) DetectFramework(path string) string {
	return project.DetectFramework(path)
}

// SetProjectPort updates the dev server port for a project
func (a *App) SetProjectPort(name string, port int) error {
	projects, err := project.ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].Port = port
			return project.SaveProjects(projects)
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// SetupProjectDomain sets up hosts file and vhost for a project
func (a *App) SetupProjectDomain(name string) error {
	projects, err := project.ListProjects()
	if err != nil {
		return err
	}
	for _, p := range projects {
		if p.Name == name {
			// Add to hosts file
			if err := project.AddHostsEntry(p.Domain); err != nil {
				return err
			}

			// Always include PHP FastCGI block so PHP projects work
			// when PHP-CGI starts (harmless for non-PHP projects).
			phpCgiPort := 9000

			// Auto-start PHP-CGI for PHP-based projects
			if isPhpFramework(p.Framework) && !runtime.IsPHPCGIRunning() {
				if phpMgr, ok := runtime.Registry["php"]; ok {
					if ver, _ := phpMgr.GetGlobal(); ver != "" {
						runtime.StartPHPCGI(ver, phpCgiPort)
					}
				}
			}

			// Generate vhost based on installed web server
			if mgr, ok := service.Registry["nginx"]; ok && mgr.IsInstalled() {
				if err := project.GenerateNginxVhost(p, phpCgiPort, mgr.Port()); err != nil {
					return err
				}
				// Reload nginx to pick up the new vhost
				if mgr.Status() == service.StatusRunning {
					mgr.Restart()
				}
				return nil
			}
			if mgr, ok := service.Registry["apache"]; ok && mgr.IsInstalled() {
				if err := project.GenerateApacheVhost(p, mgr.Port()); err != nil {
					return err
				}
				if mgr.Status() == service.StatusRunning {
					mgr.Restart()
				}
				return nil
			}
			if mgr, ok := service.Registry["caddy"]; ok && mgr.IsInstalled() {
				if err := project.GenerateCaddyVhost(p, phpCgiPort); err != nil {
					return err
				}
				if mgr.Status() == service.StatusRunning {
					mgr.Restart()
				}
				return nil
			}

			return fmt.Errorf("no web server installed")
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// SetupProjectSSL generates SSL certificates for a project
func (a *App) SetupProjectSSL(name string) error {
	return a.ToggleProjectSSL(name, true)
}

// ToggleProjectSSL enables or disables SSL for a project and regenerates vhost
func (a *App) ToggleProjectSSL(name string, enable bool) error {
	projects, err := project.ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			if enable && !p.SSL {
				// Generate SSL certificates if enabling
				if err := project.SetupProjectSSL(p.Domain); err != nil {
					return err
				}
			}
			projects[i].SSL = enable
			project.SaveProjects(projects)

			phpCgiPort := 9000

			// Regenerate vhost with/without SSL
			if mgr, ok := service.Registry["nginx"]; ok && mgr.IsInstalled() {
				if err := project.GenerateNginxVhost(projects[i], phpCgiPort, mgr.Port()); err != nil {
					return err
				}
				if mgr.Status() == service.StatusRunning {
					mgr.Restart()
				}
			} else if mgr, ok := service.Registry["apache"]; ok && mgr.IsInstalled() {
				if err := project.GenerateApacheVhost(projects[i], mgr.Port()); err != nil {
					return err
				}
				if mgr.Status() == service.StatusRunning {
					mgr.Restart()
				}
			}
			return nil
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// GetProjectVhostPath returns the vhost config file path for a project
func (a *App) GetProjectVhostPath(name string) string {
	for _, wsName := range []string{"nginx", "apache", "caddy"} {
		mgr, ok := service.Registry[wsName]
		if !ok || !mgr.IsInstalled() {
			continue
		}
		base := filepath.Join(config.GetDataDir(), "services", wsName)
		switch wsName {
		case "nginx":
			return filepath.Join(base, "conf", "vhosts", name+".conf")
		case "apache":
			return filepath.Join(base, "conf", "extra", "vhost-"+name+".conf")
		case "caddy":
			return filepath.Join(base, "Caddyfile")
		}
	}
	return ""
}

// SelectProjectFolder opens a folder selection dialog
func (a *App) SelectProjectFolder() (string, error) {
	return wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select Project Folder",
	})
}

// SelectParentFolder opens a folder selection dialog for choosing a parent directory
func (a *App) SelectParentFolder() (string, error) {
	return wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select Parent Folder",
	})
}

// GetAvailableTemplates returns framework templates with availability info
func (a *App) GetAvailableTemplates() []project.FrameworkTemplate {
	return project.GetTemplates()
}

// ScaffoldNewProject creates a new project from a template
func (a *App) ScaffoldNewProject(templateID, parentDir, name, domain string) error {
	go func() {
		progress := make(chan project.ScaffoldProgress, 10)

		go func() {
			defer close(progress)
			err := project.ScaffoldProject(templateID, parentDir, name, progress)
			if err != nil {
				wailsRuntime.EventsEmit(a.ctx, "scaffold:error", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}

			// Register the project
			projectPath := filepath.Join(parentDir, name)
			if _, err := project.AddProject(projectPath, domain); err != nil {
				wailsRuntime.EventsEmit(a.ctx, "scaffold:error", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}

			wailsRuntime.EventsEmit(a.ctx, "scaffold:complete", map[string]interface{}{
				"name": name,
				"path": projectPath,
			})
		}()

		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "scaffold:progress", map[string]interface{}{
				"percent": p.Percent,
				"message": p.Message,
			})
		}
	}()

	return nil
}

// CloneGitProject clones a git repo and registers it as a project
func (a *App) CloneGitProject(gitURL, parentDir, name, domain string) error {
	go func() {
		progress := make(chan project.ScaffoldProgress, 10)

		go func() {
			defer close(progress)
			err := project.CloneProject(gitURL, parentDir, name, progress)
			if err != nil {
				wailsRuntime.EventsEmit(a.ctx, "clone:error", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}

			// Derive actual project name
			actualName := name
			if actualName == "" {
				parts := strings.Split(strings.TrimSuffix(gitURL, "/"), "/")
				actualName = strings.TrimSuffix(parts[len(parts)-1], ".git")
			}

			projectPath := filepath.Join(parentDir, actualName)
			if _, err := project.AddProject(projectPath, domain); err != nil {
				wailsRuntime.EventsEmit(a.ctx, "clone:error", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}

			wailsRuntime.EventsEmit(a.ctx, "clone:complete", map[string]interface{}{
				"name": actualName,
				"path": projectPath,
			})
		}()

		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "clone:progress", map[string]interface{}{
				"percent": p.Percent,
				"message": p.Message,
			})
		}
	}()

	return nil
}

// IsGitInstalled checks if git is available on the system
func (a *App) IsGitInstalled() bool {
	return project.IsGitInstalled()
}

// --- Dev Server bindings ---

// StartDevServer starts the dev server for a project
func (a *App) StartDevServer(name string) error {
	projects, err := project.ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			if !project.IsAppServer(p.Framework) {
				return fmt.Errorf("%s is not an app-server project", name)
			}

			actualPort, err := project.StartDevServer(p)
			if err != nil {
				wailsRuntime.EventsEmit(a.ctx, "devserver:error", map[string]interface{}{
					"name":  name,
					"error": err.Error(),
				})
				return err
			}

			// Update port in projects.json if it changed
			if actualPort != p.Port {
				projects[i].Port = actualPort
				project.SaveProjects(projects)
			}

			// Regenerate vhosts so reverse proxy points to the actual port
			a.regenerateAllVhosts()

			wailsRuntime.EventsEmit(a.ctx, "devserver:started", map[string]interface{}{
				"name": name,
				"port": actualPort,
			})
			return nil
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// StopDevServer stops the dev server for a project
func (a *App) StopDevServer(name string) error {
	err := project.StopDevServer(name)
	if err != nil {
		return err
	}
	wailsRuntime.EventsEmit(a.ctx, "devserver:stopped", map[string]interface{}{
		"name": name,
	})
	return nil
}

// IsDevServerRunning checks if a dev server is running for a project
func (a *App) IsDevServerRunning(name string) bool {
	return project.IsDevServerRunning(name)
}

// GetRunningDevServers returns all running dev servers as a map of project name to port
func (a *App) GetRunningDevServers() map[string]int {
	return project.GetRunningDevServers()
}

// GetDevServerLogs returns the last N lines of dev server logs
func (a *App) GetDevServerLogs(name string, lines int) ([]string, error) {
	return project.GetDevServerLogs(name, lines)
}

// SetProjectStartCommand sets a custom start command for a project
func (a *App) SetProjectStartCommand(name string, cmd string) error {
	projects, err := project.ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].StartCommand = cmd
			return project.SaveProjects(projects)
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// GetWebServerPort returns the port of the installed web server (nginx/apache/caddy), or 0
func (a *App) GetWebServerPort() int {
	for _, name := range []string{"nginx", "apache", "caddy"} {
		if mgr, ok := service.Registry[name]; ok && mgr.IsInstalled() {
			return mgr.Port()
		}
	}
	return 0
}

// OpenProjectFolder opens a project folder in file explorer
func (a *App) OpenProjectFolder(path string) error {
	cmd := exec.Command("explorer", path)
	return cmd.Start()
}

// --- Database Tools ---

// IsAdminerInstalled checks if Adminer is installed
func (a *App) IsAdminerInstalled() bool {
	return tools.IsAdminerInstalled()
}

// InstallAdminer downloads adminer.php
func (a *App) InstallAdminer() error {
	return tools.InstallAdminer()
}

// DetectExternalDBTools scans for external DB tools
func (a *App) DetectExternalDBTools() []tools.ExternalTool {
	return tools.DetectExternalDBTools()
}

// LaunchExternalTool launches a detected tool
func (a *App) LaunchExternalTool(toolID, serviceName string) error {
	// Get connection info
	mgr, ok := service.Registry[serviceName]
	var args []string
	if ok && mgr.IsInstalled() {
		args = tools.GetDBConnectionArgs(toolID, serviceName, "127.0.0.1", mgr.Port())
	}
	return tools.LaunchExternalTool(toolID, args)
}

// OpenFileInEditor opens a file in the default system editor
func (a *App) OpenFileInEditor(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	return cmd.Start()
}

// OpenInBrowser opens a URL in the default browser
func (a *App) OpenInBrowser(url string) {
	wailsRuntime.BrowserOpenURL(a.ctx, url)
}

// --- Cloudflare Tunnel ---

// IsCloudflaredInstalled checks if cloudflared is available
func (a *App) IsCloudflaredInstalled() bool {
	return tunnel.IsCloudflaredInstalled()
}

// InstallCloudflared downloads cloudflared binary
func (a *App) InstallCloudflared() error {
	return tunnel.InstallCloudflared()
}

// StartTunnel starts a cloudflared quick tunnel for a specific project
func (a *App) StartTunnel(port int, projectName string, domain string, ssl bool) error {
	return tunnel.StartTunnel(port, projectName, domain, ssl)
}

// StopTunnel stops the tunnel for a specific project
func (a *App) StopTunnel(projectName string) error {
	return tunnel.StopTunnel(projectName)
}

// GetTunnelURL returns the public tunnel URL for a specific project
func (a *App) GetTunnelURL(projectName string) string {
	return tunnel.GetTunnelURL(projectName)
}

// IsTunnelRunning checks if a tunnel is active for a specific project
func (a *App) IsTunnelRunning(projectName string) bool {
	return tunnel.IsTunnelRunning(projectName)
}

// GetRunningTunnels returns all running tunnels as a map of project name to URL
func (a *App) GetRunningTunnels() map[string]string {
	return tunnel.GetRunningTunnels()
}
