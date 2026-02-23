package tools

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"DevBox/internal/config"
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

const adminerDownloadURL = "https://github.com/adminerevo/adminerevo/releases/latest/download/adminer.php"

// IsAdminerInstalled checks if adminer.php exists in tools dir
func IsAdminerInstalled() bool {
	toolDir := filepath.Join(config.GetDataDir(), "tools", "adminer")
	_, err := os.Stat(filepath.Join(toolDir, "adminer.php"))
	return err == nil
}

// InstallAdminer downloads adminer.php
func InstallAdminer() error {
	toolDir := filepath.Join(config.GetDataDir(), "tools", "adminer")
	os.MkdirAll(toolDir, 0755)

	dest := filepath.Join(toolDir, "adminer.php")

	resp, err := http.Get(adminerDownloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// GetAdminerURL returns the URL to access Adminer (assuming a web server serves it)
func GetAdminerURL(webServerPort int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/adminer.php", webServerPort)
}

// DetectExternalDBTools scans known paths for external database tools
func DetectExternalDBTools() []ExternalTool {
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
	// Also check PATH
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

// LaunchExternalTool launches an external tool by ID
func LaunchExternalTool(toolID string, args []string) error {
	allTools := DetectExternalDBTools()
	for _, t := range allTools {
		if t.ID == toolID && t.Installed {
			cmd := exec.Command(t.Path, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
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
