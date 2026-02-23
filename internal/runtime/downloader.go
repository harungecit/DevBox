package runtime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DownloadFile downloads a file from url to dest with progress reporting
func DownloadFile(url, dest string, expectedSize int64, progress chan<- Progress) error {
	if progress != nil {
		progress <- Progress{Percent: 0, Message: "Connecting..."}
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	if totalSize <= 0 && expectedSize > 0 {
		totalSize = expectedSize
	}

	// Ensure dest directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	var downloaded int64
	buf := make([]byte, 32*1024)
	lastPercent := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)

			if totalSize > 0 && progress != nil {
				percent := int(downloaded * 100 / totalSize)
				if percent != lastPercent {
					lastPercent = percent
					mb := float64(downloaded) / 1024 / 1024
					totalMB := float64(totalSize) / 1024 / 1024
					progress <- Progress{
						Percent: percent,
						Message: fmt.Sprintf("%.1f / %.1f MB", mb, totalMB),
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Download complete"}
	}

	return nil
}

// VerifySHA256 checks the SHA256 checksum of a file
func VerifySHA256(filePath, expectedHash string) error {
	if expectedHash == "" {
		return nil // Skip verification if no hash provided
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actual, expectedHash) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actual)
	}

	return nil
}

// ExtractZip extracts a zip file to destDir with progress reporting
func ExtractZip(zipPath, destDir string, progress chan<- Progress) error {
	if progress != nil {
		progress <- Progress{Percent: 0, Message: "Extracting..."}
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	totalFiles := len(r.File)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	for i, f := range r.File {
		if progress != nil && totalFiles > 0 {
			percent := (i + 1) * 100 / totalFiles
			progress <- Progress{
				Percent: percent,
				Message: fmt.Sprintf("Extracting %d/%d files", i+1, totalFiles),
			}
		}

		err := extractZipFile(f, destDir)
		if err != nil {
			return err
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Extraction complete"}
	}

	return nil
}

// ExtractTarGz extracts a .tar.gz file to destDir with progress reporting
func ExtractTarGz(tarGzPath, destDir string, progress chan<- Progress) error {
	if progress != nil {
		progress <- Progress{Percent: 0, Message: "Extracting..."}
	}

	f, err := os.Open(tarGzPath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	fileCount := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read error: %w", err)
		}

		// Sanitize path to prevent path traversal
		name := filepath.ToSlash(header.Name)
		if strings.Contains(name, "..") {
			continue
		}

		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}

		fileCount++
		if progress != nil && fileCount%200 == 0 {
			progress <- Progress{
				Percent: 50,
				Message: fmt.Sprintf("Extracting files... (%d)", fileCount),
			}
		}
	}

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Extraction complete"}
	}

	return nil
}

// GitHubRelease represents a GitHub release (public API)
type GitHubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []GitHubAsset `json:"assets"`
}

// GitHubAsset represents a release asset
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchGitHubReleasesPublic fetches releases from GitHub API
func FetchGitHubReleasesPublic(owner, repo string) ([]GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "DevBox")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func extractZipFile(f *zip.File, destDir string) error {
	// Sanitize path to prevent zip slip
	name := filepath.ToSlash(f.Name)
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid file path in zip: %s", name)
	}

	fpath := filepath.Join(destDir, name)

	if f.FileInfo().IsDir() {
		return os.MkdirAll(fpath, 0755)
	}

	if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
		return err
	}

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer outFile.Close()

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(outFile, rc)
	return err
}
