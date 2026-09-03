package project

import (
	"DevBox/internal/runtime"
	"DevBox/internal/service"
)

// IsAppServerRuntime reports whether a project serves itself through a dev
// server: the built-in app-server runtimes always do; PHP/static never; a
// plugin runtime (java, ruby, dotnet...) only when the project can actually
// be started — it has a custom start command or an app-server framework.
func IsAppServerRuntime(p Project) bool {
	switch p.Runtime {
	case "node", "go", "python", "rust":
		return true
	case "", "php", "static":
		return false
	}
	return p.StartCommand != "" || IsAppServer(p.Framework)
}

// LegacyRuntimeVersion asks a plugin runtime whether a legacy version file in
// dir (.nvmrc, .java-version, .tool-versions...) names a version.
func LegacyRuntimeVersion(rt, dir string) (string, bool) {
	pm, err := runtime.PluginManagerFor(rt)
	if err != nil {
		return "", false
	}
	return pm.ParseLegacyFile(dir)
}

// managedWebservers is the preference order DevBox uses when resolving "auto"
// for PHP / static projects: pick the first installed webserver in this list.
var managedWebservers = []string{"nginx", "caddy", "apache", "frankenphp"}

// ResolveWebserver returns the concrete webserver name that should serve a
// project, given its Runtime + Webserver fields and the current install state.
//
// Possible return values:
//   - "nginx" / "caddy" / "apache" / "frankenphp" — managed webserver service
//   - "devserver" — the project runs its own HTTP server (Node, Go, etc.)
//   - ""         — no backend is reachable for this project
//
// Resolution order:
//  1. Explicit `Webserver` choice — used if the named service is installed.
//  2. App-server runtimes (node/go/python/rust) → "devserver".
//  3. PHP / static / unknown runtime → first installed managed webserver.
//
// "auto" and empty string are treated identically.
func ResolveWebserver(p Project) string {
	ws := p.Webserver
	if ws != "" && ws != "auto" {
		if ws == "devserver" {
			return "devserver"
		}
		if mgr, ok := service.Registry[ws]; ok && mgr.IsInstalled() {
			return ws
		}
		// Explicit pick wasn't installed — fall through to defaults so the
		// project still has *some* backend instead of going dark.
	}

	if IsAppServerRuntime(p) {
		return "devserver"
	}

	for _, name := range managedWebservers {
		if mgr, ok := service.Registry[name]; ok && mgr.IsInstalled() {
			return name
		}
	}
	return ""
}
