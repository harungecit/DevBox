package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// Composer lives in <data>/tools/composer, independent of any single PHP
// version, so switching the global PHP never "loses" it:
//
//	composer.phar          the phar DevBox downloaded (managed install)
//	devbox-composer.json   {"phar": "<external path>"} for an imported system
//	                       Composer — the file stays where it is, DevBox only
//	                       points at it
//	composer.bat/composer  wrapper running `php <phar>`; php resolves through
//	                       PATH, i.e. the globally active PHP
//
// The directory itself is put on PATH by the App layer (like tools/bun).

const (
	composerDownloadURL = "https://getcomposer.org/download/latest-stable/composer.phar"
	composerVersionsURL = "https://getcomposer.org/versions"
	composerMetaFile    = "devbox-composer.json"
)

var composerVersionRe = regexp.MustCompile(`Composer version (\d+\.\d+\.\d+)`)

// ComposerInfo is what the Tools page renders for the Composer row.
type ComposerInfo struct {
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	Latest          string `json:"latest"` // newest stable on getcomposer.org, "" when unknown
	UpdateAvailable bool   `json:"updateAvailable"`
	Imported        bool   `json:"imported"` // system Composer used in place
	PharPath        string `json:"pharPath"`
}

type composerMeta struct {
	Phar string `json:"phar"`
}

// ComposerDir is the DevBox-managed Composer directory (on PATH once installed).
func ComposerDir() string {
	return filepath.Join(config.GetDataDir(), "tools", "composer")
}

// ComposerPhar returns the phar Composer runs from — DevBox's own download or
// the imported external file — and whether it is imported. "" when not installed.
func ComposerPhar() (phar string, imported bool) {
	dir := ComposerDir()
	if data, err := os.ReadFile(filepath.Join(dir, composerMetaFile)); err == nil {
		var m composerMeta
		if json.Unmarshal(data, &m) == nil && m.Phar != "" {
			if fileExists(m.Phar) {
				return m.Phar, true
			}
			return "", true // link target vanished — reported as not installed
		}
	}
	local := filepath.Join(dir, "composer.phar")
	if fileExists(local) {
		return local, false
	}
	return "", false
}

// IsComposerInstalled reports whether Composer is usable through DevBox.
func IsComposerInstalled() bool {
	phar, _ := ComposerPhar()
	return phar != ""
}

// InstallComposer downloads the latest stable composer.phar into ComposerDir
// and writes the wrapper. Replaces an imported link if there was one.
func InstallComposer(progress chan<- Progress) error {
	report := func(pct int, msg string) {
		if progress != nil {
			progress <- Progress{Percent: pct, Message: msg}
		}
	}
	dir := ComposerDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	pharPath := filepath.Join(dir, "composer.phar")

	report(10, "Downloading composer.phar...")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(composerDownloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	tmp := pharPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	if err := os.Rename(tmp, pharPath); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(filepath.Join(dir, composerMetaFile))

	report(80, "Creating wrapper...")
	if err := writeComposerWrapper(dir, pharPath); err != nil {
		return err
	}
	report(100, "Composer installed")
	return nil
}

// ImportComposer brings a system Composer under DevBox management in place:
// nothing is copied, DevBox records the phar's location and runs it through
// its own wrapper with the active PHP. A managed download, if any, is removed.
func ImportComposer(pharPath string) error {
	pharPath = filepath.Clean(pharPath)
	if !fileExists(pharPath) {
		return fmt.Errorf("composer.phar not found: %s", pharPath)
	}
	if !looksLikePhar(pharPath) {
		return fmt.Errorf("%s does not look like a Composer phar", pharPath)
	}
	if isSubPath(config.GetDataDir(), pharPath) {
		return fmt.Errorf("%s is already managed by DevBox", pharPath)
	}
	dir := ComposerDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(composerMeta{Phar: pharPath}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, composerMetaFile), data, 0644); err != nil {
		return err
	}
	os.Remove(filepath.Join(dir, "composer.phar"))
	return writeComposerWrapper(dir, pharPath)
}

// UninstallComposer removes DevBox's Composer directory. For an imported
// Composer only the link and wrapper go away; the external phar is untouched.
func UninstallComposer() error {
	return os.RemoveAll(ComposerDir())
}

// writeComposerWrapper writes the platform launcher that runs the phar with
// whichever php is first on PATH (the globally active DevBox PHP).
func writeComposerWrapper(dir, pharPath string) error {
	if goruntime.GOOS == "windows" {
		bat := "@echo off\r\nphp \"" + pharPath + "\" %*\r\n"
		return os.WriteFile(filepath.Join(dir, "composer.bat"), []byte(bat), 0644)
	}
	sh := "#!/bin/sh\nexec php \"" + pharPath + "\" \"$@\"\n"
	return os.WriteFile(filepath.Join(dir, "composer"), []byte(sh), 0755)
}

// looksLikePhar checks that a file is a PHP phar (they start with a PHP
// stub, optionally preceded by a shebang) and not a shell/batch wrapper.
func looksLikePhar(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	return bytes.Contains(head[:n], []byte("<?php"))
}

// ComposerPHP returns the PHP binary Composer runs with: the globally active
// DevBox PHP, else whatever php is on PATH. "" when there is none.
func ComposerPHP() string {
	if dir := activePHPDir(); dir != "" {
		exe := filepath.Join(dir, platform.BinaryName("php"))
		if fileExists(exe) {
			return exe
		}
	}
	if p, err := exec.LookPath("php"); err == nil {
		return p
	}
	return ""
}

// GetComposerVersion returns the installed Composer version ("" if unknown).
func GetComposerVersion() string {
	phar, _ := ComposerPhar()
	if phar == "" {
		return ""
	}
	return ComposerPharVersion(phar)
}

// ComposerPharVersion runs `php <phar> --version` and extracts the version.
func ComposerPharVersion(phar string) string {
	php := ComposerPHP()
	if php == "" {
		return ""
	}
	cmd := exec.Command(php, "-n", phar, "--version", "--no-ansi")
	platform.SetProcessAttrs(cmd, false, true)
	cmd.Env = append(os.Environ(), "COMPOSER_NO_INTERACTION=1")
	out, _ := cmd.CombinedOutput()
	if m := composerVersionRe.FindStringSubmatch(string(out)); len(m) > 1 {
		return m[1]
	}
	return ""
}

// --- latest version (getcomposer.org, cached) ---

type composerLatestCache struct {
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetchedAt"`
}

func composerLatestCacheFile() string {
	return filepath.Join(config.GetDataDir(), "cache", "composer-latest.json")
}

// GetComposerLatestVersion returns the newest stable Composer release. The
// answer is cached under <data>/cache for CacheTTL; force bypasses the cache.
// Returns "" when offline and nothing is cached.
func GetComposerLatestVersion(force bool) string {
	var cached composerLatestCache
	if data, err := os.ReadFile(composerLatestCacheFile()); err == nil {
		if json.Unmarshal(data, &cached) == nil && cached.Version != "" &&
			!force && time.Since(cached.FetchedAt) < CacheTTL() {
			return cached.Version
		}
	}
	v := fetchComposerLatest()
	if v == "" {
		return cached.Version // stale beats nothing
	}
	os.MkdirAll(filepath.Dir(composerLatestCacheFile()), 0755)
	data, _ := json.Marshal(composerLatestCache{Version: v, FetchedAt: time.Now()})
	os.WriteFile(composerLatestCacheFile(), data, 0644)
	return v
}

func fetchComposerLatest() string {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(composerVersionsURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		Stable []struct {
			Version string `json:"version"`
		} `json:"stable"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || len(body.Stable) == 0 {
		return ""
	}
	return body.Stable[0].Version
}

// GetComposerInfo assembles the Tools page view of Composer.
func GetComposerInfo() ComposerInfo {
	phar, imported := ComposerPhar()
	info := ComposerInfo{Installed: phar != "", Imported: imported, PharPath: phar}
	if !info.Installed {
		return info
	}
	info.Version = ComposerPharVersion(phar)
	info.Latest = GetComposerLatestVersion(false)
	info.UpdateAvailable = info.Version != "" && info.Latest != "" && info.Version != info.Latest
	return info
}

// UpdateComposer upgrades Composer to the latest stable release. Managed and
// imported installs alike go through `composer self-update` (the imported
// phar is updated in place, exactly as running self-update by hand would);
// a managed phar that fails to self-update is re-downloaded instead.
func UpdateComposer() error {
	phar, imported := ComposerPhar()
	if phar == "" {
		return fmt.Errorf("Composer is not installed")
	}
	php := ComposerPHP()
	if php == "" {
		return fmt.Errorf("no PHP available to run Composer")
	}
	// No -n here: self-update needs the openssl extension from php.ini.
	cmd := exec.Command(php, phar, "self-update", "--stable", "--no-interaction", "--no-ansi")
	platform.SetProcessAttrs(cmd, false, true)
	cmd.Env = append(os.Environ(), "COMPOSER_NO_INTERACTION=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		GetComposerLatestVersion(true)
		return nil
	}
	if imported {
		return fmt.Errorf("composer self-update failed: %s - %w", strings.TrimSpace(string(out)), err)
	}
	if ierr := InstallComposer(nil); ierr != nil {
		return fmt.Errorf("composer self-update failed (%s) and re-download failed: %w", strings.TrimSpace(string(out)), ierr)
	}
	GetComposerLatestVersion(true)
	return nil
}

// MigrateLegacyComposer moves a composer.phar that older DevBox builds kept
// inside a PHP version directory to ComposerDir. Returns true when something
// was migrated so the caller can put ComposerDir on PATH.
func MigrateLegacyComposer() bool {
	if phar, _ := ComposerPhar(); phar != "" {
		return false
	}
	nm, ok := Registry["php"].(*PHPManager)
	if !ok {
		return false
	}
	var phpDirs []string
	if global, _ := nm.GetGlobal(); global != "" {
		phpDirs = append(phpDirs, nm.BinaryPath(global))
	}
	if installed, err := nm.ListInstalled(); err == nil {
		for _, v := range installed {
			phpDirs = append(phpDirs, nm.BinaryPath(v.Number))
		}
	}
	migrated := false
	for _, dir := range phpDirs {
		src := filepath.Join(dir, "composer.phar")
		if !fileExists(src) {
			continue
		}
		if !migrated {
			os.MkdirAll(ComposerDir(), 0755)
			dst := filepath.Join(ComposerDir(), "composer.phar")
			if err := os.Rename(src, dst); err != nil {
				if data, rerr := os.ReadFile(src); rerr == nil && os.WriteFile(dst, data, 0644) == nil {
					os.Remove(src)
				} else {
					continue
				}
			}
			if writeComposerWrapper(ComposerDir(), dst) == nil {
				migrated = true
			}
		} else {
			os.Remove(src)
		}
		// Old per-version wrappers must go so the tools/composer one wins on PATH.
		os.Remove(filepath.Join(dir, "composer.bat"))
		os.Remove(filepath.Join(dir, "composer"))
	}
	return migrated
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
