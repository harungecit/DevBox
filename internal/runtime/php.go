package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	destDir := filepath.Join(runtimeBaseDir("php"), version)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("PHP %s is already installed", version)
	}

	// Find the correct filename by checking the releases page
	filename, err := p.findFilename(version)
	if err != nil {
		return err
	}

	downloadURL := phpReleasesURL + filename

	// Get SHA256
	expectedHash := p.fetchSHA256(filename)

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
		progress <- Progress{Percent: 100, Message: "PHP " + version + " installed"}
	}

	return nil
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
	return filepath.Join(runtimeBaseDir("php"), version)
}

// findFilename finds the NTS x64 zip filename for a given PHP version
func (p *PHPManager) findFilename(version string) (string, error) {
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

// fetchSHA256 gets the SHA256 hash for a specific file
func (p *PHPManager) fetchSHA256(filename string) string {
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
