package discovery

import (
	"os"
	"testing"

	"DevBox/internal/runtime"
	"DevBox/internal/service"
)

// Manual smoke test: probes the real machine. Enabled only via env var so CI
// never runs it.
func TestScanSmokeManual(t *testing.T) {
	if os.Getenv("DEVBOX_DISCOVERY_SMOKE") == "" {
		t.Skip("set DEVBOX_DISCOVERY_SMOKE=1 to run")
	}
	runtime.InitAll()
	service.InitAll()
	for _, f := range ScanAll() {
		t.Logf("%-8s %-10s %-10s %s", f.Kind, f.Name, f.Version, f.Path)
	}
}
