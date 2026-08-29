package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	runtime "runtime"
	"strings"
	"testing"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/project"
)

func TestRenderCaddyfile(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("DevBox targets Windows and macOS; no platform layer here")
	}
	// Fake certs so the SSL project renders an https block.
	dir := t.TempDir()
	certDir := filepath.Join(dir, "ssl", "certs")
	os.MkdirAll(certDir, 0755)
	certPEM, keyPEM := selfSigned(t, "shop.test")
	os.WriteFile(filepath.Join(certDir, "shop.test.pem"), certPEM, 0644)
	os.WriteFile(filepath.Join(certDir, "shop.test-key.pem"), keyPEM, 0644)
	cfg := config.Get()
	origDir := cfg.DataDir
	cfg.DataDir = dir
	t.Cleanup(func() { cfg.DataDir = origDir })

	projects := []project.Project{
		{Name: "app", Domain: "app.test", Framework: "Next.js", Runtime: "node", Webserver: "devserver", Port: 3000},
		{Name: "shop", Domain: "shop.test", Framework: "Next.js", Runtime: "node", Webserver: "devserver", Port: 3001, SSL: true},
		{Name: "nodomain", Framework: "Go", Runtime: "go"},
	}
	out := renderCaddyfile(projects)

	for _, want := range []string{
		"http://app.test {", "reverse_proxy 127.0.0.1:3000 {",
		"http://shop.test {", "redir https://{host}{uri} 308",
		"https://shop.test {", "header_up X-Forwarded-Proto https",
		"handle_errors 502 503 504 {", "https_port 443",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "https://app.test") {
		t.Errorf("app.test has no SSL but got an https block")
	}

	// Validate with the bundled Caddy when it is present on this machine.
	caddy := filepath.Join(origDir, "proxy", "caddy.exe")
	if _, err := os.Stat(caddy); err != nil {
		t.Skip("bundled caddy not installed; skipping validate")
	}
	path := filepath.Join(dir, "Caddyfile")
	os.WriteFile(path, []byte(out), 0644)
	cmd := exec.Command(caddy, "validate", "--config", path, "--adapter", "caddyfile")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("caddy validate failed: %v\n%s\n--- Caddyfile ---\n%s", err, b, out)
	}
}

func selfSigned(t *testing.T, host string) (certPEM, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
}
