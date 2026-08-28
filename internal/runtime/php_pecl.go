package runtime

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"time"
)

// PECL extensions for Windows are prebuilt DLLs published per PHP line on
// windows.php.net (and xdebug.org for Xdebug). DevBox downloads the matching
// NTS/x64 build for the installed PHP, drops php_<name>.dll into ext/, any
// helper DLLs (ImageMagick's CORE_RL_*.dll etc.) next to php.exe, and enables
// the extension in php.ini. Installed ones are recorded in ext/devbox-pecl.json.

// PeclExtension is a catalog entry shown in the Runtimes page.
type PeclExtension struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version"` // installed version
	Zend        bool   `json:"zend"`
}

var peclCatalog = []PeclExtension{
	{Name: "xdebug", Description: "Debugger & profiler (step debugging, coverage)", Zend: true},
	{Name: "redis", Description: "Native Redis / Valkey client (phpredis)"},
	{Name: "apcu", Description: "In-memory user cache"},
	{Name: "imagick", Description: "ImageMagick image processing (bundles ImageMagick DLLs)"},
	{Name: "mongodb", Description: "Official MongoDB driver"},
	{Name: "pcov", Description: "Fast code coverage for PHPUnit"},
	{Name: "igbinary", Description: "Compact binary serializer (used by redis/apcu)"},
	{Name: "msgpack", Description: "MessagePack serializer"},
	{Name: "memcache", Description: "Memcache client"},
	{Name: "yaml", Description: "YAML parser/emitter (libyaml)"},
	{Name: "amqp", Description: "RabbitMQ / AMQP client"},
	{Name: "ssh2", Description: "SSH2 client (libssh2)"},
	{Name: "xhprof", Description: "Hierarchical profiler"},
	{Name: "uuid", Description: "libuuid bindings"},
	{Name: "ds", Description: "Efficient data structures"},
	{Name: "grpc", Description: "gRPC client"},
	{Name: "protobuf", Description: "Protocol Buffers runtime"},
	{Name: "swoole", Description: "Async/coroutine server engine"},
}

const peclBaseURL = "https://windows.php.net/downloads/pecl/releases/"

func peclStateFile(phpDir string) string {
	return filepath.Join(phpDir, "ext", "devbox-pecl.json")
}

func readPeclState(phpDir string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(peclStateFile(phpDir))
	if err == nil {
		json.Unmarshal(data, &out)
	}
	return out
}

func writePeclState(phpDir string, state map[string]string) {
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(peclStateFile(phpDir), data, 0644)
}

func installedPeclSet(phpDir string) map[string]bool {
	set := map[string]bool{}
	for name := range readPeclState(phpDir) {
		set[name] = true
	}
	return set
}

// GetPeclExtensions returns the catalog with install state for a PHP version.
func GetPeclExtensions(version string) []PeclExtension {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	state := readPeclState(phpDir)
	out := make([]PeclExtension, len(peclCatalog))
	copy(out, peclCatalog)
	for i := range out {
		if v, ok := state[out[i].Name]; ok {
			out[i].Installed = true
			out[i].Version = v
		} else if _, err := os.Stat(filepath.Join(phpDir, "ext", extFileName(out[i].Name))); err == nil {
			out[i].Installed = true // dropped in manually
		}
	}
	return out
}

// InstallPeclExtension downloads, installs and enables a PECL extension for a
// PHP version. Windows only — macOS builds need a compiler toolchain.
func InstallPeclExtension(version, name string, progress chan<- Progress) error {
	if goruntime.GOOS != "windows" {
		return fmt.Errorf("PECL extension installs are currently supported on Windows only")
	}
	var entry *PeclExtension
	for i := range peclCatalog {
		if peclCatalog[i].Name == name {
			entry = &peclCatalog[i]
		}
	}
	if entry == nil {
		return fmt.Errorf("unknown extension: %s", name)
	}
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	if _, err := os.Stat(phpDir); err != nil {
		return fmt.Errorf("PHP %s is not installed", version)
	}
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return fmt.Errorf("invalid PHP version %q", version)
	}
	phpLine := parts[0] + "." + parts[1]

	report := func(pct int, msg string) {
		if progress != nil {
			progress <- Progress{Percent: pct, Message: msg}
		}
	}

	report(5, "Looking up "+name+" builds for PHP "+phpLine+"...")
	var (
		dlURL, dlVersion string
		err              error
	)
	if name == "xdebug" {
		dlURL, dlVersion, err = findXdebugDLL(phpLine)
	} else {
		dlURL, dlVersion, err = findPeclZip(name, phpLine)
	}
	if err != nil {
		return err
	}

	tmp := filepath.Join(tmpDir(), filepath.Base(dlURL))
	if err := DownloadFile(dlURL, tmp, 0, progress); err != nil {
		return err
	}
	defer os.Remove(tmp)

	extDir := filepath.Join(phpDir, "ext")
	os.MkdirAll(extDir, 0755)
	report(90, "Installing "+name+"...")

	if strings.HasSuffix(strings.ToLower(tmp), ".dll") {
		if err := copyFile(tmp, filepath.Join(extDir, extFileName(name))); err != nil {
			return err
		}
	} else {
		if err := installPeclZip(tmp, name, phpDir, extDir); err != nil {
			return err
		}
	}

	state := readPeclState(phpDir)
	state[name] = dlVersion
	writePeclState(phpDir, state)

	if err := TogglePHPExtension(version, name, true); err != nil {
		return fmt.Errorf("installed but could not enable in php.ini: %w", err)
	}
	if name == "xdebug" {
		applyIniDirectives(filepath.Join(phpDir, "php.ini"), map[string]string{
			"xdebug.mode":              "debug,develop",
			"xdebug.start_with_request": "trigger",
			"xdebug.client_host":       "127.0.0.1",
			"xdebug.client_port":       "9003",
		})
	}
	report(100, name+" "+dlVersion+" installed")
	return nil
}

// UninstallPeclExtension disables and removes a PECL extension's DLL.
func UninstallPeclExtension(version, name string) error {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	TogglePHPExtension(version, name, false)
	os.Remove(filepath.Join(phpDir, "ext", extFileName(name)))
	state := readPeclState(phpDir)
	delete(state, name)
	writePeclState(phpDir, state)
	return nil
}

// installPeclZip extracts php_<name>.dll into ext/ and every other DLL
// (runtime dependencies) into the PHP root where php.exe/php-cgi.exe find them.
func installPeclZip(zipPath, name, phpDir, extDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	want := strings.ToLower(extFileName(name))
	found := false
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		lower := strings.ToLower(base)
		if f.FileInfo().IsDir() || !strings.HasSuffix(lower, ".dll") {
			continue
		}
		dest := filepath.Join(phpDir, base)
		if lower == want {
			dest = filepath.Join(extDir, base)
			found = true
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("%s not found inside the downloaded package", want)
	}
	return nil
}

var httpShort = &http.Client{Timeout: 25 * time.Second}

func fetchText(url string) (string, error) {
	resp, err := httpShort.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

var stableVerRe = regexp.MustCompile(`href="(\d+\.\d+\.\d+)/"`)

// findPeclZip walks the newest stable release directories on windows.php.net
// until it finds an NTS x64 zip built for the given PHP line.
func findPeclZip(name, phpLine string) (string, string, error) {
	index, err := fetchText(peclBaseURL + name + "/")
	if err != nil {
		return "", "", fmt.Errorf("cannot list %s releases: %w", name, err)
	}
	var versions []string
	seen := map[string]bool{}
	for _, m := range stableVerRe.FindAllStringSubmatch(index, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			versions = append(versions, m[1])
		}
	}
	sort.Slice(versions, func(i, j int) bool { return compareVersions(versions[i], versions[j]) > 0 })
	if len(versions) > 8 {
		versions = versions[:8]
	}
	zipRe := regexp.MustCompile(`href="(php_` + regexp.QuoteMeta(name) + `-([\d.]+)-` + regexp.QuoteMeta(phpLine) + `-nts-vs\d+-x64\.zip)"`)
	for _, v := range versions {
		listing, err := fetchText(peclBaseURL + name + "/" + v + "/")
		if err != nil {
			continue
		}
		if m := zipRe.FindStringSubmatch(listing); m != nil {
			return peclBaseURL + name + "/" + v + "/" + m[1], m[2], nil
		}
	}
	return "", "", fmt.Errorf("no prebuilt %s DLL for PHP %s (NTS x64) on windows.php.net yet", name, phpLine)
}

func findXdebugDLL(phpLine string) (string, string, error) {
	page, err := fetchText("https://xdebug.org/download")
	if err != nil {
		return "", "", err
	}
	re := regexp.MustCompile(`files/(php_xdebug-([\d.]+)-` + regexp.QuoteMeta(phpLine) + `-nts-vs\d+-x86_64\.dll)`)
	best, bestVer := "", ""
	for _, m := range re.FindAllStringSubmatch(page, -1) {
		if bestVer == "" || compareVersions(m[2], bestVer) > 0 {
			best, bestVer = m[1], m[2]
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("no Xdebug build for PHP %s on xdebug.org", phpLine)
	}
	return "https://xdebug.org/files/" + best, bestVer, nil
}
