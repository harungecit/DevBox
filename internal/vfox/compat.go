// Package vfox runs vfox plugins (https://vfox.dev) inside DevBox: small Lua
// scripts that know where to find, download and configure a runtime. It
// reproduces vfox's plugin contract — hooks, globals, standard modules, install
// layout — so the community registry (https://github.com/version-fox/vfox-plugins)
// works unchanged. See THIRD_PARTY_NOTICES.md for the ported pieces.
package vfox

import (
	"fmt"
	goruntime "runtime"
	"strings"
)

// CompatVersion is the vfox runtime version DevBox claims to plugins via
// RUNTIME.version. Plugins compare it against their metadata's
// minRuntimeVersion; it tracks the vfox release whose behaviour we mirror.
const CompatVersion = "1.0.11"

// AppVersion is DevBox's own version, appended to the User-Agent. Set by the
// app at startup (updater.Version); the default only matters in tests.
var AppVersion = "0.0.0-dev"

// BuiltinAliases maps registry plugin names to the DevBox runtime that already
// covers them. Those plugins are hidden from the catalog and refused on
// install, because the hand-written managers carry integrations (php-cgi,
// extensions, npm/pip migration) a plugin cannot provide.
var BuiltinAliases = map[string]string{
	"golang": "go",
	"nodejs": "node",
	"php":    "php",
	"python": "python",
	"rust":   "rust",
}

// IsBuiltinAlias reports whether a plugin name duplicates a built-in runtime.
func IsBuiltinAlias(name string) bool {
	_, ok := BuiltinAliases[strings.ToLower(name)]
	return ok
}

// OSType is the value vfox plugins read from RUNTIME.osType / OS_TYPE.
func OSType() string {
	return goruntime.GOOS // "windows" | "darwin" | "linux"
}

// ArchType is the value vfox plugins read from RUNTIME.archType / ARCH_TYPE.
func ArchType() string {
	return goruntime.GOARCH // "amd64" | "arm64" | ...
}

// UserAgent mirrors vfox's "vfox/<rv> vfox-<plugin>/<pv>" so mirrors that
// sniff it behave the same, with DevBox appended for transparency.
func UserAgent(pluginName, pluginVersion string) string {
	parts := []string{"vfox/" + CompatVersion}
	if pluginName != "" {
		name := pluginName
		if !strings.HasPrefix(name, "vfox-") {
			name = "vfox-" + name
		}
		if pluginVersion != "" {
			parts = append(parts, name+"/"+pluginVersion)
		} else {
			parts = append(parts, name)
		}
	}
	parts = append(parts, "DevBox/"+AppVersion)
	return strings.Join(parts, " ")
}

// pluginError prefixes hook errors with the plugin name, like vfox does.
func pluginError(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("plugin [%s]: %w", name, err)
}
