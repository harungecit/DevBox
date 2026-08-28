package tunnel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Thin client for the handful of Cloudflare v4 API calls DevBox needs to run a
// named tunnel on the user's own zone: verify the token, pick an account and
// zone, create/reuse one tunnel per machine, push its ingress rules, and keep
// one proxied CNAME per exposed project.
//
// Token permissions required (API Tokens → Create Token → custom):
//   Account · Cloudflare Tunnel · Edit
//   Zone    · DNS               · Edit
//   Zone    · Zone              · Read

const cfAPIBase = "https://api.cloudflare.com/client/v4"

type cfClient struct {
	token string
	http  *http.Client
}

func newCFClient(token string) *cfClient {
	return &cfClient{token: token, http: &http.Client{Timeout: 20 * time.Second}}
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cfAPIError    `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *cfClient) do(method, path string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, cfAPIBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var env cfEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("cloudflare returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if !env.Success {
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			msgs = append(msgs, fmt.Sprintf("%s (%d)", e.Message, e.Code))
		}
		if len(msgs) == 0 {
			msgs = append(msgs, fmt.Sprintf("HTTP %d", resp.StatusCode))
		}
		return fmt.Errorf("cloudflare: %s", strings.Join(msgs, "; "))
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// CFAccount / CFZone are what the Settings UI lists after a token is verified.
type CFAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CFZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *cfClient) verifyToken() error {
	var r struct {
		Status string `json:"status"`
	}
	if err := c.do("GET", "/user/tokens/verify", nil, &r); err != nil {
		return err
	}
	if r.Status != "active" {
		return fmt.Errorf("token status is %q", r.Status)
	}
	return nil
}

func (c *cfClient) listAccounts() ([]CFAccount, error) {
	var out []CFAccount
	err := c.do("GET", "/accounts?per_page=50", nil, &out)
	return out, err
}

func (c *cfClient) listZones(accountID string) ([]CFZone, error) {
	var out []CFZone
	q := "/zones?per_page=50&status=active"
	if accountID != "" {
		q += "&account.id=" + url.QueryEscape(accountID)
	}
	err := c.do("GET", q, nil, &out)
	return out, err
}

type cfTunnel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token,omitempty"`
}

// ensureTunnel returns the tunnel named `name` in the account, creating it if
// it does not exist, together with its connector token.
func (c *cfClient) ensureTunnel(accountID, name string) (cfTunnel, error) {
	var found []cfTunnel
	q := fmt.Sprintf("/accounts/%s/cfd_tunnel?name=%s&is_deleted=false", accountID, url.QueryEscape(name))
	if err := c.do("GET", q, nil, &found); err != nil {
		return cfTunnel{}, err
	}
	var t cfTunnel
	if len(found) > 0 {
		t = found[0]
	} else {
		body := map[string]interface{}{"name": name, "config_src": "cloudflare"}
		if err := c.do("POST", fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID), body, &t); err != nil {
			return cfTunnel{}, err
		}
	}
	if t.Token == "" {
		var token string
		if err := c.do("GET", fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/token", accountID, t.ID), nil, &token); err != nil {
			return cfTunnel{}, err
		}
		t.Token = token
	}
	return t, nil
}

// cfIngressRule is one entry of a remotely-managed tunnel configuration.
type cfIngressRule struct {
	Hostname      string                 `json:"hostname,omitempty"`
	Service       string                 `json:"service"`
	OriginRequest map[string]interface{} `json:"originRequest,omitempty"`
}

func (c *cfClient) putIngress(accountID, tunnelID string, rules []cfIngressRule) error {
	// The catch-all must be last; Cloudflare rejects configs without one.
	rules = append(rules, cfIngressRule{Service: "http_status:404"})
	body := map[string]interface{}{"config": map[string]interface{}{"ingress": rules}}
	return c.do("PUT", fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/configurations", accountID, tunnelID), body, nil)
}

type cfDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// ensureDNS points hostname at the tunnel with a proxied CNAME, replacing any
// existing record of the same name.
func (c *cfClient) ensureDNS(zoneID, hostname, tunnelID string) error {
	target := tunnelID + ".cfargotunnel.com"
	var existing []cfDNSRecord
	if err := c.do("GET", fmt.Sprintf("/zones/%s/dns_records?name=%s", zoneID, url.QueryEscape(hostname)), nil, &existing); err != nil {
		return err
	}
	rec := map[string]interface{}{
		"type": "CNAME", "name": hostname, "content": target, "proxied": true, "ttl": 1,
		"comment": "DevBox tunnel",
	}
	for _, e := range existing {
		if e.Type == "CNAME" && e.Content == target && e.Proxied {
			return nil
		}
	}
	if len(existing) > 0 {
		return c.do("PUT", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, existing[0].ID), rec, nil)
	}
	return c.do("POST", fmt.Sprintf("/zones/%s/dns_records", zoneID), rec, nil)
}

func (c *cfClient) deleteDNS(zoneID, hostname string) error {
	var existing []cfDNSRecord
	if err := c.do("GET", fmt.Sprintf("/zones/%s/dns_records?name=%s&type=CNAME", zoneID, url.QueryEscape(hostname)), nil, &existing); err != nil {
		return err
	}
	for _, e := range existing {
		if strings.HasSuffix(e.Content, ".cfargotunnel.com") {
			if err := c.do("DELETE", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, e.ID), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
