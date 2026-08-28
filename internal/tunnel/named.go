package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// Named tunnels: one Cloudflare tunnel per machine ("devbox-<hostname>"),
// remotely managed, carrying one ingress rule per project the user exposes on
// their own domain. A single cloudflared connector process serves all routes;
// ingress changes are pushed through the API and picked up live by the
// connector, so adding/removing a project never restarts the process.

// NamedRoute is one project → public hostname mapping.
type NamedRoute struct {
	Project    string `json:"project"`
	Hostname   string `json:"hostname"`
	Service    string `json:"service"`    // e.g. http://127.0.0.1:80
	HostHeader string `json:"hostHeader"` // project's local domain, forwarded as Host
	NoTLS      bool   `json:"noTlsVerify"`
}

var namedMu sync.Mutex

func routesFile() string {
	return filepath.Join(config.GetDataDir(), "tunnel-routes.json")
}

func namedPidFile() string {
	return filepath.Join(config.GetDataDir(), "services", "cloudflared-named.pid")
}

func namedLogFile() string {
	return filepath.Join(config.GetDataDir(), "logs", "cloudflared-named.log")
}

func loadRoutes() []NamedRoute {
	data, err := os.ReadFile(routesFile())
	if err != nil {
		return nil
	}
	var routes []NamedRoute
	json.Unmarshal(data, &routes)
	return routes
}

func saveRoutes(routes []NamedRoute) error {
	sort.Slice(routes, func(i, j int) bool { return routes[i].Project < routes[j].Project })
	data, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(routesFile(), data, 0644)
}

// CloudflareStatus is what the Settings page shows.
type CloudflareStatus struct {
	Configured  bool   `json:"configured"`
	AccountName string `json:"accountName"`
	ZoneName    string `json:"zoneName"`
	TunnelName  string `json:"tunnelName"`
	Connected   bool   `json:"connected"` // connector process alive
	Routes      int    `json:"routes"`
}

// GetCloudflareStatus summarises the account link and connector state.
func GetCloudflareStatus() CloudflareStatus {
	cf := config.Get().Cloudflare
	return CloudflareStatus{
		Configured:  cf.APIToken != "" && cf.AccountID != "" && cf.ZoneID != "",
		AccountName: cf.AccountName,
		ZoneName:    cf.ZoneName,
		TunnelName:  cf.TunnelName,
		Connected:   isNamedConnectorRunning(),
		Routes:      len(loadRoutes()),
	}
}

// VerifyCloudflareToken checks the token and returns the accounts and zones
// it can see, so the UI can offer a picker.
func VerifyCloudflareToken(token string) ([]CFAccount, []CFZone, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil, fmt.Errorf("API token is empty")
	}
	c := newCFClient(token)
	if err := c.verifyToken(); err != nil {
		return nil, nil, err
	}
	accounts, err := c.listAccounts()
	if err != nil {
		return nil, nil, fmt.Errorf("token cannot list accounts (needs Account · Cloudflare Tunnel · Edit): %w", err)
	}
	zones, err := c.listZones("")
	if err != nil {
		return nil, nil, fmt.Errorf("token cannot list zones (needs Zone · Zone · Read and Zone · DNS · Edit): %w", err)
	}
	return accounts, zones, nil
}

// ConfigureCloudflare stores the account link and makes sure the machine's
// tunnel exists (creating it on first use).
func ConfigureCloudflare(token, accountID, accountName, zoneID, zoneName string) error {
	token = strings.TrimSpace(token)
	if token == "" || accountID == "" || zoneID == "" {
		return fmt.Errorf("token, account and zone are all required")
	}
	c := newCFClient(token)
	if err := c.verifyToken(); err != nil {
		return err
	}
	host, _ := os.Hostname()
	host = strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, host))
	if host == "" {
		host = "machine"
	}
	tunnelName := "devbox-" + host

	t, err := c.ensureTunnel(accountID, tunnelName)
	if err != nil {
		return fmt.Errorf("could not create tunnel: %w", err)
	}

	cfg := config.Get()
	cfg.Cloudflare = config.CloudflareConfig{
		APIToken:    token,
		AccountID:   accountID,
		AccountName: accountName,
		ZoneID:      zoneID,
		ZoneName:    zoneName,
		TunnelID:    t.ID,
		TunnelName:  t.Name,
		TunnelToken: t.Token,
	}
	return config.Save()
}

// DisconnectCloudflare stops the connector, removes DNS for every route and
// forgets the credentials. The tunnel object itself is left in the account so
// re-linking later reuses it.
func DisconnectCloudflare() error {
	namedMu.Lock()
	defer namedMu.Unlock()

	cfg := config.Get()
	cf := cfg.Cloudflare
	if cf.APIToken != "" && cf.ZoneID != "" {
		c := newCFClient(cf.APIToken)
		for _, r := range loadRoutes() {
			c.deleteDNS(cf.ZoneID, r.Hostname)
		}
		if cf.TunnelID != "" {
			c.putIngress(cf.AccountID, cf.TunnelID, nil)
		}
	}
	stopNamedConnector()
	os.Remove(routesFile())
	cfg.Cloudflare = config.CloudflareConfig{}
	return config.Save()
}

// StartNamedTunnel exposes a project at hostname on the linked zone.
func StartNamedTunnel(projectName, hostname, originURL, hostHeader string, noTLSVerify bool) error {
	namedMu.Lock()
	defer namedMu.Unlock()

	cf := config.Get().Cloudflare
	if cf.APIToken == "" || cf.TunnelID == "" || cf.ZoneID == "" {
		return fmt.Errorf("Cloudflare account is not linked — add your API token in Settings first")
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return fmt.Errorf("project has no public hostname")
	}
	if hostname != cf.ZoneName && !strings.HasSuffix(hostname, "."+cf.ZoneName) {
		return fmt.Errorf("%s is not inside the linked zone %s", hostname, cf.ZoneName)
	}

	routes := loadRoutes()
	filtered := routes[:0]
	for _, r := range routes {
		if r.Project != projectName && r.Hostname != hostname {
			filtered = append(filtered, r)
		}
	}
	routes = append(filtered, NamedRoute{
		Project:    projectName,
		Hostname:   hostname,
		Service:    originURL,
		HostHeader: hostHeader,
		NoTLS:      noTLSVerify,
	})

	c := newCFClient(cf.APIToken)
	if err := c.putIngress(cf.AccountID, cf.TunnelID, ingressFor(routes)); err != nil {
		return err
	}
	if err := c.ensureDNS(cf.ZoneID, hostname, cf.TunnelID); err != nil {
		return err
	}
	if err := saveRoutes(routes); err != nil {
		return err
	}
	return ensureNamedConnector(cf.TunnelToken)
}

// StopNamedTunnel removes a project's route (and its DNS record); the
// connector is stopped when no routes remain.
func StopNamedTunnel(projectName string) error {
	namedMu.Lock()
	defer namedMu.Unlock()

	cf := config.Get().Cloudflare
	routes := loadRoutes()
	var removed *NamedRoute
	kept := routes[:0]
	for i := range routes {
		if routes[i].Project == projectName {
			r := routes[i]
			removed = &r
			continue
		}
		kept = append(kept, routes[i])
	}
	if removed == nil {
		return nil
	}
	if cf.APIToken != "" {
		c := newCFClient(cf.APIToken)
		if err := c.putIngress(cf.AccountID, cf.TunnelID, ingressFor(kept)); err != nil {
			return err
		}
		c.deleteDNS(cf.ZoneID, removed.Hostname)
	}
	if err := saveRoutes(kept); err != nil {
		return err
	}
	if len(kept) == 0 {
		stopNamedConnector()
	}
	return nil
}

// NamedRouteFor returns the active named route of a project, if any.
func NamedRouteFor(projectName string) *NamedRoute {
	for _, r := range loadRoutes() {
		if r.Project == projectName {
			return &r
		}
	}
	return nil
}

func ingressFor(routes []NamedRoute) []cfIngressRule {
	var rules []cfIngressRule
	for _, r := range routes {
		origin := map[string]interface{}{}
		if r.HostHeader != "" {
			origin["httpHostHeader"] = r.HostHeader
		}
		if r.NoTLS {
			origin["noTLSVerify"] = true
		}
		rules = append(rules, cfIngressRule{Hostname: r.Hostname, Service: r.Service, OriginRequest: origin})
	}
	return rules
}

// --- connector process ---

func isNamedConnectorRunning() bool {
	data, err := os.ReadFile(namedPidFile())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || !platform.IsProcessRunning(pid) {
		os.Remove(namedPidFile())
		return false
	}
	return true
}

func ensureNamedConnector(token string) error {
	if isNamedConnectorRunning() {
		return nil
	}
	if !IsCloudflaredInstalled() {
		if err := InstallCloudflared(); err != nil {
			return err
		}
	}
	os.MkdirAll(filepath.Dir(namedLogFile()), 0755)
	logF, err := os.OpenFile(namedLogFile(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	cmd := exec.Command(cloudflaredPath(), "tunnel", "--no-autoupdate", "run", "--token", token)
	cmd.Stdout = logF
	cmd.Stderr = logF
	platform.SetProcessAttrs(cmd, true, true)
	if err := cmd.Start(); err != nil {
		logF.Close()
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}
	os.MkdirAll(filepath.Dir(namedPidFile()), 0755)
	os.WriteFile(namedPidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	go func() {
		cmd.Wait()
		logF.Close()
	}()
	time.Sleep(800 * time.Millisecond)
	if !platform.IsProcessRunning(cmd.Process.Pid) {
		os.Remove(namedPidFile())
		return fmt.Errorf("cloudflared exited immediately — check %s", namedLogFile())
	}
	return nil
}

func stopNamedConnector() {
	data, err := os.ReadFile(namedPidFile())
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
		if p, err := os.FindProcess(pid); err == nil {
			p.Kill()
		}
	}
	os.Remove(namedPidFile())
}

// StopNamedConnector stops the connector process without touching routes or
// DNS (app shutdown). Routes come back up on the next StartNamedTunnel /
// ResumeNamedTunnels.
func StopNamedConnector() {
	namedMu.Lock()
	defer namedMu.Unlock()
	stopNamedConnector()
}

// ResumeNamedTunnels restarts the connector at app start when routes exist.
func ResumeNamedTunnels() error {
	namedMu.Lock()
	defer namedMu.Unlock()
	cf := config.Get().Cloudflare
	if cf.TunnelToken == "" || len(loadRoutes()) == 0 {
		return nil
	}
	return ensureNamedConnector(cf.TunnelToken)
}
