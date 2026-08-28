package project

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EnvHint flags a .env value that still points at a loopback address. Such
// values (APP_URL, but also app-specific ADMIN_URL, ACCOUNT_DOMAIN, …) make the
// app redirect visitors of the DevBox domain — or of a tunnel — to a port that
// nothing serves, which looks like "DevBox is not serving my project".
type EnvHint struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var loopbackValue = regexp.MustCompile(`^"?(?:https?://)?(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::\d+)?/?"?$`)

// LocalhostEnvHints lists *_URL / *_DOMAIN / *_HOST-style keys in the project's
// .env whose value is a loopback address. Database/cache hosts (DB_HOST,
// REDIS_HOST, …) are legitimately 127.0.0.1 and are skipped.
func LocalhostEnvHints(p Project) []EnvHint {
	f, err := os.Open(filepath.Join(p.Path, ".env"))
	if err != nil {
		return nil
	}
	defer f.Close()

	skip := regexp.MustCompile(`(?i)^(DB_|REDIS_|MEMCACHED_|MAIL_|MYSQL|PG|MONGO|CACHE_|QUEUE_|SESSION_|BROADCAST_|PUSHER_|MEILISEARCH|TYPESENSE|ELASTIC|VITE_)`)
	var hints []EnvHint
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !(strings.HasSuffix(k, "_URL") || strings.HasSuffix(k, "_DOMAIN") || k == "APP_URL") || skip.MatchString(k) {
			continue
		}
		if loopbackValue.MatchString(v) {
			hints = append(hints, EnvHint{Key: k, Value: strings.Trim(v, `"`)})
		}
	}
	return hints
}
