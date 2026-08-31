// Package updater checks GitHub Releases for a newer DevBox and can download
// and launch the installer.
package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

const (
	Repo      = "harungecit/DevBox"
	latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"
)

// Version is injected at build time (-ldflags "-X DevBox/internal/updater.Version=1.2.3").
var Version = "0.0.0-dev"

// Release is what the UI shows.
type Release struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	URL         string `json:"url"`         // release page
	AssetURL    string `json:"assetUrl"`    // installer for this platform ("" if none)
	AssetName   string `json:"assetName"`
	Notes       string `json:"notes"`
	PublishedAt string `json:"publishedAt"`
	CheckedAt   string `json:"checkedAt"`
	Error       string `json:"error"`
}

var (
	mu   sync.Mutex
	last Release
)

type ghRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check queries GitHub for the latest release and compares it with Version.
func Check() Release {
	r := Release{Current: Version, CheckedAt: time.Now().Format(time.RFC3339)}
	req, err := http.NewRequest("GET", latestURL, nil)
	if err != nil {
		r.Error = err.Error()
		return store(r)
	}
	req.Header.Set("User-Agent", "DevBox/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		r.Error = err.Error()
		return store(r)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		r.Latest = Version // no release published yet
		return store(r)
	}
	if resp.StatusCode != 200 {
		r.Error = fmt.Sprintf("GitHub returned HTTP %d", resp.StatusCode)
		return store(r)
	}
	var gh ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		r.Error = err.Error()
		return store(r)
	}
	r.Latest = strings.TrimPrefix(gh.TagName, "v")
	r.URL = gh.HTMLURL
	r.Notes = gh.Body
	r.PublishedAt = gh.PublishedAt
	r.Available = !gh.Draft && compareSemver(r.Latest, Version) > 0
	for _, a := range gh.Assets {
		if matchesPlatform(a.Name) {
			r.AssetURL = a.BrowserDownloadURL
			r.AssetName = a.Name
			break
		}
	}
	return store(r)
}

func store(r Release) Release {
	mu.Lock()
	last = r
	mu.Unlock()
	return r
}

// Last returns the most recent check result (zero value if never checked).
func Last() Release {
	mu.Lock()
	defer mu.Unlock()
	return last
}

// matchesPlatform picks the installer asset for this OS/arch:
//   Windows: DevBox-Setup-<ver>-windows-amd64.exe
//   macOS:   DevBox-<ver>-darwin-<arch>.dmg / .zip
func matchesPlatform(name string) bool {
	n := strings.ToLower(name)
	switch goruntime.GOOS {
	case "windows":
		return strings.Contains(n, "setup") && strings.Contains(n, "windows") && strings.Contains(n, goruntime.GOARCH) && strings.HasSuffix(n, ".exe")
	case "darwin":
		return strings.Contains(n, "darwin") && strings.Contains(n, goruntime.GOARCH) && (strings.HasSuffix(n, ".dmg") || strings.HasSuffix(n, ".zip"))
	}
	return false
}

// DownloadAndInstall fetches the installer and launches it. On Windows the
// NSIS installer runs interactively (it upgrades in place); the caller should
// quit DevBox right after so files are not locked.
func DownloadAndInstall(progress func(percent int, msg string)) (string, error) {
	r := Last()
	if r.AssetURL == "" {
		r = Check()
	}
	if r.AssetURL == "" {
		return "", fmt.Errorf("no installer asset found for %s/%s in release %s", goruntime.GOOS, goruntime.GOARCH, r.Latest)
	}
	dest := filepath.Join(config.GetDataDir(), "tmp", r.AssetName)
	os.MkdirAll(filepath.Dir(dest), 0755)

	resp, err := http.Get(r.AssetURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64*1024)
	lastPct := -1
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return "", werr
			}
			done += int64(n)
			if total > 0 && progress != nil {
				pct := int(done * 100 / total)
				if pct != lastPct {
					lastPct = pct
					progress(pct, fmt.Sprintf("%.1f / %.1f MB", float64(done)/1048576, float64(total)/1048576))
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return "", rerr
		}
	}
	out.Close()

	if goruntime.GOOS == "windows" {
		// The NSIS installer targets Program Files and therefore requires
		// elevation; a plain exec would fail with ERROR_ELEVATION_REQUIRED.
		// /S = silent: the installer closes the running DevBox itself, copies
		// the files and relaunches DevBox as the normal user when done.
		//
		// We WAIT on the installer instead of quitting blindly: on success it
		// kills this process mid-wait (we never return); if it fails — file
		// still locked, aborted by antivirus, wrong target — it exits while
		// DevBox is still alive and the error is surfaced to the user instead
		// of the update silently vanishing.
		code, err := platform.LaunchInstallerWait(dest, installerArgs()...)
		if err != nil {
			return dest, fmt.Errorf("could not launch installer: %w", err)
		}
		if code != 0 {
			return dest, fmt.Errorf("installer exited with code %d before completing — see %%TEMP%%\\DevBox-update.log (in the elevating admin account's TEMP) for details", code)
		}
		return dest, nil
	}
	// macOS: reveal the download; installing a .dmg/.zip is a manual drag-drop.
	platform.OpenFolder(filepath.Dir(dest))
	return dest, nil
}

// installerArgs builds the silent-install arguments. When the running DevBox
// is an installed copy (its directory holds uninstall.exe), the update is
// pointed at that same directory with /D= so custom install locations keep
// working. NSIS requires /D= to be the last argument and unquoted, even when
// the path contains spaces.
func installerArgs() []string {
	args := []string{"/S"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "uninstall.exe")); err == nil {
			args = append(args, "/D="+dir)
		}
	}
	return args
}

// compareSemver compares "1.2.3" style versions; pre-release suffixes sort lower.
func compareSemver(a, b string) int {
	aCore, aPre := splitPre(a)
	bCore, bPre := splitPre(b)
	pa := strings.Split(aCore, ".")
	pb := strings.Split(bCore, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x - y
		}
	}
	switch {
	case aPre == "" && bPre != "":
		return 1
	case aPre != "" && bPre == "":
		return -1
	}
	return strings.Compare(aPre, bPre)
}

func splitPre(v string) (string, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}
