package vfox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/vfox/archive"
)

// recordFile is written next to metadata.lua after DevBox installs a plugin.
const recordFile = "devbox-plugin.json"

// InstalledPlugin describes a plugin present under <data>/plugins/<name>/.
type InstalledPlugin struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Source      string `json:"source"` // "registry" | "url" | "local"
	SourceURL   string `json:"sourceUrl,omitempty"`
	ManifestUrl string `json:"manifestUrl,omitempty"`
	InstalledAt string `json:"installedAt"`

	Description       string   `json:"description"`
	Homepage          string   `json:"homepage"`
	License           string   `json:"license"`
	MinRuntimeVersion string   `json:"minRuntimeVersion"`
	Notes             []string `json:"notes"`
	LegacyFilenames   []string `json:"legacyFilenames"`

	// Derived (not persisted)
	Dir        string `json:"dir"`
	ThirdParty bool   `json:"thirdParty"` // not from the registry
}

// PluginsDir is where plugins live: <data>/plugins.
func PluginsDir() string {
	return filepath.Join(config.GetDataDir(), "plugins")
}

func pluginDir(name string) string {
	return filepath.Join(PluginsDir(), name)
}

func tmpDir() string {
	d := filepath.Join(config.GetDataDir(), "tmp")
	os.MkdirAll(d, 0755)
	return d
}

func recordFromMeta(meta *Metadata) InstalledPlugin {
	return InstalledPlugin{
		Name:              meta.Name,
		Version:           meta.Version,
		Description:       meta.Description,
		Homepage:          meta.Homepage,
		License:           meta.License,
		MinRuntimeVersion: meta.MinRuntimeVersion,
		Notes:             meta.Notes,
		LegacyFilenames:   meta.LegacyFilenames,
		ManifestUrl:       meta.ManifestUrl,
	}
}

func writeRecord(dir string, rec *InstalledPlugin) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, recordFile), data, 0644)
}

func readRecord(dir string) (*InstalledPlugin, bool) {
	data, err := os.ReadFile(filepath.Join(dir, recordFile))
	if err != nil {
		return nil, false
	}
	var rec InstalledPlugin
	if json.Unmarshal(data, &rec) != nil || rec.Name == "" {
		return nil, false
	}
	return &rec, true
}

func isPluginDir(dir string) bool {
	return fileExists(filepath.Join(dir, "metadata.lua")) || fileExists(filepath.Join(dir, "main.lua"))
}

// GetInstalled returns the record of one installed plugin.
func GetInstalled(name string) (*InstalledPlugin, error) {
	dir := pluginDir(name)
	if !isPluginDir(dir) {
		return nil, fmt.Errorf("plugin %s is not installed", name)
	}
	if rec, ok := readRecord(dir); ok {
		rec.Dir = dir
		rec.ThirdParty = rec.Source != "registry"
		return rec, nil
	}
	// Dropped in by hand (plugin development): read metadata via Lua.
	p, err := Load(dir)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	rec := recordFromMeta(&p.Meta)
	rec.Name = filepath.Base(dir)
	rec.Source = "local"
	rec.Dir = dir
	rec.ThirdParty = true
	return &rec, nil
}

// ListInstalled scans <data>/plugins. Broken directories are skipped.
func ListInstalled() ([]InstalledPlugin, error) {
	entries, err := os.ReadDir(PluginsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []InstalledPlugin
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), "-bak") {
			continue
		}
		rec, err := GetInstalled(e.Name())
		if err != nil {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// resolveSource turns what the user typed into a (downloadable|local) origin.
//
//	""                              registry manifest for `name`
//	https://github.com/<o>/<r>      the repo's published manifest.json
//	https://…/x.json                a manifest URL
//	https://…/x.zip | …/x.lua       the plugin archive / a legacy single file
//	C:\dir | /path | x.zip          a local directory or archive
func resolveSource(name, source string, progress func(string)) (downloadURL, localPath string, manifest *Manifest, kind string, err error) {
	s := strings.TrimSpace(source)
	switch {
	case s == "":
		if name == "" {
			return "", "", nil, "", errors.New("plugin name is required")
		}
		progress("Fetching plugin manifest…")
		m, err := FetchManifest(name)
		if err != nil {
			return "", "", nil, "", err
		}
		return m.DownloadUrl, "", m, "registry", nil
	case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
		u := strings.TrimRight(s, "/")
		lower := strings.ToLower(u)
		switch {
		case strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".lua"):
			return u, "", nil, "url", nil
		case strings.HasSuffix(lower, ".json"):
			progress("Fetching plugin manifest…")
			m, err := FetchManifestURL(u)
			if err != nil {
				return "", "", nil, "", err
			}
			m.ManifestUrl = u
			return m.DownloadUrl, "", m, "url", nil
		case strings.HasPrefix(lower, "https://github.com/"):
			parts := strings.Split(strings.TrimPrefix(u, "https://github.com/"), "/")
			if len(parts) < 2 {
				return "", "", nil, "", errors.New("expected https://github.com/<owner>/<repo>")
			}
			mu := fmt.Sprintf("https://github.com/%s/%s/releases/download/manifest/manifest.json", parts[0], strings.TrimSuffix(parts[1], ".git"))
			progress("Fetching plugin manifest…")
			m, err := FetchManifestURL(mu)
			if err != nil {
				return "", "", nil, "", fmt.Errorf("this repository does not publish a vfox manifest (%s): %w", mu, err)
			}
			m.ManifestUrl = mu
			return m.DownloadUrl, "", m, "url", nil
		}
		return "", "", nil, "", errors.New("unsupported plugin URL: expected a .zip, .json manifest or GitHub repository")
	default:
		if _, err := os.Stat(s); err != nil {
			return "", "", nil, "", fmt.Errorf("plugin source not found: %s", s)
		}
		abs, _ := filepath.Abs(s)
		return "", abs, nil, "local", nil
	}
}

// stage materialises the plugin files into a fresh temp directory and
// returns the directory holding the plugin plus the temp root to clean up.
func stage(downloadURL, localPath string, progress func(string)) (string, string, error) {
	work, err := os.MkdirTemp(tmpDir(), "vfox-plugin-")
	if err != nil {
		return "", "", err
	}
	stageDir := filepath.Join(work, "plugin")
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return "", "", err
	}
	src := localPath
	if downloadURL != "" {
		progress("Downloading plugin…")
		src, err = Download(context.Background(), downloadURL, work, nil, UserAgent("", ""), nil)
		if err != nil {
			os.RemoveAll(work)
			return "", "", err
		}
	}
	fi, err := os.Stat(src)
	if err != nil {
		os.RemoveAll(work)
		return "", "", err
	}
	switch {
	case fi.IsDir():
		if err := copyTree(src, stageDir); err != nil {
			os.RemoveAll(work)
			return "", "", err
		}
	case archive.IsArchive(src):
		if err := archive.Decompress(src, stageDir); err != nil {
			os.RemoveAll(work)
			return "", "", fmt.Errorf("extract plugin: %w", err)
		}
	case strings.HasSuffix(strings.ToLower(src), ".lua"):
		if err := copyFile(src, filepath.Join(stageDir, "main.lua")); err != nil {
			os.RemoveAll(work)
			return "", "", err
		}
	default:
		os.RemoveAll(work)
		return "", "", errors.New("plugin source must be a directory, .zip or .lua file")
	}
	// A zip may still carry the files one level down when it also had a
	// stray root entry — find the directory that holds metadata.lua/main.lua.
	if !isPluginDir(stageDir) {
		found := ""
		filepath.WalkDir(stageDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || found != "" {
				return nil
			}
			if d.IsDir() && isPluginDir(p) {
				found = p
				return filepath.SkipAll
			}
			return nil
		})
		if found == "" {
			os.RemoveAll(work)
			return "", "", errors.New("not a vfox plugin: metadata.lua (or main.lua) not found")
		}
		stageDir = found
	}
	return stageDir, work, nil
}

// Install adds a plugin from the registry (source "") or another source.
// The plugin is validated before it is moved into place; an update keeps a
// backup of the previous directory until the new one is verified.
func Install(name, source string, progress func(string)) (*InstalledPlugin, error) {
	if progress == nil {
		progress = func(string) {}
	}
	name = strings.TrimSpace(name)
	if name != "" && IsBuiltinAlias(name) {
		return nil, fmt.Errorf("%s is built into DevBox (as %q); the plugin is not needed", name, BuiltinAliases[strings.ToLower(name)])
	}
	downloadURL, localPath, manifest, kind, err := resolveSource(name, source, progress)
	if err != nil {
		return nil, err
	}
	if manifest != nil && manifest.MinRuntimeVersion != "" && CompareVersions(manifest.MinRuntimeVersion, CompatVersion) > 0 {
		return nil, fmt.Errorf("plugin %s needs vfox runtime %s, DevBox provides %s", manifest.Name, manifest.MinRuntimeVersion, CompatVersion)
	}

	stageDir, work, err := stage(downloadURL, localPath, progress)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	progress("Validating plugin…")
	p, err := Load(stageDir)
	if err != nil {
		return nil, err
	}
	p.Close()
	meta := p.Meta
	if IsBuiltinAlias(meta.Name) {
		return nil, fmt.Errorf("%s is built into DevBox; the plugin is not needed", meta.Name)
	}
	if name != "" && !strings.EqualFold(name, meta.Name) && kind == "registry" {
		return nil, fmt.Errorf("registry entry %s delivered a plugin named %s", name, meta.Name)
	}
	if meta.MinRuntimeVersion != "" && CompareVersions(meta.MinRuntimeVersion, CompatVersion) > 0 {
		return nil, fmt.Errorf("plugin %s needs vfox runtime %s, DevBox provides %s", meta.Name, meta.MinRuntimeVersion, CompatVersion)
	}

	rec := recordFromMeta(&meta)
	rec.Source = kind
	rec.SourceURL = downloadURL
	if kind == "local" {
		rec.SourceURL = localPath
	}
	rec.InstalledAt = time.Now().UTC().Format(time.RFC3339)
	if manifest != nil {
		if rec.Version == "" {
			rec.Version = manifest.Version
		}
		if rec.ManifestUrl == "" {
			rec.ManifestUrl = manifest.ManifestUrl
		}
		if len(rec.Notes) == 0 {
			rec.Notes = manifest.Notes
		}
		if rec.License == "" {
			rec.License = manifest.License
		}
		if rec.Homepage == "" {
			rec.Homepage = manifest.Homepage
		}
		if rec.Description == "" {
			rec.Description = manifest.Description
		}
	}
	if err := writeRecord(stageDir, &rec); err != nil {
		return nil, err
	}

	progress("Installing plugin…")
	if err := os.MkdirAll(PluginsDir(), 0755); err != nil {
		return nil, err
	}
	dest := pluginDir(meta.Name)
	backup := dest + "-bak"
	os.RemoveAll(backup)
	hadPrevious := false
	if _, err := os.Stat(dest); err == nil {
		hadPrevious = true
		if err := os.Rename(dest, backup); err != nil {
			return nil, fmt.Errorf("cannot replace existing plugin: %w", err)
		}
	}
	if err := os.Rename(stageDir, dest); err != nil {
		// Cross-volume temp dir: copy instead.
		if cerr := copyTree(stageDir, dest); cerr != nil {
			os.RemoveAll(dest)
			if hadPrevious {
				os.Rename(backup, dest)
			}
			return nil, cerr
		}
	}
	if hadPrevious {
		os.RemoveAll(backup)
	}
	rec.Dir = dest
	rec.ThirdParty = kind != "registry"
	return &rec, nil
}

// CheckUpdate returns the newest version the plugin's origin offers ("" when
// unknown or already current).
func CheckUpdate(name string) (string, error) {
	rec, err := GetInstalled(name)
	if err != nil {
		return "", err
	}
	var m *Manifest
	switch {
	case rec.Source == "registry":
		m, err = FetchManifest(name)
	case rec.ManifestUrl != "":
		m, err = FetchManifestURL(rec.ManifestUrl)
	default:
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if CompareVersions(m.Version, rec.Version) > 0 {
		return m.Version, nil
	}
	return "", nil
}

// Update reinstalls the plugin from its origin when a newer version exists.
func Update(name string, progress func(string)) (bool, error) {
	if progress == nil {
		progress = func(string) {}
	}
	rec, err := GetInstalled(name)
	if err != nil {
		return false, err
	}
	latest, err := CheckUpdate(name)
	if err != nil {
		return false, err
	}
	if latest == "" {
		return false, nil
	}
	progress(fmt.Sprintf("Updating %s plugin %s → %s…", name, rec.Version, latest))
	source := ""
	if rec.Source != "registry" {
		source = rec.ManifestUrl
	}
	_, err = Install(name, source, progress)
	return err == nil, err
}

// Remove deletes a plugin directory. Unless force is set it refuses while
// runtime versions installed through the plugin still exist.
func Remove(name string, force bool) error {
	dir := pluginDir(name)
	if !isPluginDir(dir) {
		return fmt.Errorf("plugin %s is not installed", name)
	}
	if !force {
		if entries, err := os.ReadDir(filepath.Join(config.GetDataDir(), "runtimes", name)); err == nil {
			n := 0
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), ".") {
					n++
				}
			}
			if n > 0 {
				return fmt.Errorf("%d installed version(s) of %s still exist; uninstall them first", n, name)
			}
		}
	}
	return os.RemoveAll(dir)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		return copyFile(p, out)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
