package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"DevBox/internal/config"
)

// --- Remote version cache ---

type cachedVersion struct {
	Version string `json:"version"`
	Label   string `json:"label"`
	URL     string `json:"url"`
}

type serviceVersionCache struct {
	FetchedAt time.Time       `json:"fetchedAt"`
	Versions  []cachedVersion `json:"versions"`
}

func serviceCacheFile(name string) string {
	return filepath.Join(config.GetDataDir(), "cache", "service-"+name+".json")
}

func cacheTTL() time.Duration {
	h := config.Get().VersionCacheHours
	if h <= 0 {
		h = 48
	}
	return time.Duration(h) * time.Hour
}

func readServiceCache(name string) (*serviceVersionCache, bool) {
	data, err := os.ReadFile(serviceCacheFile(name))
	if err != nil {
		return nil, false
	}
	var c serviceVersionCache
	if json.Unmarshal(data, &c) != nil || len(c.Versions) == 0 {
		return nil, false
	}
	return &c, true
}

func writeServiceCache(name string, versions []AvailableVersion) {
	c := serviceVersionCache{FetchedAt: time.Now()}
	for _, v := range versions {
		c.Versions = append(c.Versions, cachedVersion{Version: v.Version, Label: v.Label, URL: v.URL})
	}
	os.MkdirAll(filepath.Dir(serviceCacheFile(name)), 0755)
	if data, err := json.MarshalIndent(c, "", "  "); err == nil {
		os.WriteFile(serviceCacheFile(name), data, 0644)
	}
}

func fromCache(c *serviceVersionCache) []AvailableVersion {
	out := make([]AvailableVersion, 0, len(c.Versions))
	for _, v := range c.Versions {
		out = append(out, AvailableVersion{Version: v.Version, Label: v.Label, URL: v.URL})
	}
	return out
}

// IsServiceCacheStale is true when there is no cached list or it expired.
func IsServiceCacheStale(name string) bool {
	c, ok := readServiceCache(name)
	return !ok || time.Since(c.FetchedAt) > cacheTTL()
}

// ListVersionsCached returns a service's installable versions, from cache when
// fresh. force re-fetches; a failed fetch falls back to the stale cache.
func ListVersionsCached(name string, force bool) ([]AvailableVersion, bool, error) {
	mgr, ok := Registry[name]
	if !ok {
		return nil, false, fmt.Errorf("unknown service: %s", name)
	}
	if !force {
		if c, ok := readServiceCache(name); ok && time.Since(c.FetchedAt) < cacheTTL() {
			return fromCache(c), true, nil
		}
	}
	versions, err := mgr.ListVersions()
	if err != nil || len(versions) == 0 {
		if c, ok := readServiceCache(name); ok {
			return fromCache(c), true, nil
		}
		return versions, false, err
	}
	writeServiceCache(name, versions)
	return versions, false, nil
}

// --- Update detection ---

// UpdateInfo describes what a newer release means for an installed service.
type UpdateInfo struct {
	// Latest is the newest version that can replace the installed one in place
	// (data directory kept). Empty when up to date.
	Latest string `json:"latest"`
	// LatestMajor is a newer release line that is NOT an in-place update for
	// this service (databases whose on-disk format changes between majors).
	LatestMajor string `json:"latestMajor"`
}

// dataFormatBoundToMajor lists services whose data directory is tied to the
// major version — in-place updates are restricted to the same major line.
var dataFormatBoundToMajor = map[string]bool{
	"postgres": true,
	"mysql":    true,
	"mariadb":  true,
	"mongodb":  true,
}

func updateLine(name, version string) string {
	if !dataFormatBoundToMajor[name] {
		return "*"
	}
	return strconv.Itoa(majorVersion(version))
}

// CheckUpdate compares the installed version with the cached version list.
// It never touches the network — the background refresher keeps caches warm.
func CheckUpdate(name string) UpdateInfo {
	mgr, ok := Registry[name]
	if !ok || !mgr.IsInstalled() {
		return UpdateInfo{}
	}
	installed := mgr.Version()
	if installed == "" || installed == "-" {
		return UpdateInfo{}
	}
	c, ok := readServiceCache(name)
	if !ok {
		return UpdateInfo{}
	}
	line := updateLine(name, installed)
	var info UpdateInfo
	for _, v := range c.Versions {
		if compareVer(v.Version, installed) <= 0 {
			continue
		}
		if updateLine(name, v.Version) == line {
			if info.Latest == "" || compareVer(v.Version, info.Latest) > 0 {
				info.Latest = v.Version
			}
		} else if info.LatestMajor == "" || compareVer(v.Version, info.LatestMajor) > 0 {
			info.LatestMajor = v.Version
		}
	}
	return info
}

// compareVer compares dotted numeric versions: >0 if a > b.
func compareVer(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(strings.TrimFunc(pa[i], func(r rune) bool { return r < '0' || r > '9' }))
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(strings.TrimFunc(pb[i], func(r rune) bool { return r < '0' || r > '9' }))
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// --- In-place update ---

// preservedPaths lists, per service, the files/dirs (relative to the service
// base dir) that hold user data or DevBox-managed config and must survive a
// reinstall. Everything else is replaced by the new release.
func preservedPaths(name string) []string {
	switch name {
	case "postgres":
		return []string{"data"}
	case "mysql", "mariadb":
		return []string{"data", MysqlConfigName()}
	case "mongodb":
		return []string{"data", "mongod.cfg"}
	case "redis", "valkey":
		return []string{"data", name + ".conf"}
	case "mailpit":
		return []string{"data"}
	case "nginx":
		return []string{filepath.Join("conf", "vhosts"), filepath.Join("conf", "nginx.conf"), "html"}
	case "apache":
		return []string{filepath.Join("conf", "extra"), "htdocs"}
	case "caddy":
		return []string{"Caddyfile", "html"}
	case "frankenphp":
		return []string{"Caddyfile", "vhosts", "html"}
	}
	return nil
}

// Update replaces an installed service with a newer version while keeping its
// data and configuration. The service is stopped for the duration and started
// again afterwards if it was running.
func Update(name, version string, progress chan<- Progress) error {
	mgr, ok := Registry[name]
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}
	if !mgr.IsInstalled() {
		return fmt.Errorf("%s is not installed", mgr.DisplayName())
	}
	current := mgr.Version()
	if updateLine(name, current) != updateLine(name, version) {
		return fmt.Errorf("%s %s → %s is a major upgrade; its data directory is not compatible. Back up your data, uninstall, then install %s fresh", mgr.DisplayName(), current, version, version)
	}

	wasRunning := mgr.Status() == StatusRunning
	port := mgr.Port()
	if wasRunning {
		if progress != nil {
			progress <- Progress{Percent: 0, Message: "Stopping " + mgr.DisplayName() + "..."}
		}
		if err := mgr.Stop(); err != nil {
			return fmt.Errorf("could not stop %s: %w", mgr.DisplayName(), err)
		}
		WaitForPortRelease(port, 10)
	}

	base := serviceBaseDir(name)
	stash := filepath.Join(config.GetDataDir(), "tmp", "update-"+name)
	os.RemoveAll(stash)
	os.MkdirAll(stash, 0755)

	keep := preservedPaths(name)
	var stashed []string
	for _, rel := range keep {
		src := filepath.Join(base, rel)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(stash, rel)
		os.MkdirAll(filepath.Dir(dst), 0755)
		if err := moveDir(src, dst); err != nil {
			restoreStash(stash, base, stashed)
			return fmt.Errorf("could not set aside %s: %w", rel, err)
		}
		stashed = append(stashed, rel)
	}

	if err := mgr.Install(version, port, progress); err != nil {
		restoreStash(stash, base, stashed)
		if wasRunning {
			mgr.Start()
		}
		return err
	}

	restoreStash(stash, base, stashed)
	os.RemoveAll(stash)

	// Install saved the new version; make sure the port survived the config restore.
	SaveServiceConfig(name, ServiceConfig{Port: port, Version: version})

	if wasRunning {
		if progress != nil {
			progress <- Progress{Percent: 100, Message: "Starting " + mgr.DisplayName() + "..."}
		}
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("%s updated to %s but failed to start: %w", mgr.DisplayName(), version, err)
		}
	}
	return nil
}

func restoreStash(stash, base string, rels []string) {
	for _, rel := range rels {
		src := filepath.Join(stash, rel)
		dst := filepath.Join(base, rel)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		os.RemoveAll(dst)
		os.MkdirAll(filepath.Dir(dst), 0755)
		moveDir(src, dst)
	}
}
