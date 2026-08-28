package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"DevBox/internal/config"
)

// versionCache is the on-disk snapshot of a runtime's remote version list.
// Fetching lists means scraping/hitting upstream APIs (nodejs.org, go.dev,
// windows.php.net, python.org, GitHub) — slow and rate-limited — so the UI reads
// this file and a background refresher keeps it fresh (config.VersionCacheHours).
type versionCache struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Versions  []Version `json:"versions"`
}

func cacheFile(name string) string {
	return filepath.Join(config.GetDataDir(), "cache", "runtime-"+name+".json")
}

// CacheTTL is how long a cached list stays fresh.
func CacheTTL() time.Duration {
	h := config.Get().VersionCacheHours
	if h <= 0 {
		h = 48
	}
	return time.Duration(h) * time.Hour
}

func readCache(name string) (*versionCache, bool) {
	data, err := os.ReadFile(cacheFile(name))
	if err != nil {
		return nil, false
	}
	var c versionCache
	if json.Unmarshal(data, &c) != nil || len(c.Versions) == 0 {
		return nil, false
	}
	return &c, true
}

func writeCache(name string, versions []Version) {
	os.MkdirAll(filepath.Dir(cacheFile(name)), 0755)
	data, err := json.MarshalIndent(versionCache{FetchedAt: time.Now(), Versions: versions}, "", "  ")
	if err == nil {
		os.WriteFile(cacheFile(name), data, 0644)
	}
}

// CacheFetchedAt reports when the list was last fetched (zero time if never).
func CacheFetchedAt(name string) time.Time {
	if c, ok := readCache(name); ok {
		return c.FetchedAt
	}
	return time.Time{}
}

// IsCacheStale is true when there is no cache or it is older than the TTL.
func IsCacheStale(name string) bool {
	c, ok := readCache(name)
	return !ok || time.Since(c.FetchedAt) > CacheTTL()
}

// ListRemoteCached returns the remote version list for a runtime, served from
// cache when fresh. force bypasses the cache; a failed fetch falls back to a
// stale cache rather than an empty list so the page is never blank offline.
// The second return value reports whether the result came from cache.
func ListRemoteCached(name string, force bool) ([]Version, bool, error) {
	mgr, ok := Registry[name]
	if !ok {
		return nil, false, nil
	}
	if !force {
		if c, ok := readCache(name); ok && time.Since(c.FetchedAt) < CacheTTL() {
			return withCurrent(name, c.Versions), true, nil
		}
	}
	versions, err := mgr.ListRemote()
	if err != nil {
		if c, ok := readCache(name); ok {
			return withCurrent(name, c.Versions), true, nil
		}
		return nil, false, err
	}
	writeCache(name, versions)
	return versions, false, nil
}

// withCurrent re-derives the Current flag (the global version may have changed
// since the list was cached).
func withCurrent(name string, versions []Version) []Version {
	global, _ := getGlobalVersion(name)
	out := make([]Version, len(versions))
	for i, v := range versions {
		v.Current = v.Number == global
		out[i] = v
	}
	return out
}

// UpdateLine returns the key of the release line within which a newer version
// is treated as an in-place update rather than a separate install:
//   - node, rust: major (24.x.x → 24.y.z; Rust 1.x)
//   - go, php, python: major.minor (1.26.x, 8.4.x, 3.13.x)
func UpdateLine(name, version string) string {
	parts := strings.Split(version, ".")
	switch name {
	case "node", "rust":
		return parts[0]
	}
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return parts[0]
}

// updateCandidate reports whether a remote version may be offered as an update
// (pre-releases are never offered).
func updateCandidate(name string, v Version) bool {
	switch name {
	case "go", "rust":
		return v.Stable
	}
	// node: Stable means LTS; non-LTS releases within the same major are still
	// regular releases. php/python lists only contain stable versions.
	return true
}

// RuntimeUpdate pairs an installed version with the newest version of its line.
type RuntimeUpdate struct {
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
}

// FindUpdates lists installed versions that have a newer release in their line.
func FindUpdates(name string, remote []Version) []RuntimeUpdate {
	mgr, ok := Registry[name]
	if !ok {
		return nil
	}
	installed, err := mgr.ListInstalled()
	if err != nil {
		return nil
	}
	var out []RuntimeUpdate
	for _, inst := range installed {
		line := UpdateLine(name, inst.Number)
		best := ""
		for _, r := range remote {
			if !updateCandidate(name, r) || UpdateLine(name, r.Number) != line {
				continue
			}
			if compareVersions(r.Number, inst.Number) > 0 && (best == "" || compareVersions(r.Number, best) > 0) {
				best = r.Number
			}
		}
		if best != "" {
			out = append(out, RuntimeUpdate{Installed: inst.Number, Latest: best})
		}
	}
	return out
}

// UpdateTarget returns the installed version that remoteVersion would replace
// (the highest installed version in the same line that is older), or "".
func UpdateTarget(name, remoteVersion string, installed []Version) string {
	line := UpdateLine(name, remoteVersion)
	best := ""
	for _, inst := range installed {
		if inst.Number == remoteVersion || UpdateLine(name, inst.Number) != line {
			continue
		}
		if compareVersions(remoteVersion, inst.Number) > 0 && (best == "" || compareVersions(inst.Number, best) > 0) {
			best = inst.Number
		}
	}
	return best
}
