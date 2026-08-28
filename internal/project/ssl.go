package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// mkcertPath returns the path to the mkcert executable
func mkcertPath() string {
	return filepath.Join(config.GetDataDir(), "tools", "mkcert", platform.BinaryName("mkcert"))
}

// IsMkcertInstalled checks if mkcert is available
func IsMkcertInstalled() bool {
	_, err := os.Stat(mkcertPath())
	return err == nil
}

// InstallMkcert downloads the mkcert binary
func InstallMkcert() error {
	toolDir := filepath.Dir(mkcertPath())
	os.MkdirAll(toolDir, 0755)

	if goruntime.GOOS == "darwin" {
		return installMkcertDarwin()
	}
	return installMkcertWindows()
}

func installMkcertWindows() error {
	// Try fetching from GitHub releases for FiloSottile/mkcert
	downloadURL := "https://github.com/FiloSottile/mkcert/releases/latest/download/mkcert-v1.4.4-windows-amd64.exe"

	tmpFile := filepath.Join(config.GetDataDir(), "tmp", "mkcert.exe")
	os.MkdirAll(filepath.Dir(tmpFile), 0755)

	// Simple download
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf(`Invoke-WebRequest -Uri "%s" -OutFile "%s" -UseBasicParsing`, downloadURL, tmpFile))
	platform.SetProcessAttrs(cmd, false, true)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download mkcert: %w", err)
	}

	// Move to tools dir
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return err
	}
	os.Remove(tmpFile)
	return os.WriteFile(mkcertPath(), data, 0755)
}

func installMkcertDarwin() error {
	arch := "amd64"
	if goruntime.GOARCH == "arm64" {
		arch = "arm64"
	}
	downloadURL := fmt.Sprintf("https://github.com/FiloSottile/mkcert/releases/latest/download/mkcert-v1.4.4-darwin-%s", arch)
	dest := mkcertPath()

	cmd := exec.Command("curl", "-fsSL", "-o", dest, downloadURL)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download mkcert: %w", err)
	}

	os.Chmod(dest, 0755)
	return nil
}

// SetupProjectSSL generates SSL certificates for a project domain using mkcert
func SetupProjectSSL(domain string) error {
	if !IsMkcertInstalled() {
		if err := InstallMkcert(); err != nil {
			return fmt.Errorf("mkcert not available: %w", err)
		}
	}

	certDir := filepath.Join(config.GetDataDir(), "ssl", "certs")
	os.MkdirAll(certDir, 0755)

	certFile := filepath.Join(certDir, domain+".pem")
	keyFile := filepath.Join(certDir, domain+"-key.pem")

	// Run mkcert -install first (installs root CA if not done)
	mkcert := mkcertPath()
	installCmd := exec.Command(mkcert, "-install")
	platform.SetProcessAttrs(installCmd, false, true)
	installCmd.Run() // Ignore errors - may already be installed

	// Generate certificate
	cmd := exec.Command(mkcert,
		"-cert-file", certFile,
		"-key-file", keyFile,
		domain,
		"*."+domain,
	)
	platform.SetProcessAttrs(cmd, false, true)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mkcert failed: %s - %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}
