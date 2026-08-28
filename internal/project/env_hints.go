package project

import (
	"bufio"
	"encoding/json"
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

// FixLocalhostEnv rewrites every flagged value to the project's domain:
// *_URL → http(s)://domain, *_DOMAIN → domain. Keeps .env.devbox-backup and
// drops Laravel's cached config. Returns the number of keys changed.
func FixLocalhostEnv(p Project) (int, error) {
	hints := LocalhostEnvHints(p)
	if len(hints) == 0 || p.Domain == "" {
		return 0, nil
	}
	envPath := filepath.Join(p.Path, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return 0, err
	}
	scheme := "http"
	if p.SSL {
		scheme = "https"
	}
	flagged := map[string]bool{}
	for _, h := range hints {
		flagged[h.Key] = true
	}
	lines := strings.Split(string(data), "\n")
	changed := 0
	for i, line := range lines {
		k, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		k = strings.TrimSpace(k)
		if !ok || !flagged[k] {
			continue
		}
		if strings.HasSuffix(k, "_DOMAIN") {
			lines[i] = k + "=" + p.Domain
		} else {
			lines[i] = k + "=" + scheme + "://" + p.Domain
		}
		changed++
	}
	if changed == 0 {
		return 0, nil
	}
	os.WriteFile(filepath.Join(p.Path, ".env.devbox-backup"), data, 0644)
	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return 0, err
	}
	os.Remove(filepath.Join(p.Path, "bootstrap", "cache", "config.php"))
	return changed, nil
}

// DomainBoundEnvKeys returns the .env keys whose value is the project's own
// domain (http(s)://backend.test or bare backend.test). Web servers inject
// these per request as FastCGI params derived from the request Host, so the
// same app answers correctly on its local domain AND on a tunnel hostname at
// the same time — Laravel's Dotenv never overrides variables already present
// in $_SERVER, so the .env file itself stays untouched.
// Returns url keys and domain keys separately.
func DomainBoundEnvKeys(p Project) (urlKeys, domainKeys []string) {
	if p.Domain == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(p.Path, ".env"))
	if err != nil {
		return nil, nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if !(strings.HasSuffix(k, "_URL") || strings.HasSuffix(k, "_DOMAIN")) {
			continue
		}
		val := strings.TrimSuffix(strings.Trim(strings.TrimSpace(v), `"`), "/")
		switch val {
		case "https://" + p.Domain, "http://" + p.Domain:
			urlKeys = append(urlKeys, k)
		case p.Domain:
			domainKeys = append(domainKeys, k)
		}
	}
	return urlKeys, domainKeys
}

// --- Tunnel host swap (legacy; kept only to restore files touched by 0.2.0) ---
//
// Apps that build absolute URLs from .env keys (APP_URL, and custom ones such
// as ADMIN_URL / ACCOUNT_DOMAIN) send tunnel visitors back to the local .test
// domain, which is unreachable from outside. While a tunnel is up DevBox
// swaps every *_URL / *_DOMAIN value that equals the project's domain for the
// public hostname, remembers the originals in .env.devbox-tunnel.json and
// restores them when the tunnel stops.

func tunnelSwapFile(p Project) string {
	return filepath.Join(p.Path, ".env.devbox-tunnel.json")
}

// SwapEnvHostForTunnel rewrites domain-bound .env values to publicHost.
// Returns the number of keys changed.
func SwapEnvHostForTunnel(p Project, publicHost string) int {
	if p.Domain == "" || publicHost == "" {
		return 0
	}
	envPath := filepath.Join(p.Path, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return 0
	}
	// Restore first so a second swap (new tunnel URL) starts from the originals.
	if _, err := os.Stat(tunnelSwapFile(p)); err == nil {
		RestoreEnvAfterTunnel(p)
		data, _ = os.ReadFile(envPath)
	}

	lines := strings.Split(string(data), "\n")
	originals := map[string]string{}
	for i, line := range lines {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if !(strings.HasSuffix(k, "_URL") || strings.HasSuffix(k, "_DOMAIN") || k == "APP_URL") {
			continue
		}
		val := strings.Trim(strings.TrimSpace(v), `"`)
		var repl string
		switch strings.TrimSuffix(val, "/") {
		case "https://" + p.Domain, "http://" + p.Domain:
			repl = "https://" + publicHost
		case p.Domain:
			repl = publicHost
		default:
			continue
		}
		originals[k] = strings.TrimSpace(v)
		lines[i] = k + "=" + repl
	}
	if len(originals) == 0 {
		return 0
	}
	if b, err := json.MarshalIndent(originals, "", "  "); err == nil {
		os.WriteFile(tunnelSwapFile(p), b, 0644)
	}
	os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
	os.Remove(filepath.Join(p.Path, "bootstrap", "cache", "config.php"))
	return len(originals)
}

// RestoreEnvAfterTunnel puts the original values back (no-op without a swap file).
func RestoreEnvAfterTunnel(p Project) {
	b, err := os.ReadFile(tunnelSwapFile(p))
	if err != nil {
		return
	}
	var originals map[string]string
	if json.Unmarshal(b, &originals) != nil || len(originals) == 0 {
		os.Remove(tunnelSwapFile(p))
		return
	}
	envPath := filepath.Join(p.Path, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		k, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		k = strings.TrimSpace(k)
		if ok {
			if orig, has := originals[k]; has {
				lines[i] = k + "=" + orig
			}
		}
	}
	os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
	os.Remove(filepath.Join(p.Path, "bootstrap", "cache", "config.php"))
	os.Remove(tunnelSwapFile(p))
}

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
