package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"DevBox/internal/config"
)

// mkcert download URL
const mkcertDownloadURL = "https://github.com/nicedoc/mkcert/releases/latest/download/mkcert-windows-amd64.exe"

// mkcertPath returns the path to the mkcert executable
func mkcertPath() string {
	return filepath.Join(config.GetDataDir(), "tools", "mkcert", "mkcert.exe")
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

	// Try fetching from GitHub releases for FiloSottile/mkcert
	downloadURL := "https://github.com/FiloSottile/mkcert/releases/latest/download/mkcert-v1.4.4-windows-amd64.exe"

	tmpFile := filepath.Join(config.GetDataDir(), "tmp", "mkcert.exe")
	os.MkdirAll(filepath.Dir(tmpFile), 0755)

	// Simple download
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf(`Invoke-WebRequest -Uri "%s" -OutFile "%s" -UseBasicParsing`, downloadURL, tmpFile))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
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
	installCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	installCmd.Run() // Ignore errors - may already be installed

	// Generate certificate
	cmd := exec.Command(mkcert,
		"-cert-file", certFile,
		"-key-file", keyFile,
		domain,
		"*."+domain,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mkcert failed: %s - %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}
