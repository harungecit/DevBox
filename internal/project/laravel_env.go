package project

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SyncLaravelAppURL points a Laravel project's APP_URL at its DevBox domain.
//
// Laravel builds redirects and asset links from APP_URL; a fresh project ships
// with http://localhost or http://127.0.0.1:8000 (artisan serve), which sends
// visitors of backend.test — or of a Cloudflare tunnel — back to a port that
// nothing serves. Only the localhost forms are touched: a custom APP_URL the
// developer set on purpose is left alone. The previous file is kept as
// .env.devbox-backup. Returns true when the file was changed.
func SyncLaravelAppURL(p Project) bool {
	if p.Domain == "" || (p.Framework != "Laravel" && p.Runtime != "php") {
		return false
	}
	envPath := filepath.Join(p.Path, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(p.Path, "artisan")); err != nil {
		return false // not Laravel
	}

	scheme := "http"
	if p.SSL {
		scheme = "https"
	}
	want := scheme + "://" + p.Domain

	re := regexp.MustCompile(`(?m)^APP_URL=\s*"?(https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::\d+)?/?|)"?\s*$`)
	content := string(data)
	if !re.MatchString(content) {
		return false // custom value (or already ours) — respect it
	}
	updated := re.ReplaceAllString(content, "APP_URL="+want)
	if updated == content {
		return false
	}
	os.WriteFile(filepath.Join(p.Path, ".env.devbox-backup"), data, 0644)
	if err := os.WriteFile(envPath, []byte(updated), 0644); err != nil {
		return false
	}
	// Drop a stale cached config so the new APP_URL is picked up immediately.
	os.Remove(filepath.Join(p.Path, "bootstrap", "cache", "config.php"))
	return strings.Contains(updated, want)
}
