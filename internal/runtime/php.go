package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	phpReleasesURL = "https://windows.php.net/downloads/releases/"
	phpSHA256URL   = "https://windows.php.net/downloads/releases/sha256sum.txt"
)

// PHPManager manages PHP runtime versions
type PHPManager struct{}

func NewPHPManager() *PHPManager {
	return &PHPManager{}
}

func (p *PHPManager) Name() string {
	return "php"
}

func (p *PHPManager) ListRemote() ([]Version, error) {
	if goruntime.GOOS == "darwin" {
		return p.listRemoteDarwin()
	}
	return p.listRemoteWindows()
}

func (p *PHPManager) listRemoteWindows() ([]Version, error) {
	// Fetch the releases page to find available versions
	resp, err := http.Get(phpReleasesURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PHP versions: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse filenames like php-8.4.16-nts-Win32-vs17-x64.zip
	re := regexp.MustCompile(`php-(\d+\.\d+\.\d+)-nts-Win32-vs\d+-x64\.zip`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	global, _ := p.GetGlobal()
	seen := map[string]bool{}
	var versions []Version

	for _, m := range matches {
		ver := m[1]
		if seen[ver] {
			continue
		}

		// Filter: minimum PHP 7.0
		parts := strings.Split(ver, ".")
		if len(parts) >= 1 {
			major, _ := strconv.Atoi(parts[0])
			if major < 7 {
				continue
			}
		}

		seen[ver] = true

		versions = append(versions, Version{
			Number:  ver,
			Stable:  true,
			Current: ver == global,
		})
	}

	// Sort descending
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Number > versions[j].Number
	})

	if len(versions) > 20 {
		versions = versions[:20]
	}

	return versions, nil
}

func (p *PHPManager) listRemoteDarwin() ([]Version, error) {
	releases, err := FetchGitHubReleasesPublic("shivammathur", "php-builder-darwin")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PHP versions for macOS: %w", err)
	}

	global, _ := p.GetGlobal()
	seen := map[string]bool{}
	var versions []Version

	arch := goruntime.GOARCH // "arm64" or "amd64"
	suffix := fmt.Sprintf("darwin-%s.tar.gz", arch)

	for _, rel := range releases {
		// Tags are like "php-8.3.0"
		ver := strings.TrimPrefix(rel.TagName, "php-")
		if ver == "" || seen[ver] || !isValidPHPVersion(ver) {
			continue
		}

		// Check if there's an asset for our architecture
		hasAsset := false
		for _, asset := range rel.Assets {
			if strings.Contains(asset.Name, suffix) {
				hasAsset = true
				break
			}
		}
		if !hasAsset {
			continue
		}

		seen[ver] = true
		versions = append(versions, Version{
			Number:  ver,
			Stable:  true,
			Current: ver == global,
		})
	}

	if len(versions) > 20 {
		versions = versions[:20]
	}

	return versions, nil
}

func isValidPHPVersion(ver string) bool {
	parts := strings.Split(ver, ".")
	if len(parts) < 2 {
		return false
	}
	major, _ := strconv.Atoi(parts[0])
	return major >= 7
}

func (p *PHPManager) ListInstalled() ([]Version, error) {
	installed, err := listInstalledVersions("php")
	if err != nil {
		return nil, err
	}

	global, _ := p.GetGlobal()

	var versions []Version
	for _, v := range installed {
		versions = append(versions, Version{
			Number:  v,
			Stable:  true,
			Current: v == global,
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Number > versions[j].Number
	})

	return versions, nil
}

func (p *PHPManager) Install(version string, progress chan<- Progress) error {
	if goruntime.GOOS == "darwin" {
		return p.installDarwin(version, progress)
	}
	return p.installWindows(version, progress)
}

func (p *PHPManager) installWindows(version string, progress chan<- Progress) error {
	destDir := filepath.Join(runtimeBaseDir("php"), version)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("PHP %s is already installed", version)
	}

	// Find the correct filename by checking the releases page
	filename, err := p.findWindowsFilename(version)
	if err != nil {
		return err
	}

	downloadURL := phpReleasesURL + filename

	// Get SHA256
	expectedHash := p.fetchWindowsSHA256(filename)

	tmpFile := filepath.Join(tmpDir(), filename)

	if err := DownloadFile(downloadURL, tmpFile, 0, progress); err != nil {
		return err
	}

	// Verify checksum
	if expectedHash != "" {
		if progress != nil {
			progress <- Progress{Percent: 100, Message: "Verifying checksum..."}
		}
		if err := VerifySHA256(tmpFile, expectedHash); err != nil {
			os.Remove(tmpFile)
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Extract - PHP zips have files directly at root (no subdirectory)
	os.MkdirAll(destDir, 0755)

	if err := ExtractZip(tmpFile, destDir, progress); err != nil {
		os.Remove(tmpFile)
		os.RemoveAll(destDir)
		return err
	}

	// Cleanup
	os.Remove(tmpFile)

	if progress != nil {
		progress <- Progress{Percent: 95, Message: "Configuring dev preset (extensions + ini tuning)..."}
	}
	if err := ApplyDevPreset(version); err != nil {
		// Don't fail the install — the binary is usable; surface the warning instead.
		if progress != nil {
			progress <- Progress{Percent: 98, Message: "Dev preset warning: " + err.Error()}
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "PHP " + version + " installed"}
	}

	return nil
}

func (p *PHPManager) installDarwin(version string, progress chan<- Progress) error {
	destDir := filepath.Join(runtimeBaseDir("php"), version)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("PHP %s is already installed", version)
	}

	arch := goruntime.GOARCH
	assetName := fmt.Sprintf("php-%s-darwin-%s.tar.gz", version, arch)
	downloadURL, err := p.findDarwinURL(version, assetName)
	if err != nil {
		return err
	}

	tmpFile := filepath.Join(tmpDir(), assetName)
	if err := DownloadFile(downloadURL, tmpFile, 0, progress); err != nil {
		return err
	}

	tmpExtract := filepath.Join(tmpDir(), fmt.Sprintf("php-%s-extract", version))
	os.RemoveAll(tmpExtract)

	if err := ExtractTarGz(tmpFile, tmpExtract, progress); err != nil {
		os.Remove(tmpFile)
		return err
	}

	// Find the extracted directory - might be in a subdirectory
	extractedDir := tmpExtract
	entries, _ := os.ReadDir(tmpExtract)
	if len(entries) == 1 && entries[0].IsDir() {
		extractedDir = filepath.Join(tmpExtract, entries[0].Name())
	}

	os.MkdirAll(filepath.Dir(destDir), 0755)
	if err := os.Rename(extractedDir, destDir); err != nil {
		return fmt.Errorf("failed to move PHP: %w", err)
	}

	os.Remove(tmpFile)
	os.RemoveAll(tmpExtract)

	if progress != nil {
		progress <- Progress{Percent: 95, Message: "Configuring dev preset (extensions + ini tuning)..."}
	}
	if err := ApplyDevPreset(version); err != nil {
		if progress != nil {
			progress <- Progress{Percent: 98, Message: "Dev preset warning: " + err.Error()}
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "PHP " + version + " installed"}
	}
	return nil
}

func (p *PHPManager) findDarwinURL(version, assetName string) (string, error) {
	releases, err := FetchGitHubReleasesPublic("shivammathur", "php-builder-darwin")
	if err != nil {
		return "", err
	}

	tag := "php-" + version
	for _, rel := range releases {
		if rel.TagName == tag {
			for _, asset := range rel.Assets {
				if asset.Name == assetName {
					return asset.BrowserDownloadURL, nil
				}
			}
		}
	}

	return "", fmt.Errorf("PHP %s not found for macOS/%s", version, goruntime.GOARCH)
}

func (p *PHPManager) Uninstall(version string) error {
	global, _ := p.GetGlobal()
	if global == version {
		setGlobalVersion("php", "")
	}
	return uninstallVersion("php", version)
}

func (p *PHPManager) SetGlobal(version string) error {
	dir := filepath.Join(runtimeBaseDir("php"), version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("PHP %s is not installed", version)
	}
	return setGlobalVersion("php", version)
}

func (p *PHPManager) GetGlobal() (string, error) {
	return getGlobalVersion("php")
}

func (p *PHPManager) BinaryPath(version string) string {
	if goruntime.GOOS == "windows" {
		return filepath.Join(runtimeBaseDir("php"), version)
	}
	return filepath.Join(runtimeBaseDir("php"), version, "bin")
}

// findWindowsFilename finds the NTS x64 zip filename for a given PHP version on Windows
func (p *PHPManager) findWindowsFilename(version string) (string, error) {
	resp, err := http.Get(phpReleasesURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Look for: php-VERSION-nts-Win32-vs{XX}-x64.zip
	re := regexp.MustCompile(fmt.Sprintf(`(php-%s-nts-Win32-vs\d+-x64\.zip)`, regexp.QuoteMeta(version)))
	match := re.FindStringSubmatch(string(body))
	if match == nil {
		return "", fmt.Errorf("PHP %s not found for windows/x64 NTS", version)
	}

	return match[1], nil
}

// fetchWindowsSHA256 gets the SHA256 hash for a specific file from windows.php.net
func (p *PHPManager) fetchWindowsSHA256(filename string) string {
	resp, err := http.Get(phpSHA256URL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		// Format: hash *filename  or  hash  filename
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			fname := strings.TrimPrefix(parts[1], "*")
			if fname == filename {
				return parts[0]
			}
		}
	}

	return ""
}
