package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// ExternalTool represents a detected external database tool
type ExternalTool struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

// WebTool represents a web-based database admin tool
type WebTool struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	URL       string `json:"url"`
}

// adminerFallbackURL is used if the GitHub API is unreachable or rate-limited.
// adminerevo publishes one main asset per release named `adminer-X.Y.Z.php`;
// the older `adminer.php` (no version) is no longer produced.
const adminerFallbackURL = "https://github.com/adminerevo/adminerevo/releases/download/v4.8.4/adminer-4.8.4.php"

// IsAdminerInstalled checks if adminer.php exists in tools dir
func IsAdminerInstalled() bool {
	toolDir := filepath.Join(config.GetDataDir(), "tools", "adminer")
	_, err := os.Stat(filepath.Join(toolDir, "adminer.php"))
	return err == nil
}

// fetchLatestAdminerURL queries GitHub for the most recent adminerevo release
// and returns the download URL of its `adminer-*.php` asset (excluding the
// `editor-*.php` companion). Returns the asset URL or an error on failure.
func fetchLatestAdminerURL() (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/adminerevo/adminerevo/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api: HTTP %d", resp.StatusCode)
	}

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("github api: decode: %w", err)
	}

	for _, a := range rel.Assets {
		// Match the main Adminer file; skip the Editor companion.
		if strings.HasPrefix(a.Name, "adminer-") && strings.HasSuffix(a.Name, ".php") {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("no adminer-*.php asset found in latest release")
}

// InstallAdminer downloads the latest adminerevo `adminer-X.Y.Z.php` (or the
// fallback URL if GitHub is unreachable) and stores it as `adminer.php` in the
// DevBox tools directory.
func InstallAdminer() error {
	toolDir := filepath.Join(config.GetDataDir(), "tools", "adminer")
	os.MkdirAll(toolDir, 0755)

	dest := filepath.Join(toolDir, "adminer.php")

	url, err := fetchLatestAdminerURL()
	if err != nil {
		// Fall back to a known-good pinned version if the API call fails.
		url = adminerFallbackURL
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		os.Remove(dest)
		return err
	}
	return nil
}

// GetAdminerURL returns the URL to access Adminer (assuming a web server serves it)
func GetAdminerURL(webServerPort int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/adminer.php", webServerPort)
}

// DetectExternalDBTools scans known paths for external database tools
func DetectExternalDBTools() []ExternalTool {
	if goruntime.GOOS == "darwin" {
		return detectExternalDBToolsDarwin()
	}
	return detectExternalDBToolsWindows()
}

func detectExternalDBToolsWindows() []ExternalTool {
	var tools []ExternalTool

	// HeidiSQL
	heidiPaths := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "HeidiSQL", "heidisql.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "HeidiSQL", "heidisql.exe"),
	}
	heidi := ExternalTool{ID: "heidisql", Name: "HeidiSQL", Installed: false}
	for _, p := range heidiPaths {
		if _, err := os.Stat(p); err == nil {
			heidi.Path = p
			heidi.Installed = true
			break
		}
	}
	tools = append(tools, heidi)

	// DBeaver
	dbeaverPaths := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "DBeaver", "dbeaver.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "DBeaver", "dbeaver.exe"),
	}
	dbeaver := ExternalTool{ID: "dbeaver", Name: "DBeaver", Installed: false}
	for _, p := range dbeaverPaths {
		if _, err := os.Stat(p); err == nil {
			dbeaver.Path = p
			dbeaver.Installed = true
			break
		}
	}
	tools = append(tools, dbeaver)

	// pgAdmin
	pgAdminPaths := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "pgAdmin 4"),
	}
	pgAdmin := ExternalTool{ID: "pgadmin", Name: "pgAdmin 4", Installed: false}
	for _, base := range pgAdminPaths {
		if _, err := os.Stat(base); err == nil {
			// pgAdmin installs in versioned subdirectories
			entries, _ := os.ReadDir(base)
			for _, e := range entries {
				if e.IsDir() {
					candidate := filepath.Join(base, e.Name(), "runtime", "pgAdmin4.exe")
					if _, err := os.Stat(candidate); err == nil {
						pgAdmin.Path = candidate
						pgAdmin.Installed = true
						break
					}
				}
			}
		}
	}
	tools = append(tools, pgAdmin)

	// MongoDB Compass
	compassPaths := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "MongoDBCompass", "MongoDBCompass.exe"),
	}
	compass := ExternalTool{ID: "compass", Name: "MongoDB Compass", Installed: false}
	for _, p := range compassPaths {
		if _, err := os.Stat(p); err == nil {
			compass.Path = p
			compass.Installed = true
			break
		}
	}
	tools = append(tools, compass)

	// VS Code (can be used with DB extensions)
	codePaths := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Microsoft VS Code", "Code.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft VS Code", "Code.exe"),
	}
	if codePath, err := exec.LookPath("code"); err == nil {
		codePaths = append([]string{codePath}, codePaths...)
	}
	vscode := ExternalTool{ID: "vscode", Name: "VS Code", Installed: false}
	for _, p := range codePaths {
		if _, err := os.Stat(p); err == nil {
			vscode.Path = p
			vscode.Installed = true
			break
		}
	}
	tools = append(tools, vscode)

	return tools
}

func detectExternalDBToolsDarwin() []ExternalTool {
	var tools []ExternalTool

	// DBeaver
	dbeaver := ExternalTool{ID: "dbeaver", Name: "DBeaver", Installed: false}
	dbeaverPath := "/Applications/DBeaver.app/Contents/MacOS/dbeaver"
	if _, err := os.Stat(dbeaverPath); err == nil {
		dbeaver.Path = dbeaverPath
		dbeaver.Installed = true
	}
	tools = append(tools, dbeaver)

	// pgAdmin
	pgAdmin := ExternalTool{ID: "pgadmin", Name: "pgAdmin 4", Installed: false}
	pgAdminPath := "/Applications/pgAdmin 4.app/Contents/MacOS/pgAdmin4"
	if _, err := os.Stat(pgAdminPath); err == nil {
		pgAdmin.Path = pgAdminPath
		pgAdmin.Installed = true
	}
	tools = append(tools, pgAdmin)

	// MongoDB Compass
	compass := ExternalTool{ID: "compass", Name: "MongoDB Compass", Installed: false}
	compassPath := "/Applications/MongoDB Compass.app/Contents/MacOS/MongoDB Compass"
	if _, err := os.Stat(compassPath); err == nil {
		compass.Path = compassPath
		compass.Installed = true
	}
	tools = append(tools, compass)

	// VS Code
	vscode := ExternalTool{ID: "vscode", Name: "VS Code", Installed: false}
	vscodePaths := []string{
		"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
	}
	if codePath, err := exec.LookPath("code"); err == nil {
		vscodePaths = append([]string{codePath}, vscodePaths...)
	}
	for _, p := range vscodePaths {
		if _, err := os.Stat(p); err == nil {
			vscode.Path = p
			vscode.Installed = true
			break
		}
	}
	tools = append(tools, vscode)

	return tools
}

// LaunchExternalTool launches an external tool by ID
func LaunchExternalTool(toolID string, args []string) error {
	allTools := DetectExternalDBTools()
	for _, t := range allTools {
		if t.ID == toolID && t.Installed {
			cmd := exec.Command(t.Path, args...)
			platform.SetProcessAttrs(cmd, false, true)
			return cmd.Start()
		}
	}
	return fmt.Errorf("tool %s not found", toolID)
}

// GetDBConnectionArgs returns CLI connection args for a database tool
func GetDBConnectionArgs(toolID, serviceName string, host string, port int) []string {
	switch toolID {
	case "heidisql":
		switch {
		case strings.Contains(serviceName, "mysql") || strings.Contains(serviceName, "mariadb"):
			return []string{fmt.Sprintf("--host=%s", host), fmt.Sprintf("--port=%d", port), "--user=root"}
		case strings.Contains(serviceName, "postgres"):
			return []string{fmt.Sprintf("--host=%s", host), fmt.Sprintf("--port=%d", port), "--user=postgres"}
		}
	case "dbeaver":
		// DBeaver doesn't have good CLI connection args, just open it
		return []string{}
	}
	return []string{}
}
