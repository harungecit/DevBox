package service

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"DevBox/internal/config"
)

// downloadToTmp downloads a file to the DevBox tmp directory
func downloadToTmp(url, filename string, progress chan<- Progress) (string, error) {
	dest := filepath.Join(config.GetDataDir(), "tmp", filename)

	if progress != nil {
		progress <- Progress{Percent: 0, Message: "Downloading..."}
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	os.MkdirAll(filepath.Dir(dest), 0755)
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	lastPercent := -1

	for {
		nr, readErr := resp.Body.Read(buf)
		if nr > 0 {
			out.Write(buf[:nr])
			downloaded += int64(nr)
			if totalSize > 0 && progress != nil {
				pct := int(downloaded * 70 / totalSize) // 0-70% for download
				if pct != lastPercent {
					lastPercent = pct
					mb := float64(downloaded) / 1024 / 1024
					totalMB := float64(totalSize) / 1024 / 1024
					progress <- Progress{Percent: pct, Message: fmt.Sprintf("%.1f / %.1f MB", mb, totalMB)}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", readErr
		}
	}

	return dest, nil
}

// extractZip extracts a zip file to destDir
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	os.MkdirAll(destDir, 0755)

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if strings.Contains(name, "..") {
			continue
		}

		fpath := filepath.Join(destDir, name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(fpath), 0755)
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// hiddenSysProcAttr returns SysProcAttr that hides the console window on Windows.
// Uses HideWindow only (NOT CREATE_NO_WINDOW) because CREATE_NO_WINDOW breaks
// programs that start subprocesses (like pg_ctl, mysqld --initialize, etc).
func hiddenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow: true,
	}
}

// runCommand runs a command and returns combined output (hidden console)
func runCommand(executable string, args []string, workDir string) (string, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = workDir
	cmd.SysProcAttr = hiddenSysProcAttr()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runCommandSilent runs a command silently (hidden console)
func runCommandSilent(executable string, args []string, workDir string) error {
	cmd := exec.Command(executable, args...)
	cmd.Dir = workDir
	cmd.SysProcAttr = hiddenSysProcAttr()
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// moveDir moves a directory, falling back to copy+remove if rename fails (Windows cross-dir issue)
func moveDir(src, dst string) error {
	// Try rename first (fastest)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fallback: copy then remove
	if err := copyDir(src, dst); err != nil {
		return fmt.Errorf("failed to move %s to %s: %w", src, dst, err)
	}
	os.RemoveAll(src)
	return nil
}

// copyDir recursively copies a directory tree
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// versionAtLeast checks if version >= minVersion (semantic version comparison)
func versionAtLeast(version, minVersion string) bool {
	v := strings.Split(version, ".")
	m := strings.Split(minVersion, ".")

	for i := 0; i < len(m); i++ {
		if i >= len(v) {
			return false
		}
		vi, _ := strconv.Atoi(v[i])
		mi, _ := strconv.Atoi(m[i])
		if vi > mi {
			return true
		}
		if vi < mi {
			return false
		}
	}
	return true // equal
}

// removeBaseDir removes a service base directory, retrying on Windows if needed.
// Returns nil if the directory doesn't exist.
func removeBaseDir(base string) error {
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return nil
	}
	// Try up to 3 times - Windows indexer/antivirus can hold handles briefly
	var lastErr error
	for i := 0; i < 3; i++ {
		if err := os.RemoveAll(base); err != nil {
			lastErr = err
			// Small delay before retry
			if i < 2 {
				time.Sleep(500 * time.Millisecond)
			}
			continue
		}
		// Verify it's gone
		if _, err := os.Stat(base); os.IsNotExist(err) {
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to remove %s: %w", base, lastErr)
	}
	return nil
}

// majorVersion extracts the major version number from a version string
func majorVersion(version string) int {
	parts := strings.Split(version, ".")
	if len(parts) >= 1 {
		v, _ := strconv.Atoi(parts[0])
		return v
	}
	return 0
}
