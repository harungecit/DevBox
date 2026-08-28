package proxy

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"DevBox/internal/platform"
)

// proxyCaddyVersion pins the bundled Caddy build. Updated opportunistically;
// this is the front-door proxy, independent of any user-installed Caddy service.
const proxyCaddyVersion = "2.9.1"

// caddyDownloadURL returns the GitHub release URL for the current OS/arch.
func caddyDownloadURL() string {
	if goruntime.GOOS == "darwin" {
		arch := "arm64"
		if goruntime.GOARCH == "amd64" {
			arch = "amd64"
		}
		return fmt.Sprintf(
			"https://github.com/caddyserver/caddy/releases/download/v%s/caddy_%s_darwin_%s.tar.gz",
			proxyCaddyVersion, proxyCaddyVersion, arch,
		)
	}
	return fmt.Sprintf(
		"https://github.com/caddyserver/caddy/releases/download/v%s/caddy_%s_windows_amd64.zip",
		proxyCaddyVersion, proxyCaddyVersion,
	)
}

// Install downloads the bundled Caddy binary into ~/.devbox/proxy/.
// Idempotent — does nothing if already installed.
func Install() error {
	if IsInstalled() {
		return nil
	}

	if err := os.MkdirAll(proxyDir(), 0755); err != nil {
		return err
	}

	url := caddyDownloadURL()
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	// Stream to a temp file before extracting.
	tmpFile := filepath.Join(proxyDir(), "caddy-download.tmp")
	out, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmpFile)
		return err
	}
	out.Close()
	defer os.Remove(tmpFile)

	// Extract the binary into proxyDir.
	if strings.HasSuffix(url, ".zip") {
		if err := extractCaddyFromZip(tmpFile); err != nil {
			return fmt.Errorf("zip extraction failed: %w", err)
		}
	} else {
		if err := extractCaddyFromTarGz(tmpFile); err != nil {
			return fmt.Errorf("tar.gz extraction failed: %w", err)
		}
	}

	// Sanity check.
	if !IsInstalled() {
		return fmt.Errorf("install completed but caddy binary not found at %s", caddyBinary())
	}
	return nil
}

// Uninstall removes the bundled Caddy + Caddyfile + logs. Stops the proxy first.
func Uninstall() error {
	_ = Stop()
	return os.RemoveAll(proxyDir())
}

func extractCaddyFromZip(zipPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	wantName := platform.BinaryName("caddy")
	for _, f := range r.File {
		if filepath.Base(f.Name) != wantName {
			continue
		}
		return copyZipEntry(f, caddyBinary())
	}
	return fmt.Errorf("%s not found in archive", wantName)
}

func copyZipEntry(f *zip.File, dst string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}

func extractCaddyFromTarGz(tgzPath string) error {
	f, err := os.Open(tgzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	wantName := platform.BinaryName("caddy")
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != wantName {
			continue
		}
		out, err := os.OpenFile(caddyBinary(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
		return nil
	}
	return fmt.Errorf("%s not found in archive", wantName)
}
