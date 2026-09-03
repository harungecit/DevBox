package main

import (
	"fmt"
	"sync"
	"time"

	"DevBox/internal/discovery"
	"DevBox/internal/pathenv"
	"DevBox/internal/runtime"
	"DevBox/internal/service"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Discovery scan results are cached briefly so the Runtimes and Services
// pages mounting back-to-back don't both pay for a full probe run.
var (
	discoveryMu    sync.Mutex
	discoveryCache []discovery.Found
	discoveryAt    time.Time
)

const discoveryCacheTTL = 5 * time.Minute

// ScanExternalSoftware scans the machine for runtimes and services installed
// outside DevBox (system installers, package managers, manual unzips) that
// can be imported into DevBox management. Results are cached for a few
// minutes; pass force to re-probe.
func (a *App) ScanExternalSoftware(force bool) []discovery.Found {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()

	if !force && discoveryCache != nil && time.Since(discoveryAt) < discoveryCacheTTL {
		return filterCurrentlyInstalled(discoveryCache)
	}
	results := discovery.ScanAll()
	if results == nil {
		results = []discovery.Found{}
	}
	discoveryCache = results
	discoveryAt = time.Now()
	debugLog("discovery scan: %d external installation(s) found", len(results))
	return filterCurrentlyInstalled(results)
}

// filterCurrentlyInstalled re-applies "is this already managed by DevBox?"
// against the *current* registry state, so cached scan results never offer
// something that was installed or imported since the scan; conflict info is
// refreshed the same way.
func filterCurrentlyInstalled(items []discovery.Found) []discovery.Found {
	out := make([]discovery.Found, 0, len(items))
	for _, f := range items {
		switch f.Kind {
		case "runtime":
			if mgr, ok := runtime.Registry[f.Name]; ok {
				installed, _ := mgr.ListInstalled()
				dup := false
				for _, v := range installed {
					if v.Number == f.Version {
						dup = true
						break
					}
				}
				if dup {
					continue
				}
			}
		case "service":
			if mgr, ok := service.Registry[f.Name]; ok && mgr.IsInstalled() {
				continue
			}
			f.Conflict = service.GetConflictingService(f.Name)
		case "tool":
			if f.Name == "composer" && runtime.IsComposerInstalled() {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

func invalidateDiscoveryCache() {
	discoveryMu.Lock()
	discoveryCache = nil
	discoveryMu.Unlock()
}

// ImportExternalRuntime imports an external runtime installation found by
// ScanExternalSoftware into DevBox's managed layout. Reuses the standard
// runtime:progress / runtime:installed / runtime:error events, so the UI
// behaves exactly like a normal install.
func (a *App) ImportExternalRuntime(name, path, version string) error {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return fmt.Errorf("unknown runtime: %s", name)
	}

	a.runRuntimeJob(name, version, func(progress chan<- runtime.Progress) error {
		return runtime.ImportExternal(name, path, version, progress)
	}, func() map[string]interface{} {
		// Same convenience as InstallRuntime: the first version becomes
		// the global one and lands on PATH.
		global, _ := mgr.GetGlobal()
		if global == "" {
			mgr.SetGlobal(version)
			pathenv.AddToPath(mgr.BinaryPath(version))
		}
		invalidateDiscoveryCache()
		return map[string]interface{}{"imported": true}
	})
	return nil
}

// ImportExternalService imports an external service installation found by
// ScanExternalSoftware into DevBox's managed layout. Reuses the standard
// service:progress / service:installed / service:error events.
func (a *App) ImportExternalService(name, path, version string) error {
	mgr, ok := service.Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	if conflict := service.GetConflictingService(name); conflict != "" {
		return fmt.Errorf("%s is already installed. Uninstall it before importing %s", conflict, mgr.DisplayName())
	}

	a.runServiceJob(name, func(progress chan<- service.Progress) error {
		return service.ImportExternal(name, path, version, progress)
	}, func() {
		invalidateDiscoveryCache()
		// A newly imported webserver should pick up existing projects.
		if name == "nginx" || name == "apache" || name == "caddy" {
			a.regenerateAllVhosts()
		}
	})
	return nil
}

// ImportExternalTool brings a developer tool found by ScanExternalSoftware
// under DevBox management in place. Composer is the only tool today: DevBox
// records the phar's location and runs it through its own wrapper with the
// active PHP — nothing is copied. Emits composer:installed on success.
func (a *App) ImportExternalTool(name, path string) error {
	switch name {
	case "composer":
		if err := runtime.ImportComposer(path); err != nil {
			return err
		}
		pathenv.AddToPath(runtime.ComposerDir())
		invalidateDiscoveryCache()
		wailsRuntime.EventsEmit(a.ctx, "composer:installed", map[string]interface{}{"imported": true})
		return nil
	}
	return fmt.Errorf("unknown tool: %s", name)
}
