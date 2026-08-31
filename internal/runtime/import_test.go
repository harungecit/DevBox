package runtime

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// TestImportExternalLink verifies the whole in-place import cycle with a fake
// external installation: link creation, visibility through the link, listing
// (links are not reported as directories by ReadDir), and that removal only
// unlinks — the external files must survive.
func TestImportExternalLink(t *testing.T) {
	if goruntime.GOOS != "windows" && goruntime.GOOS != "darwin" {
		t.Skip("directory links are platform-dependent (windows/darwin only)")
	}

	// Point the data dir at a temp location for the duration of the test.
	cfg := config.Get()
	oldDataDir := cfg.DataDir
	cfg.DataDir = t.TempDir()
	defer func() { cfg.DataDir = oldDataDir }()

	Register(NewNodeManager())

	// Fake external Node.js install.
	src := filepath.Join(t.TempDir(), "nodejs")
	binDir := src
	if goruntime.GOOS != "windows" {
		binDir = filepath.Join(src, "bin")
	}
	os.MkdirAll(binDir, 0755)
	nodeExe := filepath.Join(binDir, platform.BinaryName("node"))
	if err := os.WriteFile(nodeExe, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := ImportExternal("node", src, "99.0.0", nil); err != nil {
		t.Fatalf("ImportExternal: %v", err)
	}

	destDir := filepath.Join(runtimeBaseDir("node"), "99.0.0")

	// The binary must be reachable through the link.
	destExe := filepath.Join(destDir, platform.BinaryName("node"))
	if goruntime.GOOS != "windows" {
		destExe = filepath.Join(destDir, "bin", "node")
	}
	if _, err := os.Stat(destExe); err != nil {
		t.Fatalf("binary not reachable through link: %v", err)
	}

	// Linked versions must show up in the installed list even though ReadDir
	// does not report links as directories.
	versions, err := listInstalledVersions("node")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range versions {
		if v == "99.0.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("linked version not listed; got %v", versions)
	}

	// Importing the same version twice must fail cleanly.
	if err := ImportExternal("node", src, "99.0.0", nil); err == nil {
		t.Fatal("expected error importing an already-imported version")
	}

	// Uninstall removes only the link — the external install must survive.
	if err := uninstallVersion("node", "99.0.0"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Lstat(destDir); !os.IsNotExist(err) {
		t.Fatal("link still present after uninstall")
	}
	if _, err := os.Stat(nodeExe); err != nil {
		t.Fatalf("external installation was damaged by uninstall: %v", err)
	}
}
