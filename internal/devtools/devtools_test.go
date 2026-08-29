package devtools

import (
	"runtime"
	"testing"
)

func TestCatalogConsistency(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("DevBox targets Windows and macOS; no platform layer here")
	}
	seen := map[string]bool{}
	ports := map[int]string{}
	for _, tool := range Catalog {
		if seen[tool.ID] {
			t.Errorf("duplicate tool id %q", tool.ID)
		}
		seen[tool.ID] = true
		if tool.Runtime == "" || tool.Bin == "" || tool.Desc == "" {
			t.Errorf("%s: runtime, bin and desc are required", tool.ID)
		}
		switch tool.Kind {
		case KindNpm:
			if tool.run == nil || tool.Port == 0 || len(tool.ForServices) == 0 {
				t.Errorf("%s: web tools need run, port and forServices", tool.ID)
			}
			if other, dup := ports[tool.Port]; dup {
				t.Errorf("%s and %s share port %d", tool.ID, other, tool.Port)
			}
			ports[tool.Port] = tool.ID
		case KindBinary:
			if tool.asset == nil || tool.ghOwner == "" || tool.ghRepo == "" || tool.asset() == "" {
				t.Errorf("%s: binary tools need a GitHub source and asset name", tool.ID)
			}
		case KindPip, KindGo, KindCargo:
			if tool.Pkg == "" {
				t.Errorf("%s: package name required", tool.ID)
			}
		default:
			t.Errorf("%s: unknown kind %q", tool.ID, tool.Kind)
		}
		if BinPath(&tool) == "" && tool.Kind != KindPip {
			t.Errorf("%s: BinPath is empty", tool.ID)
		}
	}
}
