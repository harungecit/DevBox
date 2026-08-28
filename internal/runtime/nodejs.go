package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
)

const nodeAPIURL = "https://nodejs.org/dist/index.json"

// nodeAPIVersion represents a version from nodejs.org API
type nodeAPIVersion struct {
	Version string `json:"version"`
	LTS     any    `json:"lts"` // false or string like "Iron"
	Date    string `json:"date"`
}

// NodeManager manages Node.js runtime versions
type NodeManager struct{}

func NewNodeManager() *NodeManager {
	return &NodeManager{}
}

func (n *NodeManager) Name() string {
	return "node"
}

func (n *NodeManager) ListRemote() ([]Version, error) {
	resp, err := http.Get(nodeAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Node.js versions: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiVersions []nodeAPIVersion
	if err := json.Unmarshal(body, &apiVersions); err != nil {
		return nil, err
	}

	global, _ := n.GetGlobal()
	var versions []Version

	count := 0
	for _, av := range apiVersions {
		if count >= 30 {
			break
		}

		num := strings.TrimPrefix(av.Version, "v")

		// Determine if LTS
		isLTS := false
		switch v := av.LTS.(type) {
		case string:
			isLTS = v != ""
		case bool:
			isLTS = v
		}

		versions = append(versions, Version{
			Number:  num,
			Stable:  isLTS,
			Current: num == global,
		})
		count++
	}

	return versions, nil
}

func (n *NodeManager) ListInstalled() ([]Version, error) {
	installed, err := listInstalledVersions("node")
	if err != nil {
		return nil, err
	}

	global, _ := n.GetGlobal()

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

func (n *NodeManager) Install(version string, progress chan<- Progress) error {
	destDir := filepath.Join(runtimeBaseDir("node"), version)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("Node.js %s is already installed", version)
	}

	// Node.js archive filename varies by platform
	nodeOS, nodeArch := nodeOSArch()
	filename := fmt.Sprintf("node-v%s-%s-%s.%s", version, nodeOS, nodeArch, nodeArchiveExt())
	downloadURL := fmt.Sprintf("https://nodejs.org/dist/v%s/%s", version, filename)

	// Also get the SHASUMS256.txt for verification
	shaURL := fmt.Sprintf("https://nodejs.org/dist/v%s/SHASUMS256.txt", version)
	expectedHash := fetchNodeSHA256(shaURL, filename)

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

	// Extract - Node.js archives have "node-v{VERSION}-{os}-{arch}/" top-level directory
	tmpExtract := filepath.Join(tmpDir(), fmt.Sprintf("node-%s-extract", version))
	os.RemoveAll(tmpExtract)

	if strings.HasSuffix(filename, ".tar.gz") {
		if err := ExtractTarGz(tmpFile, tmpExtract, progress); err != nil {
			os.Remove(tmpFile)
			return err
		}
	} else {
		if err := ExtractZip(tmpFile, tmpExtract, progress); err != nil {
			os.Remove(tmpFile)
			return err
		}
	}

	// Move the extracted directory
	extractedDir := filepath.Join(tmpExtract, fmt.Sprintf("node-v%s-%s-%s", version, nodeOS, nodeArch))
	if _, err := os.Stat(extractedDir); os.IsNotExist(err) {
		extractedDir = tmpExtract
	}

	os.MkdirAll(filepath.Dir(destDir), 0755)
	if err := os.Rename(extractedDir, destDir); err != nil {
		return fmt.Errorf("failed to move Node.js: %w", err)
	}

	// Cleanup
	os.Remove(tmpFile)
	os.RemoveAll(tmpExtract)

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Node.js " + version + " installed"}
	}

	return nil
}

func (n *NodeManager) Uninstall(version string) error {
	global, _ := n.GetGlobal()
	if global == version {
		setGlobalVersion("node", "")
	}
	return uninstallVersion("node", version)
}

func (n *NodeManager) SetGlobal(version string) error {
	dir := filepath.Join(runtimeBaseDir("node"), version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("Node.js %s is not installed", version)
	}
	return setGlobalVersion("node", version)
}

func (n *NodeManager) GetGlobal() (string, error) {
	return getGlobalVersion("node")
}

func (n *NodeManager) BinaryPath(version string) string {
	if goruntime.GOOS == "windows" {
		return filepath.Join(runtimeBaseDir("node"), version)
	}
	return filepath.Join(runtimeBaseDir("node"), version, "bin")
}

// nodeOSArch returns the Node.js OS and arch identifiers for downloads
func nodeOSArch() (string, string) {
	switch goruntime.GOOS {
	case "darwin":
		if goruntime.GOARCH == "arm64" {
			return "darwin", "arm64"
		}
		return "darwin", "x64"
	default: // windows
		return "win", "x64"
	}
}

// nodeArchiveExt returns the archive extension for the current platform
func nodeArchiveExt() string {
	if goruntime.GOOS == "windows" {
		return "zip"
	}
	return "tar.gz"
}

// fetchNodeSHA256 gets the SHA256 hash for a specific file from SHASUMS256.txt
func fetchNodeSHA256(shaURL, filename string) string {
	resp, err := http.Get(shaURL)
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
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == filename {
			return parts[0]
		}
	}

	return ""
}
