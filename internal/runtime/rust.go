package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
)

const rustReleasesURL = "https://api.github.com/repos/rust-lang/rust/releases?per_page=30"
const rustDistURL = "https://static.rust-lang.org/dist/"
// rustTarget returns the Rust target triple for the current platform
func rustTarget() string {
	switch goruntime.GOOS {
	case "darwin":
		if goruntime.GOARCH == "arm64" {
			return "aarch64-apple-darwin"
		}
		return "x86_64-apple-darwin"
	default:
		return "x86_64-pc-windows-msvc"
	}
}

// rustGitHubRelease represents a release from GitHub API
type rustGitHubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// RustManager manages Rust runtime versions
type RustManager struct{}

func NewRustManager() *RustManager {
	return &RustManager{}
}

func (r *RustManager) Name() string {
	return "rust"
}

func (r *RustManager) ListRemote() ([]Version, error) {
	req, err := http.NewRequest("GET", rustReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Rust versions: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var releases []rustGitHubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, err
	}

	global, _ := r.GetGlobal()
	var versions []Version

	for _, rel := range releases {
		if rel.Draft {
			continue
		}

		ver := strings.TrimPrefix(rel.TagName, "v")

		// Only include stable version numbers (digits and dots only)
		if !isStableVersionString(ver) {
			continue
		}

		versions = append(versions, Version{
			Number:  ver,
			Stable:  !rel.Prerelease,
			Current: ver == global,
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i].Number, versions[j].Number) > 0
	})

	return versions, nil
}

func (r *RustManager) ListInstalled() ([]Version, error) {
	installed, err := listInstalledVersions("rust")
	if err != nil {
		return nil, err
	}

	global, _ := r.GetGlobal()

	var versions []Version
	for _, v := range installed {
		versions = append(versions, Version{
			Number:  v,
			Stable:  true,
			Current: v == global,
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i].Number, versions[j].Number) > 0
	})

	return versions, nil
}

func (r *RustManager) Install(version string, progress chan<- Progress) error {
	destDir := filepath.Join(runtimeBaseDir("rust"), version)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("Rust %s is already installed", version)
	}

	// Download rust-{version}-x86_64-pc-windows-msvc.tar.gz
	filename := fmt.Sprintf("rust-%s-%s.tar.gz", version, rustTarget())
	downloadURL := rustDistURL + filename

	// Get SHA256 checksum
	shaURL := downloadURL + ".sha256"
	expectedHash := fetchRustSHA256(shaURL)

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

	// Extract tar.gz
	tmpExtract := filepath.Join(tmpDir(), fmt.Sprintf("rust-%s-extract", version))
	os.RemoveAll(tmpExtract)

	if err := ExtractTarGz(tmpFile, tmpExtract, progress); err != nil {
		os.Remove(tmpFile)
		return err
	}

	// The archive extracts to rust-{version}-{target}/
	extractedDir := filepath.Join(tmpExtract, fmt.Sprintf("rust-%s-%s", version, rustTarget()))
	if _, err := os.Stat(extractedDir); os.IsNotExist(err) {
		// Fallback: find the single directory inside tmpExtract
		entries, _ := os.ReadDir(tmpExtract)
		if len(entries) == 1 && entries[0].IsDir() {
			extractedDir = filepath.Join(tmpExtract, entries[0].Name())
		}
	}

	// Merge components (rustc, cargo, etc.) into a single directory
	os.MkdirAll(destDir, 0755)
	if progress != nil {
		progress <- Progress{Percent: 80, Message: "Setting up toolchain..."}
	}

	if err := r.mergeComponents(extractedDir, destDir); err != nil {
		os.RemoveAll(destDir)
		os.Remove(tmpFile)
		os.RemoveAll(tmpExtract)
		return fmt.Errorf("failed to set up Rust toolchain: %w", err)
	}

	// Cleanup
	os.Remove(tmpFile)
	os.RemoveAll(tmpExtract)

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Rust " + version + " installed"}
	}

	return nil
}

func (r *RustManager) Uninstall(version string) error {
	global, _ := r.GetGlobal()
	if global == version {
		setGlobalVersion("rust", "")
	}
	return uninstallVersion("rust", version)
}

func (r *RustManager) SetGlobal(version string) error {
	dir := filepath.Join(runtimeBaseDir("rust"), version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("Rust %s is not installed", version)
	}
	return setGlobalVersion("rust", version)
}

func (r *RustManager) GetGlobal() (string, error) {
	return getGlobalVersion("rust")
}

func (r *RustManager) BinaryPath(version string) string {
	return filepath.Join(runtimeBaseDir("rust"), version, "bin")
}

// mergeComponents copies all component directories (rustc, cargo, clippy, etc.)
// into a single merged directory tree so bin/, lib/ etc. are unified.
func (r *RustManager) mergeComponents(extractedDir, destDir string) error {
	entries, err := os.ReadDir(extractedDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		componentDir := filepath.Join(extractedDir, entry.Name())

		// Walk each component and copy files (except manifest.in) to destDir
		err := filepath.WalkDir(componentDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(componentDir, path)
			if err != nil {
				return err
			}

			// Skip manifest files and the component root
			if relPath == "." || relPath == "manifest.in" {
				return nil
			}

			destPath := filepath.Join(destDir, relPath)

			if d.IsDir() {
				return os.MkdirAll(destPath, 0755)
			}

			return copyFile(path, destPath)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// fetchRustSHA256 fetches the SHA256 checksum for a Rust archive
func fetchRustSHA256(shaURL string) string {
	resp, err := http.Get(shaURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	// Format: "hash  filename" or just "hash"
	parts := strings.Fields(strings.TrimSpace(string(body)))
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	os.MkdirAll(filepath.Dir(dst), 0755)

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// isStableVersionString checks if a version string contains only digits and dots
func isStableVersionString(ver string) bool {
	if ver == "" {
		return false
	}
	for _, ch := range ver {
		if ch != '.' && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}
