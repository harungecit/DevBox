package runtime

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"DevBox/internal/config"
	"DevBox/internal/vfox"
)

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(content))
	}
	w.Close()
	f.Close()
}

func sha256Of(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestPluginManagerInstallPipeline(t *testing.T) {
	cfg := config.Get()
	oldDataDir := cfg.DataDir
	cfg.DataDir = t.TempDir()
	defer func() { cfg.DataDir = oldDataDir }()
	delete(cfg.ActiveRuntimes, "fixture")

	fixture, err := filepath.Abs(filepath.Join("..", "vfox", "testdata", "plugins", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vfox.Install("", fixture, nil); err != nil {
		t.Fatalf("install plugin: %v", err)
	}

	// The archive the fixture's PreInstall hands back (with a root folder,
	// which must be stripped) plus an addition without a root folder.
	arch := filepath.Join(t.TempDir(), "fixture-2.0.0-win.zip")
	writeTestZip(t, arch, map[string]string{
		"fixture-2.0.0/bin/fixture.exe": "binary",
		"fixture-2.0.0/README":          "readme",
	})
	add := filepath.Join(t.TempDir(), "helper-9.zip")
	writeTestZip(t, add, map[string]string{"helper.exe": "helper"})
	t.Setenv("DEVBOX_FIXTURE_ARCHIVE", arch)
	t.Setenv("DEVBOX_FIXTURE_SHA256", sha256Of(t, arch))
	t.Setenv("DEVBOX_FIXTURE_ADDITION", add)

	if err := RegisterPlugin("fixture"); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	defer UnregisterPlugin("fixture")
	if !IsPluginRuntime("fixture") || Registry["fixture"] == nil {
		t.Fatal("plugin runtime not registered")
	}
	names := Names()
	if names[len(names)-1] != "fixture" {
		t.Fatalf("Names: %v", names)
	}
	mgr := Registry["fixture"].(*PluginManager)

	remote, err := mgr.ListRemote()
	if err != nil || len(remote) != 3 {
		t.Fatalf("ListRemote: %v %+v", err, remote)
	}
	if !remote[0].Stable || remote[1].Stable || !remote[2].Stable {
		t.Fatalf("stable flags: %+v", remote)
	}

	progress := make(chan Progress, 100)
	if err := mgr.Install("latest", progress); err != nil {
		t.Fatalf("Install: %v", err)
	}
	close(progress)
	last := Progress{}
	for p := range progress {
		last = p
	}
	if last.Percent != 100 {
		t.Fatalf("final progress: %+v", last)
	}

	verDir := filepath.Join(cfg.DataDir, "runtimes", "fixture", "2.0.0")
	main := filepath.Join(verDir, "fixture-2.0.0")
	if _, err := os.Stat(filepath.Join(main, "bin", "fixture.exe")); err != nil {
		t.Fatalf("archive root not stripped / files missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(verDir, "add-helper-9", "helper.exe")); err != nil {
		t.Fatalf("addition missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(main, "post.txt")); err != nil {
		t.Fatalf("PostInstall did not run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(verDir, installingMarker)); !os.IsNotExist(err) {
		t.Fatal("installing marker left behind")
	}
	data, err := os.ReadFile(filepath.Join(verDir, envFileName))
	if err != nil {
		t.Fatal("devbox-env.json missing")
	}
	var env versionEnv
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	sep := "/"
	if goruntime.GOOS == "windows" {
		sep = "\\"
	}
	if len(env.Paths) != 2 || env.Paths[0] != main+sep+"bin" || !strings.HasSuffix(env.Paths[1], "add-helper-9") {
		t.Fatalf("paths: %v", env.Paths)
	}
	if env.Vars["FIXTURE_HOME"] != main || env.Vars["FIXTURE_VERSION"] != "2.0.0" {
		t.Fatalf("vars: %v", env.Vars)
	}
	if mgr.BinaryPath("2.0.0") != main+sep+"bin" {
		t.Fatalf("BinaryPath: %s", mgr.BinaryPath("2.0.0"))
	}
	if paths := ActivationPaths(mgr, "2.0.0"); len(paths) != 2 {
		t.Fatalf("ActivationPaths: %v", paths)
	}
	if vars := ActivationVars(mgr, "2.0.0"); vars["FIXTURE_HOME"] != main {
		t.Fatalf("ActivationVars: %v", vars)
	}

	installed, err := mgr.ListInstalled()
	if err != nil || len(installed) != 1 || installed[0].Number != "2.0.0" {
		t.Fatalf("ListInstalled: %v %+v", err, installed)
	}
	if err := mgr.Install("2.0.0", nil); err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("expected already-installed error, got %v", err)
	}

	// Half-finished install dirs are hidden and cleaned on registration.
	broken := filepath.Join(cfg.DataDir, "runtimes", "fixture", "9.9.9")
	os.MkdirAll(broken, 0755)
	os.WriteFile(filepath.Join(broken, installingMarker), []byte("x"), 0644)
	installed, _ = mgr.ListInstalled()
	if len(installed) != 1 {
		t.Fatalf("marker dir listed: %+v", installed)
	}
	if err := RegisterPlugin("fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Fatal("marker dir not cleaned on register")
	}
	mgr = Registry["fixture"].(*PluginManager)

	if err := mgr.SetGlobal("2.0.0"); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if g, _ := mgr.GetGlobal(); g != "2.0.0" {
		t.Fatalf("GetGlobal: %s", g)
	}
	if mgr.SetGlobal("1.2.3") == nil {
		t.Fatal("SetGlobal must refuse an uninstalled version")
	}

	// Legacy file parsing goes through the plugin.
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, ".fixture-version"), []byte("installed\n"), 0644)
	if v, ok := mgr.ParseLegacyFile(proj); !ok || v != "2.0.0" {
		t.Fatalf("ParseLegacyFile: %q %v", v, ok)
	}

	marker := filepath.Join(t.TempDir(), "pre-uninstall-ran")
	t.Setenv("DEVBOX_FIXTURE_UNINSTALL_MARKER", marker)
	if err := mgr.Uninstall("2.0.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "2.0.0" {
		t.Fatalf("PreUninstall hook did not run: %v %q", err, data)
	}
	if _, err := os.Stat(verDir); !os.IsNotExist(err) {
		t.Fatal("version dir still exists")
	}
	if g, _ := mgr.GetGlobal(); g != "" {
		t.Fatalf("global not cleared: %s", g)
	}
}

func TestPluginManagerRejectsBuiltinAlias(t *testing.T) {
	if err := RegisterPlugin("nodejs"); err == nil {
		t.Fatal("nodejs must be refused")
	}
	if DisplayName("java") != "Java" || DisplayName("go") != "Go" || DisplayName("foo") != "Foo" {
		t.Fatal("DisplayName")
	}
}
