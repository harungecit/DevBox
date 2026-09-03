package main

import (
	"fmt"
	"sort"
	"strings"

	"DevBox/internal/config"
	"DevBox/internal/project"
	"DevBox/internal/runtime"
	"DevBox/internal/vfox"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// --- Runtime catalog & vfox plugin bindings ---
//
// Runtime identity comes from runtime.Names() (built-ins first, then plugin
// runtimes). GetRuntimeCatalog is the single source the frontend uses for
// tabs, labels, logos and counts — no page hard-codes runtime names.

// RuntimeMeta describes one runtime for the UI.
type RuntimeMeta struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	BuiltIn     bool   `json:"builtIn"`
	Plugin      bool   `json:"plugin"`
	// Kind is "language" (offered as a project runtime) or "tool".
	Kind string `json:"kind"`
	// Plugin details (empty for built-ins)
	Description   string   `json:"description"`
	Homepage      string   `json:"homepage"`
	License       string   `json:"license"`
	PluginVersion string   `json:"pluginVersion"`
	PluginUpdate  string   `json:"pluginUpdate"` // newer plugin version, "" if none known
	ThirdParty    bool     `json:"thirdParty"`
	Notes         []string `json:"notes"`
	// Install state
	Installed int               `json:"installed"`
	Global    string            `json:"global"`
	EnvVars   map[string]string `json:"envVars"` // variables applied for Global (plugins)
}

// pluginUpdates caches CheckUpdate results so the catalog stays network-free.
var pluginUpdates = map[string]string{}

// GetRuntimeCatalog lists every registered runtime with its install state.
func (a *App) GetRuntimeCatalog() []RuntimeMeta {
	var out []RuntimeMeta
	for _, name := range runtime.Names() {
		mgr := runtime.Registry[name]
		installed, _ := mgr.ListInstalled()
		global, _ := mgr.GetGlobal()
		meta := RuntimeMeta{
			Name:        name,
			DisplayName: runtime.DisplayName(name),
			Kind:        "language",
			Installed:   len(installed),
			Global:      global,
			Notes:       []string{},
			EnvVars:     map[string]string{},
		}
		if rec, ok := runtime.PluginInfo(name); ok {
			meta.Plugin = true
			meta.Description = rec.Description
			meta.Homepage = rec.Homepage
			meta.License = rec.License
			meta.PluginVersion = rec.Version
			meta.ThirdParty = rec.ThirdParty
			meta.PluginUpdate = pluginUpdates[name]
			if rec.Notes != nil {
				meta.Notes = rec.Notes
			}
			if runtime.ToolPlugins[name] {
				meta.Kind = "tool"
			}
			if global != "" {
				if vars := runtime.ActivationVars(mgr, global); vars != nil {
					meta.EnvVars = vars
				}
			}
		} else {
			meta.BuiltIn = true
		}
		out = append(out, meta)
	}
	if out == nil {
		out = []RuntimeMeta{}
	}
	return out
}

// ManagedEnvVar is one environment variable DevBox wrote for a runtime.
type ManagedEnvVar struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Runtime string `json:"runtime"`
	Version string `json:"version"`
}

// GetManagedEnv lists the user environment variables DevBox manages (Path page).
func (a *App) GetManagedEnv() []ManagedEnvVar {
	cfg := config.Get()
	out := make([]ManagedEnvVar, 0, len(cfg.ManagedEnv))
	for k, e := range cfg.ManagedEnv {
		out = append(out, ManagedEnvVar{Key: k, Value: e.Value, Runtime: e.Runtime, Version: e.Version})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// RegistryEntry is one row of the "Add runtime" catalog.
type RegistryEntry struct {
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	Desc             string `json:"desc"`
	Homepage         string `json:"homepage"`
	Kind             string `json:"kind"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion"`
	UpdateAvailable  string `json:"updateAvailable"`
	BuiltIn          bool   `json:"builtIn"`
	BuiltInAs        string `json:"builtInAs"`
	ThirdParty       bool   `json:"thirdParty"`
}

// VfoxRegistryResult bundles the registry list with cache metadata.
type VfoxRegistryResult struct {
	Entries   []RegistryEntry `json:"entries"`
	FetchedAt string          `json:"fetchedAt"`
	Registry  string          `json:"registry"`
}

// GetVfoxRegistry returns the plugin registry merged with local state.
// force re-fetches the index (Refresh button).
func (a *App) GetVfoxRegistry(force bool) (VfoxRegistryResult, error) {
	items, fetchedAt, err := vfox.FetchIndex(force)
	if err != nil {
		return VfoxRegistryResult{}, err
	}
	installed, _ := vfox.ListInstalled()
	byName := map[string]vfox.InstalledPlugin{}
	for _, p := range installed {
		byName[p.Name] = p
	}
	res := VfoxRegistryResult{Registry: vfox.RegistryBase()}
	if !fetchedAt.IsZero() {
		res.FetchedAt = fetchedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.Name] = true
		e := RegistryEntry{
			Name:        it.Name,
			DisplayName: runtime.DisplayName(it.Name),
			Desc:        it.Desc,
			Homepage:    it.Homepage,
			Kind:        "language",
		}
		if runtime.ToolPlugins[it.Name] {
			e.Kind = "tool"
		}
		if alias, ok := vfox.BuiltinAliases[strings.ToLower(it.Name)]; ok {
			e.BuiltIn = true
			e.BuiltInAs = alias
		}
		if p, ok := byName[it.Name]; ok {
			e.Installed = true
			e.InstalledVersion = p.Version
			e.UpdateAvailable = pluginUpdates[it.Name]
			e.ThirdParty = p.ThirdParty
		}
		res.Entries = append(res.Entries, e)
	}
	// Plugins installed from a URL or dropped in by hand are not in the index.
	for _, p := range installed {
		if seen[p.Name] {
			continue
		}
		e := RegistryEntry{
			Name: p.Name, DisplayName: runtime.DisplayName(p.Name), Desc: p.Description, Homepage: p.Homepage,
			Kind: "language", Installed: true, InstalledVersion: p.Version, ThirdParty: true,
			UpdateAvailable: pluginUpdates[p.Name],
		}
		if runtime.ToolPlugins[p.Name] {
			e.Kind = "tool"
		}
		res.Entries = append(res.Entries, e)
	}
	if res.Entries == nil {
		res.Entries = []RegistryEntry{}
	}
	return res, nil
}

// GetVfoxPluginManifest fetches a plugin's manifest (version, license, notes)
// so the catalog can show details before installing.
func (a *App) GetVfoxPluginManifest(name string) (*vfox.Manifest, error) {
	return vfox.FetchManifest(name)
}

// runPluginJob mirrors runRuntimeJob for plugin management: plugin:progress
// while it runs, then plugin:installed / plugin:error once every progress
// event has been forwarded. `after` runs on success (registration etc.).
func (a *App) runPluginJob(name, action string, job func(progress func(string)) error, after func() (map[string]interface{}, error)) {
	go func() {
		progress := make(chan string, 10)
		errCh := make(chan error, 1)
		go func() {
			defer close(progress)
			defer func() {
				if r := recover(); r != nil {
					debugLog("plugin job PANIC: %v", r)
					errCh <- fmt.Errorf("internal error: %v", r)
				}
			}()
			errCh <- job(func(msg string) { progress <- msg })
		}()
		for msg := range progress {
			wailsRuntime.EventsEmit(a.ctx, "plugin:progress", map[string]interface{}{
				"name": name, "action": action, "message": msg,
			})
		}
		err := <-errCh
		payload := map[string]interface{}{"name": name, "action": action}
		if err == nil && after != nil {
			var extra map[string]interface{}
			extra, err = after()
			for k, v := range extra {
				payload[k] = v
			}
		}
		if err != nil {
			wailsRuntime.EventsEmit(a.ctx, "plugin:error", map[string]interface{}{
				"name": name, "action": action, "error": err.Error(),
			})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "plugin:installed", payload)
	}()
}

func (a *App) emitRuntimesChanged() {
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "runtimes:changed", nil)
	}
}

// afterPluginInstalled registers the manager, tells the UI, and warms the
// version cache so the new tab opens with a list.
func (a *App) afterPluginInstalled(name string) (map[string]interface{}, error) {
	if err := runtime.RegisterPlugin(name); err != nil {
		return nil, err
	}
	delete(pluginUpdates, name)
	a.emitRuntimesChanged()
	go func() {
		if _, _, err := runtime.ListRemoteCached(name, true); err != nil {
			debugLog("plugin %s version list: %v", name, err)
		}
		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "versions:refreshed", nil)
		}
	}()
	return map[string]interface{}{"runtime": name}, nil
}

// InstallVfoxPlugin installs a plugin from the registry (fire-and-forget).
func (a *App) InstallVfoxPlugin(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if alias, ok := vfox.BuiltinAliases[strings.ToLower(name)]; ok {
		return fmt.Errorf("%s is built into DevBox as %s", name, runtime.DisplayName(alias))
	}
	if _, ok := runtime.Registry[name]; ok {
		return fmt.Errorf("%s is already installed", runtime.DisplayName(name))
	}
	a.runPluginJob(name, "install", func(progress func(string)) error {
		_, err := vfox.Install(name, "", progress)
		return err
	}, func() (map[string]interface{}, error) { return a.afterPluginInstalled(name) })
	return nil
}

// InstallVfoxPluginFromURL installs a plugin from a zip/manifest/GitHub URL
// or a local path. The plugin's real name is only known afterwards, so
// progress events are keyed by the source until then.
func (a *App) InstallVfoxPluginFromURL(source string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return fmt.Errorf("plugin source is required")
	}
	var installedName string
	a.runPluginJob(source, "install", func(progress func(string)) error {
		rec, err := vfox.Install("", source, progress)
		if err != nil {
			return err
		}
		installedName = rec.Name
		return nil
	}, func() (map[string]interface{}, error) {
		extra, err := a.afterPluginInstalled(installedName)
		if extra == nil {
			extra = map[string]interface{}{}
		}
		extra["installedName"] = installedName
		return extra, err
	})
	return nil
}

// UpdateVfoxPlugin re-installs a plugin from its origin when newer.
func (a *App) UpdateVfoxPlugin(name string) error {
	if !runtime.IsPluginRuntime(name) {
		return fmt.Errorf("%s is not a plugin runtime", name)
	}
	a.runPluginJob(name, "update", func(progress func(string)) error {
		updated, err := vfox.Update(name, progress)
		if err != nil {
			return err
		}
		if !updated {
			progress("Plugin is already up to date")
		}
		return nil
	}, func() (map[string]interface{}, error) {
		runtime.UnregisterPlugin(name)
		extra, err := a.afterPluginInstalled(name)
		return extra, err
	})
	return nil
}

// CheckVfoxPluginUpdates queries each installed plugin's origin for a newer
// version and returns name → latest for those that have one.
func (a *App) CheckVfoxPluginUpdates() map[string]string {
	installed, _ := vfox.ListInstalled()
	out := map[string]string{}
	for _, p := range installed {
		latest, err := vfox.CheckUpdate(p.Name)
		if err != nil {
			debugLog("plugin update check %s: %v", p.Name, err)
			continue
		}
		if latest != "" {
			out[p.Name] = latest
			pluginUpdates[p.Name] = latest
		} else {
			delete(pluginUpdates, p.Name)
		}
	}
	if len(out) > 0 {
		a.emitRuntimesChanged()
	}
	return out
}

// RemoveVfoxPlugin removes a plugin. With force, its installed versions are
// uninstalled first (PATH and variables cleaned); otherwise it refuses while
// versions exist.
func (a *App) RemoveVfoxPlugin(name string, force bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	mgr, ok := runtime.Registry[name]
	if !ok || !runtime.IsPluginRuntime(name) {
		return fmt.Errorf("%s is not a plugin runtime", name)
	}
	installed, _ := mgr.ListInstalled()
	if len(installed) > 0 {
		if !force {
			return fmt.Errorf("%d installed version(s) of %s still exist", len(installed), runtime.DisplayName(name))
		}
		global, _ := mgr.GetGlobal()
		for _, v := range installed {
			if v.Number == global {
				a.deactivateRuntimeEnv(mgr, v.Number)
			}
			if err := mgr.Uninstall(v.Number); err != nil {
				return fmt.Errorf("uninstall %s %s: %w", name, v.Number, err)
			}
		}
	}
	// Anything DevBox ever wrote for this runtime goes away with it.
	runtime.ClearManagedVars(name, "")
	cfg := config.Get()
	delete(cfg.ActiveRuntimes, name)
	config.Save()

	runtime.UnregisterPlugin(name)
	delete(pluginUpdates, name)
	if err := vfox.Remove(name, true); err != nil {
		return err
	}
	a.emitRuntimesChanged()
	go a.regenerateAllVhosts()
	return nil
}

// DetectLegacyRuntimeVersion asks the project's runtime plugin whether a
// legacy version file (.nvmrc, .java-version, .tool-versions…) in the project
// names a version, so the Projects page can offer to pin it.
func (a *App) DetectLegacyRuntimeVersion(projectName string) string {
	projects, err := project.ListProjects()
	if err != nil {
		return ""
	}
	for _, p := range projects {
		if p.Name != projectName {
			continue
		}
		if pm, err := runtime.PluginManagerFor(p.Runtime); err == nil {
			if v, ok := pm.ParseLegacyFile(p.Path); ok {
				return v
			}
		}
	}
	return ""
}
