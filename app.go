package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/devtools"
	"DevBox/internal/i18n"
	"DevBox/internal/pathenv"
	"DevBox/internal/platform"
	"DevBox/internal/project"
	"DevBox/internal/proxy"
	"DevBox/internal/runtime"
	"DevBox/internal/service"
	"DevBox/internal/tools"
	"DevBox/internal/tunnel"
	"DevBox/internal/updater"
	"DevBox/internal/vfox"

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

	// quitting distinguishes a real Quit (tray menu / Ctrl+Q) from the window
	// close button, which only hides to the tray when CloseToTray is on.
	quitting bool
	trayEnd  func()

	// vhostMu serialises vhost regeneration (startup, project edits, runtime
	// switches can all trigger it concurrently).
	vhostMu sync.Mutex

	// restartLog records watchdog restarts per project (see onDevServerExit).
	restartMu  sync.Mutex
	restartLog map[string][]time.Time
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	debugLog("App shutting down, stopping all tunnels, dev servers, and services...")
	if projects, err := project.ListProjects(); err == nil {
		for _, p := range projects {
			project.RestoreEnvAfterTunnel(p) // undo tunnel host swaps
		}
	}
	tunnel.StopAllTunnels()
	project.StopAllDevServers()
	devtools.StopAll()
	runtime.StopPHPCGI()
	service.StopAll()
	tools.StopAdminerServer()
	proxy.Stop()
	if a.trayEnd != nil {
		a.trayEnd()
	}
	debugLog("All tunnels, dev servers, services, and proxy stopped")
}

// beforeClose runs when the window's close button is pressed. With CloseToTray
// the window is hidden and everything keeps running until Quit is chosen.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting {
		return false
	}
	if config.Get().CloseToTray {
		wailsRuntime.WindowHide(ctx)
		return true
	}
	return false
}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	// May be called from the tray thread; never block it on the UI loop.
	go func() {
		wailsRuntime.WindowShow(a.ctx)
		wailsRuntime.WindowUnminimise(a.ctx)
	}()
}

func (a *App) quit() {
	a.quitting = true
	if a.ctx != nil {
		// Called from the tray thread — hand the quit to Wails' loop.
		go wailsRuntime.Quit(a.ctx)
	}
}

// Quit exits DevBox completely (stops services). Bound for the UI.
func (a *App) Quit() {
	a.quit()
}

// HideToTray hides the window (same as the close button with CloseToTray on).
func (a *App) HideToTray() {
	if a.ctx != nil {
		wailsRuntime.WindowHide(a.ctx)
	}
}

func (a *App) emitServicesChanged() {
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "services:changed", nil)
	}
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

	// Initialize runtime and service managers, then the vfox plugin runtimes
	// installed under <data>/plugins (java, dotnet, ruby...).
	runtime.InitAll()
	vfox.AppVersion = updater.Version
	for _, err := range runtime.RegisterPlugins() {
		debugLog("plugin runtime: %v", err)
	}
	service.InitAll()

	// Composer used to live inside the active PHP directory; it now has its
	// own tools/composer dir so switching PHP versions never loses it.
	if runtime.MigrateLegacyComposer() {
		pathenv.AddToPath(runtime.ComposerDir())
	}

	// Keep the OS login entry in sync with the executable's current location
	// (installer upgrades move it; a stale entry silently breaks autostart).
	if cfg.AutoStart {
		if err := platform.SetAutoStart(true); err != nil {
			debugLog("autostart re-register failed: %v", err)
		}
	}

	// Undo .env host swaps a 0.2.0 build may have left behind (that approach
	// was replaced by per-request FastCGI params).
	if projects, err := project.ListProjects(); err == nil {
		for _, p := range projects {
			project.RestoreEnvAfterTunnel(p)
		}
	}

	// Regenerate all project vhosts on startup — also reconciles the php-cgi
	// instances every PHP project needs (one per PHP version in use).
	a.regenerateAllVhosts()

	// Auto-start configured services
	go a.autoStartServices()

	// Keep app-server projects marked AUTO running: start them now and let
	// the exit hook restart them if they crash.
	project.OnDevServerExit = a.onDevServerExit
	go a.autoStartDevServers()

	// Auto-start the front-door proxy if it's installed and the user enabled it.
	// Failures are logged but non-fatal — DevBox keeps running without proxy.
	if cfg.ProxyEnabled && proxy.IsInstalled() {
		go func() {
			if err := proxy.Start(); err != nil {
				debugLog("proxy auto-start failed: %v", err)
			}
		}()
	}

	// Bring back custom-domain tunnels that were active last session.
	go func() {
		if err := tunnel.ResumeNamedTunnels(); err != nil {
			debugLog("named tunnel resume failed: %v", err)
		}
	}()

	// A quick tunnel learns its public URL a moment after starting; add that
	// hostname to the project's vhosts so the tunnel serves the right site.
	tunnel.OnURLDiscovered = func(string) { a.regenerateAllVhosts() }

	// Tray icon (Windows / macOS with cgo). Runs on the app's own event loop.
	start, end := a.setupTray()
	a.trayEnd = end
	if start != nil {
		start()
	}

	// Keep remote version lists warm so the Runtimes/Services pages open with
	// fresh data and update badges without the user pressing Refresh.
	go a.versionRefreshLoop()

	// Check for a newer DevBox release once shortly after launch.
	go func() {
		time.Sleep(8 * time.Second)
		if r := updater.Check(); r.Available && a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "appupdate:available", r)
		}
	}()
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
	a.emitServicesChanged()
}

// versionRefreshLoop refreshes stale version caches shortly after launch and
// then periodically. Emits "versions:refreshed" so open pages reload.
func (a *App) versionRefreshLoop() {
	time.Sleep(4 * time.Second)
	a.refreshVersionCaches(false)
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		a.refreshVersionCaches(false)
	}
}

func (a *App) refreshVersionCaches(force bool) {
	changed := false
	for name := range runtime.Registry {
		if force || runtime.IsCacheStale(name) {
			if _, fromCache, err := runtime.ListRemoteCached(name, true); err == nil && !fromCache {
				changed = true
			} else if err != nil {
				debugLog("version refresh %s: %v", name, err)
			}
		}
	}
	for name := range service.Registry {
		if force || service.IsServiceCacheStale(name) {
			if _, fromCache, err := service.ListVersionsCached(name, true); err == nil && !fromCache {
				changed = true
			} else if err != nil {
				debugLog("service version refresh %s: %v", name, err)
			}
		}
	}
	if changed && a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "versions:refreshed", nil)
	}
}

// RefreshAllVersions forces a re-fetch of every runtime/service version list.
func (a *App) RefreshAllVersions() {
	go a.refreshVersionCaches(true)
}

// --- Config bindings ---

func (a *App) GetConfig() *config.Config {
	return config.Get()
}

func (a *App) SetLanguage(lang string) error {
	i18n.SetLanguage(lang)
	if err := config.SetLanguage(lang); err != nil {
		return err
	}
	a.rebuildTrayMenu()
	return nil
}

func (a *App) SetTheme(theme string) error {
	return config.SetTheme(theme)
}

// SetCloseToTray toggles whether the close button hides to the tray.
func (a *App) SetCloseToTray(enabled bool) error {
	cfg := config.Get()
	cfg.CloseToTray = enabled
	return config.Save()
}

// SetStartMinimized toggles launching hidden in the tray.
func (a *App) SetStartMinimized(enabled bool) error {
	cfg := config.Get()
	cfg.StartMinimized = enabled
	return config.Save()
}

// GetDataDir returns the data directory path (for display / open folder).
func (a *App) GetDataDir() string {
	return config.GetDataDir()
}

// OpenDataDir opens the data directory in the file manager.
func (a *App) OpenDataDir() error {
	return platform.OpenFolder(config.GetDataDir())
}

// MigrationNotice describes a data-dir move performed at this launch.
type MigrationNotice struct {
	Migrated bool   `json:"migrated"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// GetMigrationNotice lets the UI show a one-time "your data moved" banner.
func (a *App) GetMigrationNotice() MigrationNotice {
	return MigrationNotice{Migrated: config.Migrated, From: config.MigratedFrom, To: config.GetDataDir()}
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
	// UpdateFor is the installed version this remote version would replace
	// in place (same release line, newer). Empty for plain installs.
	UpdateFor string `json:"updateFor"`
}

// RemoteVersionsResult bundles the list with cache metadata for the UI.
type RemoteVersionsResult struct {
	Versions  []RuntimeVersionInfo `json:"versions"`
	FromCache bool                 `json:"fromCache"`
	FetchedAt string               `json:"fetchedAt"`
}

func (a *App) buildRemoteResult(name string, remote []runtime.Version, fromCache bool) RemoteVersionsResult {
	mgr := runtime.Registry[name]
	installed, _ := mgr.ListInstalled()
	installedMap := map[string]bool{}
	for _, v := range installed {
		installedMap[v.Number] = true
	}
	result := RemoteVersionsResult{FromCache: fromCache}
	if t := runtime.CacheFetchedAt(name); !t.IsZero() {
		result.FetchedAt = t.Format(time.RFC3339)
	}
	for _, v := range remote {
		result.Versions = append(result.Versions, RuntimeVersionInfo{
			Number:    v.Number,
			Stable:    v.Stable,
			Current:   v.Current,
			Installed: installedMap[v.Number],
			UpdateFor: runtime.UpdateTarget(name, v.Number, installed),
		})
	}
	return result
}

// GetRemoteVersions fetches available versions for a runtime (cached).
func (a *App) GetRemoteVersions(name string) ([]RuntimeVersionInfo, error) {
	r, err := a.GetRemoteVersionsInfo(name, false)
	if err != nil {
		return nil, err
	}
	return r.Versions, nil
}

// GetRemoteVersionsInfo returns the version list plus cache metadata. force
// bypasses the cache (the Refresh button).
func (a *App) GetRemoteVersionsInfo(name string, force bool) (RemoteVersionsResult, error) {
	if _, ok := runtime.Registry[name]; !ok {
		return RemoteVersionsResult{}, nil
	}
	remote, fromCache, err := runtime.ListRemoteCached(name, force)
	if err != nil {
		return RemoteVersionsResult{}, err
	}
	return a.buildRemoteResult(name, remote, fromCache), nil
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

// GetRuntimeUpdates lists installed versions with a newer release in their
// line (from the cached remote list — no network).
func (a *App) GetRuntimeUpdates(name string) []runtime.RuntimeUpdate {
	if _, ok := runtime.Registry[name]; !ok {
		return nil
	}
	remote, _, err := runtime.ListRemoteCached(name, false)
	if err != nil {
		return nil
	}
	return runtime.FindUpdates(name, remote)
}

// GetInstalledCounts returns how many versions of each runtime are installed.
func (a *App) GetInstalledCounts() map[string]int {
	out := map[string]int{}
	for name, mgr := range runtime.Registry {
		versions, _ := mgr.ListInstalled()
		out[name] = len(versions)
	}
	return out
}

// InstallRuntime installs a runtime version with progress events.
// Returns immediately - progress/completion/errors via events.
func (a *App) InstallRuntime(name, version string) error {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return fmt.Errorf("unknown runtime: %s", name)
	}

	// Plugin runtimes accept aliases ("latest", "21"); the plugin resolves
	// them during install and the resolved version is what becomes global.
	resolved := version
	a.runRuntimeJob(name, version, func(progress chan<- runtime.Progress) error {
		if pm, err := runtime.PluginManagerFor(name); err == nil {
			v, err := pm.InstallResolved(version, progress)
			if v != "" {
				resolved = v
			}
			return err
		}
		return mgr.Install(version, progress)
	}, func() map[string]interface{} {
		extra := map[string]interface{}{"resolvedVersion": resolved}
		// Auto-set as global if it's the first install
		global, _ := mgr.GetGlobal()
		if global == "" {
			if err := mgr.SetGlobal(resolved); err == nil {
				if err := a.activateRuntimeEnv(mgr, "", resolved); err != nil {
					debugLog("activate %s %s: %v", name, resolved, err)
				}
			}
		}
		return extra
	})
	return nil
}

// runRuntimeJob runs an install-like runtime job in the background. The final
// runtime:installed / runtime:error event is emitted only AFTER every queued
// progress event has been forwarded — emitting it earlier lets a late
// runtime:progress event resurrect the already-cleared install state in the
// frontend store and leaves a progress bar stuck at 100% (very visible with
// near-instant jobs like link-based imports). `extra` runs on success and may
// return additional fields for the runtime:installed payload.
func (a *App) runRuntimeJob(name, version string, job func(chan<- runtime.Progress) error, extra func() map[string]interface{}) {
	go func() {
		progress := make(chan runtime.Progress, 10)
		errCh := make(chan error, 1)

		go func() {
			defer close(progress)
			defer func() {
				if r := recover(); r != nil {
					debugLog("runtime job PANIC: %v", r)
					errCh <- fmt.Errorf("internal error: %v", r)
				}
			}()
			errCh <- job(progress)
		}()

		// Returns once the job closed the channel — every progress event has
		// been emitted by then.
		a.forwardRuntimeProgress(name, version, progress)

		if err := <-errCh; err != nil {
			wailsRuntime.EventsEmit(a.ctx, "runtime:error", map[string]interface{}{
				"name":    name,
				"version": version,
				"error":   err.Error(),
			})
			return
		}

		payload := map[string]interface{}{"name": name, "version": version}
		if extra != nil {
			for k, v := range extra() {
				payload[k] = v
			}
		}
		wailsRuntime.EventsEmit(a.ctx, "runtime:installed", payload)
	}()
}

func (a *App) forwardRuntimeProgress(name, version string, progress <-chan runtime.Progress) {
	for p := range progress {
		wailsRuntime.EventsEmit(a.ctx, "runtime:progress", map[string]interface{}{
			"name":    name,
			"version": version,
			"percent": p.Percent,
			"message": p.Message,
		})
	}
}

// UpdateRuntime replaces an installed version with a newer one of the same
// release line: installs the new version, carries over per-version state
// (php.ini, Composer, yarn/pnpm, global npm/pip packages), moves the global
// flag, PATH entry and project pins across, then removes the old version.
func (a *App) UpdateRuntime(name, from, to string) error {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return fmt.Errorf("unknown runtime: %s", name)
	}
	if runtime.UpdateLine(name, from) != runtime.UpdateLine(name, to) {
		return fmt.Errorf("%s → %s is not an in-place update; install it as a separate version", from, to)
	}

	a.runRuntimeJob(name, to, func(progress chan<- runtime.Progress) error {
		// The target may already be installed (user installed it separately
		// earlier): then this is a consolidation — skip the download.
		if _, err := os.Stat(mgr.BinaryPath(to)); err != nil {
			if err := mgr.Install(to, progress); err != nil {
				return err
			}
		}

		progress <- runtime.Progress{Percent: 99, Message: "Migrating settings from " + from + "..."}
		runtime.MigrateVersionData(name, from, to, progress)

		global, _ := mgr.GetGlobal()
		if global == from {
			a.deactivateRuntimeEnv(mgr, from)
			mgr.SetGlobal(to)
			if err := a.activateRuntimeEnv(mgr, "", to); err != nil {
				debugLog("activate %s %s: %v", name, to, err)
			}
		}

		// Re-point project pins.
		if projects, err := project.ListProjects(); err == nil {
			changed := false
			for i := range projects {
				if projects[i].Runtime == name && projects[i].RuntimeVersion == from {
					projects[i].RuntimeVersion = to
					changed = true
				}
			}
			if changed {
				project.SaveProjects(projects)
			}
		}

		if name == "php" {
			runtime.StopPHPCGIVersion(from)
			cfg := config.Get()
			if p, ok := cfg.PhpCgiPorts[from]; ok {
				cfg.PhpCgiPorts[to] = p
				delete(cfg.PhpCgiPorts, from)
				config.Save()
			}
		}

		progress <- runtime.Progress{Percent: 99, Message: "Removing " + from + "..."}
		if err := mgr.Uninstall(from); err != nil {
			progress <- runtime.Progress{Percent: 99, Message: "Old version could not be removed: " + err.Error()}
		}

		a.regenerateAllVhosts()
		return nil
	}, func() map[string]interface{} {
		return map[string]interface{}{"updatedFrom": from}
	})
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

	// Remove from PATH (and drop managed variables) if this is the global version
	global, _ := mgr.GetGlobal()
	if global == version {
		a.deactivateRuntimeEnv(mgr, version)
	}

	if name == "php" {
		runtime.StopPHPCGIVersion(version)
		cfg := config.Get()
		if _, ok := cfg.PhpCgiPorts[version]; ok {
			delete(cfg.PhpCgiPorts, version)
			config.Save()
		}
	}

	if err := mgr.Uninstall(version); err != nil {
		return err
	}
	go a.regenerateAllVhosts()
	return nil
}

// SetGlobalRuntime sets a version as the global active version. "Global" means:
// first on the user PATH (CLI), the default FastCGI instance (PHP), and the
// version every project without an explicit pin runs under.
func (a *App) SetGlobalRuntime(name, version string) error {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return nil
	}

	oldGlobal, _ := mgr.GetGlobal()
	if err := mgr.SetGlobal(version); err != nil {
		return err
	}

	// Switch PATH (and JAVA_HOME-style variables for plugin runtimes).
	if err := a.activateRuntimeEnv(mgr, oldGlobal, version); err != nil {
		return err
	}

	// The default php-cgi (port 9000) follows the global version; unpinned
	// projects of any runtime follow it too, so vhosts and instances are
	// reconciled.
	go a.regenerateAllVhosts()
	return nil
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
	result := map[string][]string{}
	for _, name := range runtime.Names() {
		result[name] = []string{}
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
	return service.GetAll()
}

// GetServiceVersions returns available versions for a service (cached).
func (a *App) GetServiceVersions(name string) ([]service.AvailableVersion, error) {
	versions, _, err := service.ListVersionsCached(name, false)
	return versions, err
}

// RefreshServiceVersions re-fetches a service's version list.
func (a *App) RefreshServiceVersions(name string) ([]service.AvailableVersion, error) {
	versions, _, err := service.ListVersionsCached(name, true)
	return versions, err
}

// CheckPort checks if a port is available
func (a *App) CheckPort(port int) service.PortStatus {
	return service.CheckPort(port)
}

// SetServicePort changes the port for an installed service
// shown renders a password for the info panel.
func shown(pw string) string {
	if pw == "" {
		return "(none)"
	}
	return pw
}

func userInfo(user, pw string) string {
	if pw == "" {
		return user
	}
	return user + ":" + url.PathEscape(pw)
}

func redisUserInfo(pw string) string {
	if pw == "" {
		return ""
	}
	return ":" + url.PathEscape(pw) + "@"
}

func pwFlag(pw string) string {
	if pw == "" {
		return ""
	}
	return " -p"
}

func aFlag(pw string) string {
	if pw == "" {
		return ""
	}
	return " -a " + pw
}

// SetServiceSetting edits one connection setting from the info panel.
// key: port | user | password | databases.
func (a *App) SetServiceSetting(name, key, value string) error {
	var err error
	if key == "port" {
		p, convErr := strconv.Atoi(strings.TrimSpace(value))
		if convErr != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid port")
		}
		err = a.SetServicePort(name, p)
	} else {
		err = service.SetSetting(name, key, value)
	}
	if err != nil {
		return err
	}
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "services:changed", map[string]interface{}{"name": name})
	}
	return nil
}

func (a *App) SetServicePort(name string, port int) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	if err := mgr.SetPort(port); err != nil {
		return err
	}

	// If it's a web server, regenerate all project vhosts with the new port
	if name == "nginx" || name == "apache" || name == "caddy" || name == "frankenphp" {
		a.regenerateAllVhosts()
	}
	return nil
}

// isPhpProject reports whether a project is served through PHP FastCGI
// (as opposed to running its own dev server or being static).
func isPhpProject(p project.Project) bool {
	return p.Runtime == "php"
}

// regenerateAllVhosts writes a vhost config per project to whichever webserver
// the project picked (nginx / caddy / apache / frankenphp), or skips the project
// when it runs its own dev server. Multiple webservers can co-exist — each one
// is restarted exactly once at the end if it both holds new vhosts and is
// currently running.
//
// PHP projects are handed to the php-cgi instance of the PHP version they
// resolve to (their pin, or the global version), which is started here.
func (a *App) regenerateAllVhosts() {
	a.vhostMu.Lock()
	defer a.vhostMu.Unlock()

	projects, err := project.ListProjects()
	if err != nil {
		return
	}

	// Which PHP versions do domain-bound PHP projects need?
	var phpVersions []string
	seen := map[string]bool{}
	for _, p := range projects {
		if p.Domain == "" || !isPhpProject(p) {
			continue
		}
		ws := project.ResolveWebserver(p)
		if ws == "" || ws == "devserver" || ws == "frankenphp" {
			continue // FrankenPHP bundles its own PHP
		}
		if v := project.ResolveRuntimeVersion(p); v != "" && !seen[v] {
			seen[v] = true
			phpVersions = append(phpVersions, v)
		}
	}
	phpPorts, phpErrs := runtime.EnsurePHPCGI(phpVersions)
	for v, err := range phpErrs {
		debugLog("php-cgi %s: %v", v, err)
	}

	// Caddy appends per-project blocks to its Caddyfile: reset it first.
	if mgr, ok := service.Registry["caddy"]; ok && mgr.IsInstalled() {
		if cm, ok := mgr.(*service.CaddyManager); ok {
			cm.ResetConfig()
		}
	}

	// Track which webserver services received writes so we restart only those.
	touched := map[string]bool{}

	for _, p := range projects {
		phpPort := 0
		if isPhpProject(p) {
			phpPort = phpPorts[project.ResolveRuntimeVersion(p)]
		}
		ws := project.ResolveWebserver(p)
		switch ws {
		case "nginx":
			if mgr, ok := service.Registry["nginx"]; ok && mgr.IsInstalled() {
				project.GenerateNginxVhost(p, phpPort, mgr.Port(), proxy.TLSAtFrontDoor())
				touched["nginx"] = true
			}
		case "apache":
			if mgr, ok := service.Registry["apache"]; ok && mgr.IsInstalled() {
				project.GenerateApacheVhost(p, mgr.Port(), phpPort)
				touched["apache"] = true
			}
		case "caddy":
			if mgr, ok := service.Registry["caddy"]; ok && mgr.IsInstalled() {
				project.GenerateCaddyVhost(p, phpPort)
				touched["caddy"] = true
			}
		case "frankenphp":
			if mgr, ok := service.Registry["frankenphp"]; ok && mgr.IsInstalled() {
				project.GenerateFrankenPHPVhost(p)
				touched["frankenphp"] = true
			}
		case "devserver", "":
			// No vhost: the front-door proxy routes traffic directly to the
			// project's dev server port (or there's no backend at all).
		}
	}

	// Restart each touched webserver if it's currently running so it picks up
	// its new vhost set. FrankenPHP supports live reload via the import glob —
	// a restart still works and is simpler than wiring caddy admin API.
	for name := range touched {
		mgr, ok := service.Registry[name]
		if !ok {
			continue
		}
		if mgr.Status() == service.StatusRunning {
			mgr.Restart()
		}
	}

	// Refresh the front-door proxy's Caddyfile so domain routing stays in sync
	// with the latest project list / runtime+webserver choices. No-op if the
	// proxy isn't running yet.
	if err := proxy.Reload(); err != nil {
		debugLog("proxy reload failed: %v", err)
	}
}

// RegenerateVhosts is the UI-triggered variant (e.g. after fixing a service).
func (a *App) RegenerateVhosts() {
	a.regenerateAllVhosts()
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

	a.runServiceJob(name, func(progress chan<- service.Progress) error {
		return mgr.Install(version, port, progress)
	}, func() {
		// A newly installed webserver should pick up existing projects.
		if name == "nginx" || name == "apache" || name == "caddy" || name == "frankenphp" {
			a.regenerateAllVhosts()
		}
	})
	return nil
}

// UpdateService replaces an installed service with a newer version, keeping
// its data and configuration. Same events as InstallService, plus
// "updatedFrom" on service:installed.
func (a *App) UpdateService(name string, version string) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	if !mgr.IsInstalled() {
		return fmt.Errorf("%s is not installed", mgr.DisplayName())
	}
	from := mgr.Version()
	debugLog("UpdateService: %s %s -> %s", name, from, version)

	a.runServiceJob(name, func(progress chan<- service.Progress) error {
		return service.Update(name, version, progress)
	}, func() {
		if name == "nginx" || name == "apache" || name == "caddy" || name == "frankenphp" {
			a.regenerateAllVhosts()
		}
	})
	return nil
}

// runServiceJob runs an install-like job in the background, forwarding
// progress events and emitting service:installed / service:error at the end.
func (a *App) runServiceJob(name string, job func(chan<- service.Progress) error, after func()) {
	go func() {
		progress := make(chan service.Progress, 10)
		errCh := make(chan error, 1)

		go func() {
			defer close(progress)
			defer func() {
				if r := recover(); r != nil {
					debugLog("service job PANIC: %v", r)
					errCh <- fmt.Errorf("internal error: %v", r)
				}
			}()
			errCh <- job(progress)
		}()

		// Drain every progress event BEFORE emitting the final event: a
		// service:progress arriving after service:installed would resurrect
		// the already-cleared install state in the frontend store and leave a
		// stuck progress bar (very visible with near-instant jobs like
		// link-based imports).
		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "service:progress", map[string]interface{}{
				"name":    name,
				"percent": p.Percent,
				"message": p.Message,
			})
		}

		if err := <-errCh; err != nil {
			debugLog("service job %s error: %v", name, err)
			wailsRuntime.EventsEmit(a.ctx, "service:error", map[string]interface{}{
				"name":  name,
				"error": err.Error(),
			})
			return
		}
		if after != nil {
			after()
		}
		wailsRuntime.EventsEmit(a.ctx, "service:installed", map[string]interface{}{
			"name": name,
		})
	}()
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
	err := mgr.Start()
	a.emitServicesChanged()
	return err
}

// StopService stops a service
func (a *App) StopService(name string) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	err := mgr.Stop()
	a.emitServicesChanged()
	return err
}

// RestartService restarts a service
func (a *App) RestartService(name string) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	err := mgr.Restart()
	a.emitServicesChanged()
	return err
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

// SetAutoStart enables or disables DevBox launching at system login
func (a *App) SetAutoStart(enabled bool) error {
	if err := platform.SetAutoStart(enabled); err != nil {
		return err
	}
	cfg := config.Get()
	cfg.AutoStart = enabled
	return config.Save()
}

// IsAutoStartEnabled reports the OS-level login entry (source of truth), not
// just the config flag.
func (a *App) IsAutoStartEnabled() bool {
	return platform.IsAutoStartEnabled()
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

// GetPathHealth diagnoses the PATH: cmd.exe's 8191-character limit,
// duplicates, dead directories and %PATH% self-references. Past the limit
// cmd.exe cannot resolve anything on PATH, so .bat wrappers (composer.bat)
// fail with "'php' is not recognized" although php is installed.
func (a *App) GetPathHealth() pathenv.Health {
	managed := runtime.ManagedPathDirs()
	tools := filepath.Join(config.GetDataDir(), "tools")
	for _, sub := range []string{"composer", "bun", "uv", "gobin", filepath.Join("cargo", "bin"), "mkcert", "cloudflared"} {
		managed = append(managed, filepath.Join(tools, sub))
	}
	return pathenv.CheckWith(managed)
}

// RemoveSystemPathEntry drops a directory from the machine PATH (UAC prompt) —
// used to stop a Laragon/XAMPP folder from shadowing DevBox's tools.
func (a *App) RemoveSystemPathEntry(dir string) error {
	return pathenv.RemoveSystemEntry(dir)
}

// GetPathEditor returns the raw, ordered entries of both PATH scopes.
func (a *App) GetPathEditor() pathenv.Editor {
	return pathenv.GetEditor()
}

// SaveUserPath replaces the user PATH with the given order.
func (a *App) SaveUserPath(entries []string) error {
	return pathenv.SaveUser(entries)
}

// SaveSystemPath replaces the machine PATH (UAC prompt).
func (a *App) SaveSystemPath(entries []string) error {
	return pathenv.SaveSystem(entries)
}

// RefreshPath broadcasts the environment change and reloads DevBox's own PATH.
func (a *App) RefreshPath() error {
	return pathenv.Refresh()
}

// CleanUserPath removes duplicates, dead directories and %PATH% entries from
// the user PATH (a backup is written under <data>/backups). Returns the count.
func (a *App) CleanUserPath() (int, error) {
	return pathenv.CleanUser()
}

// CleanSystemPath does the same for the machine PATH via an elevated write (UAC).
func (a *App) CleanSystemPath() (int, error) {
	return pathenv.CleanSystem()
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
	// Key names an editable setting ("port", "user", "password", "databases");
	// empty means read-only.
	Key string `json:"key,omitempty"`
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
	pgUser, pgPass := service.Credentials("postgres")
	myUser, myPass := service.Credentials(name)
	_, kvPass := service.Credentials(name)
	kvDBs := service.RedisDatabases(name)

	switch name {
	case "nginx":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
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
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
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
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
				{Label: "Document Root", Value: filepath.Join(baseDir, "html")},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "Caddyfile", Path: filepath.Join(baseDir, "Caddyfile")},
			},
			WebLinks: []WebLinkEntry{
				{Label: "Open in Browser", URL: fmt.Sprintf("http://127.0.0.1:%d", port)},
			},
		}

	case "frankenphp":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
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
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
				{Label: "User", Value: pgUser, Key: "user"},
				{Label: "Password", Value: shown(pgPass), Key: "password"},
				{Label: "URI", Value: fmt.Sprintf("postgresql://%s@127.0.0.1:%d/postgres", userInfo(pgUser, pgPass), port)},
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
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
				{Label: "User", Value: myUser, Key: "user"},
				{Label: "Password", Value: shown(myPass), Key: "password"},
				{Label: "CLI", Value: fmt.Sprintf("mysql -u %s%s -h 127.0.0.1 -P %d", myUser, pwFlag(myPass), port)},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: service.MysqlConfigName(), Path: filepath.Join(baseDir, service.MysqlConfigName())},
			},
		}

	case "mariadb":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
				{Label: "User", Value: myUser, Key: "user"},
				{Label: "Password", Value: shown(myPass), Key: "password"},
				{Label: "CLI", Value: fmt.Sprintf("mysql -u %s%s -h 127.0.0.1 -P %d", myUser, pwFlag(myPass), port)},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: service.MysqlConfigName(), Path: filepath.Join(baseDir, service.MysqlConfigName())},
			},
		}

	case "mongodb":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
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
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
				{Label: "Password", Value: shown(kvPass), Key: "password"},
				{Label: "CLI", Value: fmt.Sprintf("redis-cli -h 127.0.0.1 -p %d%s", port, aFlag(kvPass))},
				{Label: "URI", Value: fmt.Sprintf("redis://%s127.0.0.1:%d/0", redisUserInfo(kvPass), port)},
				{Label: "Databases", Value: fmt.Sprintf("%d (0-%d)", kvDBs, kvDBs-1), Key: "databases"},
			},
			ConfigFiles: []ConfigFileEntry{
				{Label: "redis.conf", Path: filepath.Join(baseDir, "redis.conf")},
			},
		}

	case "valkey":
		return ServiceDetailInfo{
			ConnectionInfo: []ConnectionEntry{
				{Label: "Host", Value: "127.0.0.1"},
				{Label: "Port", Value: fmt.Sprintf("%d", port), Key: "port"},
				{Label: "Password", Value: shown(kvPass), Key: "password"},
				{Label: "CLI", Value: fmt.Sprintf("valkey-cli -h 127.0.0.1 -p %d%s", port, aFlag(kvPass))},
				{Label: "URI", Value: fmt.Sprintf("redis://%s127.0.0.1:%d/0", redisUserInfo(kvPass), port)},
				{Label: "Databases", Value: fmt.Sprintf("%d (0-%d)", kvDBs, kvDBs-1), Key: "databases"},
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
				{Label: "SMTP Port", Value: fmt.Sprintf("%d", smtpPort), Key: "port"},
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
	_, err := os.Stat(filepath.Join(toolDir, platform.BinaryName("bun")))
	return err == nil
}

// InstallBun downloads and installs Bun
func (a *App) InstallBun() error {
	go func() {
		toolDir := filepath.Join(config.GetDataDir(), "tools", "bun")
		os.MkdirAll(toolDir, 0755)

		assetName := bunAssetName()

		// Fetch latest release URL from GitHub
		downloadURL := ""
		resp, err := runtime.FetchGitHubReleasesPublic("oven-sh", "bun")
		if err == nil && len(resp) > 0 {
			for _, asset := range resp[0].Assets {
				if asset.Name == assetName {
					downloadURL = asset.BrowserDownloadURL
					break
				}
			}
		}
		if downloadURL == "" {
			downloadURL = "https://github.com/oven-sh/bun/releases/latest/download/" + assetName
		}

		tmpFile := filepath.Join(config.GetDataDir(), "tmp", assetName)
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
		bunExe := filepath.Join(tmpExtract, platform.BinaryName("bun"))
		if _, err := os.Stat(bunExe); os.IsNotExist(err) {
			entries, _ := os.ReadDir(tmpExtract)
			for _, e := range entries {
				if e.IsDir() {
					candidate := filepath.Join(tmpExtract, e.Name(), platform.BinaryName("bun"))
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
		if err := os.WriteFile(filepath.Join(toolDir, platform.BinaryName("bun")), data, 0755); err != nil {
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
	bunExe := filepath.Join(toolDir, platform.BinaryName("bun"))
	if _, err := os.Stat(bunExe); os.IsNotExist(err) {
		return ""
	}

	cmd := exec.Command(bunExe, "--version")
	platform.SetProcessAttrs(cmd, false, true)
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

// bunAssetName picks the correct release asset for the current OS/arch.
// Bun publishes per-platform zips on GitHub releases.
func bunAssetName() string {
	switch goruntime.GOOS {
	case "darwin":
		if goruntime.GOARCH == "arm64" {
			return "bun-darwin-aarch64.zip"
		}
		return "bun-darwin-x64.zip"
	default:
		return "bun-windows-x64.zip"
	}
}

// --- Yarn / pnpm Package Managers (corepack-based, per active Node version) ---

// IsYarnEnabled reports whether yarn is available for the active Node version.
func (a *App) IsYarnEnabled() bool {
	return runtime.IsPkgMgrEnabled("yarn")
}

// EnableYarn enables yarn on the active Node version via corepack.
// Runs asynchronously; emits "yarn:installed" on success or "yarn:error" on failure.
func (a *App) EnableYarn() error {
	go func() {
		if err := runtime.EnablePkgMgr("yarn"); err != nil {
			wailsRuntime.EventsEmit(a.ctx, "yarn:error", map[string]interface{}{"error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "yarn:installed", map[string]interface{}{})
	}()
	return nil
}

// DisableYarn removes the yarn shim from the active Node bin dir.
func (a *App) DisableYarn() error {
	return runtime.DisablePkgMgr("yarn")
}

// GetYarnVersion returns the active yarn version or "" if not enabled.
func (a *App) GetYarnVersion() string {
	return runtime.GetPkgMgrVersion("yarn")
}

// IsPnpmEnabled reports whether pnpm is available for the active Node version.
func (a *App) IsPnpmEnabled() bool {
	return runtime.IsPkgMgrEnabled("pnpm")
}

// EnablePnpm enables pnpm on the active Node version via corepack.
func (a *App) EnablePnpm() error {
	go func() {
		if err := runtime.EnablePkgMgr("pnpm"); err != nil {
			wailsRuntime.EventsEmit(a.ctx, "pnpm:error", map[string]interface{}{"error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "pnpm:installed", map[string]interface{}{})
	}()
	return nil
}

// DisablePnpm removes the pnpm shim from the active Node bin dir.
func (a *App) DisablePnpm() error {
	return runtime.DisablePkgMgr("pnpm")
}

// GetPnpmVersion returns the active pnpm version or "" if not enabled.
func (a *App) GetPnpmVersion() string {
	return runtime.GetPkgMgrVersion("pnpm")
}

// GetNpmVersion returns the npm version bundled with the active Node.
func (a *App) GetNpmVersion() string {
	return runtime.GetNpmVersion()
}

// GetNpmLatestVersion returns the newest npm release on the registry ("" offline).
func (a *App) GetNpmLatestVersion() string {
	return runtime.GetNpmLatestVersion()
}

// UpdateNpm upgrades npm inside the active Node version. Emits npm:updated / npm:error.
func (a *App) UpdateNpm() error {
	go func() {
		if err := runtime.UpdateNpm(); err != nil {
			wailsRuntime.EventsEmit(a.ctx, "npm:error", map[string]interface{}{"error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "npm:updated", map[string]interface{}{"version": runtime.GetNpmVersion()})
	}()
	return nil
}

// --- PHP Extensions & Settings & Composer & PHP-CGI ---

// GetPHPExtensions returns extensions for a PHP version
func (a *App) GetPHPExtensions(version string) ([]runtime.PHPExtension, error) {
	return runtime.GetPHPExtensions(version)
}

// GetPeclExtensions returns the PECL catalog with install state for a PHP version.
func (a *App) GetPeclExtensions(version string) []runtime.PeclExtension {
	return runtime.GetPeclExtensions(version)
}

// InstallPeclExtension downloads + enables a PECL extension. Async; events:
// phpext:progress {version,name,percent,message}, phpext:installed, phpext:error.
func (a *App) InstallPeclExtension(version, name string) error {
	go func() {
		progress := make(chan runtime.Progress, 10)
		errCh := make(chan error, 1)
		go func() {
			defer close(progress)
			errCh <- runtime.InstallPeclExtension(version, name, progress)
		}()
		// Drain progress fully before the final event (see runRuntimeJob).
		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "phpext:progress", map[string]interface{}{"version": version, "name": name, "percent": p.Percent, "message": p.Message})
		}
		if err := <-errCh; err != nil {
			wailsRuntime.EventsEmit(a.ctx, "phpext:error", map[string]interface{}{"version": version, "name": name, "error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "phpext:installed", map[string]interface{}{"version": version, "name": name})
	}()
	return nil
}

// UninstallPeclExtension disables and removes a PECL extension.
func (a *App) UninstallPeclExtension(version, name string) error {
	return runtime.UninstallPeclExtension(version, name)
}

// --- App updates (GitHub Releases) ---

// GetAppVersion returns the running build's version.
func (a *App) GetAppVersion() string {
	return updater.Version
}

// CheckForUpdate queries GitHub Releases for a newer DevBox.
func (a *App) CheckForUpdate() updater.Release {
	return updater.Check()
}

// GetLastUpdateCheck returns the cached result of the last check.
func (a *App) GetLastUpdateCheck() updater.Release {
	return updater.Last()
}

// InstallAppUpdate downloads the installer and runs it silently. On Windows
// the call waits on the installer: a successful update kills DevBox from the
// installer side (this goroutine never resumes); an installer failure returns
// here with DevBox still running so the error is SHOWN instead of the update
// silently vanishing. Events: appupdate:progress {percent,message},
// appupdate:error {error}.
func (a *App) InstallAppUpdate() error {
	go func() {
		_, err := updater.DownloadAndInstall(func(pct int, msg string) {
			wailsRuntime.EventsEmit(a.ctx, "appupdate:progress", map[string]interface{}{"percent": pct, "message": msg})
		})
		if err != nil {
			wailsRuntime.EventsEmit(a.ctx, "appupdate:error", map[string]interface{}{"error": err.Error()})
			return
		}
		if goruntime.GOOS == "windows" {
			// Reaching here means the installer finished (exit 0) while we
			// are still alive — its taskkill missed us (renamed/portable
			// executable). Quit so the updated copy can start cleanly.
			time.Sleep(1500 * time.Millisecond)
			a.quit()
		}
	}()
	return nil
}

// GetPHPIniSettings returns common php.ini directive values for a PHP version
func (a *App) GetPHPIniSettings(version string) (map[string]string, error) {
	return runtime.GetPHPIniSettings(version)
}

// SetPHPIniSetting updates a single php.ini directive
func (a *App) SetPHPIniSetting(version, key, value string) error {
	return runtime.SetPHPIniSetting(version, key, value)
}

// GetPHPIniPath returns the php.ini file path for a PHP version
func (a *App) GetPHPIniPath(version string) string {
	return runtime.GetPHPIniPath(version)
}

// TogglePHPExtension toggles a PHP extension
func (a *App) TogglePHPExtension(version, extName string, enable bool) error {
	return runtime.TogglePHPExtension(version, extName, enable)
}

// IsComposerInstalled checks if Composer is installed
func (a *App) IsComposerInstalled() bool {
	return runtime.IsComposerInstalled()
}

// GetComposerInfo returns installed/imported state, version and the newest
// stable release (cached) so the Tools page can show an update badge.
func (a *App) GetComposerInfo() runtime.ComposerInfo {
	return runtime.GetComposerInfo()
}

// InstallComposer downloads and installs Composer
func (a *App) InstallComposer() error {
	go func() {
		progress := make(chan runtime.Progress, 10)
		errCh := make(chan error, 1)
		go func() {
			defer close(progress)
			errCh <- runtime.InstallComposer(progress)
		}()
		// Drain progress fully before the final event (see runRuntimeJob).
		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "composer:progress", map[string]interface{}{
				"percent": p.Percent,
				"message": p.Message,
			})
		}
		if err := <-errCh; err != nil {
			wailsRuntime.EventsEmit(a.ctx, "composer:error", map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		pathenv.AddToPath(runtime.ComposerDir())
		invalidateDiscoveryCache()
		wailsRuntime.EventsEmit(a.ctx, "composer:installed", map[string]interface{}{})
	}()
	return nil
}

// UpdateComposer runs `composer self-update` (managed and imported installs
// alike). Emits composer:updated / composer:error.
func (a *App) UpdateComposer() error {
	go func() {
		if err := runtime.UpdateComposer(); err != nil {
			wailsRuntime.EventsEmit(a.ctx, "composer:error", map[string]interface{}{"error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "composer:updated", runtime.GetComposerInfo())
	}()
	return nil
}

// UninstallComposer removes DevBox's Composer (for an imported one only the
// link — the system phar stays where it is).
func (a *App) UninstallComposer() error {
	pathenv.RemoveFromPath(runtime.ComposerDir())
	if err := runtime.UninstallComposer(); err != nil {
		return err
	}
	invalidateDiscoveryCache()
	return nil
}

// GetComposerVersion returns composer version
func (a *App) GetComposerVersion() string {
	return runtime.GetComposerVersion()
}

// GetPHPCGIInstances lists running php-cgi processes (one per PHP version in use).
func (a *App) GetPHPCGIInstances() []runtime.PHPCGIInstance {
	return runtime.RunningPHPCGIInstances()
}

// StartPHPCGI starts the FastCGI instance for a PHP version on its managed port.
func (a *App) StartPHPCGI(version string, port int) error {
	return runtime.StartPHPCGI(version, port)
}

// StopPHPCGI stops every php-cgi instance.
func (a *App) StopPHPCGI() error {
	return runtime.StopPHPCGI()
}

// RestartPHPCGI restarts every php-cgi instance projects need (picks up php.ini edits).
func (a *App) RestartPHPCGI() error {
	runtime.StopPHPCGI()
	a.regenerateAllVhosts()
	return nil
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

// AddProject adds a project from a selected folder and provisions it right
// away: hosts entry (UAC prompt), local SSL certificate, vhost.
func (a *App) AddProject(projectPath, domain string) (*project.Project, error) {
	p, err := project.AddProject(projectPath, domain)
	if err != nil {
		return nil, err
	}
	a.provisionProject(p.Name)
	projects, _ := project.ListProjects()
	for i := range projects {
		if projects[i].Name == p.Name {
			return &projects[i], nil
		}
	}
	return p, nil
}

// provisionProject makes a freshly added project reachable without further
// clicks: hosts-file entry for its domain, mkcert certificate (SSL on), and
// web-server vhost. Each step is best-effort and logged — a declined UAC
// prompt or a missing web server must not undo the import itself.
func (a *App) provisionProject(name string) {
	projects, err := project.ListProjects()
	if err != nil {
		return
	}
	for i, p := range projects {
		if p.Name != name || p.Domain == "" {
			continue
		}
		if !p.HostsRegistered {
			if err := project.AddHostsEntry(p.Domain); err != nil {
				debugLog("provision %s: hosts entry failed: %v", name, err)
				a.projectWarning(name, "projects.warnHosts", p.Domain, err.Error())
			}
		}
		viaDevServer := project.ResolveWebserver(p) == "devserver"
		if viaDevServer {
			// App-server projects (Next.js, Django, Go…) have no web-server vhost:
			// the only thing that can answer on their .test domain is the
			// front-door proxy. Bring it up so the domain works out of the box.
			if err := a.ensureProxy(); err != nil {
				debugLog("provision %s: front-door failed: %v", name, err)
				a.projectWarning(name, "projects.warnFrontDoor", p.Domain, err.Error())
			}
		}
		if !p.SSL {
			if err := project.SetupProjectSSL(p.Domain); err != nil {
				debugLog("provision %s: ssl failed: %v", name, err)
				a.projectWarning(name, "projects.warnSSL", p.Domain, err.Error())
			} else {
				projects[i].SSL = true
				project.SaveProjects(projects)
				project.SyncLaravelAppURL(projects[i])
			}
		}
		break
	}
	a.regenerateAllVhosts()
}

// ensureProxy installs (if needed) and starts the front-door proxy, and marks
// it enabled so it comes back on the next launch. No-op when already running.
func (a *App) ensureProxy() error {
	if proxy.IsRunning() {
		return nil
	}
	if !proxy.IsInstalled() {
		if err := proxy.Install(); err != nil {
			return err
		}
	}
	return a.startProxyWithVhosts()
}

// startProxyWithVhosts flips ProxyEnabled on, rewrites the web-server vhosts
// so they release :443 to the front-door, then starts it. On failure the flag
// and the vhosts are rolled back so HTTPS keeps working the old way.
func (a *App) startProxyWithVhosts() error {
	cfg := config.Get()
	was := cfg.ProxyEnabled
	cfg.ProxyEnabled = true
	config.Save()
	a.regenerateAllVhosts()
	if err := proxy.Start(); err != nil {
		cfg.ProxyEnabled = was
		config.Save()
		a.regenerateAllVhosts()
		return err
	}
	return nil
}

// projectWarning surfaces a non-fatal provisioning problem to the Projects
// page. key is an i18n key; args fill its {0}, {1}… placeholders (frontend).
func (a *App) projectWarning(name, key string, args ...string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, "project:warning", map[string]interface{}{
		"name": name,
		"key":  key,
		"args": args,
	})
}

// RemoveProject removes a project
func (a *App) RemoveProject(name string) error {
	project.RemoveNginxVhost(name)
	project.RemoveApacheVhost(name)
	project.RemoveFrankenPHPVhost(name)
	tunnel.StopTunnel(name)
	tunnel.StopNamedTunnel(name)
	projects, _ := project.ListProjects()
	for _, p := range projects {
		if p.Name == name {
			project.RemoveHostsEntry(p.Domain)
			// mkcert files are per domain; nothing else references them.
			if p.Domain != "" {
				c, k := project.CertPaths(p.Domain)
				os.Remove(c)
				os.Remove(k)
			}
			break
		}
	}
	if err := project.RemoveProject(name); err != nil {
		return err
	}
	go a.regenerateAllVhosts()
	return nil
}

// DetectFramework detects framework for a path
func (a *App) DetectFramework(path string) string {
	return project.DetectFramework(path)
}

// GetFrameworkCatalog returns every framework DevBox recognises with its
// runtime, app-server flag and default port (drives the Projects UI).
func (a *App) GetFrameworkCatalog() []project.Framework {
	return project.Catalog
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
			if err := project.SaveProjects(projects); err != nil {
				return err
			}
			go a.regenerateAllVhosts()
			return nil
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
			ws := project.ResolveWebserver(p)
			if ws == "" {
				return fmt.Errorf("no web server installed")
			}
			if ws == "devserver" {
				if err := a.ensureProxy(); err != nil {
					return fmt.Errorf("front-door proxy: %w", err)
				}
			}
			if project.SyncLaravelAppURL(p) {
				debugLog("SetupProjectDomain: APP_URL of %s synced to its domain", p.Name)
			}
			a.regenerateAllVhosts()
			return nil
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
			if err := project.SaveProjects(projects); err != nil {
				return err
			}
			project.SyncLaravelAppURL(projects[i])
			a.regenerateAllVhosts()
			return nil
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// GetProjectVhostPath returns the vhost config file path for a project
func (a *App) GetProjectVhostPath(name string) string {
	projects, _ := project.ListProjects()
	for _, p := range projects {
		if p.Name != name {
			continue
		}
		base := filepath.Join(config.GetDataDir(), "services")
		switch project.ResolveWebserver(p) {
		case "nginx":
			return filepath.Join(base, "nginx", "conf", "vhosts", name+".conf")
		case "apache":
			return filepath.Join(base, "apache", "conf", "extra", "vhost-"+name+".conf")
		case "caddy":
			return filepath.Join(base, "caddy", "Caddyfile")
		case "frankenphp":
			return filepath.Join(base, "frankenphp", "vhosts", name+".caddy")
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
		Title:            "Select Parent Folder",
		DefaultDirectory: filepath.Join(config.GetDataDir(), "projects"),
	})
}

// GetDefaultProjectsDir returns <data>/projects — the suggested home for new projects.
func (a *App) GetDefaultProjectsDir() string {
	dir := filepath.Join(config.GetDataDir(), "projects")
	os.MkdirAll(dir, 0755)
	return dir
}

// GetAvailableTemplates returns framework templates with availability info
func (a *App) GetAvailableTemplates() []project.FrameworkTemplate {
	return project.GetTemplates()
}

// ScaffoldNewProject creates a new project from a template
func (a *App) ScaffoldNewProject(templateID, parentDir, name, domain string) error {
	if parentDir == "" {
		parentDir = a.GetDefaultProjectsDir()
	}
	go func() {
		progress := make(chan project.ScaffoldProgress, 10)
		errCh := make(chan error, 1)

		go func() {
			defer close(progress)
			if err := project.ScaffoldProject(templateID, parentDir, name, progress); err != nil {
				errCh <- err
				return
			}
			// Register the project
			if _, err := project.AddProject(filepath.Join(parentDir, name), domain); err != nil {
				errCh <- err
				return
			}
			progress <- project.ScaffoldProgress{Percent: 98, Message: "Registering domain, SSL and vhost..."}
			a.provisionProject(name)
			errCh <- nil
		}()

		// Drain progress fully before the final event (see runRuntimeJob).
		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "scaffold:progress", map[string]interface{}{
				"percent": p.Percent,
				"message": p.Message,
			})
		}

		if err := <-errCh; err != nil {
			wailsRuntime.EventsEmit(a.ctx, "scaffold:error", map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "scaffold:complete", map[string]interface{}{
			"name": name,
			"path": filepath.Join(parentDir, name),
		})
	}()

	return nil
}

// CloneGitProject clones a git repo and registers it as a project
func (a *App) CloneGitProject(gitURL, parentDir, name, domain string) error {
	if parentDir == "" {
		parentDir = a.GetDefaultProjectsDir()
	}
	// Derive actual project name
	actualName := name
	if actualName == "" {
		parts := strings.Split(strings.TrimSuffix(gitURL, "/"), "/")
		actualName = strings.TrimSuffix(parts[len(parts)-1], ".git")
	}

	go func() {
		progress := make(chan project.ScaffoldProgress, 10)
		errCh := make(chan error, 1)

		go func() {
			defer close(progress)
			if err := project.CloneProject(gitURL, parentDir, name, progress); err != nil {
				errCh <- err
				return
			}
			if _, err := project.AddProject(filepath.Join(parentDir, actualName), domain); err != nil {
				errCh <- err
				return
			}
			progress <- project.ScaffoldProgress{Percent: 98, Message: "Registering domain, SSL and vhost..."}
			a.provisionProject(actualName)
			errCh <- nil
		}()

		// Drain progress fully before the final event (see runRuntimeJob).
		for p := range progress {
			wailsRuntime.EventsEmit(a.ctx, "clone:progress", map[string]interface{}{
				"percent": p.Percent,
				"message": p.Message,
			})
		}

		if err := <-errCh; err != nil {
			wailsRuntime.EventsEmit(a.ctx, "clone:error", map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "clone:complete", map[string]interface{}{
			"name": actualName,
			"path": filepath.Join(parentDir, actualName),
		})
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

// SetProjectAutoStart toggles keep-alive for an app-server project: its dev
// server starts with DevBox and is restarted after an unexpected exit.
func (a *App) SetProjectAutoStart(name string, on bool) error {
	projects, err := project.ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].AutoStart = on
			if err := project.SaveProjects(projects); err != nil {
				return err
			}
			if on && project.IsAppServer(p.Framework) && !project.IsDevServerRunning(name) {
				go a.StartDevServer(name)
			}
			return nil
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// autoStartDevServers starts every AUTO project's dev server at launch.
func (a *App) autoStartDevServers() {
	projects, err := project.ListProjects()
	if err != nil {
		return
	}
	for _, p := range projects {
		if !p.AutoStart || !project.IsAppServer(p.Framework) || project.IsDevServerRunning(p.Name) {
			continue
		}
		if err := a.StartDevServer(p.Name); err != nil {
			debugLog("auto-start dev server %s: %v", p.Name, err)
		}
	}
}

// onDevServerExit is the watchdog: it tells the UI a dev server ended and,
// for AUTO projects that crashed, brings it back — at most 3 times in 5
// minutes so a broken build doesn't spin forever.
func (a *App) onDevServerExit(name string, crashed bool) {
	if a.ctx == nil {
		return
	}
	logTail, _ := project.GetDevServerLogs(name, 30)
	wailsRuntime.EventsEmit(a.ctx, "devserver:stopped", map[string]interface{}{
		"name":    name,
		"crashed": crashed,
		"reason":  project.LastMeaningfulLogLine(logTail),
	})
	if !crashed || a.quitting {
		return
	}
	projects, err := project.ListProjects()
	if err != nil {
		return
	}
	var proj *project.Project
	for i := range projects {
		if projects[i].Name == name {
			proj = &projects[i]
			break
		}
	}
	if proj == nil || !proj.AutoStart {
		return
	}

	a.restartMu.Lock()
	if a.restartLog == nil {
		a.restartLog = map[string][]time.Time{}
	}
	now := time.Now()
	var recent []time.Time
	for _, t := range a.restartLog[name] {
		if now.Sub(t) < 5*time.Minute {
			recent = append(recent, t)
		}
	}
	giveUp := len(recent) >= 3
	if !giveUp {
		recent = append(recent, now)
	}
	a.restartLog[name] = recent
	a.restartMu.Unlock()

	if giveUp {
		debugLog("watchdog: %s crashed 3 times in 5 minutes, not restarting", name)
		wailsRuntime.EventsEmit(a.ctx, "devserver:error", map[string]interface{}{
			"name":  name,
			"error": "crashed repeatedly; auto-restart paused — check the dev server log",
		})
		return
	}
	debugLog("watchdog: %s exited unexpectedly, restarting (%d/3)", name, len(recent))
	time.Sleep(2 * time.Second)
	if err := a.StartDevServer(name); err != nil {
		debugLog("watchdog: restart of %s failed: %v", name, err)
	}
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

// SetProjectRuntime overrides a project's runtime (php/node/go/python/rust/static).
// Empty string resets the field — the runtime is then re-derived from Framework.
// Triggers a vhost regen so the front-door + per-webserver configs follow suit.
func (a *App) SetProjectRuntime(name, rt string) error {
	debugLog("SetProjectRuntime: name=%s runtime=%q", name, rt)
	if err := project.SetProjectRuntime(name, rt); err != nil {
		debugLog("SetProjectRuntime ERROR: %v", err)
		return err
	}
	a.regenerateAllVhosts()
	return nil
}

// SetProjectRuntimeVersion pins a project to a specific runtime version. Empty
// string clears the pin, falling back to the globally active version. PHP
// projects get their own php-cgi instance for the pinned version.
func (a *App) SetProjectRuntimeVersion(name, version string) error {
	if err := project.SetProjectRuntimeVersion(name, version); err != nil {
		return err
	}
	a.regenerateAllVhosts()
	return nil
}

// SetProjectWebserver overrides which webserver routes the project's domain
// (nginx/caddy/apache/frankenphp/devserver). Empty = auto, derived from Runtime.
// Triggers a vhost regen so the chosen webserver actually picks up the project.
func (a *App) SetProjectWebserver(name, ws string) error {
	debugLog("SetProjectWebserver: name=%s webserver=%q", name, ws)
	if err := project.SetProjectWebserver(name, ws); err != nil {
		debugLog("SetProjectWebserver ERROR: %v", err)
		return err
	}
	a.regenerateAllVhosts()
	return nil
}

// GetProjectEnvHints lists .env values that still point at localhost and would
// break redirects on the project's domain / tunnel.
func (a *App) GetProjectEnvHints(name string) []project.EnvHint {
	projects, _ := project.ListProjects()
	for _, p := range projects {
		if p.Name == name {
			return project.LocalhostEnvHints(p)
		}
	}
	return nil
}

// FixProjectEnvHints rewrites localhost .env values to the project's domain.
func (a *App) FixProjectEnvHints(name string) (int, error) {
	projects, _ := project.ListProjects()
	for _, p := range projects {
		if p.Name == name {
			return project.FixLocalhostEnv(p)
		}
	}
	return 0, fmt.Errorf("project not found: %s", name)
}

// SetProjectPublicHostname sets the custom-domain hostname used by named tunnels.
func (a *App) SetProjectPublicHostname(name, hostname string) error {
	return project.SetProjectPublicHostname(name, hostname)
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

// OpenProjectFolder opens a project folder in the system file explorer
func (a *App) OpenProjectFolder(path string) error {
	return platform.OpenFolder(path)
}

// --- Database Tools ---

// --- Developer tools (uv, pipx, air, cargo-watch, Redis Commander…) ---

// GetDevTools lists the tool catalog with install/run state.
func (a *App) GetDevTools() []devtools.Status {
	return devtools.List()
}

// InstallDevTool installs a catalog tool in the background.
// Events: devtool:progress {id, message}, devtool:installed {id}, devtool:error {id, error}.
func (a *App) InstallDevTool(id string) error {
	go func() {
		progress := make(chan devtools.Progress, 20)
		done := make(chan struct{})
		go func() {
			for p := range progress {
				wailsRuntime.EventsEmit(a.ctx, "devtool:progress", map[string]interface{}{"id": p.ID, "message": p.Message})
			}
			close(done)
		}()
		err := devtools.Install(id, progress)
		close(progress)
		<-done
		if err != nil {
			wailsRuntime.EventsEmit(a.ctx, "devtool:error", map[string]interface{}{"id": id, "error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "devtool:installed", map[string]interface{}{"id": id})
	}()
	return nil
}

// UninstallDevTool removes a catalog tool.
func (a *App) UninstallDevTool(id string) error {
	return devtools.Uninstall(id)
}

// OpenDevTool starts a web tool (if needed) and opens it in the browser.
func (a *App) OpenDevTool(id string) error {
	url, err := devtools.Start(id)
	if err != nil {
		return err
	}
	wailsRuntime.BrowserOpenURL(a.ctx, url)
	return nil
}

// StopDevTool stops a running web tool.
func (a *App) StopDevTool(id string) error {
	return devtools.Stop(id)
}

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

// IsAdminerServerRunning reports whether the bundled PHP dev server hosting Adminer is up.
func (a *App) IsAdminerServerRunning() bool {
	return tools.IsAdminerServerRunning()
}

// GetAdminerURL returns the loopback URL the running Adminer server is reachable at.
func (a *App) GetAdminerURL() string {
	return tools.GetAdminerServerURL()
}

// OpenAdminer starts the bundled Adminer server (if not already running) using the
// active PHP version's built-in web server, then opens it in the default browser.
// Requires Adminer to be installed and an active PHP runtime to be set as global.
func (a *App) OpenAdminer() error {
	if !tools.IsAdminerInstalled() {
		return fmt.Errorf("Adminer is not installed")
	}
	phpMgr := runtime.NewPHPManager()
	activeVer, _ := phpMgr.GetGlobal()
	if activeVer == "" {
		return fmt.Errorf("no active PHP version — install PHP and set it as global first")
	}
	phpBinDir := phpMgr.BinaryPath(activeVer)
	if err := tools.StartAdminerServer(phpBinDir); err != nil {
		return err
	}
	wailsRuntime.BrowserOpenURL(a.ctx, tools.GetAdminerServerURL())
	return nil
}

// StopAdminerServer kills the bundled Adminer dev server process.
func (a *App) StopAdminerServer() error {
	return tools.StopAdminerServer()
}

// UninstallAdminer stops the server (if running) and removes Adminer files.
func (a *App) UninstallAdminer() error {
	return tools.UninstallAdminer()
}

// --- Front-door Proxy ---

// ProxyStatus is what the frontend renders on the Dashboard.
type ProxyStatus struct {
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
	Enabled   bool `json:"enabled"`
	Port      int  `json:"port"`
	HTTPSPort int  `json:"httpsPort"`
}

// GetProxyStatus reports install/run/enabled state of the front-door proxy.
func (a *App) GetProxyStatus() ProxyStatus {
	cfg := config.Get()
	return ProxyStatus{
		Installed: proxy.IsInstalled(),
		Running:   proxy.IsRunning(),
		Enabled:   cfg.ProxyEnabled,
		Port:      proxy.HTTPPort,
		HTTPSPort: proxy.HTTPSPort,
	}
}

// InstallProxy downloads the bundled Caddy binary used as DevBox's front-door.
func (a *App) InstallProxy() error {
	return proxy.Install()
}

// UninstallProxy stops the proxy and removes its bundled binary + config.
// Also clears the auto-start preference so it doesn't try to come back next launch.
func (a *App) UninstallProxy() error {
	if err := proxy.Uninstall(); err != nil {
		return err
	}
	if cfg := config.Get(); cfg != nil {
		cfg.ProxyEnabled = false
	}
	return config.Save()
}

// StartProxy launches the front-door proxy. Sets ProxyEnabled so DevBox brings
// it back automatically on next launch.
func (a *App) StartProxy() error {
	return a.startProxyWithVhosts()
}

// StopProxy stops the proxy and clears ProxyEnabled — DevBox won't auto-start
// it next launch until the user explicitly clicks Start again.
func (a *App) StopProxy() error {
	if err := proxy.Stop(); err != nil {
		return err
	}
	if cfg := config.Get(); cfg != nil {
		cfg.ProxyEnabled = false
	}
	err := config.Save()
	// Web servers take :443 back for their SSL vhosts.
	a.regenerateAllVhosts()
	return err
}

// ReloadProxy rewrites the Caddyfile from the current project list and asks
// the running proxy to apply it without restart.
func (a *App) ReloadProxy() error {
	return proxy.Reload()
}

// GetProxyLogPath returns the proxy log file path for diagnostics.
func (a *App) GetProxyLogPath() string {
	return proxy.LogPath()
}

// OpenFileInEditor opens a file in the default system editor
func (a *App) OpenFileInEditor(path string) error {
	return platform.OpenFile(path)
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

// StartTunnel exposes a project publicly. Projects with a PublicHostname use
// the linked Cloudflare account (custom domain); everything else gets a
// random *.trycloudflare.com quick tunnel.
func (a *App) StartTunnel(port int, projectName string, domain string, ssl bool) error {
	projects, _ := project.ListProjects()
	for _, p := range projects {
		if p.Name != projectName || p.PublicHostname == "" {
			continue
		}
		if !tunnel.GetCloudflareStatus().Configured {
			return fmt.Errorf("%s has a custom hostname but no Cloudflare account is linked — add your API token in Settings, or clear the hostname to use a quick tunnel", projectName)
		}
		origin := fmt.Sprintf("http://127.0.0.1:%d", port)
		if ssl {
			origin = "https://127.0.0.1:443"
		}
		if err := tunnel.StartNamedTunnel(projectName, p.PublicHostname, origin, "", ssl); err != nil {
			return err
		}
		a.regenerateAllVhosts()
		return nil
	}
	return tunnel.StartTunnel(port, projectName, domain, ssl)
}


// StopTunnel stops the tunnel for a specific project (quick or custom-domain).
func (a *App) StopTunnel(projectName string) error {
	if err := tunnel.StopTunnel(projectName); err != nil {
		return err
	}
	if err := tunnel.StopNamedTunnel(projectName); err != nil {
		return err
	}
	go a.regenerateAllVhosts()
	return nil
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

// GetCloudflareStatus summarises the linked account and connector state.
func (a *App) GetCloudflareStatus() tunnel.CloudflareStatus {
	return tunnel.GetCloudflareStatus()
}

// CloudflareVerifyResult is what the Settings page uses to offer account/zone pickers.
type CloudflareVerifyResult struct {
	Accounts []tunnel.CFAccount `json:"accounts"`
	Zones    []tunnel.CFZone    `json:"zones"`
}

// VerifyCloudflareToken validates an API token and lists what it can manage.
func (a *App) VerifyCloudflareToken(token string) (CloudflareVerifyResult, error) {
	accounts, zones, err := tunnel.VerifyCloudflareToken(token)
	if err != nil {
		return CloudflareVerifyResult{}, err
	}
	sort.Slice(zones, func(i, j int) bool { return zones[i].Name < zones[j].Name })
	return CloudflareVerifyResult{Accounts: accounts, Zones: zones}, nil
}

// ConfigureCloudflare links the account/zone and creates this machine's tunnel.
func (a *App) ConfigureCloudflare(token, accountID, accountName, zoneID, zoneName string) error {
	return tunnel.ConfigureCloudflare(token, accountID, accountName, zoneID, zoneName)
}

// DisconnectCloudflare removes all custom-domain routes and forgets the token.
func (a *App) DisconnectCloudflare() error {
	return tunnel.DisconnectCloudflare()
}
