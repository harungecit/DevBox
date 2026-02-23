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

const pythonFTPURL = "https://www.python.org/ftp/python/"

// PythonManager manages Python runtime versions
type PythonManager struct{}

func NewPythonManager() *PythonManager {
	return &PythonManager{}
}

func (p *PythonManager) Name() string {
	return "python"
}

func (p *PythonManager) ListRemote() ([]Version, error) {
	resp, err := http.Get(pythonFTPURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Python versions: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse directory listing for version directories like href="3.12.0/"
	re := regexp.MustCompile(`href="(\d+\.\d+\.\d+)/"`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	global, _ := p.GetGlobal()
	seen := map[string]bool{}
	var versions []Version

	for _, m := range matches {
		ver := m[1]
		if seen[ver] {
			continue
		}

		// Only Python 3.8+
		parts := strings.Split(ver, ".")
		if len(parts) < 2 {
			continue
		}
		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		if major < 3 || (major == 3 && minor < 8) {
			continue
		}

		seen[ver] = true
		versions = append(versions, Version{
			Number:  ver,
			Stable:  true,
			Current: ver == global,
		})
	}

	// Sort descending by version number
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i].Number, versions[j].Number) > 0
	})

	if len(versions) > 30 {
		versions = versions[:30]
	}

	return versions, nil
}

func (p *PythonManager) ListInstalled() ([]Version, error) {
	installed, err := listInstalledVersions("python")
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
		return compareVersions(versions[i].Number, versions[j].Number) > 0
	})

	return versions, nil
}

func (p *PythonManager) Install(version string, progress chan<- Progress) error {
	destDir := filepath.Join(runtimeBaseDir("python"), version)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("Python %s is already installed", version)
	}

	// Python embeddable zip: python-{version}-embed-amd64.zip
	filename := fmt.Sprintf("python-%s-embed-amd64.zip", version)
	downloadURL := fmt.Sprintf("%s%s/%s", pythonFTPURL, version, filename)

	tmpFile := filepath.Join(tmpDir(), filename)

	if err := DownloadFile(downloadURL, tmpFile, 0, progress); err != nil {
		return err
	}

	// Extract - Python embeddable zips have files directly at root
	os.MkdirAll(destDir, 0755)

	if err := ExtractZip(tmpFile, destDir, progress); err != nil {
		os.Remove(tmpFile)
		os.RemoveAll(destDir)
		return err
	}

	// Enable pip/site-packages: uncomment "import site" in python{major}{minor}._pth
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		pthFile := filepath.Join(destDir, fmt.Sprintf("python%s%s._pth", parts[0], parts[1]))
		p.enableSitePackages(pthFile)
	}

	// Download get-pip.py and install pip
	if progress != nil {
		progress <- Progress{Percent: 90, Message: "Installing pip..."}
	}
	p.installPip(destDir)

	// Cleanup
	os.Remove(tmpFile)

	if progress != nil {
		progress <- Progress{Percent: 100, Message: "Python " + version + " installed"}
	}

	return nil
}

func (p *PythonManager) Uninstall(version string) error {
	global, _ := p.GetGlobal()
	if global == version {
		setGlobalVersion("python", "")
	}
	return uninstallVersion("python", version)
}

func (p *PythonManager) SetGlobal(version string) error {
	dir := filepath.Join(runtimeBaseDir("python"), version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("Python %s is not installed", version)
	}
	return setGlobalVersion("python", version)
}

func (p *PythonManager) GetGlobal() (string, error) {
	return getGlobalVersion("python")
}

func (p *PythonManager) BinaryPath(version string) string {
	// Python embeddable has python.exe directly in the root
	return filepath.Join(runtimeBaseDir("python"), version)
}

// enableSitePackages uncomments "import site" in the ._pth file to enable pip/site-packages
func (p *PythonManager) enableSitePackages(pthFile string) {
	data, err := os.ReadFile(pthFile)
	if err != nil {
		return
	}
	content := string(data)
	content = strings.Replace(content, "#import site", "import site", 1)
	os.WriteFile(pthFile, []byte(content), 0644)
}

// installPip downloads get-pip.py and runs it to install pip into the embeddable package
func (p *PythonManager) installPip(destDir string) {
	getPipURL := "https://bootstrap.pypa.io/get-pip.py"
	getPipPath := filepath.Join(destDir, "get-pip.py")

	if err := DownloadFile(getPipURL, getPipPath, 0, nil); err != nil {
		return
	}

	// Run python get-pip.py
	pythonExe := filepath.Join(destDir, "python.exe")
	cmd := execCommand(pythonExe, getPipPath)
	cmd.Dir = destDir
	cmd.Run()

	// Cleanup get-pip.py
	os.Remove(getPipPath)
}
