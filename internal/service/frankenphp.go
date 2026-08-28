package service

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/platform"
)

const frankenphpMaxVersions = 5

// frankenphpAssetName returns the GitHub release asset filename for the active OS/arch.
// Windows ships as a zip; macOS publishes the raw binary directly (no archive).
func frankenphpAssetName() string {
	switch goruntime.GOOS {
	case "darwin":
		if goruntime.GOARCH == "arm64" {
			return "frankenphp-mac-arm64"
		}
		return "frankenphp-mac-x86_64"
	default:
		return "frankenphp-windows-x86_64.zip"
	}
}

func frankenphpKnownVersionsList() []AvailableVersion {
	asset := frankenphpAssetName()
	return []AvailableVersion{
		{Version: "1.12.2", Label: "1.12.2 (Latest)", URL: fmt.Sprintf("https://github.com/dunglas/frankenphp/releases/download/v1.12.2/%s", asset)},
		{Version: "1.11.0", Label: "1.11.0", URL: fmt.Sprintf("https://github.com/dunglas/frankenphp/releases/download/v1.11.0/%s", asset)},
	}
}

type FrankenPHPManager struct{}

func NewFrankenPHPManager() *FrankenPHPManager { return &FrankenPHPManager{} }

func (f *FrankenPHPManager) Name() string        { return "frankenphp" }
func (f *FrankenPHPManager) DisplayName() string { return "FrankenPHP" }
func (f *FrankenPHPManager) DefaultPort() int    { return 8501 }

func (f *FrankenPHPManager) binaryPath() string {
	return filepath.Join(serviceBaseDir("frankenphp"), platform.BinaryName("frankenphp"))
}

func (f *FrankenPHPManager) IsInstalled() bool {
	_, err := os.Stat(f.binaryPath())
	return err == nil
}

func (f *FrankenPHPManager) ListVersions() ([]AvailableVersion, error) {
	versions, err := f.fetchVersions()
	if err != nil || len(versions) == 0 {
		return frankenphpKnownVersionsList(), nil
	}
	if len(versions) > frankenphpMaxVersions {
		versions = versions[:frankenphpMaxVersions]
	}
	return versions, nil
}

func (f *FrankenPHPManager) Install(version string, port int, progress chan<- Progress) error {
	base := serviceBaseDir("frankenphp")
	if port <= 0 {
		port = f.DefaultPort()
	}

	downloadURL, err := f.findDownloadURL(version)
	if err != nil {
		return err
	}

	asset := frankenphpAssetName()
	tmpFile, err := downloadToTmp(downloadURL, asset, progress)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpFile)

	if err := removeBaseDir(base); err != nil {
		return fmt.Errorf("failed to clean old installation: %w", err)
	}
	os.MkdirAll(base, 0755)

	if progress != nil {
		progress <- Progress{Percent: 75, Message: "Installing binary..."}
	}

	dest := f.binaryPath()

	if goruntime.GOOS == "windows" {
		// Windows ships a zip that MAY contain the exe at the top level OR inside
		// a single nested directory (e.g. frankenphp-windows-x86_64/). Some builds
		// also include adjacent files (LICENSE, README, sometimes runtime deps).
		// Copy the whole flattened contents into `base` so nothing is lost.
		tmpExtract := tmpFile + "-extract"
		os.RemoveAll(tmpExtract)
		defer os.RemoveAll(tmpExtract)
		if err := extractZip(tmpFile, tmpExtract); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}

		srcDir := tmpExtract
		if entries, _ := os.ReadDir(tmpExtract); len(entries) == 1 && entries[0].IsDir() {
			candidate := filepath.Join(tmpExtract, entries[0].Name())
			if _, err := os.Stat(filepath.Join(candidate, "frankenphp.exe")); err == nil {
				srcDir = candidate
			}
		}
		if _, err := os.Stat(filepath.Join(srcDir, "frankenphp.exe")); os.IsNotExist(err) {
			return fmt.Errorf("frankenphp.exe not found in archive")
		}

		// Move every entry of srcDir into base (handles both single-exe and
		// folder-with-extras layouts).
		entries, _ := os.ReadDir(srcDir)
		for _, e := range entries {
			src := filepath.Join(srcDir, e.Name())
			dst := filepath.Join(base, e.Name())
			os.RemoveAll(dst)
			if err := os.Rename(src, dst); err != nil {
				// Fallback: copy when rename across volumes fails.
				if err := moveDir(src, dst); err != nil {
					return fmt.Errorf("failed to install %s: %w", e.Name(), err)
				}
			}
		}
	} else {
		// macOS: the asset is the raw binary. Copy and mark executable.
		data, err := os.ReadFile(tmpFile)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0755); err != nil {
			return err
		}
	}

	os.MkdirAll(filepath.Join(base, "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "html"), 0755)

	f.writeConfig(port)
	f.writeDefaultPage()

	SaveServiceConfig("frankenphp", ServiceConfig{Port: port, Version: version})

	if progress != nil {
		progress <- Progress{Percent: 100, Message: fmt.Sprintf("FrankenPHP %s installed (port %d)", version, port)}
	}
	return nil
}

func (f *FrankenPHPManager) Uninstall() error {
	if IsRunning("frankenphp") {
		f.Stop()
	}
	return os.RemoveAll(serviceBaseDir("frankenphp"))
}

func (f *FrankenPHPManager) Start() error {
	if !f.IsInstalled() {
		return fmt.Errorf("FrankenPHP is not installed")
	}
	if IsRunning("frankenphp") {
		return nil
	}

	port := f.Port()
	if IsPortInUse(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	base := serviceBaseDir("frankenphp")
	exe := f.binaryPath()
	logFile := filepath.Join(base, "logs", "frankenphp.log")

	_, err := StartProcess("frankenphp", exe, []string{
		"run",
		"--config", filepath.Join(base, "Caddyfile"),
		"--adapter", "caddyfile",
	}, base, logFile)
	return err
}

func (f *FrankenPHPManager) Stop() error {
	if !IsRunning("frankenphp") {
		return nil
	}
	return StopProcess("frankenphp")
}

func (f *FrankenPHPManager) Restart() error {
	port := f.Port()
	f.Stop()
	WaitForPortRelease(port, 5)
	return f.Start()
}

func (f *FrankenPHPManager) Status() ServiceStatus {
	if !f.IsInstalled() {
		return StatusNotInstalled
	}
	if IsRunning("frankenphp") {
		return StatusRunning
	}
	return StatusStopped
}

func (f *FrankenPHPManager) Port() int {
	cfg := LoadServiceConfig("frankenphp")
	if cfg.Port > 0 {
		return cfg.Port
	}
	return f.DefaultPort()
}

func (f *FrankenPHPManager) Version() string {
	cfg := LoadServiceConfig("frankenphp")
	if cfg.Version != "" {
		return cfg.Version
	}
	return "-"
}

func (f *FrankenPHPManager) SetPort(port int) error {
	cfg := LoadServiceConfig("frankenphp")
	cfg.Port = port
	if err := SaveServiceConfig("frankenphp", cfg); err != nil {
		return err
	}
	f.writeConfig(port)
	return nil
}

func (f *FrankenPHPManager) Logs(lines int) ([]string, error) {
	logFile := filepath.Join(serviceBaseDir("frankenphp"), "logs", "frankenphp.log")
	return readLastLines(logFile, lines)
}

func (f *FrankenPHPManager) Info() ServiceInfo {
	return ServiceInfo{
		Name:        f.Name(),
		DisplayName: f.DisplayName(),
		Status:      f.Status(),
		Port:        f.Port(),
		Version:     f.Version(),
		Installed:   f.IsInstalled(),
	}
}

// writeConfig writes the base Caddyfile. Per-project vhosts are appended in a
// separate include block by project/vhost.go (FrankenPHP path is handled there).
func (f *FrankenPHPManager) writeConfig(port int) {
	base := serviceBaseDir("frankenphp")
	confPath := filepath.Join(base, "Caddyfile")
	htmlDir := strings.ReplaceAll(filepath.Join(base, "html"), "\\", "/")
	vhostsGlob := strings.ReplaceAll(filepath.Join(base, "vhosts", "*.caddy"), "\\", "/")

	// Note: the `frankenphp` global directive is registered by FrankenPHP's
	// own build at parse time — we don't need (and some versions reject) an
	// explicit declaration in globals. The php_server directive enables it
	// per-site inside vhost blocks.
	conf := fmt.Sprintf(`# DevBox FrankenPHP Configuration
{
	auto_https off
	admin off
}

:%d {
	root * %s
	file_server
	encode gzip
}

# Per-project vhosts (managed by DevBox)
import %s
`, port, htmlDir, vhostsGlob)

	vhostsDir := filepath.Join(base, "vhosts")
	os.MkdirAll(vhostsDir, 0755)
	// Touch a placeholder so `import vhosts/*.caddy` always matches at least
	// one file. Some Caddy versions error out on an empty glob; this keeps
	// the config valid until DevBox writes real per-project vhost files.
	placeholder := filepath.Join(vhostsDir, "_placeholder.caddy")
	if _, err := os.Stat(placeholder); os.IsNotExist(err) {
		os.WriteFile(placeholder, []byte("# DevBox placeholder — keeps the import glob non-empty.\n"), 0644)
	}
	os.WriteFile(confPath, []byte(conf), 0644)
}

func (f *FrankenPHPManager) writeDefaultPage() {
	indexPath := filepath.Join(serviceBaseDir("frankenphp"), "html", "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return
	}
	html := `<!DOCTYPE html>
<html><head><title>FrankenPHP - DevBox</title></head>
<body>
<h1>FrankenPHP is running!</h1>
<p>Managed by DevBox. Your projects are served from per-project vhosts in the <code>vhosts/</code> directory.</p>
</body></html>`
	os.WriteFile(indexPath, []byte(html), 0644)
}

func (f *FrankenPHPManager) findDownloadURL(version string) (string, error) {
	for _, v := range frankenphpKnownVersionsList() {
		if v.Version == version {
			return v.URL, nil
		}
	}
	versions, err := f.fetchVersions()
	if err == nil {
		for _, v := range versions {
			if v.Version == version {
				return v.URL, nil
			}
		}
	}
	asset := frankenphpAssetName()
	return fmt.Sprintf("https://github.com/dunglas/frankenphp/releases/download/v%s/%s", version, asset), nil
}

func (f *FrankenPHPManager) fetchVersions() ([]AvailableVersion, error) {
	releases, err := fetchGitHubReleases("dunglas", "frankenphp")
	if err != nil {
		return nil, err
	}

	wantAsset := frankenphpAssetName()
	var versions []AvailableVersion
	for _, rel := range releases {
		tag := strings.TrimPrefix(rel.TagName, "v")
		for _, asset := range rel.Assets {
			if asset.Name == wantAsset {
				label := tag
				if len(versions) == 0 {
					label = tag + " (Latest)"
				}
				versions = append(versions, AvailableVersion{
					Version: tag,
					Label:   label,
					URL:     asset.BrowserDownloadURL,
				})
				break
			}
		}
		if len(versions) >= frankenphpMaxVersions {
			break
		}
	}
	return versions, nil
}
