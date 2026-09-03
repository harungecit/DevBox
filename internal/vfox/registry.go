package vfox

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/vfox/modules"
)

// DefaultRegistry is the public vfox plugin registry (GitHub Pages of
// version-fox/vfox-plugins). config.PluginRegistry overrides it (mirrors).
const DefaultRegistry = "https://version-fox.github.io/vfox-plugins"

// RegistryBase returns the registry URL without a trailing slash.
func RegistryBase() string {
	if r := strings.TrimSpace(config.Get().PluginRegistry); r != "" {
		return strings.TrimRight(r, "/")
	}
	return DefaultRegistry
}

// IndexItem is one row of the registry index.
type IndexItem struct {
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Homepage string `json:"homepage"`
}

// Manifest is a plugin's release descriptor (<registry>/<name>.json, or the
// manifest.json a plugin repo publishes via the vfox template CI).
type Manifest struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	Description       string   `json:"description"`
	Homepage          string   `json:"homepage"`
	License           string   `json:"license"`
	DownloadUrl       string   `json:"downloadUrl"`
	MinRuntimeVersion string   `json:"minRuntimeVersion"`
	ManifestUrl       string   `json:"manifestUrl"`
	Notes             []string `json:"notes"`
	LegacyFilenames   []string `json:"legacyFilenames"`
}

func cacheDir() string {
	return filepath.Join(config.GetDataDir(), "cache")
}

func cacheTTL() time.Duration {
	h := config.Get().VersionCacheHours
	if h <= 0 {
		h = 48
	}
	return time.Duration(h) * time.Hour
}

type indexCache struct {
	FetchedAt time.Time   `json:"fetchedAt"`
	Items     []IndexItem `json:"items"`
}

func getJSON(url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent("", ""))
	req.Header.Set("Accept", "application/json")
	client := modules.DefaultHTTPClient()
	client.Timeout = 30 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// FetchIndex returns the registry index, served from
// <data>/cache/vfox-index.json while fresh (config.VersionCacheHours). A
// failed refresh falls back to a stale cache.
func FetchIndex(force bool) ([]IndexItem, time.Time, error) {
	file := filepath.Join(cacheDir(), "vfox-index.json")
	var cached indexCache
	haveCache := false
	if data, err := os.ReadFile(file); err == nil && json.Unmarshal(data, &cached) == nil && len(cached.Items) > 0 {
		haveCache = true
		if !force && time.Since(cached.FetchedAt) < cacheTTL() {
			return cached.Items, cached.FetchedAt, nil
		}
	}
	var items []IndexItem
	if err := getJSON(RegistryBase()+"/index.json", &items); err != nil {
		if haveCache {
			return cached.Items, cached.FetchedAt, nil
		}
		return nil, time.Time{}, fmt.Errorf("plugin registry unavailable: %w", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	os.MkdirAll(cacheDir(), 0755)
	if data, err := json.MarshalIndent(indexCache{FetchedAt: time.Now(), Items: items}, "", "  "); err == nil {
		os.WriteFile(file, data, 0644)
	}
	return items, time.Now(), nil
}

// FetchManifest returns the registry manifest of a plugin, cached for a day.
func FetchManifest(name string) (*Manifest, error) {
	if name == "" || strings.ContainsAny(name, "/\\ ") {
		return nil, errors.New("invalid plugin name")
	}
	file := filepath.Join(cacheDir(), "vfox-manifest-"+name+".json")
	if fi, err := os.Stat(file); err == nil && time.Since(fi.ModTime()) < 24*time.Hour {
		var m Manifest
		if data, err := os.ReadFile(file); err == nil && json.Unmarshal(data, &m) == nil && m.DownloadUrl != "" {
			return &m, nil
		}
	}
	m, err := FetchManifestURL(RegistryBase() + "/" + name + ".json")
	if err != nil {
		return nil, err
	}
	os.MkdirAll(cacheDir(), 0755)
	if data, err := json.MarshalIndent(m, "", "  "); err == nil {
		os.WriteFile(file, data, 0644)
	}
	return m, nil
}

// FetchManifestURL downloads and validates a manifest from an arbitrary URL.
func FetchManifestURL(url string) (*Manifest, error) {
	var m Manifest
	if err := getJSON(url, &m); err != nil {
		return nil, err
	}
	if m.DownloadUrl == "" {
		return nil, fmt.Errorf("manifest at %s has no downloadUrl", url)
	}
	if m.Name == "" {
		m.Name = strings.TrimSuffix(filepath.Base(url), ".json")
	}
	return &m, nil
}
