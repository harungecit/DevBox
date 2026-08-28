package project

import "DevBox/internal/service"

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

	switch p.Runtime {
	case "node", "go", "python", "rust":
		return "devserver"
	}

	for _, name := range managedWebservers {
		if mgr, ok := service.Registry[name]; ok && mgr.IsInstalled() {
			return name
		}
	}
	return ""
}
