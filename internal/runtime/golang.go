package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const goAPIURL = "https://go.dev/dl/?mode=json&include=all"

// goAPIVersion represents a version from go.dev/dl API
type goAPIVersion struct {
	Version string       `json:"version"`
	Stable  bool         `json:"stable"`
	Files   []goAPIFile  `json:"files"`
}

type goAPIFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kind     string `json:"kind"` // "archive", "installer", "source"
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// GoManager manages Go runtime versions
type GoManager struct{}

func NewGoManager() *GoManager {
	return &GoManager{}
}

func (g *GoManager) Name() string {
	return "go"
}

func (g *GoManager) ListRemote() ([]Version, error) {
	resp, err := http.Get(goAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Go versions: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiVersions []goAPIVersion
	if err := json.Unmarshal(body, &apiVersions); err != nil {
		return nil, err
	}

	global, _ := g.GetGlobal()
	var versions []Version
	seen := map[string]bool{}

	for _, av := range apiVersions {
		// Only include versions with windows/amd64 archives
		hasWindows := false
		for _, f := range av.Files {
			if f.OS == "windows" && f.Arch == "amd64" && f.Kind == "archive" {
				hasWindows = true
				break
			}
		}
		if !hasWindows {
			continue
		}

		num := strings.TrimPrefix(av.Version, "go")
		if seen[num] {
			continue
		}
		seen[num] = true

		versions = append(versions, Version{
			Number:  num,
			Stable:  av.Stable,
			Current: num == global,
		})
	}

	// Limit to most recent 30 versions
	if len(versions) > 30 {
		versions = versions[:30]
	}

	return versions, nil
}

func (g *GoManager) ListInstalled() ([]Version, error) {
	installed, err := listInstalledVersions("go")
	if err != nil {
		return nil, err
	}

	global, _ := g.GetGlobal()

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

func (g *GoManager) Install(version string, progress chan<- Progress) error {
	// Fetch version info to get download URL and checksum
	file, err := g.findFile(version)
	if err != nil {
		return err
	}

	destDir := filepath.Join(runtimeBaseDir("go"), version)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("Go %s is already installed", version)
	}

	// Download
	tmpFile := filepath.Join(tmpDir(), file.Filename)
	downloadURL := fmt.Sprintf("https://go.dev/dl/%s", file.Filename)

	if err := DownloadFile(downloadURL, tmpFile, file.Size, progress); err != nil {
		return err
	}

	// Verify checksum
	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Verifying checksum..."}
	}
	if err := VerifySHA256(tmpFile, file.SHA256); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract - Go zips have a "go/" top-level directory
	tmpExtract := filepath.Join(tmpDir(), fmt.Sprintf("go-%s-extract", version))
	os.RemoveAll(tmpExtract)

	if err := ExtractZip(tmpFile, tmpExtract, progress); err != nil {
		os.Remove(tmpFile)
		return err
	}

	// Move the extracted "go" folder to the final destination
	extractedGoDir := filepath.Join(tmpExtract, "go")
	if _, err := os.Stat(extractedGoDir); os.IsNotExist(err) {
		// If no "go" subdirectory, the zip extracted directly
		extractedGoDir = tmpExtract
	}

	os.MkdirAll(filepath.Dir(destDir), 0755)
	if err := os.Rename(extractedGoDir, destDir); err != nil {
		// Fallback: copy if rename fails (cross-device)
		return fmt.Errorf("failed to move Go SDK: %w", err)
	}

	// Cleanup
	os.Remove(tmpFile)
	os.RemoveAll(tmpExtract)

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Go " + version + " installed"}
	}

	return nil
}

func (g *GoManager) Uninstall(version string) error {
	global, _ := g.GetGlobal()
	if global == version {
		setGlobalVersion("go", "")
	}
	return uninstallVersion("go", version)
}

func (g *GoManager) SetGlobal(version string) error {
	dir := filepath.Join(runtimeBaseDir("go"), version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("Go %s is not installed", version)
	}
	return setGlobalVersion("go", version)
}

func (g *GoManager) GetGlobal() (string, error) {
	return getGlobalVersion("go")
}

func (g *GoManager) BinaryPath(version string) string {
	return filepath.Join(runtimeBaseDir("go"), version, "bin")
}

// findFile finds the windows/amd64 archive for a specific version
func (g *GoManager) findFile(version string) (*goAPIFile, error) {
	resp, err := http.Get(goAPIURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiVersions []goAPIVersion
	if err := json.Unmarshal(body, &apiVersions); err != nil {
		return nil, err
	}

	goVersion := "go" + version
	for _, av := range apiVersions {
		if av.Version == goVersion {
			for _, f := range av.Files {
				if f.OS == "windows" && f.Arch == "amd64" && f.Kind == "archive" {
					return &f, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("Go %s not found for windows/amd64", version)
}
